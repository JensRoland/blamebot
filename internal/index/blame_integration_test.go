package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/provenance"
)

func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRebuild_CommitSHAFromManifest verifies that commit_sha from manifests
// is stored correctly in the index.
func TestRebuild_CommitSHAFromManifest(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls, _ := lineset.FromString("5-7")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		CommitSHA: "abc123def456",
		Author:    "test",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "main.go", Lines: ls, Change: "modified lines 5-7", Tool: "Edit"},
		},
	})

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var commitSHA string
	err = db.QueryRow("SELECT commit_sha FROM reasons WHERE file = 'main.go'").Scan(&commitSHA)
	if err != nil {
		t.Fatal(err)
	}
	if commitSHA != "abc123def456" {
		t.Errorf("commit_sha = %q, want %q", commitSHA, "abc123def456")
	}
}

// TestRebuild_MultipleManifestCommitSHAs verifies that different manifests
// with different commit_sha values are stored correctly.
func TestRebuild_MultipleManifestCommitSHAs(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls1, _ := lineset.FromString("7-8")
	ls2, _ := lineset.FromString("2-4")

	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		CommitSHA: "sha-c1",
		Author:    "test",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "main.go", Lines: ls1, Change: "edit A: lines 7-8", Tool: "Edit"},
		},
	})
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m2",
		CommitSHA: "sha-c2",
		Author:    "test",
		Timestamp: "2025-01-01T00:01:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "main.go", Lines: ls2, Change: "edit B: insert 3 lines", Tool: "Edit"},
		},
	})

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var shaA string
	err = db.QueryRow("SELECT commit_sha FROM reasons WHERE change LIKE '%edit A%'").Scan(&shaA)
	if err != nil {
		t.Fatal(err)
	}
	if shaA != "sha-c1" {
		t.Errorf("record A commit_sha = %q, want %q", shaA, "sha-c1")
	}

	var shaB string
	err = db.QueryRow("SELECT commit_sha FROM reasons WHERE change LIKE '%edit B%'").Scan(&shaB)
	if err != nil {
		t.Fatal(err)
	}
	if shaB != "sha-c2" {
		t.Errorf("record B commit_sha = %q, want %q", shaB, "sha-c2")
	}
}

// TestRebuild_CommitSHAMultipleEditsInManifest verifies that all edits in
// a single manifest share the same commit_sha.
func TestRebuild_CommitSHAMultipleEditsInManifest(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	commitSHA := git.HeadSHA(paths.Root)

	ls1, _ := lineset.FromString("1")
	ls2, _ := lineset.FromString("2")

	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		CommitSHA: commitSHA,
		Author:    "test",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "main.go", Lines: ls1, Change: "first"},
			{File: "main.go", Lines: ls2, Change: "second"},
		},
	})

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT commit_sha FROM reasons ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var shas []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		shas = append(shas, s)
	}

	if len(shas) != 2 {
		t.Fatalf("expected 2 records, got %d", len(shas))
	}
	for i, s := range shas {
		if s != commitSHA {
			t.Errorf("record %d commit_sha = %q, want %q", i+1, s, commitSHA)
		}
	}
}

// TestStaleness_HeadChange verifies that the index becomes stale when HEAD changes.
func TestStaleness_HeadChange(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls, _ := lineset.FromString("1")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		Author:    "test",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "main.go", Lines: ls, Change: "test"},
		},
	})

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if IsStale(paths) {
		t.Error("index should not be stale immediately after rebuild")
	}

	// Make another commit (changes HEAD)
	writeFile(t, paths.Root, "main.go", "hello world\n")
	runGit(t, paths.Root, "add", "main.go")
	runGit(t, paths.Root, "commit", "-m", "second commit")

	if !IsStale(paths) {
		t.Error("index should be stale after HEAD changed")
	}
}

// TestStaleness_ProvBranchChange verifies that the index becomes stale
// when the provenance branch gets a new manifest.
func TestStaleness_ProvBranchChange(t *testing.T) {
	paths, cleanup := setupTestPaths(t)
	defer cleanup()

	ls, _ := lineset.FromString("1")
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m1",
		Author:    "test",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "a.go", Lines: ls, Change: "test"},
		},
	})

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if IsStale(paths) {
		t.Error("index should not be stale immediately after rebuild")
	}

	// Add another manifest to the provenance branch
	writeTestManifest(t, paths, provenance.Manifest{
		ID:        "m2",
		Author:    "test",
		Timestamp: "2025-01-01T00:01:00Z",
		Edits: []provenance.ManifestEdit{
			{File: "b.go", Lines: ls, Change: "another"},
		},
	})

	if !IsStale(paths) {
		t.Error("index should be stale after provenance branch tip changed")
	}
}
