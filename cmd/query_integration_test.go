package cmd

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/project"
)

// setupGitRepoWithIndex creates a real git repo with committed source files,
// committed JSONL log files, and a built SQLite index.
//
// The source file and JSONL records are committed together in a single commit
// so that git blame on src/main.go returns the same SHA that git blame on the
// JSONL file returns. This mirrors the real workflow where blamebot records are
// committed alongside the code changes they describe.
func setupGitRepoWithIndex(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()

	// Initialize git repo
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

	// Create source file
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/main.go"), []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create JSONL log directory structure
	if err := os.MkdirAll(filepath.Join(dir, ".blamebot", "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git", "blamebot"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write JSONL records (commit_sha is ignored by index.Rebuild; it uses
	// git blame on the JSONL file to derive the real commit_sha).
	jsonl := `{"file":"src/main.go","lines":"2-4","ts":"2025-01-01T00:00:00Z","change":"modified lines","prompt":"fix bug","tool":"Edit","content_hash":"abc"}
{"file":"src/main.go","lines":"1","ts":"2025-01-02T00:00:00Z","change":"updated header","prompt":"add header","tool":"Edit","content_hash":"def"}
`

	if err := os.WriteFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}

	// Commit the source file and JSONL records together so git blame on
	// src/main.go returns the same SHA as git blame on the JSONL file.
	run("add", "src/main.go", ".blamebot/log/session.jsonl")
	run("commit", "-m", "initial commit with source and blamebot logs")

	paths := project.Paths{
		Root:     dir,
		LogDir:   filepath.Join(dir, ".blamebot", "log"),
		CacheDir: filepath.Join(dir, ".git", "blamebot"),
		IndexDB:  filepath.Join(dir, ".git", "blamebot", "index.db"),
	}

	db, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}

	return db, dir
}

// ---------- blameAdjustFile tests ----------

func TestBlameAdjustFile(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	defer db.Close()

	// Query all rows for src/main.go
	rows, err := queryRows(db,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts DESC",
		"src/main.go", "%/src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected rows for src/main.go, got none")
	}

	adjMap := blameAdjustFile(dir, "src/main.go", rows)

	for i, row := range rows {
		adj, ok := adjMap[row]
		if !ok || adj == nil {
			t.Errorf("row %d: expected non-nil LineAdjustment, got nil", i)
			continue
		}
		if adj.CurrentLines.IsEmpty() {
			t.Errorf("row %d (lines=%v): expected non-empty CurrentLines", i, row.LineStart)
		}
	}
}

// ---------- queryLineBlame tests ----------

func TestQueryLineBlame(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	defer db.Close()

	matches, adjustments := queryLineBlame(db, "src/main.go", dir, "2")
	if len(matches) == 0 {
		t.Fatal("expected matches for line 2, got none")
	}

	for _, m := range matches {
		adj, ok := adjustments[m]
		if !ok {
			t.Errorf("match for file=%s has no adjustment entry", m.File)
		}
		if adj == nil {
			t.Errorf("match for file=%s has nil adjustment", m.File)
		}
	}
}

func TestQueryLineBlame_Range(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	defer db.Close()

	matches, adjustments := queryLineBlame(db, "src/main.go", dir, "1:3")
	if len(matches) == 0 {
		t.Fatal("expected matches for line range 1:3, got none")
	}

	// Should find at least one record since lines 1, 2-4 are covered
	for _, m := range matches {
		if _, ok := adjustments[m]; !ok {
			t.Errorf("match for file=%s missing from adjustments map", m.File)
		}
	}
}

// ---------- queryAdjustedLineFallback tests ----------

