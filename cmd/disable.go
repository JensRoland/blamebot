package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jensroland/git-blamebot/internal/project"
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

	// 1. Remove .blamebot/ (committed tracking directory)
	blamebotDir := filepath.Join(projectRoot, ".blamebot")
	if info, err := os.Stat(blamebotDir); err == nil && info.IsDir() {
		_ = os.RemoveAll(blamebotDir)
		removed = append(removed, ".blamebot/")
	}

	// 2. Remove .git/blamebot/ (local cache + index)
	if info, err := os.Stat(paths.CacheDir); err == nil && info.IsDir() {
		_ = os.RemoveAll(paths.CacheDir)
		removed = append(removed, ".git/blamebot/")
	}

	// 3. Clean pre-commit hook
	preCommit := filepath.Join(projectRoot, ".git", "hooks", "pre-commit")
	if data, err := os.ReadFile(preCommit); err == nil {
		content := string(data)
		marker := "# blamebot: fill reasons"
		if strings.Contains(content, marker) {
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
					if strings.HasPrefix(stripped, "#") ||
						strings.HasPrefix(stripped, "if ") ||
						stripped == "git-blamebot --fill-reasons" ||
						stripped == "fi" {
						continue
					}
					skip = false
				}
				cleaned = append(cleaned, line)
			}

			remaining := strings.TrimSpace(strings.Join(cleaned, "\n"))
			if remaining == "" || remaining == "#!/usr/bin/env bash" {
				_ = os.Remove(preCommit)
				removed = append(removed, ".git/hooks/pre-commit (deleted)")
			} else {
				_ = os.WriteFile(preCommit, []byte(strings.Join(cleaned, "\n")), 0o755)
				removed = append(removed, ".git/hooks/pre-commit (cleaned)")
			}
		}
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
