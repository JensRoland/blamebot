package format

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/lineset"
)

// LineAdjustment holds computed current line positions for a record.
type LineAdjustment struct {
	CurrentLines lineset.LineSet
	Superseded   bool
}

// currentContentHash computes the hash for the current file content at given lines.
func currentContentHash(filePath string, lineStart, lineEnd int) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")

	var region string
	if lineStart > 0 && lineEnd > 0 {
		start := lineStart - 1
		end := lineEnd
		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		region = strings.Join(lines[start:end], "\n")
	} else {
		region = strings.Join(lines, "\n")
	}

	normalized := strings.Join(strings.Fields(region), " ")
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h)[:16]
}

// FormatReason formats a ReasonRow for terminal output.
// If adj is non-nil, adjusted line positions are shown.
func FormatReason(row *index.ReasonRow, projectRoot string, verbose bool, adj *LineAdjustment) string {
	ts := row.Ts
	if ts == "" {
		ts = "?"
	}
	date := ts
	if len(ts) >= 10 {
		date = ts[:10]
	}

	prompt := row.Prompt
	reason := row.Reason
	change := row.Change
	file := row.File
	if file == "" {
		file = "?"
	}
	author := row.Author

	// Determine which lines to display and use for hash verification
	var hashStart, hashEnd *int
	var lines string
	if adj != nil && !adj.CurrentLines.IsEmpty() {
		// Show current adjusted lines only
		lines = "L" + adj.CurrentLines.String()
		// Use adjusted positions for hash verification
		mn := adj.CurrentLines.Min()
		mx := adj.CurrentLines.Max()
		hashStart = &mn
		hashEnd = &mx
	} else if row.LineStart != nil {
		if row.LineEnd != nil && *row.LineEnd != *row.LineStart {
			lines = fmt.Sprintf("L%d-%d", *row.LineStart, *row.LineEnd)
		} else {
			lines = fmt.Sprintf("L%d", *row.LineStart)
		}
		hashStart = row.LineStart
		hashEnd = row.LineEnd
	}

	// Content hash match indicator
	matchIndicator := ""
	if row.ContentHash != "" && hashStart != nil {
		fp := filepath.Join(projectRoot, row.File)
		lineEnd := *hashStart
		if hashEnd != nil {
			lineEnd = *hashEnd
		}
		current := currentContentHash(fp, *hashStart, lineEnd)
		if current == row.ContentHash {
			matchIndicator = " " + Green + "\u2713" + Reset
		} else if current != "" {
			matchIndicator = " " + Yellow + "~" + Reset
		}
	}

	// Build header
	header := Dim + fmt.Sprintf("#%d", row.ID) + Reset + " " + Cyan + date + Reset
	if author != "" {
		header += " " + Blue + author + Reset
	}
	header += "  " + Bold + file + Reset
	if lines != "" {
		header += " " + Dim + lines + Reset
	}
	header += matchIndicator

	parts := []string{header}

	if prompt != "" {
		promptDisplay := prompt
		if len(prompt) > 120 {
			promptDisplay = prompt[:117] + "..."
		}
		parts = append(parts, fmt.Sprintf("  %sPrompt:%s %s", Yellow, Reset, promptDisplay))
	}

	if reason != "" {
		reasonDisplay := reason
		if len(reason) > 200 {
			reasonDisplay = reason[:197] + "..."
		}
		parts = append(parts, fmt.Sprintf("  %sReason:%s %s", Magenta, Reset, reasonDisplay))
	} else if change != "" {
		parts = append(parts, fmt.Sprintf("  %sChange:%s %s", Magenta, Reset, change))
	}

	if verbose {
		if reason != "" && change != "" {
			parts = append(parts, fmt.Sprintf("  %sDiff:    %s%s", Dim, change, Reset))
		}
		if row.Tool != "" {
			parts = append(parts, fmt.Sprintf("  %sTool:    %s%s", Dim, row.Tool, Reset))
		}
		if row.ContentHash != "" {
			parts = append(parts, fmt.Sprintf("  %sHash:    %s%s", Dim, row.ContentHash, Reset))
		}
		if row.Session != "" {
			session := row.Session
			if len(session) > 36 {
				session = session[:36]
			}
			parts = append(parts, fmt.Sprintf("  %sSession: %s%s", Dim, session, Reset))
		}
		if row.Trace != "" {
			trace := row.Trace
			if strings.Contains(trace, "#") {
				parts = append(parts, fmt.Sprintf("  %sTrace:   #%s%s", Dim, trace[strings.Index(trace, "#")+1:], Reset))
			} else {
				display := trace
				if len(display) > 60 {
					display = display[len(display)-60:]
				}
				parts = append(parts, fmt.Sprintf("  %sTrace:   %s%s", Dim, display, Reset))
			}
		}
		if row.SourceFile != "" {
			parts = append(parts, fmt.Sprintf("  %sSource:  %s%s", Dim, row.SourceFile, Reset))
		}

		// Git blame cross-reference (use adjusted line if available)
		var blameLine *int
		if adj != nil && !adj.CurrentLines.IsEmpty() {
			mn := adj.CurrentLines.Min()
			blameLine = &mn
		} else {
			blameLine = row.LineStart
		}
		if blameLine != nil {
			blame, err := git.BlameForLine(projectRoot, row.File, *blameLine)
			if err == nil && blame != nil && blame.SHA != "" {
				shaShort := blame.SHA[:8]
				parts = append(parts, fmt.Sprintf("  %sCommit:  %s %s%s", Dim, shaShort, blame.Summary, Reset))
			}
		}
	}

	return strings.Join(parts, "\n")
}

// RowToJSON converts a ReasonRow to a JSON-serializable map.
func RowToJSON(row *index.ReasonRow, projectRoot string, adj *LineAdjustment) map[string]interface{} {
	d := map[string]interface{}{
		"file":         row.File,
		"lines":        [2]interface{}{row.LineStart, row.LineEnd},
		"ts":           row.Ts,
		"prompt":       row.Prompt,
		"reason":       row.Reason,
		"change":       row.Change,
		"tool":         row.Tool,
		"author":       row.Author,
		"content_hash": row.ContentHash,
		"session":      row.Session,
		"trace":        row.Trace,
		"source_file":  row.SourceFile,
	}

	// Use adjusted positions for hash verification if available
	var hashStart, hashEnd *int
	if adj != nil && !adj.CurrentLines.IsEmpty() {
		d["current_lines"] = adj.CurrentLines.String()
		mn := adj.CurrentLines.Min()
		mx := adj.CurrentLines.Max()
		hashStart = &mn
		hashEnd = &mx
		if adj.Superseded {
			d["superseded"] = true
		}
	} else {
		hashStart = row.LineStart
		hashEnd = row.LineEnd
	}

	if row.ContentHash != "" && hashStart != nil {
		fp := filepath.Join(projectRoot, row.File)
		lineEnd := *hashStart
		if hashEnd != nil {
			lineEnd = *hashEnd
		}
		current := currentContentHash(fp, *hashStart, lineEnd)
		if current == row.ContentHash {
			d["match"] = "exact"
		} else if current != "" {
			d["match"] = "changed"
		} else {
			d["match"] = "unknown"
		}
	}
	return d
}
