package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/project"
)

// setupTestDB creates a temp directory with sample JSONL data and builds a SQLite index.
// Returns the opened DB, project paths, and the root directory.
func setupTestDB(t *testing.T) (*sql.DB, project.Paths, string) {
	t.Helper()

	tmpDir := t.TempDir()

	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	pendingDir := filepath.Join(cacheDir, "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := project.Paths{
		Root:       tmpDir,
		GitDir:     filepath.Join(tmpDir, ".git"),
		PendingDir: pendingDir,
		CacheDir:   cacheDir,
		IndexDB:    filepath.Join(cacheDir, "index.db"),
	}

	// Write sample pending edit records with known data.
	// Session 1: alice's work on src/main.go
	pending1 := `{"file":"src/main.go","lines":"5-10","ts":"2025-01-01T00:00:00Z","prompt":"fix authentication bug","reason":"Fixed auth token validation","change":"Replaced broken token check with proper JWT validation","tool":"Edit","author":"alice","session":"sess-aaa","trace":"traces/sess-aaa#tool-1","content_hash":"hash1"}`
	pending2 := `{"file":"src/main.go","lines":"20","ts":"2025-01-15T00:00:00Z","prompt":"add logging to main","reason":"","change":"Added structured logging to request handler","tool":"Edit","author":"alice","session":"sess-aaa","trace":"","content_hash":"hash2"}`
	// Session 2: bob's work on src/handler.go
	pending3 := `{"file":"src/handler.go","lines":"1-3","ts":"2025-02-01T00:00:00Z","prompt":"refactor handler for clarity","reason":"Simplified handler interface","change":"Refactored handler to use middleware pattern","tool":"Write","author":"bob","session":"sess-bbb","trace":"traces/sess-bbb#tool-1","content_hash":"hash3"}`
	pending4 := `{"file":"src/handler.go","lines":"15-20","ts":"2025-02-01T00:01:00Z","prompt":"add error handling","reason":"","change":"Added proper error propagation in handler chain","tool":"Edit","author":"bob","session":"sess-bbb","trace":"","content_hash":"hash4"}`

	for i, data := range []string{pending1, pending2, pending3, pending4} {
		fname := filepath.Join(pendingDir, fmt.Sprintf("edit-%d.json", i+1))
		if err := os.WriteFile(fname, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}

	return db, paths, tmpDir
}

// captureStdout captures everything written to os.Stdout during fn().
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old
	return string(out)
}

// ---------- queryRows tests ----------

func TestQueryRows(t *testing.T) {
	db, _, _ := setupTestDB(t)
	defer db.Close()

	rows, err := queryRows(db, "SELECT * FROM reasons WHERE author = ? ORDER BY ts", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows for alice, got %d", len(rows))
	}
	if rows[0].File != "src/main.go" {
		t.Errorf("expected file src/main.go, got %s", rows[0].File)
	}
	if rows[0].Author != "alice" {
		t.Errorf("expected author alice, got %s", rows[0].Author)
	}
}

func TestQueryRows_NoMatches(t *testing.T) {
	db, _, _ := setupTestDB(t)
	defer db.Close()

	rows, err := queryRows(db, "SELECT * FROM reasons WHERE author = ?", "nonexistent_person")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}

// ---------- cmdStats tests ----------

func TestCmdStats_Text(t *testing.T) {
	db, _, _ := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdStats(db, false)
	})

	if !strings.Contains(out, "Total records:") {
		t.Error("output should contain 'Total records:'")
	}
	if !strings.Contains(out, "4") {
		t.Error("output should contain total count of 4")
	}
	if !strings.Contains(out, "src/main.go") {
		t.Error("output should contain file src/main.go")
	}
	if !strings.Contains(out, "src/handler.go") {
		t.Error("output should contain file src/handler.go")
	}
	if !strings.Contains(out, "alice") {
		t.Error("output should contain author alice")
	}
	if !strings.Contains(out, "bob") {
		t.Error("output should contain author bob")
	}
}

func TestCmdStats_JSON(t *testing.T) {
	db, _, _ := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdStats(db, true)
	})

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, out)
	}

	if total, ok := result["total_records"].(float64); !ok || int(total) != 4 {
		t.Errorf("total_records = %v, want 4", result["total_records"])
	}
	if files, ok := result["files_tracked"].(float64); !ok || int(files) != 2 {
		t.Errorf("files_tracked = %v, want 2", result["files_tracked"])
	}
	if authors, ok := result["authors"].(float64); !ok || int(authors) != 2 {
		t.Errorf("authors = %v, want 2", result["authors"])
	}
	if _, ok := result["top_files"].([]interface{}); !ok {
		t.Error("top_files should be an array")
	}
	if _, ok := result["top_authors"].([]interface{}); !ok {
		t.Error("top_authors should be an array")
	}
}

