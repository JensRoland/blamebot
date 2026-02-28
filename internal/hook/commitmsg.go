package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jensroland/git-blamebot/internal/checkpoint"
	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/lineset"
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

	// 4. Compute checkpoint-based attribution
	manifest.Attributions = computeAttributions(root, paths, pending)

	// 5. Extract and write traces
	writeManifestTraces(root, paths.GitDir, &manifest)

	// 6. Ensure provenance branch exists
	if err := provenance.InitBranch(root); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("Failed to init provenance branch: %v", err), nil)
		return nil
	}

	// 7. Write manifest to provenance branch
	if err := provenance.WriteManifest(root, paths.GitDir, manifest); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("Failed to write manifest: %v", err), nil)
		return nil
	}

	// 8. Append trailer to commit message
	if err := appendTrailer(commitMsgFile, manifestID); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("Failed to append trailer: %v", err), nil)
	}

	// 9. Clear pending edits and checkpoints
	provenance.ClearPending(paths.GitDir)
	checkpoint.ClearAll(paths.CheckpointDir)

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

// computeAttributions builds per-file attribution maps from checkpoints.
func computeAttributions(root string, paths project.Paths, pending []provenance.PendingEdit) map[string]provenance.FileAttribution {
	allCheckpoints, err := checkpoint.ReadAllCheckpoints(paths.CheckpointDir)
	if err != nil || len(allCheckpoints) == 0 {
		return nil
	}

	// Build pending edit ID → manifest edit index mapping
	editIDToIdx := make(map[string]int)
	for i, pe := range pending {
		editIDToIdx[pe.ID] = i
	}

	// Find unique files that have checkpoints
	filesSeen := make(map[string]bool)
	var files []string
	for _, cp := range allCheckpoints {
		if !filesSeen[cp.File] {
			filesSeen[cp.File] = true
			files = append(files, cp.File)
		}
	}

	result := make(map[string]provenance.FileAttribution)

	blobReader := func(sha string) string {
		content, _ := checkpoint.ReadBlob(paths.CheckpointDir, sha)
		return content
	}

	for _, file := range files {
		fileCheckpoints := checkpoint.CheckpointsForFile(allCheckpoints, file)
		if len(fileCheckpoints) == 0 {
			continue
		}

		// Get base content (HEAD version)
		baseContent, _ := git.ShowFile(root, "HEAD", file)

		// Get current content (the staged version being committed)
		absFile := filepath.Join(root, file)
		currentBytes, err := os.ReadFile(absFile)
		if err != nil {
			continue
		}
		currentContent := string(currentBytes)

		// Compute attribution
		attr := checkpoint.ComputeFileAttribution(baseContent, currentContent, fileCheckpoints, blobReader)
		if len(attr) == 0 {
			continue
		}

		// Convert edit IDs to manifest edit indices
		editLines := make(map[int]lineset.LineSet)
		for editID, lineSet := range attr {
			if idx, ok := editIDToIdx[editID]; ok {
				editLines[idx] = lineSet
			}
		}
		if len(editLines) > 0 {
			result[file] = provenance.FileAttribution{EditLines: editLines}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
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
