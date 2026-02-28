package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jensroland/git-blamebot/internal/checkpoint"
	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/record"
)

// HandlePreToolUse processes a PreToolUse hook payload from stdin.
// It snapshots the file content before an edit tool executes.
func HandlePreToolUse(r io.Reader) error {
	root, err := project.FindRoot()
	if err != nil {
		return err
	}

	if !project.IsInitialized(root) {
		return nil
	}

	paths := project.NewPaths(root)

	raw, err := io.ReadAll(r)
	if err != nil {
		debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("PreToolUse: failed to read stdin: %v", err), nil)
		return nil
	}

	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("PreToolUse: failed to parse JSON: %v", err), nil)
		return nil
	}

	toolName := getString(data, "tool_name")
	toolUseID := getString(data, "tool_use_id")
	toolInput := getMap(data, "tool_input")

	debug.Log(paths.CacheDir, "hook.log", "PreToolUse payload", map[string]interface{}{
		"tool_name":   toolName,
		"tool_use_id": toolUseID,
	})

	// Extract file paths from tool input
	filePaths := extractPreEditFilePaths(toolName, toolInput, root)
	if len(filePaths) == 0 {
		return nil
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	for _, relFile := range filePaths {
		absFile := filepath.Join(root, relFile)
		content, err := os.ReadFile(absFile)
		if err != nil {
			// File doesn't exist yet (new file) — use empty content
			content = nil
		}

		contentSHA, err := checkpoint.WriteBlob(paths.CheckpointDir, string(content))
		if err != nil {
			debug.Log(paths.CacheDir, "hook.log",
				fmt.Sprintf("PreToolUse: failed to write blob for %s: %v", relFile, err), nil)
			continue
		}

		_, err = checkpoint.WriteCheckpoint(paths.CheckpointDir, checkpoint.Checkpoint{
			Kind:       "pre-edit",
			File:       relFile,
			ContentSHA: contentSHA,
			ToolUseID:  toolUseID,
			Ts:         now,
		})
		if err != nil {
			debug.Log(paths.CacheDir, "hook.log",
				fmt.Sprintf("PreToolUse: failed to write checkpoint for %s: %v", relFile, err), nil)
			continue
		}

		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("PreToolUse: checkpoint for %s (sha=%s)", relFile, contentSHA[:12]), nil)
	}

	return nil
}

// extractPreEditFilePaths extracts file paths from the tool input for Edit/Write/MultiEdit.
func extractPreEditFilePaths(toolName string, toolInput map[string]interface{}, projectDir string) []string {
	switch toolName {
	case "Edit", "Write":
		filePath := getString(toolInput, "file_path")
		if filePath == "" {
			filePath = getString(toolInput, "path")
		}
		if filePath == "" {
			return nil
		}
		return []string{record.RelativizePath(filePath, projectDir)}

	case "MultiEdit":
		subEdits := getArray(toolInput, "edits")
		if subEdits == nil {
			subEdits = getArray(toolInput, "changes")
		}

		seen := map[string]bool{}
		var paths []string

		// Check top-level file_path
		topFile := getString(toolInput, "file_path")
		if topFile == "" {
			topFile = getString(toolInput, "path")
		}

		for _, editRaw := range subEdits {
			edit, ok := editRaw.(map[string]interface{})
			if !ok {
				continue
			}
			editFile := getString(edit, "file_path")
			if editFile == "" {
				editFile = topFile
			}
			if editFile == "" {
				continue
			}
			rel := record.RelativizePath(editFile, projectDir)
			if !seen[rel] {
				seen[rel] = true
				paths = append(paths, rel)
			}
		}
		return paths

	default:
		return nil
	}
}
