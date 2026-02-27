package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Paths holds all relevant directories for a blamebot-enabled repo.
type Paths struct {
	Root      string // git repo root
	GitDir    string // actual .git directory (resolved for worktrees)
	LogDir    string // .blamebot/log/
	TracesDir string // .blamebot/traces/
	CacheDir  string // <gitdir>/blamebot/
	IndexDB   string // <gitdir>/blamebot/index.db
}

// FindRoot returns the git project root, preferring CLAUDE_PROJECT_DIR if set.
func FindRoot() (string, error) {
	if dir := os.Getenv("CLAUDE_PROJECT_DIR"); dir != "" {
		return dir, nil
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// NewPaths constructs all path constants from a project root.
func NewPaths(root string) Paths {
	gitDir := resolveGitDir(root)
	return Paths{
		Root:      root,
		GitDir:    gitDir,
		LogDir:    filepath.Join(root, ".blamebot", "log"),
		TracesDir: filepath.Join(root, ".blamebot", "traces"),
		CacheDir:  filepath.Join(gitDir, "blamebot"),
		IndexDB:   filepath.Join(gitDir, "blamebot", "index.db"),
	}
}

// resolveGitDir returns the actual .git directory, handling worktrees
// where .git is a file containing "gitdir: <path>".
func resolveGitDir(root string) string {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return dotGit
	}
	if info.IsDir() {
		return dotGit
	}
	// .git is a file (worktree) — read the gitdir pointer
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return dotGit
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return dotGit
	}
	gitdir := strings.TrimPrefix(content, "gitdir: ")
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(root, gitdir)
	}
	return gitdir
}

// IsInitialized returns true if .blamebot/ directory exists.
func IsInitialized(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".blamebot"))
	return err == nil && info.IsDir()
}
