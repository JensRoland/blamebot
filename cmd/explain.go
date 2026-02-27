package cmd

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/llm"
	"github.com/jensroland/git-blamebot/internal/transcript"
)

const maxExplainLines = 50

func cmdExplain(db *sql.DB, target, projectRoot, line string) {
	// If a line range is given, validate it
	if line != "" {
		start, end := parseLineRange(line)
		if end < start {
			fmt.Fprintln(os.Stderr, "Invalid line range: end must be >= start")
			os.Exit(1)
		}
		if end-start+1 > maxExplainLines {
			fmt.Fprintf(os.Stderr, "Line range too large (%d lines); maximum is %d\n", end-start+1, maxExplainLines)
			os.Exit(1)
		}
	}

	// If target looks like a record ID (and no line filter), look it up directly
	if id, err := strconv.Atoi(target); err == nil && line == "" {
		rows, _ := queryRows(db, "SELECT * FROM reasons WHERE id = ?", id)
		if len(rows) == 0 {
			fmt.Printf("No record found with id %d\n", id)
			return
		}
		explainSingle(db, rows[0], projectRoot)
		return
	}

	// File path query
	rel := relativePath(target, projectRoot)

	if line != "" {
		start, end := parseLineRange(line)
		isRange := end > start

		if isRange {
			// For ranges, blame the whole file so CurrentLines reflect true positions
			allRows, err := queryRows(db,
				"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts DESC",
				rel, "%/"+rel)
			if err != nil || len(allRows) == 0 {
				fmt.Printf("No reasons found for %s\n", rel)
				return
			}
			adjMap := blameAdjustFile(projectRoot, rel, allRows)

			var filtered []*index.ReasonRow
			for _, row := range allRows {
				adj := adjMap[row]
				if adj != nil && !adj.Superseded && !adj.CurrentLines.IsEmpty() && adj.CurrentLines.Overlaps(start, end) {
					filtered = append(filtered, row)
				}
			}
			if len(filtered) == 0 {
				fmt.Printf("No reasons found for %s at lines %d-%d\n", rel, start, end)
				return
			}
			sortNewestFirst(filtered)
			explainRange(db, filtered, rel, projectRoot, start, end)
		} else {
			matches, _ := queryLineBlame(db, rel, projectRoot, line)
			if len(matches) == 0 {
				fmt.Printf("No reasons found for %s at line %s\n", rel, line)
				return
			}
			if len(matches) > 1 {
				fmt.Fprintf(os.Stderr, "%s(%d records match — explaining the most recent)%s\n\n",
					format.Dim, len(matches), format.Reset)
			}
			explainSingle(db, matches[0], projectRoot)
		}
	} else {
		rows, err := queryRows(db,
			"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts DESC",
			rel, "%/"+rel)
		if err != nil || len(rows) == 0 {
			fmt.Printf("No reasons found for %s\n", rel)
			return
		}
		if len(rows) > 1 {
			fmt.Fprintf(os.Stderr, "%s(%d records match — explaining the most recent)%s\n\n",
				format.Dim, len(rows), format.Reset)
		}
		explainSingle(db, rows[0], projectRoot)
	}
}

// explainSingle explains a single record (original behavior).
func explainSingle(db *sql.DB, row *index.ReasonRow, projectRoot string) {
	fmt.Println(format.FormatReason(row, projectRoot, false, nil))
	fmt.Println()

	// Show side-by-side diff if available
	oldStr, newStr, hasDiff := transcript.ExtractDiffFromTrace(row.Trace)
	if hasDiff {
		if (oldStr != "" || newStr != "") &&
			strings.Count(oldStr, "\n")+strings.Count(newStr, "\n") < 200 {
			fmt.Println(format.FormatSideBySideDiff(oldStr, newStr))
			fmt.Println()
		}
	} else if row.Change != "" {
		showChangeDiff(row.Change)
	}

	traceContext, sessionPrompts := gatherContext(db, row, projectRoot)

	prompt := buildSinglePrompt(row, hasDiff, oldStr, newStr, traceContext, sessionPrompts)
	callAndPrint(prompt)
}

