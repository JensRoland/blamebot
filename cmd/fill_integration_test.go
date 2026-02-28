package cmd

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/project"
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

// setupFillTestPaths creates the .blamebot/log/ and .git/blamebot/ dirs and returns Paths.
func setupFillTestPaths(t *testing.T, dir string) project.Paths {
	t.Helper()
	logDir := filepath.Join(dir, ".blamebot", "log")
	cacheDir := filepath.Join(dir, ".git", "blamebot")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return project.Paths{
		Root:     dir,
		LogDir:   logDir,
		CacheDir: cacheDir,
		IndexDB:  filepath.Join(cacheDir, "index.db"),
	}
}

func TestCmdFillReasons_DryRun(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	// Create a fake transcript file in the temp dir so ExtractSessionPrompts
	// can find it (it will return nil since the file won't have valid entries,
	// but that's fine — the fallback to record prompts will kick in).
	transcriptPath := filepath.Join(dir, "fake-transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a session JSONL file with records that have empty reasons.
	// The trace field uses the transcript path so grouping works.
	records := strings.Join([]string{
		`{"file":"src/main.go","lines":"5-10","ts":"2025-01-01T00:00:00Z","change":"added error handling","prompt":"add error handling","trace":"` + transcriptPath + `#tool-1","tool":"Edit"}`,
		`{"file":"src/handler.go","lines":"20","ts":"2025-01-01T00:01:00Z","change":"refactored handler","prompt":"refactor handler","trace":"` + transcriptPath + `#tool-2","tool":"Edit"}`,
	}, "\n") + "\n"

	sessionFile := filepath.Join(paths.LogDir, "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte(records), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage the JSONL file
	cmd := exec.Command("git", "add", ".blamebot/log/session.jsonl")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	stdout, stderr := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, true)
	})

	// Verify stderr contains the count message
	if !strings.Contains(stderr, "Found 2 record(s) to fill") {
		t.Errorf("stderr should contain 'Found 2 record(s) to fill', got: %s", stderr)
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

func TestCmdFillReasons_NoStagedFiles(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	// Don't stage any JSONL files — just create the directories
	_, stderr := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, false)
	})

	if !strings.Contains(stderr, "No staged .blamebot/log/*.jsonl files found") {
		t.Errorf("stderr should contain 'No staged .blamebot/log/*.jsonl files found', got: %s", stderr)
	}
}

func TestCmdFillReasons_DryRun_EdgeCases(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	transcriptPath := filepath.Join(dir, "fake-transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mix of edge cases:
	// 1. Invalid JSON line (exercises nil record / continue at fill.go:56-58)
	// 2. Record with no trace (exercises transcriptPath=="" skip at fill.go:97-98)
	// 3. Record with legacy [start, end] array format (exercises fill.go:114-128)
	// 4. Record with reason already set (exercises reason != "" continue at fill.go:89-90)
	records := strings.Join([]string{
		`this is not valid json`,
		`{"file":"a.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"untraceable","prompt":"test"}`,
		`{"file":"b.go","lines":[5,10],"ts":"2025-01-01T00:00:00Z","change":"legacy lines","prompt":"legacy test","trace":"` + transcriptPath + `#tool-3","tool":"Edit"}`,
		`{"file":"c.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"already done","prompt":"done","reason":"Has reason","trace":"` + transcriptPath + `#tool-4","tool":"Edit"}`,
	}, "\n") + "\n"

	sessionFile := filepath.Join(paths.LogDir, "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte(records), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "add", ".blamebot/log/session.jsonl")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	stdout, stderr := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, true)
	})

	// Should count 2 records to fill: the untraceable one and the legacy one
	// (invalid JSON is skipped, reason-filled one is skipped)
	if !strings.Contains(stderr, "Found 2 record(s) to fill") {
		t.Errorf("stderr should mention 2 records to fill, got: %s", stderr)
	}

	// The legacy-line record should appear in the output (it has a valid trace)
	if !strings.Contains(stdout, "b.go") {
		t.Errorf("stdout should contain file 'b.go' (legacy lines), got: %s", stdout)
	}
	// Legacy lines should show L5-10 range
	if !strings.Contains(stdout, "L5-10") {
		t.Errorf("stdout should contain 'L5-10' for legacy line format, got: %s", stdout)
	}
}

func TestCmdFillReasons_DryRun_LongTranscriptPath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	// Create a transcript path longer than 60 chars to exercise truncation (fill.go:173-175)
	longPath := filepath.Join(dir, "a-very-long-path-that-exceeds-sixty-characters-for-sure-yes-it-does.jsonl")
	if err := os.WriteFile(longPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	records := `{"file":"x.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"test","prompt":"test","trace":"` + longPath + `#tool-1","tool":"Edit"}` + "\n"

	sessionFile := filepath.Join(paths.LogDir, "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte(records), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "add", ".blamebot/log/session.jsonl")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	stdout, _ := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, true)
	})

	// Long path should be truncated with "..." prefix
	if !strings.Contains(stdout, "...") {
		t.Errorf("stdout should contain '...' for truncated transcript path, got: %s", stdout)
	}
}

func TestCmdFillReasons_AllHaveReasons(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	paths := setupFillTestPaths(t, dir)

	// Write a session JSONL file where all records already have reasons filled in
	records := strings.Join([]string{
		`{"file":"src/main.go","lines":"5-10","ts":"2025-01-01T00:00:00Z","change":"added error handling","prompt":"add error handling","reason":"Added error handling for robustness","trace":"/tmp/t.jsonl#tool-1","tool":"Edit"}`,
		`{"file":"src/handler.go","lines":"20","ts":"2025-01-01T00:01:00Z","change":"refactored handler","prompt":"refactor handler","reason":"Simplified handler code","trace":"/tmp/t.jsonl#tool-2","tool":"Edit"}`,
	}, "\n") + "\n"

	sessionFile := filepath.Join(paths.LogDir, "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte(records), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage the JSONL file
	cmd := exec.Command("git", "add", ".blamebot/log/session.jsonl")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	_, stderr := captureBothOutputs(t, func() {
		cmdFillReasons(paths, dir, false)
	})

	if !strings.Contains(stderr, "All records already have reasons") {
		t.Errorf("stderr should contain 'All records already have reasons', got: %s", stderr)
	}
}
