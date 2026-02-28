package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/llm"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
	"github.com/jensroland/git-blamebot/internal/transcript"
)

// HandleCommitMsg processes the commit-msg git hook.
// It bundles pending edits into a manifest, fills reasons, writes traces,
// stores the manifest on the provenance branch, and appends a trailer.
func HandleCommitMsg(commitMsgFile string) error {
	root, err := project.FindRoot()
	if err != nil {
		return err
	}
	if !project.IsInitialized(root) {
		return nil
	}
	paths := project.NewPaths(root)

	// 1. Read pending edits
	pending, err := provenance.ReadAllPending(paths.GitDir)
	if err != nil || len(pending) == 0 {
		return nil
	}

	debug.Log(paths.CacheDir, "hook.log",
		fmt.Sprintf("commit-msg: bundling %d pending edit(s)", len(pending)), nil)

	// 2. Build manifest
	manifestID := uuid.New().String()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	author := pending[0].Author

	manifest := provenance.Manifest{
		ID:        manifestID,
		Author:    author,
		Timestamp: now,
		Edits:     make([]provenance.ManifestEdit, len(pending)),
	}
	for i, pe := range pending {
		manifest.Edits[i] = provenance.ManifestEdit{
			File:        pe.File,
			Lines:       pe.Lines,
			Hunk:        pe.Hunk,
			ContentHash: pe.ContentHash,
			Prompt:      pe.Prompt,
			Reason:      "",
			Change:      pe.Change,
			Tool:        pe.Tool,
			Session:     pe.Session,
			Trace:       pe.Trace,
		}
	}

	// 3. Fill reasons via Haiku
	fillManifestReasons(&manifest, paths)

	// 4. Extract and write traces
	writeManifestTraces(root, paths.GitDir, &manifest)

	// 5. Ensure provenance branch exists
	if err := provenance.InitBranch(root); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("Failed to init provenance branch: %v", err), nil)
		return nil
	}

	// 6. Write manifest to provenance branch
	if err := provenance.WriteManifest(root, paths.GitDir, manifest); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("Failed to write manifest: %v", err), nil)
		return nil
	}

	// 7. Append trailer to commit message
	if err := appendTrailer(commitMsgFile, manifestID); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("Failed to append trailer: %v", err), nil)
	}

	// 8. Clear pending edits
	provenance.ClearPending(paths.GitDir)

	debug.Log(paths.CacheDir, "hook.log",
		fmt.Sprintf("Created manifest %s with %d edits", manifestID, len(pending)), nil)
	return nil
}

// appendTrailer adds a Blamebot-Ref trailer to the commit message file.
func appendTrailer(commitMsgFile, manifestID string) error {
	data, err := os.ReadFile(commitMsgFile)
	if err != nil {
		return err
	}
	msg := strings.TrimRight(string(data), "\n")

	// Ensure blank line before trailer block
	if !strings.Contains(msg, "\n\n") {
		msg += "\n"
	}
	msg += "\nBlamebot-Ref: " + manifestID + "\n"
	return os.WriteFile(commitMsgFile, []byte(msg), 0o644)
}

// fillManifestReasons fills empty reason fields using Claude Haiku.
func fillManifestReasons(manifest *provenance.Manifest, paths project.Paths) {
	// Group edits by transcript path
	type editRef struct {
		idx  int
		edit *provenance.ManifestEdit
	}
	groups := make(map[string][]editRef)

	for i := range manifest.Edits {
		e := &manifest.Edits[i]
		transcriptPath := ""
		if idx := strings.Index(e.Trace, "#"); idx >= 0 {
			transcriptPath = e.Trace[:idx]
		}
		if transcriptPath == "" {
			continue
		}
		groups[transcriptPath] = append(groups[transcriptPath], editRef{idx: i, edit: e})
	}

	reasonMap := make(map[int]string) // edit index → reason

	for transcriptPath, refs := range groups {
		sessionPrompts := transcript.ExtractSessionPrompts(transcriptPath)

		// Fall back to prompts from edit records
		if len(sessionPrompts) == 0 {
			seen := make(map[string]bool)
			for _, ref := range refs {
				if ref.edit.Prompt != "" && !seen[ref.edit.Prompt] {
					sessionPrompts = append(sessionPrompts, ref.edit.Prompt)
					seen[ref.edit.Prompt] = true
				}
			}
		}

		// Build fill prompt
		var edits []manifestFillEdit
		for _, ref := range refs {
			edits = append(edits, manifestFillEdit{
				id:     ref.idx + 1, // 1-indexed for Haiku
				file:   ref.edit.File,
				change: ref.edit.Change,
			})
		}

		prompt := buildManifestFillPrompt(sessionPrompts, edits)

		results, err := llm.CallHaiku(prompt)
		if err != nil {
			debug.Log(paths.CacheDir, "hook.log",
				fmt.Sprintf("Haiku fill failed: %v", err), nil)
			continue
		}

		for _, item := range results {
			if item.ID > 0 && item.Reason != "" {
				reasonMap[item.ID] = item.Reason
			}
		}
	}

	// Apply reasons
	for i := range manifest.Edits {
		if reason, ok := reasonMap[i+1]; ok {
			manifest.Edits[i].Reason = reason
		}
	}
}

type manifestFillEdit struct {
	id     int
	file   string
	change string
}

func buildManifestFillPrompt(sessionPrompts []string, edits []manifestFillEdit) string {
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
		parts = append(parts, fmt.Sprintf("[%d] File: %s", edit.id, edit.file))
		parts = append(parts, fmt.Sprintf("    Change: %s", edit.change))
	}

	parts = append(parts, "", `Respond with ONLY a JSON array: [{"id": 1, "reason": "..."}, ...]`)
	return strings.Join(parts, "\n")
}

// writeManifestTraces extracts trace contexts from transcripts and writes to provenance branch.
func writeManifestTraces(root, gitDir string, manifest *provenance.Manifest) {
	// Group edits by transcript path
	transcriptEdits := make(map[string][]string) // transcriptPath → []toolUseID

	for _, e := range manifest.Edits {
		if idx := strings.Index(e.Trace, "#"); idx >= 0 {
			transcriptPath := e.Trace[:idx]
			toolUseID := e.Trace[idx+1:]
			if transcriptPath != "" && toolUseID != "" {
				transcriptEdits[transcriptPath] = append(transcriptEdits[transcriptPath], toolUseID)
			}
		}
	}

	for transcriptPath, toolUseIDs := range transcriptEdits {
		contexts := transcript.ExtractTraceContexts(transcriptPath, toolUseIDs)
		if len(contexts) == 0 {
			continue
		}

		sessionID := filepath.Base(transcriptPath)
		sessionID = strings.TrimSuffix(sessionID, filepath.Ext(sessionID))

		if err := provenance.WriteTrace(root, gitDir, sessionID, contexts); err != nil {
			// Best effort — don't fail on trace extraction errors
			continue
		}
	}
}