// ---------- cmdGrep tests ----------

func TestCmdGrep_Found(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdGrep(db, "authentication", root, false, false)
	})

	if !strings.Contains(out, "Found") {
		t.Error("output should contain 'Found'")
	}
	if !strings.Contains(out, "authentication") || !strings.Contains(out, "src/main.go") {
		t.Error("output should contain matching record info")
	}
}

func TestCmdGrep_NotFound(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdGrep(db, "nonexistent_xyz", root, false, false)
	})

	if !strings.Contains(out, "No reasons matching") {
		t.Errorf("output should contain 'No reasons matching', got: %s", out)
	}
}

func TestCmdGrep_JSON(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdGrep(db, "authentication", root, false, true)
	})

	var result []interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON array: %v\noutput: %s", err, out)
	}
	if len(result) == 0 {
		t.Error("expected at least one result in JSON array")
	}
}

// ---------- cmdSince tests ----------

func TestCmdSince(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdSince(db, "2025-01-10", "", root, false, false)
	})

	if !strings.Contains(out, "Found") {
		t.Error("output should contain 'Found'")
	}
	// Should include Jan 15 and Feb 1 records (3 total), but not Jan 1
	if !strings.Contains(out, "3 reason(s)") {
		t.Errorf("expected 3 reasons since 2025-01-10, got: %s", out)
	}
}

func TestCmdSince_NoResults(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdSince(db, "2099-01-01", "", root, false, false)
	})

	if !strings.Contains(out, "No reasons found since") {
		t.Errorf("output should contain 'No reasons found since', got: %s", out)
	}
}

// ---------- cmdAuthor tests ----------

func TestCmdAuthor_Found(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdAuthor(db, "alice", root, false, false)
	})

	if !strings.Contains(out, "Found") {
		t.Error("output should contain 'Found'")
	}
	if !strings.Contains(out, "alice") {
		t.Error("output should contain 'alice'")
	}
}

func TestCmdAuthor_NotFound(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdAuthor(db, "unknown_person", root, false, false)
	})

	if !strings.Contains(out, "No reasons found for author") {
		t.Errorf("output should contain 'No reasons found for author', got: %s", out)
	}
}

// ---------- cmdFile tests ----------

func TestCmdFile_Found(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdFile(db, "src/main.go", root, "", false, false)
	})

	if !strings.Contains(out, "src/main.go") {
		t.Errorf("output should contain record info for src/main.go, got: %s", out)
	}
}

func TestCmdFile_NotFound(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdFile(db, "nonexistent.go", root, "", false, false)
	})

	if !strings.Contains(out, "No reasons found for") {
		t.Errorf("output should contain 'No reasons found for', got: %s", out)
	}
}

func TestCmdFile_JSON(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdFile(db, "src/main.go", root, "", false, true)
	})

	var result []interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON array: %v\noutput: %s", err, out)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results for src/main.go, got %d", len(result))
	}
}

// ---------- cmdSince with file filter and JSON tests ----------

func TestCmdSince_WithFileFilter(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdSince(db, "2025-01-01", "src/main.go", root, false, false)
	})

	// Should only match records for src/main.go since 2025-01-01 (both alice records)
	if !strings.Contains(out, "Found") {
		t.Error("output should contain 'Found'")
	}
	if !strings.Contains(out, "2 reason(s)") {
		t.Errorf("expected 2 reasons for src/main.go since 2025-01-01, got: %s", out)
	}
	// Should NOT contain handler.go records
	if strings.Contains(out, "handler.go") {
		t.Error("output should not contain handler.go when filtering by src/main.go")
	}
}

func TestCmdSince_JSON(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdSince(db, "2025-01-10", "", root, false, true)
	})

	var result []interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON array: %v\noutput: %s", err, out)
	}
	// Should include Jan 15 and Feb 1 records (3 total), but not Jan 1
	if len(result) != 3 {
		t.Errorf("expected 3 results in JSON array, got %d", len(result))
	}
}

// ---------- cmdAuthor JSON and verbose tests ----------

func TestCmdAuthor_JSON(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdAuthor(db, "alice", root, false, true)
	})

	var result []interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON array: %v\noutput: %s", err, out)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results for alice in JSON array, got %d", len(result))
	}
}

