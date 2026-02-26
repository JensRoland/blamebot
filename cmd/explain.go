package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/llm"
	"github.com/jensroland/git-blamebot/internal/transcript"
)

func cmdExplain(db *sql.DB, target, projectRoot, line string) {
	var row *index.ReasonRow

	// If target looks like a record ID, look it up directly
	if id, err := strconv.Atoi(target); err == nil {
		rows, _ := queryRows(db, "SELECT * FROM reasons WHERE id = ?", id)
		if len(rows) == 0 {
			fmt.Printf("No record found with id %d\n", id)
			return
		}
		row = rows[0]
	} else {
		// File path query
		rel := relativePath(target, projectRoot)

		conditions := []string{"(file = ? OR file LIKE ?)"}
		params := []interface{}{rel, "%/" + rel}

		if line != "" {
			if strings.Contains(line, ":") {
				parts := strings.SplitN(line, ":", 2)
				start, _ := strconv.Atoi(parts[0])
				end, _ := strconv.Atoi(parts[1])
				conditions = append(conditions, "(line_start <= ? AND (line_end >= ? OR line_end IS NULL))")
				params = append(params, end, start)
			} else {
				lineNum, _ := strconv.Atoi(line)
				conditions = append(conditions, "(line_start <= ? AND (line_end >= ? OR line_start = ?))")
				params = append(params, lineNum, lineNum, lineNum)
			}
		}

		where := strings.Join(conditions, " AND ")
		rows, err := queryRows(db,
			fmt.Sprintf("SELECT * FROM reasons WHERE %s ORDER BY ts DESC", where),
			params...)
		if err != nil {
			fmt.Printf("No reasons found for %s\n", rel)
			return
		}

		// Post-filter by precise changed lines when available
		if line != "" {
			rows = filterByPreciseLines(rows, line)
		}

		if len(rows) == 0 {
			loc := ""
			if line != "" {
				loc = " at line " + line
			}
			fmt.Printf("No reasons found for %s%s\n", rel, loc)
			return
		}

		if len(rows) > 1 {
			fmt.Fprintf(os.Stderr, "%s(%d records match — explaining the most recent)%s\n\n",
				format.Dim, len(rows), format.Reset)
		}

		row = rows[0]
	}
	fmt.Println(format.FormatReason(row, projectRoot, false))
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
		change := row.Change
		if strings.Contains(change, " \u2192 ") {
			idx := strings.Index(change, " \u2192 ")
			left := strings.TrimLeft(change[:idx], "\u2026 ")
			right := strings.TrimLeft(change[idx+len(" \u2192 "):], "\u2026 ")
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

	// Gather context
	var traceContext string
	if row.Trace != "" {
		traceContext = transcript.ReadTraceContext(row.Trace, projectRoot)
	}

	// Get session prompts
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

	// Build the explain prompt
	var parts []string
	parts = append(parts,
		"You are explaining why a specific AI-authored code edit was made.",
		"Given the context below, write a clear, thorough explanation (2-5 sentences)",
		"of WHY this change was made — the user's intent, the reasoning, and how",
		"this edit fits into the broader task. Write in third person past tense.",
		"")

	if len(sessionPrompts) > 0 {
		parts = append(parts, "Session prompt history (in order):")
		for i, p := range sessionPrompts {
			display := p
			if len(display) > 300 {
				display = display[:297] + "..."
			}
			parts = append(parts, fmt.Sprintf("%d. \"%s\"", i+1, display))
		}
		parts = append(parts, "")
	}

	parts = append(parts, fmt.Sprintf("File: %s", row.File))
	if row.ChangedLines != "" {
		parts = append(parts, fmt.Sprintf("Lines: %s", row.ChangedLines))
	} else if row.LineStart != nil {
		if row.LineEnd != nil && *row.LineEnd != *row.LineStart {
			parts = append(parts, fmt.Sprintf("Lines: %d-%d", *row.LineStart, *row.LineEnd))
		} else {
			parts = append(parts, fmt.Sprintf("Line: %d", *row.LineStart))
		}
	}

	if row.Change != "" {
		parts = append(parts, fmt.Sprintf("Change: %s", row.Change))
	}

	if hasDiff {
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
	}

	if row.Reason != "" {
		parts = append(parts, fmt.Sprintf("One-line reason: %s", row.Reason))
	}

	if traceContext != "" {
		parts = append(parts, fmt.Sprintf("\nAgent's internal reasoning:\n%s", traceContext))
	}

	parts = append(parts, "", "Write the explanation as plain text, no markdown or formatting.")

	prompt := strings.Join(parts, "\n")

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
