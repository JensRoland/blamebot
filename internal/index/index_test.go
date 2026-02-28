package index

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jensroland/git-blamebot/internal/project"
)

func setupTestPaths(t *testing.T) (project.Paths, func()) {
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
	return paths, func() {}
}

func writeJSONL(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRebuild_NewStringFormat(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl", `{"file":"src/main.go","lines":"5,7-8,12","ts":"2025-01-01T00:00:00Z","change":"test","tool":"Edit","hunk":{"old_start":5,"old_lines":8,"new_start":5,"new_lines":8}}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM reasons")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	row, err := ScanRow(rows)
	if err != nil {
		t.Fatal(err)
	}

	// Verify changed_lines stored as-is
	if row.ChangedLines == nil || *row.ChangedLines != "5,7-8,12" {
		t.Errorf("changed_lines = %v, want '5,7-8,12'", row.ChangedLines)
	}
	// Verify bounding range derived from Min/Max
	if row.LineStart == nil || *row.LineStart != 5 {
		t.Errorf("line_start = %v, want 5", row.LineStart)
	}
	if row.LineEnd == nil || *row.LineEnd != 12 {
		t.Errorf("line_end = %v, want 12", row.LineEnd)
	}
	// Verify hunk data
	if row.OldStart == nil || *row.OldStart != 5 {
		t.Errorf("old_start = %v, want 5", row.OldStart)
	}
	if row.OldLines == nil || *row.OldLines != 8 {
		t.Errorf("old_lines = %v, want 8", row.OldLines)
	}
}

func TestRebuild_LegacyArrayFormat(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl", `{"file":"src/main.go","lines":[5,12],"ts":"2025-01-01T00:00:00Z","change":"test"}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM reasons")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	row, err := ScanRow(rows)
	if err != nil {
		t.Fatal(err)
	}

	// Legacy format: no changed_lines
	if row.ChangedLines != nil {
		t.Errorf("changed_lines should be nil for legacy format, got %q", *row.ChangedLines)
	}
	if row.LineStart == nil || *row.LineStart != 5 {
		t.Errorf("line_start = %v, want 5", row.LineStart)
	}
	if row.LineEnd == nil || *row.LineEnd != 12 {
		t.Errorf("line_end = %v, want 12", row.LineEnd)
	}
}

func TestRebuild_LegacyNullLines(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl", `{"file":"src/main.go","lines":[null,null],"ts":"2025-01-01T00:00:00Z","change":"test"}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM reasons")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	row, err := ScanRow(rows)
	if err != nil {
		t.Fatal(err)
	}

	if row.LineStart != nil {
		t.Errorf("line_start should be nil, got %v", *row.LineStart)
	}
	if row.LineEnd != nil {
		t.Errorf("line_end should be nil, got %v", *row.LineEnd)
	}
	if row.ChangedLines != nil {
		t.Errorf("changed_lines should be nil, got %v", *row.ChangedLines)
	}
}

func TestRebuild_HunkMetadata(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"f.go","lines":"10-12","ts":"2025-01-01T00:00:00Z","change":"test","hunk":{"old_start":10,"old_lines":5,"new_start":10,"new_lines":3}}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM reasons")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	row, err := ScanRow(rows)
	if err != nil {
		t.Fatal(err)
	}

	if row.OldStart == nil || *row.OldStart != 10 {
		t.Errorf("old_start = %v, want 10", row.OldStart)
	}
	if row.OldLines == nil || *row.OldLines != 5 {
		t.Errorf("old_lines = %v, want 5", row.OldLines)
	}
	if row.NewStart == nil || *row.NewStart != 10 {
		t.Errorf("new_start = %v, want 10", row.NewStart)
	}
	if row.NewLines == nil || *row.NewLines != 3 {
		t.Errorf("new_lines = %v, want 3", row.NewLines)
	}
}

func TestRebuild_NoHunk(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"f.go","lines":"5","ts":"2025-01-01T00:00:00Z","change":"test"}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM reasons")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	row, err := ScanRow(rows)
	if err != nil {
		t.Fatal(err)
	}

	if row.OldStart != nil {
		t.Errorf("old_start should be nil without hunk, got %v", *row.OldStart)
	}
}

func TestRebuild_ChangeFallback(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	// Empty "change" field, non-empty "reason" → should use reason as change
	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"f.go","lines":"5","ts":"2025-01-01T00:00:00Z","reason":"added logging"}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM reasons")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	row, err := ScanRow(rows)
	if err != nil {
		t.Fatal(err)
	}

	if row.Change != "added logging" {
		t.Errorf("change = %q, want %q (fallback from reason)", row.Change, "added logging")
	}
}