func TestQueryAdjustedLineFallback(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	// Close and rebuild with an uncommitted record
	db.Close()

	// Modify the source file without committing
	if err := os.WriteFile(filepath.Join(dir, "src/main.go"), []byte("new1\nline1\nline2\nline3\nline4\nline5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a new JSONL record without commit_sha (simulates uncommitted edit)
	existingData, err := os.ReadFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	uncommittedRecord := `{"file":"src/main.go","lines":"1","ts":"2025-01-03T00:00:00Z","change":"added new line","prompt":"insert header","tool":"Edit","content_hash":"ghi","commit_sha":""}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"), append(existingData, []byte(uncommittedRecord)...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Rebuild the index with the new record
	paths := project.Paths{
		Root:     dir,
		LogDir:   filepath.Join(dir, ".blamebot", "log"),
		CacheDir: filepath.Join(dir, ".git", "blamebot"),
		IndexDB:  filepath.Join(dir, ".git", "blamebot", "index.db"),
	}
	db2, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	// Call the fallback function for lines 1-3
	matches, adjMap := queryAdjustedLineFallback(db2, "src/main.go", 1, 3)

	// The function should return without error. Matches may or may not be found
	// depending on the forward simulation, but the function itself should not panic.
	_ = matches
	_ = adjMap
}

// ---------- cmdFile with line filter tests ----------

func TestCmdFile_WithLine(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdFile(db, "src/main.go", dir, "2", false, false)
	})

	// Should output record information (not "No reasons found")
	if strings.Contains(out, "No reasons found") {
		t.Errorf("expected record info for line 2, got: %s", out)
	}
	if out == "" {
		t.Error("expected non-empty output for line 2 query")
	}
}

func TestCmdFile_WithLine_JSON(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdFile(db, "src/main.go", dir, "2", false, true)
	})

	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("expected JSON output, got empty string")
	}

	// Verify the output is valid JSON
	var result []interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(result) == 0 {
		t.Error("expected at least one JSON result for line 2")
	}
}

// ---------- blameAdjustFile with uncommitted records ----------

func TestBlameAdjustFile_Uncommitted(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	db.Close()

	// Modify the source file without committing (add a line at the top)
	if err := os.WriteFile(filepath.Join(dir, "src/main.go"), []byte("new_header\nline1\nline2\nline3\nline4\nline5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a new uncommitted JSONL record (empty commit_sha)
	existingData, err := os.ReadFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	uncommittedRecord := `{"file":"src/main.go","lines":"1","ts":"2025-01-03T00:00:00Z","change":"added header line","prompt":"add header","tool":"Edit","content_hash":"uncommitted1","commit_sha":""}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"), append(existingData, []byte(uncommittedRecord)...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Rebuild the index
	paths := project.Paths{
		Root:     dir,
		LogDir:   filepath.Join(dir, ".blamebot", "log"),
		CacheDir: filepath.Join(dir, ".git", "blamebot"),
		IndexDB:  filepath.Join(dir, ".git", "blamebot", "index.db"),
	}
	db2, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	// Query all rows for the file
	rows, err := queryRows(db2,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts DESC",
		"src/main.go", "%/src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected rows for src/main.go, got none")
	}

	// Call blameAdjustFile — this exercises the uncommittedRows > 0 branch
	adjMap := blameAdjustFile(dir, "src/main.go", rows)

	// Verify that every row got some adjustment entry
	for i, row := range rows {
		adj, ok := adjMap[row]
		if !ok || adj == nil {
			t.Errorf("row %d (commit_sha=%q): expected non-nil LineAdjustment, got nil", i, row.CommitSHA)
		}
	}

	// The uncommitted record (empty commit_sha) should have been processed via fallback
	var foundUncommitted bool
	for _, row := range rows {
		if row.CommitSHA == "" {
			foundUncommitted = true
			adj := adjMap[row]
			if adj == nil {
				t.Error("uncommitted record should have an adjustment entry")
			}
		}
	}
	if !foundUncommitted {
		t.Error("expected at least one uncommitted record (empty commit_sha)")
	}
}

// ---------- blameAdjustFile with superseded SHA ----------

func TestBlameAdjustFile_SupersededSHA(t *testing.T) {
	dir := t.TempDir()

	// Initialize git repo
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

	// Create source file and JSONL, commit together (C1)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/main.go"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".blamebot", "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git", "blamebot"), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := `{"file":"src/main.go","lines":"1-3","ts":"2025-01-01T00:00:00Z","change":"initial content","prompt":"create file","tool":"Write","content_hash":"abc"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "src/main.go", ".blamebot/log/session.jsonl")
	run("commit", "-m", "initial commit with source and logs")

	// Now completely rewrite the file (all lines replaced) and commit (C2)
	if err := os.WriteFile(filepath.Join(dir, "src/main.go"), []byte("totally_new1\ntotally_new2\ntotally_new3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "src/main.go")
	run("commit", "-m", "completely rewrite file")

	// Rebuild index
	paths := project.Paths{
		Root:     dir,
		LogDir:   filepath.Join(dir, ".blamebot", "log"),
		CacheDir: filepath.Join(dir, ".git", "blamebot"),
		IndexDB:  filepath.Join(dir, ".git", "blamebot", "index.db"),
	}
	db, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Query all rows
	rows, err := queryRows(db,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts DESC",
		"src/main.go", "%/src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected rows for src/main.go")
	}

	// Call blameAdjustFile — the record's commit_sha (C1) won't appear in
	// blame anymore because all lines were rewritten in C2
	adjMap := blameAdjustFile(dir, "src/main.go", rows)

	// The record should be marked as superseded
	for _, row := range rows {
		adj, ok := adjMap[row]
		if !ok || adj == nil {
			t.Errorf("expected adjustment for row with commit_sha=%q", row.CommitSHA)
			continue
		}
		if !adj.Superseded {
			t.Errorf("expected Superseded=true for row with commit_sha=%q (content was fully replaced)", row.CommitSHA)
		}
	}
}

// ---------- queryAdjustedLineFallback with matching lines ----------

func TestQueryAdjustedLineFallback_Match(t *testing.T) {
	db, dir := setupGitRepoWithIndex(t)
	db.Close()

	// Add an uncommitted record to the JSONL without commit_sha
	existingData, err := os.ReadFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	uncommittedRecord := `{"file":"src/main.go","lines":"3-4","ts":"2025-01-03T00:00:00Z","change":"modified middle","prompt":"update logic","tool":"Edit","content_hash":"uncommitted2","commit_sha":""}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"), append(existingData, []byte(uncommittedRecord)...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Rebuild the index
	paths := project.Paths{
		Root:     dir,
		LogDir:   filepath.Join(dir, ".blamebot", "log"),
		CacheDir: filepath.Join(dir, ".git", "blamebot"),
		IndexDB:  filepath.Join(dir, ".git", "blamebot", "index.db"),
	}
	db2, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	// Call queryAdjustedLineFallback with a line range that overlaps the uncommitted record (lines 3-4)
	matches, adjMap := queryAdjustedLineFallback(db2, "src/main.go", 3, 4)

	// Should find at least one uncommitted match
	if len(matches) == 0 {
		t.Error("expected at least one match from queryAdjustedLineFallback for lines 3-4")
	}

	// Verify all matches have adjustment entries
	for _, m := range matches {
		if _, ok := adjMap[m]; !ok {
			t.Errorf("match for file=%s missing from adjMap", m.File)
		}
	}

	// Verify only uncommitted records are returned (commit_sha == "")
	for _, m := range matches {
		if m.CommitSHA != "" {
			t.Errorf("expected only uncommitted records (empty commit_sha), got commit_sha=%q", m.CommitSHA)
		}
	}
}

func TestQueryAdjustedLineFallback_NoRows(t *testing.T) {
	db, _ := setupGitRepoWithIndex(t)
	defer db.Close()

	// Query a file that doesn't exist in the DB
	matches, adjMap := queryAdjustedLineFallback(db, "nonexistent.go", 1, 5)
	if len(matches) != 0 {
		t.Errorf("expected no matches for nonexistent file, got %d", len(matches))
	}
	if adjMap != nil {
		t.Errorf("expected nil adjMap for nonexistent file, got %v", adjMap)
	}
}

func TestQueryAdjustedLineFallback_LineStartFallback(t *testing.T) {
	// This tests the path where CurrentLines is empty but LineStart/LineEnd
	// overlap the query range (lines 351-359 of query.go)
	db, dir := setupGitRepoWithIndex(t)
	db.Close()

	// Write a record with only line numbers (no changed_lines), no commit SHA
	// and no hunk data, so forward simulation can't compute CurrentLines
	existingData, _ := os.ReadFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"))
	noHunkRecord := `{"file":"src/main.go","lines":"3","ts":"2025-01-04T00:00:00Z","change":"small fix","prompt":"fix","tool":"Edit","commit_sha":""}` + "\n"
	_ = os.WriteFile(filepath.Join(dir, ".blamebot", "log", "session.jsonl"), append(existingData, []byte(noHunkRecord)...), 0o644)

	paths := project.Paths{
		Root:     dir,
		LogDir:   filepath.Join(dir, ".blamebot", "log"),
		CacheDir: filepath.Join(dir, ".git", "blamebot"),
		IndexDB:  filepath.Join(dir, ".git", "blamebot", "index.db"),
	}
	db2, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	// Query a range that overlaps line 3
	matches, _ := queryAdjustedLineFallback(db2, "src/main.go", 2, 4)
	// The function should run without errors regardless of whether matches are found
	_ = matches
}

// ---------- printAdjustedJSON tests ----------

func TestPrintAdjustedJSON(t *testing.T) {
	// Create a simple ReasonRow
	lineStart := 5
	lineEnd := 10
	row := &index.ReasonRow{
		ID:          1,
		File:        "src/example.go",
		LineStart:   &lineStart,
		LineEnd:     &lineEnd,
		ContentHash: "testhash",
		Ts:          "2025-01-01T00:00:00Z",
		Prompt:      "test prompt",
		Reason:      "test reason",
		Change:      "test change",
		Tool:        "Edit",
		Author:      "testuser",
		Session:     "sess-123",
		Trace:       "",
		SourceFile:  "session.jsonl",
	}

	out := captureStdout(t, func() {
		printAdjustedJSON([]*index.ReasonRow{row}, nil, "/tmp/fake-root")
	})

	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("expected JSON output, got empty string")
	}

	// Verify the output is valid JSON
	var result []interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(result) != 1 {
		t.Errorf("expected exactly 1 JSON result, got %d", len(result))
	}

	// Check that the JSON contains expected fields
	item, ok := result[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected JSON object as first element")
	}
	if item["file"] != "src/example.go" {
		t.Errorf("expected file=src/example.go, got %v", item["file"])
	}
	if item["prompt"] != "test prompt" {
		t.Errorf("expected prompt=test prompt, got %v", item["prompt"])
	}
}
