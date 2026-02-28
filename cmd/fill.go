package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/llm"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
	"github.com/jensroland/git-blamebot/internal/transcript"
)

type fillEdit struct {
	ID         int
	File       string
	LineStart  *int
	LineEnd    *int
	Change     string
	SourceFile string
	RecordIdx  int
}

// cmdFillReasons fills reasons for pending edits (manual fallback).
func cmdFillReasons(paths project.Paths, projectRoot string, dryRun bool) {
	pending, err := provenance.ReadAllPending(paths.GitDir)
	if err != nil || len(pending) == 0 {
		fmt.Fprintln(os.Stderr, "No pending edits found.")
		return
	}

	needsFill := len(pending) // all pending edits need reasons
	fmt.Fprintf(os.Stderr, "Found %d pending edit(s) to fill.\n", needsFill)

	// Group by transcript path
	type transcriptGroup struct {
		edits []fillEdit
	}
	groups := make(map[string]*transcriptGroup)

	for i, pe := range pending {
		transcriptPath := ""
		if idx := strings.Index(pe.Trace, "#"); idx >= 0 {
			transcriptPath = pe.Trace[:idx]
		}
		if transcriptPath == "" {
			continue
		}

		var lineStart, lineEnd *int
		if !pe.Lines.IsEmpty() {
			mn := pe.Lines.Min()
			mx := pe.Lines.Max()
			lineStart = &mn
			lineEnd = &mx
		}

		g, ok := groups[transcriptPath]
		if !ok {
			g = &transcriptGroup{}
			groups[transcriptPath] = g
		}
		g.edits = append(g.edits, fillEdit{
			ID:        i + 1,
			File:      pe.File,
			LineStart: lineStart,
			LineEnd:   lineEnd,
			Change:    pe.Change,
		})
	}

	reasonMap := make(map[int]string)

	for transcriptPath, group := range groups {
		sessionPrompts := transcript.ExtractSessionPrompts(transcriptPath)

		if len(sessionPrompts) == 0 {
			seen := make(map[string]bool)
			for _, edit := range group.edits {
				idx := edit.ID - 1
				if idx >= 0 && idx < len(pending) {
					p := pending[idx].Prompt
					if p != "" && !seen[p] {
						sessionPrompts = append(sessionPrompts, p)
						seen[p] = true
					}
				}
			}
		}

		prompt := buildFillPrompt(sessionPrompts, group.edits)

		if dryRun {
			display := transcriptPath
			if len(display) > 60 {
				display = "..." + display[len(display)-60:]
			}
			fmt.Printf("\n%s── Transcript: %s%s\n", format.Bold, display, format.Reset)
			fmt.Printf("%s%s%s\n\n", format.Dim, prompt, format.Reset)
			continue
		}

		display := transcriptPath
		if len(display) > 50 {
			display = "..." + display[len(display)-50:]
		}
		fmt.Fprintf(os.Stderr, "  Filling %d edit(s) from %s", len(group.edits), display)

		results, err := llm.CallHaiku(prompt)
		if err != nil {
			fmt.Fprintln(os.Stderr, " → failed")
			continue
		}

		filled := 0
		for _, item := range results {
			if item.ID > 0 && item.Reason != "" {
				reasonMap[item.ID] = item.Reason
				filled++
			}
		}
		fmt.Fprintf(os.Stderr, " → %d reasons\n", filled)
	}

	if dryRun {
		return
	}

	if len(reasonMap) == 0 {
		fmt.Fprintln(os.Stderr, "No reasons generated.")
		return
	}

	fmt.Fprintf(os.Stderr, "Filled %d reason(s). They will be included in the next commit's manifest.\n", len(reasonMap))
}

func buildFillPrompt(sessionPrompts []string, edits []fillEdit) string {
	var parts []string
	parts = append(parts,
		"You are generating concise reasons for AI code edits.",
		"Given the session prompt history and edit details below,",
		"write a brief reason (1 sentence max) for each edit",
		"explaining WHY the change was made.",
		"",
		"Session prompt history (in order):")

	for i, p := range sessionPrompts {
		display := p
		if len(display) > 200 {
			display = display[:197] + "..."
		}
		parts = append(parts, fmt.Sprintf("%d. \"%s\"", i+1, display))
	}

	parts = append(parts, "", "Edits:")
	for _, edit := range edits {
		lines := ""
		if edit.LineStart != nil {
			if edit.LineEnd != nil && *edit.LineEnd != *edit.LineStart {
				lines = fmt.Sprintf(" L%d-%d", *edit.LineStart, *edit.LineEnd)
			} else {
				lines = fmt.Sprintf(" L%d", *edit.LineStart)
			}
		}
		parts = append(parts, fmt.Sprintf("[%d] File: %s%s", edit.ID, edit.File, lines))
		parts = append(parts, fmt.Sprintf("    Change: %s", edit.Change))
	}

	parts = append(parts, "", `Respond with ONLY a JSON array: [{"id": 1, "reason": "..."}, ...]`)
	return strings.Join(parts, "\n")
}