func TestRebuild_MultipleFiles(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "aaa-session1.jsonl",
		`{"file":"a.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"first"}
{"file":"b.go","lines":"2","ts":"2025-01-01T00:01:00Z","change":"second"}
`)
	writeJSONL(t, paths.LogDir, "bbb-session2.jsonl",
		`{"file":"c.go","lines":"3","ts":"2025-01-01T00:02:00Z","change":"third"}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Count total records
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("total records = %d, want 3", count)
	}

	// Verify source_file is set correctly (deterministic ordering by filename)
	rows, err := db.Query("SELECT source_file FROM reasons ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, s)
	}
	if len(sources) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(sources))
	}
	if sources[0] != "aaa-session1.jsonl" || sources[1] != "aaa-session1.jsonl" {
		t.Errorf("first two records should be from aaa-session1.jsonl, got %v", sources[:2])
	}
	if sources[2] != "bbb-session2.jsonl" {
		t.Errorf("third record should be from bbb-session2.jsonl, got %s", sources[2])
	}
}

func TestRebuild_SkipsInvalidJSON(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl", fmt.Sprintf(
		"%s\n%s\n%s\n",
		`{"file":"a.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"valid1"}`,
		`not json at all`,
		`{"file":"b.go","lines":"2","ts":"2025-01-01T00:01:00Z","change":"valid2"}`,
	))

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("total records = %d, want 2 (invalid line skipped)", count)
	}
}

func TestRebuild_EmptyLogDir(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	// No JSONL files
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("total records = %d, want 0", count)
	}
}

// ---------- Open tests ----------

func TestOpen_Fresh(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"src/app.go","lines":"1-5","ts":"2025-01-01T00:00:00Z","change":"initial setup","tool":"Write","author":"alice"}
{"file":"src/app.go","lines":"10","ts":"2025-01-02T00:00:00Z","change":"add logging","tool":"Edit","author":"bob"}
`)

	// No existing index — Open should build one (IsStale returns true when DB doesn't exist)
	db, err := Open(paths, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify the DB has the expected records
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 records, got %d", count)
	}

	// Verify record data is correct
	var file, author string
	err = db.QueryRow("SELECT file, author FROM reasons WHERE change = 'initial setup'").Scan(&file, &author)
	if err != nil {
		t.Fatal(err)
	}
	if file != "src/app.go" {
		t.Errorf("file = %q, want 'src/app.go'", file)
	}
	if author != "alice" {
		t.Errorf("author = %q, want 'alice'", author)
	}
}

func TestOpen_ForceRebuild(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	// Write initial JSONL with 1 record
	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"src/app.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"first record","tool":"Edit"}
`)

	// Build initial index
	db1, err := Open(paths, false)
	if err != nil {
		t.Fatal(err)
	}
	var count1 int
	db1.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count1)
	db1.Close()

	if count1 != 1 {
		t.Fatalf("expected 1 record initially, got %d", count1)
	}

	// Add another record to the JSONL
	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"src/app.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"first record","tool":"Edit"}
{"file":"src/app.go","lines":"5","ts":"2025-01-02T00:00:00Z","change":"second record","tool":"Edit"}
`)

	// Force rebuild should pick up the new record
	db2, err := Open(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	var count2 int
	err = db2.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count2)
	if err != nil {
		t.Fatal(err)
	}
	if count2 != 2 {
		t.Errorf("expected 2 records after force rebuild, got %d", count2)
	}
}

func TestOpen_ExistingNotStale(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"src/app.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"test","tool":"Edit"}
`)

	// Build initial index
	db1, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db1.Close()

	// Open again — index exists and JSONL hasn't changed, so Open should
	// just open the existing DB (no rebuild).
	db2, err := Open(paths, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	var count int
	err = db2.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 record, got %d", count)
	}
}

func TestIsStale_NoIndex(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	// No index file → stale
	if !IsStale(paths) {
		t.Error("expected IsStale=true when no index exists")
	}
}

func TestIsStale_FreshIndex(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"a.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"test"}
`)

	// Build index
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Index is fresh (no JSONL changes since rebuild)
	if IsStale(paths) {
		t.Error("expected IsStale=false when index is fresh")
	}
}

func TestRebuild_AllFields(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeJSONL(t, paths.LogDir, "session.jsonl",
		`{"file":"main.go","lines":"5,7-8","ts":"2025-01-15T12:00:00Z","content_hash":"abc123","prompt":"fix bug","reason":"fixed it","change":"a → b","tool":"Edit","author":"claude","session":"sess-123","trace":"transcript#tool-1","hunk":{"old_start":5,"old_lines":4,"new_start":5,"new_lines":3}}
`)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM reasons")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}
	row, err := ScanRow(rows)
	if err != nil {
		t.Fatal(err)
	}

	if row.File != "main.go" {
		t.Errorf("file = %q", row.File)
	}
	if row.ContentHash != "abc123" {
		t.Errorf("content_hash = %q", row.ContentHash)
	}
	if row.Prompt != "fix bug" {
		t.Errorf("prompt = %q", row.Prompt)
	}
	if row.Reason != "fixed it" {
		t.Errorf("reason = %q", row.Reason)
	}
	if row.Tool != "Edit" {
		t.Errorf("tool = %q", row.Tool)
	}
	if row.Author != "claude" {
		t.Errorf("author = %q", row.Author)
	}
	if row.Session != "sess-123" {
		t.Errorf("session = %q", row.Session)
	}
	if row.Trace != "transcript#tool-1" {
		t.Errorf("trace = %q", row.Trace)
	}
	if row.ChangedLines == nil || *row.ChangedLines != "5,7-8" {
		t.Errorf("changed_lines = %v", row.ChangedLines)
	}
}
