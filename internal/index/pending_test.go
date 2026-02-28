package index

import (
	"testing"

	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/provenance"
	"github.com/jensroland/git-blamebot/internal/record"
)

// TestRebuild_IncludesPendingEdits verifies that pending edits (uncommitted
// AI changes in .git/blamebot/pending/) are included in the index.
func TestRebuild_IncludesPendingEdits(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls, _ := lineset.FromString("3-5")
	provenance.WritePending(paths.GitDir, provenance.PendingEdit{
		ID:     "pending-1",
		Ts:     "2025-01-01T00:01:00Z",
		File:   "README.md",
		Lines:  ls,
		Hunk:   &record.HunkInfo{OldStart: 3, OldLines: 3, NewStart: 3, NewLines: 3},
		Change: "AI modified lines 3-5",
		Tool:   "Edit",
		Author: "claude",
	})

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 record from pending edit, got %d", count)
	}

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

	if row.CommitSHA != "" {
		t.Errorf("commit_sha = %q, want empty for pending edit", row.CommitSHA)
	}
	if row.SourceFile != "pending" {
		t.Errorf("source_file = %q, want %q", row.SourceFile, "pending")
	}
	if row.File != "README.md" {
		t.Errorf("file = %q, want %q", row.File, "README.md")
	}
	if row.Change != "AI modified lines 3-5" {
		t.Errorf("change = %q, want %q", row.Change, "AI modified lines 3-5")
	}
	if row.ChangedLines == nil || *row.ChangedLines != "3-5" {
		t.Errorf("changed_lines = %v, want '3-5'", row.ChangedLines)
	}
	if row.OldStart == nil || *row.OldStart != 3 {
		t.Errorf("old_start = %v, want 3", row.OldStart)
	}
	if row.OldLines == nil || *row.OldLines != 3 {
		t.Errorf("old_lines = %v, want 3", row.OldLines)
	}
}

// TestRebuild_PendingEditsWithManifests verifies that both committed manifests
// and pending edits appear in the index together.
func TestRebuild_PendingEditsWithManifests(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls1, _ := lineset.FromString("1-2")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		CommitSHA: "sha-committed",
		Author:    "claude",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "README.md", Lines: ls1, Change: "committed edit", Tool: "Edit"},
		},
	})

	ls2, _ := lineset.FromString("3-5")
	provenance.WritePending(paths.GitDir, provenance.PendingEdit{
		ID:     "pending-1",
		Ts:     "2025-01-01T00:01:00Z",
		File:   "README.md",
		Lines:  ls2,
		Change: "pending edit",
		Tool:   "Edit",
		Author: "claude",
	})

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 records (1 committed + 1 pending), got %d", count)
	}

	var committedSHA string
	db.QueryRow("SELECT commit_sha FROM reasons WHERE change = 'committed edit'").Scan(&committedSHA)
	if committedSHA != "sha-committed" {
		t.Errorf("committed record SHA = %q, want %q", committedSHA, "sha-committed")
	}

	var pendingSHA string
	db.QueryRow("SELECT commit_sha FROM reasons WHERE change = 'pending edit'").Scan(&pendingSHA)
	if pendingSHA != "" {
		t.Errorf("pending record SHA = %q, want empty", pendingSHA)
	}
}

// TestStaleness_PendingCountChange verifies that the index becomes stale
// when the number of pending edits changes.
func TestStaleness_PendingCountChange(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if IsStale(paths) {
		t.Error("index should not be stale immediately after rebuild")
	}

	// Add a pending edit
	ls, _ := lineset.FromString("5")
	provenance.WritePending(paths.GitDir, provenance.PendingEdit{
		ID:    "pending-1",
		Ts:    "2025-01-01T00:00:00Z",
		File:  "main.go",
		Lines: ls,
		Tool:  "Edit",
	})

	if !IsStale(paths) {
		t.Error("index should be stale after adding a pending edit")
	}

	// Rebuild with the pending edit
	db, err = Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if IsStale(paths) {
		t.Error("index should not be stale after rebuild with pending edit")
	}

	// Remove pending edits
	provenance.ClearPending(paths.GitDir)

	if !IsStale(paths) {
		t.Error("index should be stale after clearing pending edits")
	}
}
