package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
)

// RunDisable handles the "disable" subcommand.
func RunDisable(args []string) {
	root, err := project.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	paths := project.NewPaths(root)
	cmdDisable(paths, root)
}

func cmdDisable(paths project.Paths, projectRoot string) {
	var removed []string

	// 1. Delete provenance branch
	if provenance.BranchExists(projectRoot) {
		cmd := exec.Command("git", "branch", "-D", provenance.BranchName)
		cmd.Dir = projectRoot
		if err := cmd.Run(); err == nil {
			removed = append(removed, provenance.BranchName+" branch")
		}
	}

	// 2. Remove legacy .blamebot/ directory (if exists)
	blamebotDir := filepath.Join(projectRoot, ".blamebot")
	if info, err := os.Stat(blamebotDir); err == nil && info.IsDir() {
		_ = os.RemoveAll(blamebotDir)
		removed = append(removed, ".blamebot/")
	}

	// 3. Remove .git/blamebot/ (local cache, pending edits, index)
	if info, err := os.Stat(paths.CacheDir); err == nil && info.IsDir() {
		_ = os.RemoveAll(paths.CacheDir)
		removed = append(removed, ".git/blamebot/")
	}

	// 4. Clean git hooks
	for _, hookInfo := range []struct {
		name   string
		marker string
	}{
		{"commit-msg", "# blamebot: bundle provenance"},
		{"post-commit", "# blamebot: backfill commit SHA"},
		{"pre-push", "# blamebot: push provenance branch"},
		{"pre-commit", "# blamebot: fill reasons"}, // legacy
	} {
		cleanGitHook(paths.GitDir, hookInfo.name, hookInfo.marker, &removed)
	}

	if len(removed) > 0 {
		for _, item := range removed {
			fmt.Printf("  Removed %s\n", item)
		}
		fmt.Println()
		fmt.Println("blamebot tracking removed from this repo.")
		fmt.Println("Note: the global CLI and hooks are still installed.")
		fmt.Println("Run 'git-blamebot enable' to re-initialize.")
	} else {
		fmt.Println("blamebot is not initialized in this repo.")
	}
}

// cleanGitHook removes the blamebot section from a git hook file.
func cleanGitHook(gitDir, hookName, marker string, removed *[]string) {
	hookFile := filepath.Join(gitDir, "hooks", hookName)
	data, err := os.ReadFile(hookFile)
	if err != nil {
		return
	}
	content := string(data)
	if !strings.Contains(content, marker) {
		return
	}

	lines := strings.Split(content, "\n")
	var cleaned []string
	skip := false
	for _, line := range lines {
		if strings.Contains(line, marker) {
			skip = true
			// Remove preceding blank line
			if len(cleaned) > 0 && strings.TrimSpace(cleaned[len(cleaned)-1]) == "" {
				cleaned = cleaned[:len(cleaned)-1]
			}
			continue
		}
		if skip {
			stripped := strings.TrimSpace(line)
			// Skip the command line(s) following the marker
			if strings.HasPrefix(stripped, "git-blamebot ") ||
				strings.HasPrefix(stripped, "#") ||
				strings.HasPrefix(stripped, "if ") ||
				stripped == "fi" {
				continue
			}
			skip = false
		}
		cleaned = append(cleaned, line)
	}

	remaining := strings.TrimSpace(strings.Join(cleaned, "\n"))
	if remaining == "" || remaining == "#!/usr/bin/env bash" {
		_ = os.Remove(hookFile)
		*removed = append(*removed, fmt.Sprintf(".git/hooks/%s (deleted)", hookName))
	} else {
		_ = os.WriteFile(hookFile, []byte(strings.Join(cleaned, "\n")), 0o755)
		*removed = append(*removed, fmt.Sprintf(".git/hooks/%s (cleaned)", hookName))
	}
}
