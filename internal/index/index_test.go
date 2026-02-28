package index

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
	"github.com/jensroland/git-blamebot/internal/record"
)

func setupTestPaths(t *testing.T) (project.Paths, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	initRepo(t, tmpDir)

	cacheDir := filepath.Join(tmpDir, ".git", "blamebot")
	_ = os.MkdirAll(cacheDir, 0o755)

	paths := project.Paths{
		Root:       tmpDir,
		GitDir:     filepath.Join(tmpDir, ".git"),
		PendingDir: filepath.Join(cacheDir, "pending"),
		CacheDir:   cacheDir,
		IndexDB:    filepath.Join(cacheDir, "index.db"),
	}
	return paths, func() {}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")
	if err := provenance.InitBranch(dir); err != nil {
		t.Fatalf("InitBranch: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeTestManifest(t *testing.T, paths project.Paths, m provenance.Manifest) {
	t.Helper()
	if err := provenance.WriteManifest(paths.Root, paths.GitDir, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
}

func TestRebuild_LineSetFormat(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls, _ := lineset.FromString("5,7-8,12")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{
				File:   "src/main.go",
				Lines:  ls,
				Change: "test",
				Tool:   "Edit",
				Hunk:   &record.HunkInfo{OldStart: 5, OldLines: 8, NewStart: 5, NewLines: 8},
			},
		},
	})

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

	if row.ChangedLines == nil || *row.ChangedLines != "5,7-8,12" {
		t.Errorf("changed_lines = %v, want '5,7-8,12'", row.ChangedLines)
	}
	if row.LineStart == nil || *row.LineStart != 5 {
		t.Errorf("line_start = %v, want 5", row.LineStart)
	}
	if row.LineEnd == nil || *row.LineEnd != 12 {
		t.Errorf("line_end = %v, want 12", row.LineEnd)
	}
	if row.OldStart == nil || *row.OldStart != 5 {
		t.Errorf("old_start = %v, want 5", row.OldStart)
	}
	if row.OldLines == nil || *row.OldLines != 8 {
		t.Errorf("old_lines = %v, want 8", row.OldLines)
	}
}

func TestRebuild_EmptyLines(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "src/main.go", Change: "test"},
		},
	})

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

	ls, _ := lineset.FromString("10-12")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{
				File:   "f.go",
				Lines:  ls,
				Change: "test",
				Hunk:   &record.HunkInfo{OldStart: 10, OldLines: 5, NewStart: 10, NewLines: 3},
			},
		},
	})

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

	ls, _ := lineset.FromString("5")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "f.go", Lines: ls, Change: "test"},
		},
	})

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

	ls, _ := lineset.FromString("5")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "f.go", Lines: ls, Reason: "added logging"},
		},
	})

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

func TestRebuild_MultipleManifests(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls1, _ := lineset.FromString("1")
	ls2, _ := lineset.FromString("2")
	ls3, _ := lineset.FromString("3")

	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "aaa-manifest1",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "a.go", Lines: ls1, Change: "first"},
			{File: "b.go", Lines: ls2, Change: "second"},
		},
	})
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "bbb-manifest2",
		Timestamp: "2025-01-01T00:02:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "c.go", Lines: ls3, Change: "third"},
		},
	})

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
	if count != 3 {
		t.Errorf("total records = %d, want 3", count)
	}

	// source_file stores manifest ID
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
	for _, s := range sources {
		if s != "aaa-manifest1" && s != "bbb-manifest2" {
			t.Errorf("unexpected source_file %q", s)
		}
	}
}

func TestRebuild_NoManifests(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

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

	ls1, _ := lineset.FromString("1-5")
	ls2, _ := lineset.FromString("10")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m-open-fresh",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "alice",
		Edits: []provenance.ManifestEdit{
			{File: "src/app.go", Lines: ls1, Change: "initial setup", Tool: "Write"},
			{File: "src/app.go", Lines: ls2, Change: "add logging", Tool: "Edit"},
		},
	})

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

	ls1, _ := lineset.FromString("1")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m-force-1",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "src/app.go", Lines: ls1, Change: "first record", Tool: "Edit"},
		},
	})

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

	// Add another manifest to the provenance branch
	ls2, _ := lineset.FromString("5")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m-force-2",
		Timestamp: "2025-01-02T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "src/app.go", Lines: ls2, Change: "second record", Tool: "Edit"},
		},
	})

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

	ls, _ := lineset.FromString("1")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m-notstale",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "src/app.go", Lines: ls, Change: "test", Tool: "Edit"},
		},
	})

	// Build initial index
	db1, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db1.Close()

	// Open again — index exists and provenance hasn't changed, so Open should
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

	ls, _ := lineset.FromString("1")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m-fresh",
		Timestamp: "2025-01-01T00:00:00Z",
		Author:    "test",
		Edits: []provenance.ManifestEdit{
			{File: "a.go", Lines: ls, Change: "test"},
		},
	})

	// Build index
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Index is fresh (no provenance changes since rebuild)
	if IsStale(paths) {
		t.Error("expected IsStale=false when index is fresh")
	}
}

func TestRebuild_AllFields(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls, _ := lineset.FromString("5,7-8")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m-all",
		CommitSHA: "abc123def456",
		Author:    "claude",
		Timestamp: "2025-01-15T12:00:00Z",
		Edits: []provenance.ManifestEdit{
			{
				File:        "main.go",
				Lines:       ls,
				ContentHash: "abc123",
				Prompt:      "fix bug",
				Reason:      "fixed it",
				Change:      "a \u2192 b",
				Tool:        "Edit",
				Session:     "sess-123",
				Trace:       "transcript#tool-1",
				Hunk:        &record.HunkInfo{OldStart: 5, OldLines: 4, NewStart: 5, NewLines: 3},
			},
		},
	})

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
	if row.CommitSHA != "abc123def456" {
		t.Errorf("commit_sha = %q", row.CommitSHA)
	}
}
