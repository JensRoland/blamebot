package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/project"
)

// setupTestDBWithArrow creates a test DB that includes a record with an arrow change summary.
func setupTestDBWithArrow(t *testing.T) (*sql.DB, project.Paths, string) {
	t.Helper()

	tmpDir := t.TempDir()

	logDir := filepath.Join(tmpDir, ".blamebot", "log")
	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := project.Paths{
		Root:     tmpDir,
		LogDir:   logDir,
		CacheDir: cacheDir,
		IndexDB:  filepath.Join(cacheDir, "index.db"),
	}

	// Write a session with an arrow-format change summary
	session := `{"file":"src/config.go","lines":"10","ts":"2025-03-01T00:00:00Z","prompt":"rename variable","reason":"Renamed for clarity","change":"oldName \u2192 newName","tool":"Edit","author":"alice","session":"sess-ccc","trace":"","content_hash":"hash5"}` + "\n"

	if err := os.WriteFile(filepath.Join(logDir, "ccc-session.jsonl"), []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}

	return db, paths, tmpDir
}

func TestCmdExplain_ByRecordID(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	stdout, stderr := captureBothOutputs(t, func() {
		cmdExplain(db, "1", root, "")
	})

	// Record 1 is alice's first edit on src/main.go
	if !strings.Contains(stdout, "src/main.go") {
		t.Errorf("stdout should contain file name 'src/main.go', got: %s", stdout)
	}

	// The LLM call either succeeds (Explanation in stdout) or fails (error in stderr).
	// Both paths exercise the code we care about.
	llmSucceeded := strings.Contains(stdout, "Explanation")
	llmFailed := strings.Contains(stderr, "Failed to generate explanation")
	if !llmSucceeded && !llmFailed {
		t.Errorf("expected either 'Explanation' in stdout or 'Failed to generate explanation' in stderr.\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestCmdExplain_ByFile(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	stdout, stderr := captureBothOutputs(t, func() {
		cmdExplain(db, "src/main.go", root, "")
	})

	// Should find records for src/main.go (there are 2, so it explains the most recent)
	if !strings.Contains(stdout, "src/main.go") {
		t.Errorf("stdout should contain 'src/main.go', got: %s", stdout)
	}

	// Multiple records match, so stderr should note it
	if !strings.Contains(stderr, "2 records match") {
		t.Errorf("stderr should contain '2 records match', got: %s", stderr)
	}

	// The LLM call either succeeds (Explanation in stdout) or fails (error in stderr).
	llmSucceeded := strings.Contains(stdout, "Explanation")
	llmFailed := strings.Contains(stderr, "Failed to generate explanation")
	if !llmSucceeded && !llmFailed {
		t.Errorf("expected either 'Explanation' in stdout or 'Failed to generate explanation' in stderr.\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestCmdExplain_ByFile_WithLine(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	// Query line 5 on src/main.go — git blame will fail (not a real git repo),
	// so queryLineBlame falls back to queryAdjustedLineFallback.
	// Records have no commit_sha so the fallback should find matches.
	stdout, stderr := captureBothOutputs(t, func() {
		cmdExplain(db, "src/main.go", root, "5")
	})

	// The fallback may find matches or not depending on forward simulation.
	// Either we get a record and then LLM failure, or "No reasons found".
	hasRecord := strings.Contains(stdout, "src/main.go")
	hasNoReasons := strings.Contains(stdout, "No reasons found")

	if !hasRecord && !hasNoReasons {
		t.Errorf("stdout should contain either record info or 'No reasons found', got stdout: %s, stderr: %s", stdout, stderr)
	}
}

func TestCmdExplain_RecordNotFound(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	stdout, _ := captureBothOutputs(t, func() {
		cmdExplain(db, "99999", root, "")
	})

	if !strings.Contains(stdout, "No record found") {
		t.Errorf("stdout should contain 'No record found', got: %s", stdout)
	}
}

func TestCmdExplain_FileNotFound(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	stdout, _ := captureBothOutputs(t, func() {
		cmdExplain(db, "nonexistent.go", root, "")
	})

	if !strings.Contains(stdout, "No reasons found") {
		t.Errorf("stdout should contain 'No reasons found', got: %s", stdout)
	}
}

func TestCmdExplain_WithAddedChange(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, ".blamebot", "log")
	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	_ = os.MkdirAll(logDir, 0o755)
	_ = os.MkdirAll(cacheDir, 0o755)

	paths := project.Paths{
		Root:     tmpDir,
		LogDir:   logDir,
		CacheDir: cacheDir,
		IndexDB:  filepath.Join(cacheDir, "index.db"),
	}

	session := `{"file":"src/new.go","lines":"1-5","ts":"2025-03-01T00:00:00Z","prompt":"create file","change":"added: package main\nfunc init() {}","tool":"Write","author":"alice"}` + "\n"
	_ = os.WriteFile(filepath.Join(logDir, "session.jsonl"), []byte(session), 0o644)

	db, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	stdout, _ := captureBothOutputs(t, func() {
		cmdExplain(db, "1", tmpDir, "")
	})

	if !strings.Contains(stdout, "package main") {
		t.Errorf("stdout should contain added content, got: %s", stdout)
	}
}

func TestCmdExplain_WithRemovedChange(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, ".blamebot", "log")
	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	_ = os.MkdirAll(logDir, 0o755)
	_ = os.MkdirAll(cacheDir, 0o755)

	paths := project.Paths{
		Root:     tmpDir,
		LogDir:   logDir,
		CacheDir: cacheDir,
		IndexDB:  filepath.Join(cacheDir, "index.db"),
	}

	session := `{"file":"src/old.go","lines":"1","ts":"2025-03-01T00:00:00Z","prompt":"clean up","change":"removed: obsolete_func()","tool":"Edit","author":"bob"}` + "\n"
	_ = os.WriteFile(filepath.Join(logDir, "session.jsonl"), []byte(session), 0o644)

	db, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	stdout, _ := captureBothOutputs(t, func() {
		cmdExplain(db, "1", tmpDir, "")
	})

	if !strings.Contains(stdout, "obsolete_func") {
		t.Errorf("stdout should contain removed content, got: %s", stdout)
	}
}

func TestCmdExplain_WithChangeSummary(t *testing.T) {
	db, _, root := setupTestDBWithArrow(t)
	defer db.Close()

	stdout, stderr := captureBothOutputs(t, func() {
		cmdExplain(db, "1", root, "")
	})

	// Record 1 should be the config.go record with arrow change
	if !strings.Contains(stdout, "src/config.go") {
		t.Errorf("stdout should contain 'src/config.go', got: %s", stdout)
	}

	// The arrow change "oldName → newName" should trigger the side-by-side diff fallback path.
	// The diff renderer shows a Before/After box. Column width may cause wrapping,
	// so just check that the box headers appear.
	if !strings.Contains(stdout, "Before") {
		t.Errorf("stdout should contain 'Before' header from the change diff, got: %s", stdout)
	}
	if !strings.Contains(stdout, "After") {
		t.Errorf("stdout should contain 'After' header from the change diff, got: %s", stdout)
	}

	// The LLM call either succeeds (Explanation in stdout) or fails (error in stderr).
	llmSucceeded := strings.Contains(stdout, "Explanation")
	llmFailed := strings.Contains(stderr, "Failed to generate explanation")
	if !llmSucceeded && !llmFailed {
		t.Errorf("expected either 'Explanation' in stdout or 'Failed to generate explanation' in stderr.\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}
