package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/project"
)

var ideTagRe = regexp.MustCompile(`(?s)<ide_\w+>.*?</ide_\w+>\s*`)

// cleanPrompt strips IDE metadata tags from the prompt.
func cleanPrompt(raw string) string {
	cleaned := ideTagRe.ReplaceAllString(raw, "")
	return strings.TrimSpace(cleaned)
}

// promptState is written to current_prompt.json for the PostToolUse hook.
type promptState struct {
	Prompt         string `json:"prompt"`
	PromptRaw      string `json:"prompt_raw"`
	Timestamp      string `json:"timestamp"`
	Author         string `json:"author"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

// HandlePromptSubmit processes a UserPromptSubmit hook payload from stdin.
func HandlePromptSubmit(r io.Reader) error {
	root, err := project.FindRoot()
	if err != nil {
		return err
	}

	if !project.IsInitialized(root) {
		return nil // not initialized, exit silently
	}

	paths := project.NewPaths(root)

	raw, err := io.ReadAll(r)
	if err != nil {
		debug.Log(paths.CacheDir, "capture_prompt.log", fmt.Sprintf("Failed to read stdin: %v", err), nil)
		return nil
	}

	debug.Log(paths.CacheDir, "capture_prompt.log", "Raw stdin received", map[string]interface{}{
		"raw_length":  len(raw),
		"raw_preview": string(raw[:min(len(raw), 2000)]),
	})

	var data map[string]interface{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &data); err != nil {
			debug.Log(paths.CacheDir, "capture_prompt.log", fmt.Sprintf("Failed to parse JSON: %v", err), nil)
			data = map[string]interface{}{}
		}
	} else {
		data = map[string]interface{}{}
	}

	debug.Log(paths.CacheDir, "capture_prompt.log", "Parsed payload", data)

	rawPrompt, _ := data["prompt"].(string)
	prompt := cleanPrompt(rawPrompt)

	sessionID, _ := data["session_id"].(string)
	transcriptPath, _ := data["transcript_path"].(string)

	now := time.Now().UTC()

	state := promptState{
		Prompt:         prompt,
		PromptRaw:      rawPrompt,
		Timestamp:      now.Format("2006-01-02T15:04:05Z"),
		Author:         git.Author(),
		SessionID:      sessionID,
		TranscriptPath: transcriptPath,
	}

	_ = os.MkdirAll(paths.CacheDir, 0o755)

	stateFile := filepath.Join(paths.CacheDir, "current_prompt.json")
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(stateFile, b, 0o644); err != nil {
		return err
	}

	debug.Log(paths.CacheDir, "capture_prompt.log", "Stashed prompt state", state)
	return nil
}
