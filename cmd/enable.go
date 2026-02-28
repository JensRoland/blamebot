package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
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
	paths := project.NewPaths(projDir)

	fmt.Printf("Initializing blamebot in %s\n", projDir)

	// 1. Initialize provenance branch
	if err := provenance.InitBranch(projDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating provenance branch: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  \u2713 Provenance branch '%s' initialized\n", provenance.BranchName)

	// 2. Local cache directories
	_ = os.MkdirAll(paths.PendingDir, 0o755)
	_ = os.MkdirAll(filepath.Join(paths.CacheDir, "logs"), 0o755)
	fmt.Println("  \u2713 Local cache at .git/blamebot/")

	// 3. Install git hooks
	installGitHook(paths.GitDir, "commit-msg",
		"# blamebot: bundle provenance",
		`git-blamebot hook commit-msg "$1"`)

	installGitHook(paths.GitDir, "post-commit",
		"# blamebot: backfill commit SHA",
		"git-blamebot hook post-commit")

	installGitHook(paths.GitDir, "pre-push",
		"# blamebot: push provenance branch",
		"git-blamebot hook pre-push")

	// 4. Try to fetch provenance branch from remote (if it exists)
	cmd := exec.Command("git", "fetch", "origin", provenance.BranchName)
	cmd.Dir = projDir
	_ = cmd.Run() // ignore errors — remote may not have the branch

	fmt.Println()
	fmt.Println("  Ready! Provenance data will be stored on the")
	fmt.Printf("  '%s' branch automatically.\n", provenance.BranchName)
}

// installGitHook installs or appends a blamebot section to a git hook script.
func installGitHook(gitDir, hookName, marker, command string) {
	hookDir := filepath.Join(gitDir, "hooks")
	hookFile := filepath.Join(hookDir, hookName)

	if data, err := os.ReadFile(hookFile); err == nil && strings.Contains(string(data), marker) {
		fmt.Printf("  \u2713 %s hook already installed\n", hookName)
		return
	}

	_ = os.MkdirAll(hookDir, 0o755)
	hookContent := fmt.Sprintf("\n%s\n%s\n", marker, command)

	if _, err := os.Stat(hookFile); err == nil {
		// Append to existing hook
		f, err := os.OpenFile(hookFile, os.O_APPEND|os.O_WRONLY, 0o755)
		if err == nil {
			f.WriteString(hookContent)
			f.Close()
			fmt.Printf("  \u2713 Appended to existing %s hook\n", hookName)
		}
	} else {
		_ = os.WriteFile(hookFile, []byte("#!/usr/bin/env bash\n"+hookContent), 0o755)
		fmt.Printf("  \u2713 Installed %s hook\n", hookName)
	}
}
