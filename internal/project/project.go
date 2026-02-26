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
	LogDir    string // .blamebot/log/
	TracesDir string // .blamebot/traces/
	CacheDir  string // .git/blamebot/
	IndexDB   string // .git/blamebot/index.db
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
	return Paths{
		Root:      root,
		LogDir:    filepath.Join(root, ".blamebot", "log"),
		TracesDir: filepath.Join(root, ".blamebot", "traces"),
		CacheDir:  filepath.Join(root, ".git", "blamebot"),
		IndexDB:   filepath.Join(root, ".git", "blamebot", "index.db"),
	}
}

// IsInitialized returns true if .blamebot/ directory exists.
func IsInitialized(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".blamebot"))
	return err == nil && info.IsDir()
}
