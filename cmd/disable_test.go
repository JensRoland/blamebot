package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/project"
)

func TestCmdDisable_FullCleanup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .blamebot/log/ with a session file
	logDir := filepath.Join(tmpDir, ".blamebot", "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "session.jsonl"), []byte(`{"file":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create .git/blamebot/ with an index.db file
	gitBlamebotDir := filepath.Join(tmpDir, ".git", "blamebot")
	if err := os.MkdirAll(gitBlamebotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitBlamebotDir, "index.db"), []byte("fake db"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create .git/hooks/pre-commit with blamebot marker only
	hooksDir := filepath.Join(tmpDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preCommitContent := "#!/usr/bin/env bash\n\n# blamebot: fill reasons\nif command -v git-blamebot >/dev/null 2>&1; then\ngit-blamebot --fill-reasons\nfi\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte(preCommitContent), 0o755); err != nil {
		t.Fatal(err)
	}

	paths := project.Paths{
		Root:       tmpDir,
		GitDir:     filepath.Join(tmpDir, ".git"),
		PendingDir: filepath.Join(gitBlamebotDir, "pending"),
		CacheDir:   gitBlamebotDir,
		IndexDB:    filepath.Join(gitBlamebotDir, "index.db"),
	}

	out := captureStdout(t, func() {
		cmdDisable(paths, tmpDir)
	})

	// Verify stdout messages
	if !strings.Contains(out, "Removed .blamebot/") {
		t.Errorf("expected output to contain 'Removed .blamebot/', got: %s", out)
	}
	if !strings.Contains(out, "Removed .git/blamebot/") {
		t.Errorf("expected output to contain 'Removed .git/blamebot/', got: %s", out)
	}
	if !strings.Contains(out, "Removed .git/hooks/pre-commit") {
		t.Errorf("expected output to contain 'Removed .git/hooks/pre-commit', got: %s", out)
	}

	// Verify .blamebot/ was deleted
	if _, err := os.Stat(filepath.Join(tmpDir, ".blamebot")); !os.IsNotExist(err) {
		t.Error(".blamebot/ directory should have been deleted")
	}

	// Verify .git/blamebot/ was deleted
	if _, err := os.Stat(gitBlamebotDir); !os.IsNotExist(err) {
		t.Error(".git/blamebot/ directory should have been deleted")
	}
}

func TestCmdDisable_PreCommitHookCleaned(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .blamebot/ so the "not initialized" path is not hit
	if err := os.MkdirAll(filepath.Join(tmpDir, ".blamebot", "log"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create pre-commit hook with BOTH blamebot content AND other custom content
	hooksDir := filepath.Join(tmpDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preCommitContent := "#!/usr/bin/env bash\n\n# Run linter\nnpx eslint .\n\n# blamebot: fill reasons\nif command -v git-blamebot >/dev/null 2>&1; then\ngit-blamebot --fill-reasons\nfi\n"
	preCommitPath := filepath.Join(hooksDir, "pre-commit")
	if err := os.WriteFile(preCommitPath, []byte(preCommitContent), 0o755); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	paths := project.Paths{
		Root:       tmpDir,
		GitDir:     filepath.Join(tmpDir, ".git"),
		PendingDir: filepath.Join(cacheDir, "pending"),
		CacheDir:   cacheDir,
		IndexDB:    filepath.Join(cacheDir, "index.db"),
	}

	out := captureStdout(t, func() {
		cmdDisable(paths, tmpDir)
	})

	// Verify stdout contains "cleaned"
	if !strings.Contains(out, "cleaned") {
		t.Errorf("expected output to contain 'cleaned', got: %s", out)
	}

	// Verify pre-commit file still exists
	if _, err := os.Stat(preCommitPath); os.IsNotExist(err) {
		t.Error("pre-commit hook should still exist (has non-blamebot content)")
	}

	// Verify blamebot lines were removed but other content remains
	data, err := os.ReadFile(preCommitPath)
	if err != nil {
		t.Fatal(err)
	}
	remaining := string(data)
	if strings.Contains(remaining, "blamebot") {
		t.Errorf("pre-commit should not contain blamebot references, got: %s", remaining)
	}
	if !strings.Contains(remaining, "npx eslint") {
		t.Errorf("pre-commit should still contain linter command, got: %s", remaining)
	}
}

func TestCmdDisable_NotInitialized(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .git/ but NOT .blamebot/ or .git/blamebot/
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	paths := project.Paths{
		Root:       tmpDir,
		GitDir:     filepath.Join(tmpDir, ".git"),
		PendingDir: filepath.Join(cacheDir, "pending"),
		CacheDir:   cacheDir,
		IndexDB:    filepath.Join(cacheDir, "index.db"),
	}

	out := captureStdout(t, func() {
		cmdDisable(paths, tmpDir)
	})

	if !strings.Contains(out, "blamebot is not initialized") {
		t.Errorf("expected output to contain 'blamebot is not initialized', got: %s", out)
	}
}