func TestCmdAuthor_Verbose(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdAuthor(db, "alice", root, true, false)
	})

	if !strings.Contains(out, "Found") {
		t.Error("output should contain 'Found'")
	}
	// Verbose mode should include Tool, Hash, and Session fields
	if !strings.Contains(out, "Tool:") {
		t.Error("verbose output should contain 'Tool:'")
	}
	if !strings.Contains(out, "Hash:") {
		t.Error("verbose output should contain 'Hash:'")
	}
	if !strings.Contains(out, "Session:") {
		t.Error("verbose output should contain 'Session:'")
	}
}

// ---------- cmdGrep verbose test ----------

func TestCmdGrep_Verbose(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdGrep(db, "authentication", root, true, false)
	})

	if !strings.Contains(out, "Found") {
		t.Error("output should contain 'Found'")
	}
	// Verbose mode should include Tool, Hash, and Session fields
	if !strings.Contains(out, "Tool:") {
		t.Error("verbose output should contain 'Tool:'")
	}
	if !strings.Contains(out, "Hash:") {
		t.Error("verbose output should contain 'Hash:'")
	}
	if !strings.Contains(out, "Session:") {
		t.Error("verbose output should contain 'Session:'")
	}
}

// ---------- cmdTrace tests ----------

func TestCmdTrace_JSON(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	// Record 1 has trace="traces/sess-aaa#tool-1"
	out := captureStdout(t, func() {
		cmdTrace(db, "1", root, true)
	})

	var result []interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, out)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result in JSON array, got %d", len(result))
	}

	item, ok := result[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected JSON object as first element")
	}
	// Should contain trace field
	if item["trace"] != "traces/sess-aaa#tool-1" {
		t.Errorf("expected trace='traces/sess-aaa#tool-1', got %v", item["trace"])
	}
	// Should contain record data
	if item["file"] != "src/main.go" {
		t.Errorf("expected file='src/main.go', got %v", item["file"])
	}
}

func TestCmdTrace_ByTraceFragment(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	// Use a fragment of the trace value "traces/sess-bbb#tool-1" to exercise
	// the fallback path that searches by trace LIKE '%fragment%'
	out := captureStdout(t, func() {
		cmdTrace(db, "sess-bbb", root, false)
	})

	// Should find the record with trace containing "sess-bbb"
	if strings.Contains(out, "No record found") {
		t.Errorf("expected to find record by trace fragment 'sess-bbb', got: %s", out)
	}
	if !strings.Contains(out, "src/handler.go") {
		t.Errorf("expected output to contain 'src/handler.go', got: %s", out)
	}
}

func TestCmdTrace_Found(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	// Records get auto-incrementing IDs starting from 1.
	// Record 1 is the first inserted record (alice's first edit).
	out := captureStdout(t, func() {
		cmdTrace(db, "1", root, false)
	})

	if !strings.Contains(out, "src/main.go") {
		t.Errorf("output should contain record details for ID 1, got: %s", out)
	}
}

func TestCmdTrace_NotFound(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdTrace(db, "99999", root, false)
	})

	if !strings.Contains(out, "No record found") {
		t.Errorf("output should contain 'No record found', got: %s", out)
	}
}

func TestCmdTrace_NotFound_JSON(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	out := captureStdout(t, func() {
		cmdTrace(db, "99999", root, true)
	})

	if strings.TrimSpace(out) != "[]" {
		t.Errorf("JSON not-found should output '[]', got: %s", out)
	}
}

func TestCmdTrace_NoTraceRef(t *testing.T) {
	db, _, root := setupTestDB(t)
	defer db.Close()

	// Insert a record with empty trace field (all columns must be present for ScanRow)
	_, err := db.Exec(`INSERT INTO reasons
		(file, line_start, line_end, content_hash, ts, prompt, reason, change, tool,
		 author, session, trace, source_file, old_start, old_lines, new_start, new_lines,
		 changed_lines, commit_sha)
		VALUES ('no-trace.go', NULL, NULL, '', '2025-01-01T00:00:00Z', 'test', '', 'test', 'Edit',
		        'bob', '', '', '', NULL, NULL, NULL, NULL, '', '')`)
	if err != nil {
		t.Fatal(err)
	}

	var id int
	err = db.QueryRow("SELECT id FROM reasons WHERE file = 'no-trace.go'").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		cmdTrace(db, fmt.Sprintf("%d", id), root, false)
	})

	if !strings.Contains(out, "No trace reference") {
		t.Errorf("should show 'No trace reference' for record without trace, got: %s", out)
	}
}
