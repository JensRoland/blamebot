package linemap

import (
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/lineset"
)

// AdjustedRow wraps a ReasonRow with computed current line positions.
type AdjustedRow struct {
	*index.ReasonRow
	CurrentLines lineset.LineSet
	Superseded   bool
}

// AdjustLinePositions takes records for a single file sorted by timestamp
// (oldest first) and computes each record's current line positions by forward-
// simulating the effects of subsequent edits.
//
// For each record i, iterate through all later records j. Each record j's hunk
// data tells us where j edited (OldStart), how many lines it replaced (OldLines),
// and how many it inserted (NewLines). For each line in record i's set:
//   - Lines before the edit region: unchanged
//   - Lines within the overwritten region: removed (superseded by edit j)
//   - Lines after the edit region: shifted by delta (NewLines - OldLines)
func AdjustLinePositions(rows []*index.ReasonRow) []*AdjustedRow {
	results := make([]*AdjustedRow, len(rows))

	for i, row := range rows {
		adj := &AdjustedRow{ReasonRow: row}

		// Parse the current lines from ChangedLines or fall back to bounding range
		currentLines := parseRowLines(row)
		if currentLines.IsEmpty() {
			results[i] = adj
			continue
		}

		for j := i + 1; j < len(rows); j++ {
			rj := rows[j]

			// Write tool supersedes all prior records for this file
			if rj.Tool == "Write" {
				adj.Superseded = true
				break
			}

			// Need hunk data to compute the shift
			if rj.OldStart == nil || rj.OldLines == nil || rj.NewLines == nil {
				continue
			}

			editStart := *rj.OldStart
			oldCount := *rj.OldLines
			newCount := *rj.NewLines
			delta := newCount - oldCount

			var shifted []int
			for _, line := range currentLines.Lines() {
				if oldCount == 0 {
					// Pure insertion at editStart
					if line >= editStart {
						shifted = append(shifted, line+newCount)
					} else {
						shifted = append(shifted, line)
					}
				} else {
					editEnd := editStart + oldCount - 1
					if line < editStart {
						// Before edit: unchanged
						shifted = append(shifted, line)
					} else if line <= editEnd {
						// Within overwritten region: removed
						continue
					} else {
						// After edit: shift by delta
						shifted = append(shifted, line+delta)
					}
				}
			}

			currentLines = lineset.New(shifted...)
			if currentLines.IsEmpty() {
				adj.Superseded = true
				break
			}
		}

		if !adj.Superseded {
			adj.CurrentLines = currentLines
		}

		results[i] = adj
	}

	return results
}

// parseRowLines extracts a LineSet from a ReasonRow.
// Prefers ChangedLines (precise), falls back to bounding range.
func parseRowLines(row *index.ReasonRow) lineset.LineSet {
	if row.ChangedLines != nil && *row.ChangedLines != "" {
		ls, err := lineset.FromString(*row.ChangedLines)
		if err == nil && !ls.IsEmpty() {
			return ls
		}
	}
	// Fallback to bounding range
	if row.LineStart != nil {
		end := *row.LineStart
		if row.LineEnd != nil {
			end = *row.LineEnd
		}
		return lineset.FromRange(*row.LineStart, end)
	}
	return lineset.LineSet{}
}
