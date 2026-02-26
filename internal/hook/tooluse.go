package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/record"
)

// editInfo holds extracted edit details from a tool payload.
type editInfo struct {
	File        string
	LineStart   *int
	LineEnd     *int
	ContentHash string
	Change      string
}

// HandlePostToolUse processes a PostToolUse hook payload from stdin.
func HandlePostToolUse(r io.Reader) error {
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
		debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("Failed to read stdin: %v", err), nil)
		return nil
	}

	debug.Log(paths.CacheDir, "hook.log", "Raw stdin received", map[string]interface{}{
		"raw_length":  len(raw),
		"raw_preview": string(raw[:min(len(raw), 3000)]),
	})

	var data map[string]interface{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &data); err != nil {
			debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("Failed to parse JSON: %v", err), nil)
			return nil
		}
	} else {
		return nil
	}

	debug.Log(paths.CacheDir, "hook.log", "Parsed payload", map[string]interface{}{
		"tool_name": getString(data, "tool_name"),
	})

	// Load stashed prompt state
	stateFile := filepath.Join(paths.CacheDir, "current_prompt.json")
	var ps promptState
	if b, err := os.ReadFile(stateFile); err == nil {
		_ = json.Unmarshal(b, &ps)
	}
	debug.Log(paths.CacheDir, "hook.log", "Loaded prompt state", ps)

	// Determine session file
	sessionFilename := ps.SessionFile
	if sessionFilename == "" {
		now := time.Now().UTC()
		ts := now.Format("20060102T150405Z")
		randStr := randomString(6)
		sessionFilename = fmt.Sprintf("%s-%s-orphan.jsonl", ts, randStr)
		debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("No session file in prompt state, fallback: %s", sessionFilename), nil)
	}

	sessionPath := filepath.Join(paths.LogDir, sessionFilename)

	// Extract edit records
	edits := extractEdits(data, root)
	debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("Extracted %d edit(s)", len(edits)), edits)

	// Trace reference
	transcriptPath := getString(data, "transcript_path")
	if transcriptPath == "" {
		transcriptPath = ps.TranscriptPath
	}
	toolUseID := getString(data, "tool_use_id")
	traceRef := transcriptPath
	if toolUseID != "" {
		traceRef = traceRef + "#" + toolUseID
	}

	// Session ID
	sessionID := getString(data, "session_id")
	if sessionID == "" {
		sessionID = ps.SessionID
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	toolName := getString(data, "tool_name")
	author := ps.Author
	if author == "" {
		author = "unknown"
	}

	_ = os.MkdirAll(paths.LogDir, 0o755)

	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("Failed to open session file: %v", err), nil)
		return nil
	}
	defer f.Close()

	recordsWritten := 0
	for _, edit := range edits {
		rec := record.Record{
			Ts:          now,
			File:        edit.File,
			ContentHash: edit.ContentHash,
			Prompt:      ps.Prompt,
			Reason:      "",
			Change:      edit.Change,
			Tool:        toolName,
			Author:      author,
			Session:     sessionID,
			Trace:       traceRef,
		}
		if edit.LineStart != nil {
			rec.Lines[0] = record.NewInt(*edit.LineStart)
		}
		if edit.LineEnd != nil {
			rec.Lines[1] = record.NewInt(*edit.LineEnd)
		}

		b, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		fmt.Fprintf(f, "%s\n", b)
		recordsWritten++
	}

	debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("Wrote %d record(s) to %s", recordsWritten, sessionPath), nil)
	return nil
}

// extractEdits extracts file edit details from the Claude Code payload.
func extractEdits(data map[string]interface{}, projectDir string) []editInfo {
	toolName := getString(data, "tool_name")
	toolInput := getMap(data, "tool_input")
	toolResponse := getMap(data, "tool_response")

	filePath := getString(toolInput, "file_path")
	if filePath == "" {
		filePath = getString(toolInput, "path")
	}
	filePath = record.RelativizePath(filePath, projectDir)

	switch toolName {
	case "Edit":
		oldStr := getString(toolInput, "old_string")
		newStr := getString(toolInput, "new_string")
		lineStart, lineEnd := extractLineNumbers(toolResponse)
		return []editInfo{{
			File:        filePath,
			LineStart:   lineStart,
			LineEnd:     lineEnd,
			ContentHash: record.ContentHash(newStr),
			Change:      record.CompactChangeSummary(oldStr, newStr),
		}}

	case "Write":
		content := getString(toolInput, "content")
		if content == "" {
			content = getString(toolInput, "file_text")
		}
		var nLines *int
		if content != "" {
			n := strings.Count(content, "\n") + 1
			nLines = &n
		}
		one := 1
		change := "created file"
		if nLines != nil {
			change = fmt.Sprintf("created file (%d lines)", *nLines)
		}
		return []editInfo{{
			File:        filePath,
			LineStart:   &one,
			LineEnd:     nLines,
			ContentHash: record.ContentHash(content[:min(len(content), 500)]),
			Change:      change,
		}}

	case "MultiEdit":
		subEdits := getArray(toolInput, "edits")
		if subEdits == nil {
			subEdits = getArray(toolInput, "changes")
		}
		patches := getArray(toolResponse, "structuredPatch")

		var edits []editInfo
		for i, editRaw := range subEdits {
			edit, ok := editRaw.(map[string]interface{})
			if !ok {
				continue
			}
			newStr := getString(edit, "new_string")
			oldStr := getString(edit, "old_string")

			var start, end *int
			if i < len(patches) {
				p, ok := patches[i].(map[string]interface{})
				if ok {
					if s, ok := getIntPtr(p, "newStart"); ok {
						start = &s
						nLines := getIntOr(p, "newLines", 1)
						e := s + max(nLines-1, 0)
						end = &e
					}
				}
			}

			editFilePath := getString(edit, "file_path")
			if editFilePath != "" {
				editFilePath = record.RelativizePath(editFilePath, projectDir)
			} else {
				editFilePath = filePath
			}

			edits = append(edits, editInfo{
				File:        editFilePath,
				LineStart:   start,
				LineEnd:     end,
				ContentHash: record.ContentHash(newStr),
				Change:      record.CompactChangeSummary(oldStr, newStr),
			})
		}
		return edits

	default:
		f := filePath
		if f == "" {
			f = fmt.Sprintf("unknown:%s", toolName)
		}
		return []editInfo{{
			File:   f,
			Change: fmt.Sprintf("unknown tool: %s", toolName),
		}}
	}
}

// extractLineNumbers reads line numbers from structuredPatch.
func extractLineNumbers(toolResponse map[string]interface{}) (*int, *int) {
	patches := getArray(toolResponse, "structuredPatch")
	if len(patches) == 0 {
		return nil, nil
	}

	patch, ok := patches[0].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	start, ok := getIntPtr(patch, "newStart")
	if !ok {
		return nil, nil
	}

	nLines := getIntOr(patch, "newLines", 0)
	end := start + max(nLines-1, 0)
	return &start, &end
}

// Helper functions for safe map access.

func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	sub, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return sub
}

func getArray(m map[string]interface{}, key string) []interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return arr
}

func getIntPtr(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func getIntOr(m map[string]interface{}, key string, def int) int {
	v, ok := getIntPtr(m, key)
	if !ok {
		return def
	}
	return v
}
