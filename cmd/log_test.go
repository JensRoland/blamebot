package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/project"
)

func setupLogPaths(t *testing.T) project.Paths {
	t.Helper()
	tmpDir := t.TempDir()

	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	logsDir := filepath.Join(cacheDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	return project.Paths{
		Root:     tmpDir,
		GitDir:   filepath.Join(tmpDir, ".git"),
		CacheDir: cacheDir,
		IndexDB:  filepath.Join(cacheDir, "index.db"),
	}
}

func TestCmdLog(t *testing.T) {
	paths := setupLogPaths(t)

	logsDir := filepath.Join(paths.CacheDir, "logs")
	logContent := "2025-01-01 10:00:00 [INFO] Hook fired for session sess-aaa\n2025-01-01 10:00:01 [INFO] Prompt captured successfully\n"
	if err := os.WriteFile(filepath.Join(logsDir, "capture_prompt.log"), []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		cmdLog(paths, false)
	})

	if !strings.Contains(out, "Hook fired for session sess-aaa") {
		t.Errorf("output should contain log content, got: %s", out)
	}
	if !strings.Contains(out, "Prompt captured successfully") {
		t.Errorf("output should contain log content, got: %s", out)
	}
}

func TestCmdLog_MissingFile(t *testing.T) {
	paths := setupLogPaths(t)

	out := captureStdout(t, func() {
		cmdLog(paths, false)
	})

	if !strings.Contains(out, "No log file") {
		t.Errorf("output should contain 'No log file', got: %s", out)
	}
}

func TestCmdDumpPayload(t *testing.T) {
	paths := setupLogPaths(t)

	logsDir := filepath.Join(paths.CacheDir, "logs")
	separator := strings.Repeat("=", 60)
	payloadContent := "Entry 1: payload data here" + "\n" + separator + "\n" + "Entry 2: more payload data" + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "hook.log"), []byte(payloadContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		cmdDumpPayload(paths)
	})

	if !strings.Contains(out, "payload data") {
		t.Errorf("output should contain payload entries, got: %s", out)
	}
}
