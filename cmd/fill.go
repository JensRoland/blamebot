package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jensroland/git-blamebot/internal/format"
	gitutil "github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/llm"
	"github.com/jensroland/git-blamebot/internal/project"
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

func cmdFillReasons(paths project.Paths, projectRoot string, dryRun bool) {
	stagedFiles, err := gitutil.StagedJSONLFiles(projectRoot)
	if err != nil || len(stagedFiles) == 0 {
		fmt.Fprintln(os.Stderr, "No staged .blamebot/log/*.jsonl files found.")
		return
	}

	// Read all records from staged files
	type fileRecords struct {
		records []map[string]interface{}
	}
	allRecords := make(map[string]*fileRecords)
	needsFill := 0

	for _, relPath := range stagedFiles {
		absPath := filepath.Join(projectRoot, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		fr := &fileRecords{}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var rec map[string]interface{}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				fr.records = append(fr.records, nil)
				continue
			}
			fr.records = append(fr.records, rec)
			reason, _ := rec["reason"].(string)
			if reason == "" {
				needsFill++
			}
		}
		allRecords[relPath] = fr
	}

	if needsFill == 0 {
		fmt.Fprintln(os.Stderr, "All records already have reasons.")
		return
	}

	fmt.Fprintf(os.Stderr, "Found %d record(s) to fill across %d file(s).\n", needsFill, len(stagedFiles))

	// Group by transcript path
	type transcriptGroup struct {
		edits []fillEdit
	}
	groups := make(map[string]*transcriptGroup)
	editID := 0

	for relPath, fr := range allRecords {
		for i, rec := range fr.records {
			if rec == nil {
				continue
			}
			reason, _ := rec["reason"].(string)
			if reason != "" {
				continue
			}
			trace, _ := rec["trace"].(string)
			transcriptPath := ""
			if idx := strings.Index(trace, "#"); idx >= 0 {
				transcriptPath = trace[:idx]
			}
			if transcriptPath == "" {
				continue
			}
			editID++

			var lineStart, lineEnd *int
			if lines, ok := rec["lines"].([]interface{}); ok {
				if len(lines) > 0 {
					if v, ok := lines[0].(float64); ok {
						n := int(v)
						lineStart = &n
					}
				}
				if len(lines) > 1 {
					if v, ok := lines[1].(float64); ok {
						n := int(v)
						lineEnd = &n
					}
				}
			}

			file, _ := rec["file"].(string)
			change, _ := rec["change"].(string)

			g, ok := groups[transcriptPath]
			if !ok {
				g = &transcriptGroup{}
				groups[transcriptPath] = g
			}
			g.edits = append(g.edits, fillEdit{
				ID:         editID,
				File:       file,
				LineStart:  lineStart,
				LineEnd:    lineEnd,
				Change:     change,
				SourceFile: relPath,
				RecordIdx:  i,
			})
		}
	}

	// Process each transcript group
	reasonMap := make(map[int]string)

	for transcriptPath, group := range groups {
		sessionPrompts := transcript.ExtractSessionPrompts(transcriptPath)

		if len(sessionPrompts) == 0 {
			seen := make(map[string]bool)
			for _, edit := range group.edits {
				rec := allRecords[edit.SourceFile].records[edit.RecordIdx]
				p, _ := rec["prompt"].(string)
				if p != "" && !seen[p] {
					sessionPrompts = append(sessionPrompts, p)
					seen[p] = true
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

	// Extract and write trace contexts
	tracesDir := filepath.Join(projectRoot, ".blamebot", "traces")
	if !dryRun {
		_ = os.MkdirAll(tracesDir, 0o755)
	}

	for transcriptPath, group := range groups {
		var toolUseIDs []string
		for _, edit := range group.edits {
			rec := allRecords[edit.SourceFile].records[edit.RecordIdx]
			trace, _ := rec["trace"].(string)
			if idx := strings.Index(trace, "#"); idx >= 0 {
				toolUseIDs = append(toolUseIDs, trace[idx+1:])
			}
		}

		if len(toolUseIDs) == 0 {
			continue
		}

		contexts := transcript.ExtractTraceContexts(transcriptPath, toolUseIDs)
		if len(contexts) == 0 {
			continue
		}

		sessionID := filepath.Base(transcriptPath)
		sessionID = strings.TrimSuffix(sessionID, filepath.Ext(sessionID))

		if dryRun {
			fmt.Printf("%s  Traces: %d context(s) for session %s...%s\n",
				format.Dim, len(contexts), sessionID[:min(len(sessionID), 12)], format.Reset)
			continue
		}

		tracesFile := filepath.Join(tracesDir, sessionID+".json")
		existing := make(map[string]string)
		if data, err := os.ReadFile(tracesFile); err == nil {
			_ = json.Unmarshal(data, &existing)
		}
		for k, v := range contexts {
			existing[k] = v
		}
		b, _ := json.MarshalIndent(existing, "", "  ")
		_ = os.WriteFile(tracesFile, append(b, '\n'), 0o644)

		rel, _ := filepath.Rel(projectRoot, tracesFile)
		_ = gitutil.StageFile(projectRoot, rel)
	}

	if dryRun {
		return
	}

	if len(reasonMap) == 0 {
		fmt.Fprintln(os.Stderr, "No reasons generated.")
		return
	}

	// Patch JSONL files in place
	patched := 0
	for relPath, fr := range allRecords {
		changed := false
		for i, rec := range fr.records {
			if rec == nil {
				continue
			}
			for _, group := range groups {
				for _, edit := range group.edits {
					if edit.SourceFile == relPath && edit.RecordIdx == i {
						if reason, ok := reasonMap[edit.ID]; ok {
							rec["reason"] = reason
							changed = true
							patched++
						}
					}
				}
			}
		}

		if changed {
			absPath := filepath.Join(projectRoot, relPath)
			f, err := os.Create(absPath)
			if err != nil {
				continue
			}
			for _, rec := range fr.records {
				if rec != nil {
					b, _ := json.Marshal(rec)
					fmt.Fprintf(f, "%s\n", b)
				}
			}
			f.Close()
			_ = gitutil.StageFile(projectRoot, relPath)
		}
	}

	fmt.Fprintf(os.Stderr, "Filled %d reason(s). Index will rebuild on next query.\n", patched)

	// Force index rebuild
	_ = os.Remove(paths.IndexDB)
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