// explainRange explains multiple records that cover a line range.
func explainRange(db *sql.DB, matches []*index.ReasonRow, rel, projectRoot string, lineStart, lineEnd int) {
	// Print all matching records
	for _, row := range matches {
		fmt.Println(format.FormatReason(row, projectRoot, false, nil))
		fmt.Println()
	}

	// Read current file content for the range
	fileContent := readFileLines(filepath.Join(projectRoot, rel), lineStart, lineEnd)

	// Gather context from all records
	var allTraceContexts []string
	sessionPromptSet := make(map[string]bool)
	var sessionPrompts []string

	for _, row := range matches {
		tc, sp := gatherContext(db, row, projectRoot)
		if tc != "" {
			allTraceContexts = append(allTraceContexts, tc)
		}
		for _, p := range sp {
			if !sessionPromptSet[p] {
				sessionPromptSet[p] = true
				sessionPrompts = append(sessionPrompts, p)
			}
		}
	}

	prompt := buildRangePrompt(matches, rel, lineStart, lineEnd, fileContent, allTraceContexts, sessionPrompts)
	callAndPrint(prompt)
}

// gatherContext collects trace context and session prompts for a record.
func gatherContext(db *sql.DB, row *index.ReasonRow, projectRoot string) (string, []string) {
	var traceContext string
	if row.Trace != "" {
		traceContext = transcript.ReadTraceContext(row.Trace, projectRoot)
	}

	var sessionPrompts []string
	if row.Trace != "" {
		transcriptPath := row.Trace
		if idx := strings.Index(transcriptPath, "#"); idx >= 0 {
			transcriptPath = transcriptPath[:idx]
		}
		sessionPrompts = transcript.ExtractSessionPrompts(transcriptPath)
	}

	if len(sessionPrompts) == 0 && row.Session != "" {
		siblings, _ := queryRows(db,
			"SELECT DISTINCT prompt FROM reasons WHERE session = ? AND prompt != '' ORDER BY ts",
			row.Session)
		for _, s := range siblings {
			sessionPrompts = append(sessionPrompts, s.Prompt)
		}
	}

	return traceContext, sessionPrompts
}

// buildSinglePrompt builds the LLM prompt for a single-record explain.
func buildSinglePrompt(row *index.ReasonRow, hasDiff bool, oldStr, newStr, traceContext string, sessionPrompts []string) string {
	var parts []string
	parts = append(parts,
		"You are explaining why a specific AI-authored code edit was made.",
		"Given the context below, write a clear, thorough explanation (2-5 sentences)",
		"of WHY this change was made — the user's intent, the reasoning, and how",
		"this edit fits into the broader task. Write in third person past tense.",
		"")

	parts = appendSessionPrompts(parts, sessionPrompts)

	parts = append(parts, fmt.Sprintf("File: %s", row.File))
	parts = appendLineInfo(parts, row)

	if row.Change != "" {
		parts = append(parts, fmt.Sprintf("Change: %s", row.Change))
	}

	if hasDiff {
		parts = appendDiff(parts, oldStr, newStr)
	}

	if row.Reason != "" {
		parts = append(parts, fmt.Sprintf("One-line reason: %s", row.Reason))
	}

	if traceContext != "" {
		parts = append(parts, fmt.Sprintf("\nAgent's internal reasoning:\n%s", traceContext))
	}

	parts = append(parts, "", "Write the explanation as plain text, no markdown or formatting.")

	return strings.Join(parts, "\n")
}

