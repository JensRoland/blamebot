package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunEnable handles the "enable" subcommand.
func RunEnable(args []string) {
	fs := flag.NewFlagSet("enable", flag.ExitOnError)
	global := fs.Bool("global", false, "Also configure Claude Code hooks globally")
	fs.Parse(args)

	if *global {
		enableGlobal()
	}

	enableRepo()
}

func enableGlobal() {
	fmt.Println("Installing blamebot globally...")

	// Find the binary path
	binaryPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not determine binary path: %v\n", err)
		os.Exit(1)
	}

	// Configure Claude Code hooks
	settingsFile := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	_ = os.MkdirAll(filepath.Dir(settingsFile), 0o755)

	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsFile); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = map[string]interface{}{}
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	hookCmd := binaryPath + " hook post-tool-use"
	captureCmd := binaryPath + " hook prompt-submit"

	// PostToolUse — replace blamebot entries
	postTool := filterHookEntries(hooks, "PostToolUse", "git-blamebot")
	postTool = append(postTool, map[string]interface{}{
		"matcher": "Edit|Write|MultiEdit",
		"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": hookCmd}},
	})
	hooks["PostToolUse"] = postTool

	// UserPromptSubmit — replace blamebot entries
	userPrompt := filterHookEntries(hooks, "UserPromptSubmit", "git-blamebot")
	userPrompt = append(userPrompt, map[string]interface{}{
		"hooks": []interface{}{map[string]interface{}{"type": "command", "command": captureCmd}},
	})
	hooks["UserPromptSubmit"] = userPrompt

	settings["hooks"] = hooks

	b, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsFile, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing settings: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  \u2713 Claude Code hooks configured in %s\n", settingsFile)
}

func filterHookEntries(hooks map[string]interface{}, key, exclude string) []interface{} {
	existing, _ := hooks[key].([]interface{})
	var filtered []interface{}
	for _, entry := range existing {
		e, ok := entry.(map[string]interface{})
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		hooksList, _ := e["hooks"].([]interface{})
		hasExcluded := false
		for _, h := range hooksList {
			hm, ok := h.(map[string]interface{})
			if ok {
				cmd, _ := hm["command"].(string)
				if strings.Contains(cmd, exclude) {
					hasExcluded = true
					break
				}
			}
		}
		if !hasExcluded {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func enableRepo() {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: not inside a git repository")
		os.Exit(1)
	}
	projDir := strings.TrimSpace(string(out))

	fmt.Printf("Initializing blamebot in %s\n", projDir)

	// 1. Create .blamebot/log/
	logDir := filepath.Join(projDir, ".blamebot", "log")
	_ = os.MkdirAll(logDir, 0o755)

	// .gitattributes for clean merging
	gitattr := filepath.Join(projDir, ".blamebot", ".gitattributes")
	if _, err := os.Stat(gitattr); os.IsNotExist(err) {
		_ = os.WriteFile(gitattr, []byte("*.jsonl merge=union\n"), 0o644)
		fmt.Println("  \u2713 Created .blamebot/ with merge=union strategy")
	} else {
		fmt.Println("  \u2713 .blamebot/ already exists")
	}

	// README
	readme := filepath.Join(projDir, ".blamebot", "README")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		_ = os.WriteFile(readme, []byte(`This directory is maintained by blamebot.
It tracks the prompts and reasoning behind AI-authored code edits.
See: https://github.com/jensroland/git-blamebot

JSONL files in log/ are append-only and merge cleanly across branches.
Do not edit these files manually.
`), 0o644)
	}

	// 2. Local cache
	cacheDir := filepath.Join(projDir, ".git", "blamebot", "logs")
	_ = os.MkdirAll(cacheDir, 0o755)
	fmt.Println("  \u2713 Local cache at .git/blamebot/")

	// 3. Pre-commit hook
	hookDir := filepath.Join(projDir, ".git", "hooks")
	preCommit := filepath.Join(hookDir, "pre-commit")
	fillMarker := "# blamebot: fill reasons"

	if data, err := os.ReadFile(preCommit); err == nil && strings.Contains(string(data), fillMarker) {
		fmt.Println("  \u2713 Pre-commit hook already installed")
	} else {
		_ = os.MkdirAll(hookDir, 0o755)
		hookContent := `
# blamebot: fill reasons
# Auto-generate reasons for AI edits using Claude Haiku
if git diff --cached --name-only -- '.blamebot/log/*.jsonl' | grep -q .; then
    git-blamebot --fill-reasons
fi
`
		if _, err := os.Stat(preCommit); err == nil {
			// Append to existing hook
			f, err := os.OpenFile(preCommit, os.O_APPEND|os.O_WRONLY, 0o755)
			if err == nil {
				f.WriteString(hookContent)
				f.Close()
				fmt.Println("  \u2713 Appended to existing pre-commit hook")
			}
		} else {
			_ = os.WriteFile(preCommit, []byte("#!/usr/bin/env bash\n"+hookContent), 0o755)
			fmt.Println("  \u2713 Installed pre-commit hook")
		}
	}

	fmt.Println()
	fmt.Println("  Ready! Commit .blamebot/ to share reasoning with your team:")
	fmt.Println("    git add .blamebot && git commit -m 'Initialize blamebot tracking'")
}
