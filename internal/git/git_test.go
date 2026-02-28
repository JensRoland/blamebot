package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlameForLine(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5\n"
	dir := setupGitRepo(t, "test.txt", content)

	info, err := BlameForLine(dir, "test.txt", 3)
	if err != nil {
		t.Fatalf("BlameForLine returned error: %v", err)
	}
	if info == nil {
		t.Fatal("BlameForLine returned nil")
	}
	if info.SHA == "" {
		t.Error("expected non-empty SHA")
	}
	if len(info.SHA) != 40 {
		t.Errorf("expected 40-char SHA, got %d chars: %s", len(info.SHA), info.SHA)
	}
	if info.Author == "" {
		t.Error("expected non-empty Author")
	}
	if info.Summary == "" {
		t.Error("expected non-empty Summary")
	}
}

func TestStagedJSONLFiles(t *testing.T) {
	// Create a git repo manually
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Create an initial commit so HEAD exists
	placeholder := filepath.Join(dir, "README")
	if err := os.WriteFile(placeholder, []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "initial commit")

	// Test with no staged JSONL files
	files, err := StagedJSONLFiles(dir)
	if err != nil {
		t.Fatalf("StagedJSONLFiles returned error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 staged files, got %d", len(files))
	}

	// Create and stage a JSONL file
	logDir := filepath.Join(dir, ".blamebot", "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonlFile := filepath.Join(logDir, "session.jsonl")
	if err := os.WriteFile(jsonlFile, []byte(`{"file":"a.go","ts":"2025-01-01T00:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".blamebot/log/session.jsonl")

	files, err = StagedJSONLFiles(dir)
	if err != nil {
		t.Fatalf("StagedJSONLFiles returned error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 staged file, got %d", len(files))
	}
	if files[0] != ".blamebot/log/session.jsonl" {
		t.Errorf("expected .blamebot/log/session.jsonl, got %s", files[0])
	}
}

func TestStageFile(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Create initial commit
	placeholder := filepath.Join(dir, "README")
	if err := os.WriteFile(placeholder, []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "initial commit")

	// Create a new file
	newFile := filepath.Join(dir, "newfile.txt")
	if err := os.WriteFile(newFile, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage it using StageFile
	if err := StageFile(dir, "newfile.txt"); err != nil {
		t.Fatalf("StageFile returned error: %v", err)
	}

	// Verify the file is staged
	status := run("status", "--porcelain")
	if !strings.Contains(status, "A  newfile.txt") {
		t.Errorf("expected newfile.txt to be staged, got status: %s", status)
	}
}

func TestRevParseTopLevel(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	run("init")

	// RevParseTopLevel uses cwd, so we can't easily test it with an arbitrary dir.
	// Instead, just verify it runs without error and returns a non-empty string
	// (since the test itself runs inside a git repo).
	result, err := RevParseTopLevel()
	if err != nil {
		t.Fatalf("RevParseTopLevel returned error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestAuthor(t *testing.T) {
	// Author() uses exec.Command without Dir, running in cwd.
	// Just verify it runs without panicking and returns a string.
	result := Author()
	if result == "" {
		t.Error("Author returned empty string (expected at least 'unknown')")
	}
	// In CI or unconfigured environments, it may return "unknown" which is fine.
}
