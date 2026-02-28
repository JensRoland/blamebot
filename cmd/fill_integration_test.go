package cmd

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
)

// captureBothOutputs captures everything written to os.Stdout and os.Stderr during fn().
func captureBothOutputs(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	oldOut := os.Stdout
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wOut

	oldErr := os.Stderr
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wErr

	fn()

	wOut.Close()
	wErr.Close()
	outBytes, _ := io.ReadAll(rOut)
	errBytes, _ := io.ReadAll(rErr)
	os.Stdout = oldOut
	os.Stderr = oldErr

	return string(outBytes), string(errBytes)
}

// initGitRepo creates a git repo in the given directory with initial config.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "Test User"},
		{"git", "config", "user.email", "test@example.com"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to run %v: %v\n%s", args, err, out)
		}
	}
	// Create an initial commit so HEAD exists (needed for staging to work properly)
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "README.md")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// setupFillTestPaths creates .git/blamebot/pending/ dir and returns Paths.
func setupFillTestPaths(t *testing.T, dir string) project.Paths {
	t.Helper()
	cacheDir := filepath.Join(dir, ".git", "blamebot")
	pendingDir := filepath.Join(cacheDir, "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return project.Paths{
		Root:       dir,
		GitDir:     filepath.Join(dir, ".git"),
		PendingDir: pendingDir,
		CacheDir:   cacheDir,
		IndexDB:    filepath.Join(cacheDir, "index.db"),
	}
}

// writePendingEdit writes a PendingEdit as JSON to the pending dir.
func writePendingEdit(t *testing.T, pendingDir string, edit provenance.PendingEdit) {
	t.Helper()
	data, err := json.MarshalIndent(edit, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir, edit.ID+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCmdFillReasons_DryRun(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	// Create a fake transcript file in the temp dir so ExtractSessionPrompts
	// can find it (it will return nil since the file won't have valid entries,
	// but that's fine -- the fallback to record prompts will kick in).
	transcriptPath := filepath.Join(dir, "fake-transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write pending edits with trace fields pointing to the transcript.
	writePendingEdit(t, paths.PendingDir, provenance.PendingEdit{
		ID:     "edit-1",
		Ts:     "2025-01-01T00:00:00Z",
		File:   "src/main.go",
		Change: "added error handling",
		Prompt: "add error handling",
		Trace:  transcriptPath + "#tool-1",
		Tool:   "Edit",
	})
	writePendingEdit(t, paths.PendingDir, provenance.PendingEdit{
		ID:     "edit-2",
		Ts:     "2025-01-01T00:01:00Z",
		File:   "src/handler.go",
		Change: "refactored handler",
		Prompt: "refactor handler",
		Trace:  transcriptPath + "#tool-2",
		Tool:   "Edit",
	})

	stdout, stderr := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, true)
	})

	// Verify stderr contains the count message
	if !strings.Contains(stderr, "Found 2 pending edit(s) to fill") {
		t.Errorf("stderr should contain 'Found 2 pending edit(s) to fill', got: %s", stderr)
	}

	// Verify stdout contains the transcript display
	if !strings.Contains(stdout, "Transcript:") {
		t.Errorf("stdout should contain 'Transcript:', got: %s", stdout)
	}

	// Verify stdout contains the prompt content (fallback from record prompts)
	if !strings.Contains(stdout, "add error handling") {
		t.Errorf("stdout should contain prompt 'add error handling', got: %s", stdout)
	}
	if !strings.Contains(stdout, "refactor handler") {
		t.Errorf("stdout should contain prompt 'refactor handler', got: %s", stdout)
	}

	// Verify stdout contains the edit details
	if !strings.Contains(stdout, "src/main.go") {
		t.Errorf("stdout should contain file 'src/main.go', got: %s", stdout)
	}
	if !strings.Contains(stdout, "src/handler.go") {
		t.Errorf("stdout should contain file 'src/handler.go', got: %s", stdout)
	}
	if !strings.Contains(stdout, "added error handling") {
		t.Errorf("stdout should contain change 'added error handling', got: %s", stdout)
	}
}

func TestCmdFillReasons_NoPendingEdits(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	// Don't write any pending edits -- just create the directories
	_, stderr := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, false)
	})

	if !strings.Contains(stderr, "No pending edits found") {
		t.Errorf("stderr should contain 'No pending edits found', got: %s", stderr)
	}
}

func TestCmdFillReasons_DryRun_NoTrace(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	transcriptPath := filepath.Join(dir, "fake-transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write pending edits with different scenarios:
	// 1. Edit with no trace (exercises transcriptPath=="" skip)
	writePendingEdit(t, paths.PendingDir, provenance.PendingEdit{
		ID:     "edit-a",
		Ts:     "2025-01-01T00:00:00Z",
		File:   "a.go",
		Change: "untraceable",
		Prompt: "test",
	})
	// 2. Edit with valid trace (should appear in output)
	writePendingEdit(t, paths.PendingDir, provenance.PendingEdit{
		ID:     "edit-b",
		Ts:     "2025-01-01T00:00:00Z",
		File:   "b.go",
		Change: "traceable edit",
		Prompt: "test trace",
		Trace:  transcriptPath + "#tool-3",
		Tool:   "Edit",
	})

	stdout, stderr := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, true)
	})

	// Should count all pending edits
	if !strings.Contains(stderr, "Found 2 pending edit(s) to fill") {
		t.Errorf("stderr should mention 2 pending edits, got: %s", stderr)
	}

	// The traceable edit should appear in the output
	if !strings.Contains(stdout, "b.go") {
		t.Errorf("stdout should contain file 'b.go' (traceable edit), got: %s", stdout)
	}
}

func TestCmdFillReasons_DryRun_LongTranscriptPath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	// Create a transcript path longer than 60 chars to exercise truncation
	longPath := filepath.Join(dir, "a-very-long-path-that-exceeds-sixty-characters-for-sure-yes-it-does.jsonl")
	if err := os.WriteFile(longPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	writePendingEdit(t, paths.PendingDir, provenance.PendingEdit{
		ID:     "edit-x",
		Ts:     "2025-01-01T00:00:00Z",
		File:   "x.go",
		Change: "test",
		Prompt: "test",
		Trace:  longPath + "#tool-1",
		Tool:   "Edit",
	})

	stdout, _ := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, true)
	})

	// Long path should be truncated with "..." prefix
	if !strings.Contains(stdout, "...") {
		t.Errorf("stdout should contain '...' for truncated transcript path, got: %s", stdout)
	}
}