// buildRangePrompt builds the LLM prompt for a multi-record range explain.
func buildRangePrompt(matches []*index.ReasonRow, rel string, lineStart, lineEnd int, fileContent string, traceContexts, sessionPrompts []string) string {
	var parts []string
	parts = append(parts,
		"You are explaining the AI-authored changes across a range of lines in a file.",
		"Multiple edits may have contributed to this code region. Given the context below,",
		"write a clear, thorough explanation (3-8 sentences) of WHY these changes were made —",
		"the user's intent, the reasoning, and how the edits fit together. Write in third",
		"person past tense.",
		"")

	parts = appendSessionPrompts(parts, sessionPrompts)

	parts = append(parts, fmt.Sprintf("File: %s", rel))
	parts = append(parts, fmt.Sprintf("Line range: %d-%d", lineStart, lineEnd))
	parts = append(parts, "")

	if fileContent != "" {
		parts = append(parts, fmt.Sprintf("Current code at lines %d-%d:", lineStart, lineEnd))
		parts = append(parts, fileContent)
		parts = append(parts, "")
	}

	parts = append(parts, fmt.Sprintf("Edits in this range (%d record(s)):", len(matches)))
	for i, row := range matches {
		parts = append(parts, fmt.Sprintf("\n--- Edit %d ---", i+1))
		parts = appendLineInfo(parts, row)
		if row.Change != "" {
			parts = append(parts, fmt.Sprintf("Change: %s", row.Change))
		}
		if row.Reason != "" {
			parts = append(parts, fmt.Sprintf("Reason: %s", row.Reason))
		}
	}

	if len(traceContexts) > 0 {
		parts = append(parts, "\nAgent's internal reasoning (combined):")
		for _, tc := range traceContexts {
			if len(tc) > 1500 {
				tc = tc[:1500] + "..."
			}
			parts = append(parts, tc)
		}
	}

	parts = append(parts, "", "Write the explanation as plain text, no markdown or formatting.")

	return strings.Join(parts, "\n")
}

// readFileLines reads lines [start, end] (1-indexed) from a file.
func readFileLines(path string, start, end int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum > end {
			break
		}
		if lineNum >= start {
			lines = append(lines, scanner.Text())
		}
	}
	return strings.Join(lines, "\n")
}

func showChangeDiff(change string) {
	if strings.Contains(change, " → ") {
		idx := strings.Index(change, " → ")
		left := strings.TrimLeft(change[:idx], "… ")
		right := strings.TrimLeft(change[idx+len(" → "):], "… ")
		if left != "" || right != "" {
			fmt.Println(format.FormatSideBySideDiff(left, right))
			fmt.Println()
		}
	} else if strings.HasPrefix(change, "added: ") {
		fmt.Println(format.FormatSideBySideDiff("", change[7:]))
		fmt.Println()
	} else if strings.HasPrefix(change, "removed: ") {
		fmt.Println(format.FormatSideBySideDiff(change[9:], ""))
		fmt.Println()
	}
}

func appendSessionPrompts(parts []string, prompts []string) []string {
	if len(prompts) > 0 {
		parts = append(parts, "Session prompt history (in order):")
		for i, p := range prompts {
			display := p
			if len(display) > 300 {
				display = display[:297] + "..."
			}
			parts = append(parts, fmt.Sprintf("%d. \"%s\"", i+1, display))
		}
		parts = append(parts, "")
	}
	return parts
}

func appendLineInfo(parts []string, row *index.ReasonRow) []string {
	if row.ChangedLines != nil && *row.ChangedLines != "" {
		parts = append(parts, fmt.Sprintf("Lines: %s", *row.ChangedLines))
	} else if row.LineStart != nil {
		if row.LineEnd != nil && *row.LineEnd != *row.LineStart {
			parts = append(parts, fmt.Sprintf("Lines: %d-%d", *row.LineStart, *row.LineEnd))
		} else {
			parts = append(parts, fmt.Sprintf("Line: %d", *row.LineStart))
		}
	}
	return parts
}

func appendDiff(parts []string, oldStr, newStr string) []string {
	if oldStr != "" {
		if len(oldStr) > 2000 {
			oldStr = oldStr[:2000]
		}
		parts = append(parts, fmt.Sprintf("\nOriginal code:\n%s", oldStr))
	}
	if newStr != "" {
		if len(newStr) > 2000 {
			newStr = newStr[:2000]
		}
		parts = append(parts, fmt.Sprintf("\nNew code:\n%s", newStr))
	}
	return parts
}

func callAndPrint(prompt string) {
	fmt.Fprintf(os.Stderr, "%s(calling Sonnet...)%s", format.Dim, format.Reset)

	explanation, err := llm.Call(prompt, "claude-sonnet-4-6", 90*1000000000) // 90s
	fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 30))

	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to generate explanation:", err)
		return
	}

	if explanation != "" {
		fmt.Println(format.FormatBorderedText(explanation, "Explanation"))
	} else {
		fmt.Fprintln(os.Stderr, "Failed to generate explanation.")
	}
}
