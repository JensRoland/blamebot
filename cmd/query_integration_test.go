package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
	"github.com/jensroland/git-blamebot/internal/record"
)

// ── test helpers ──────────────────────────────────────────────────────────────

func initQueryTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "commit", "--allow-empty", "-m", "init")
	if err := provenance.InitBranch(dir); err != nil {
		t.Fatalf("InitBranch: %v", err)
	}
	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitHeadSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func writeTestFileAt(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeTestPaths(root string) project.Paths {
	cacheDir := filepath.Join(root, ".git", "blamebot")
	os.MkdirAll(cacheDir, 0o755)
	return project.Paths{
		Root:       root,
		GitDir:     filepath.Join(root, ".git"),
		PendingDir: filepath.Join(cacheDir, "pending"),
		CacheDir:   cacheDir,
		IndexDB:    filepath.Join(cacheDir, "index.db"),
	}
}

// generateLines generates "line N\n" for each N in [from, to].
func generateLines(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// buildModifiedFile builds a file where lines in [modFrom, modTo] are
// replaced with "changed N\n" and all other lines are "line N\n".
func buildModifiedFile(totalLines, modFrom, modTo int) string {
	var b strings.Builder
	for i := 1; i <= totalLines; i++ {
		if i >= modFrom && i <= modTo {
			fmt.Fprintf(&b, "changed %d\n", i)
		} else {
			fmt.Fprintf(&b, "line %d\n", i)
		}
	}
	return b.String()
}

func rebuildTestIndex(t *testing.T, paths project.Paths) *sql.DB {
	t.Helper()
	db, err := index.Rebuild(paths, true)
	if err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	return db
}

func queryFileRows(t *testing.T, db *sql.DB, file string) []*index.ReasonRow {
	t.Helper()
	rows, err := queryRows(db,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts ASC",
		file, "%/"+file)
	if err != nil {
		t.Fatalf("queryRows: %v", err)
	}
	return rows
}

// ── Bug #1: Pending (uncommitted) edits visible in queries ────────────────────

// TestQueryLineBlame_PendingEditVisible verifies that uncommitted AI edits
// (stored as pending edits) are found by line-level queries.
//
// Reproduces Bug #1: "git blamebot -L 3 README.md" returned "No reasons found"
// when there was an uncommitted pending AI edit for that line.
func TestQueryLineBlame_PendingEditVisible(t *testing.T) {
	root := initQueryTestRepo(t)
	paths := makeTestPaths(root)

	// 1. Create file with 20 lines, commit
	writeTestFileAt(t, root, "README.md", generateLines(1, 20))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "initial file")

	// 2. AI modifies lines 3-5 (don't commit — simulates uncommitted state)
	writeTestFileAt(t, root, "README.md", buildModifiedFile(20, 3, 5))

	// 3. Create pending edit recording AI's change
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

	// 4. Rebuild index
	db := rebuildTestIndex(t, paths)
	defer db.Close()

	// 5. Query line 3 — MUST find the pending edit
	matches, adjMap := queryLineBlame(db, "README.md", root, "3")
	if len(matches) == 0 {
		t.Fatal("Bug #1: queryLineBlame returned no matches for line 3, but a pending AI edit exists")
	}

	found := false
	for _, m := range matches {
		if m.CommitSHA == "" && m.Change == "AI modified lines 3-5" {
			found = true
			adj := adjMap[m]
			if adj == nil {
				t.Error("pending edit match has no line adjustment")
				break
			}
			if !adj.CurrentLines.Contains(3) || !adj.CurrentLines.Contains(5) {
				t.Errorf("pending edit currentLines = %v, want to contain 3-5",
					adj.CurrentLines.String())
			}
			break
		}
	}
	if !found {
		t.Error("Bug #1: pending edit not found in queryLineBlame matches")
	}
}

// TestBlameAdjustFile_PendingEditVisible verifies that pending edits appear
// in file-level queries (no line filter).
//
// Reproduces Bug #1: "git blamebot README.md" only showed old committed records
// and omitted the pending (uncommitted) AI edit.
func TestBlameAdjustFile_PendingEditVisible(t *testing.T) {
	root := initQueryTestRepo(t)
	paths := makeTestPaths(root)

	// 1. Create file, commit
	writeTestFileAt(t, root, "README.md", generateLines(1, 20))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "initial file")
	initialSHA := gitHeadSHA(t, root)

	// 2. Write a manifest for an earlier committed AI edit (lines 1-2)
	ls1, _ := lineset.FromString("1-2")
	provenance.WriteManifest(root, paths.GitDir, provenance.Manifest{
		ID:        "m1",
		CommitSHA: initialSHA,
		Author:    "claude",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{
				File:   "README.md",
				Lines:  ls1,
				Change: "AI wrote lines 1-2",
				Tool:   "Edit",
				Hunk:   &record.HunkInfo{OldStart: 1, OldLines: 0, NewStart: 1, NewLines: 2},
			},
		},
	})

	// 3. AI modifies lines 3-5 (don't commit)
	writeTestFileAt(t, root, "README.md", buildModifiedFile(20, 3, 5))

	// 4. Create pending edit
	ls2, _ := lineset.FromString("3-5")
	provenance.WritePending(paths.GitDir, provenance.PendingEdit{
		ID:     "pending-1",
		Ts:     "2025-01-01T00:01:00Z",
		File:   "README.md",
		Lines:  ls2,
		Hunk:   &record.HunkInfo{OldStart: 3, OldLines: 3, NewStart: 3, NewLines: 3},
		Change: "AI modified lines 3-5",
		Tool:   "Edit",
		Author: "claude",
	})

	// 5. Rebuild index
	db := rebuildTestIndex(t, paths)
	defer db.Close()

	// 6. Query all file rows — MUST include both committed and pending records
	rows := queryFileRows(t, db, "README.md")
	if len(rows) < 2 {
		t.Fatalf("Bug #1: expected at least 2 rows (committed + pending), got %d", len(rows))
	}

	adjMap := blameAdjustFile(root, "README.md", rows)

	foundPending := false
	for _, row := range rows {
		if row.CommitSHA == "" && row.Change == "AI modified lines 3-5" {
			foundPending = true
			adj := adjMap[row]
			if adj == nil {
				t.Error("Bug #1: pending edit has no line adjustment")
			} else if adj.CurrentLines.IsEmpty() {
				t.Error("Bug #1: pending edit has empty currentLines")
			}
			break
		}
	}
	if !foundPending {
		t.Error("Bug #1: pending edit not found in file query results")
	}
}

// ── Bug #2: AI and manual changes separated in same commit ────────────────────

// TestBlameAdjustFile_SeparatesAIAndManual verifies that when AI edits
// lines 3-5 and manual edits touch lines 6-16 in the same commit,
// the AI record shows only L3-5 (not L3-16).
//
// Reproduces Bug #2: blamebot showed "L3-16" for an AI edit that only
// touched lines 3-5, because manual changes in the same commit were lumped in.
func TestBlameAdjustFile_SeparatesAIAndManual(t *testing.T) {
	root := initQueryTestRepo(t)
	paths := makeTestPaths(root)

	// 1. Create file with 20 lines, commit
	writeTestFileAt(t, root, "README.md", generateLines(1, 20))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "initial")

	// 2. Change lines 3-16 in a single commit (AI: 3-5, manual: 6-16)
	writeTestFileAt(t, root, "README.md", buildModifiedFile(20, 3, 16))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "mixed AI and manual changes")
	commitSHA := gitHeadSHA(t, root)

	// 3. Write manifest recording ONLY the AI edit (lines 3-5)
	ls, _ := lineset.FromString("3-5")
	provenance.WriteManifest(root, paths.GitDir, provenance.Manifest{
		ID:        "m1",
		CommitSHA: commitSHA,
		Author:    "claude",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{
				File:   "README.md",
				Lines:  ls,
				Change: "AI modified lines 3-5",
				Tool:   "Edit",
				Hunk:   &record.HunkInfo{OldStart: 3, OldLines: 3, NewStart: 3, NewLines: 3},
			},
		},
	})

	// 4. Rebuild index
	db := rebuildTestIndex(t, paths)
	defer db.Close()

	// 5. Get rows and run blameAdjustFile
	rows := queryFileRows(t, db, "README.md")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	adjMap := blameAdjustFile(root, "README.md", rows)
	adj := adjMap[rows[0]]
	if adj == nil {
		t.Fatal("no line adjustment for AI record")
	}
	if adj.Superseded {
		t.Fatal("AI record should not be superseded")
	}

	// 6. Verify: AI record shows ONLY L3-5
	currentLines := adj.CurrentLines.Lines()
	if len(currentLines) != 3 {
		t.Errorf("Bug #2: currentLines = %v (len %d), want [3,4,5]",
			currentLines, len(currentLines))
	}
	for _, expected := range []int{3, 4, 5} {
		if !adj.CurrentLines.Contains(expected) {
			t.Errorf("Bug #2: AI line %d missing from currentLines %v",
				expected, currentLines)
		}
	}

	// 7. Verify: manual lines 6-16 are NOT attributed to AI
	for line := 6; line <= 16; line++ {
		if adj.CurrentLines.Contains(line) {
			t.Errorf("Bug #2: manual line %d incorrectly attributed to AI "+
				"(currentLines=%v)", line, currentLines)
		}
	}
}

// TestQueryLineBlame_AILineReturnsMatch verifies that querying an AI-edited
// line returns a match when AI and manual edits are in the same commit.
func TestQueryLineBlame_AILineReturnsMatch(t *testing.T) {
	root := initQueryTestRepo(t)
	paths := makeTestPaths(root)

	writeTestFileAt(t, root, "README.md", generateLines(1, 20))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "initial")

	writeTestFileAt(t, root, "README.md", buildModifiedFile(20, 3, 16))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "mixed changes")
	commitSHA := gitHeadSHA(t, root)

	ls, _ := lineset.FromString("3-5")
	provenance.WriteManifest(root, paths.GitDir, provenance.Manifest{
		ID:        "m1",
		CommitSHA: commitSHA,
		Author:    "claude",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{
				File:   "README.md",
				Lines:  ls,
				Change: "AI modified lines 3-5",
				Tool:   "Edit",
				Hunk:   &record.HunkInfo{OldStart: 3, OldLines: 3, NewStart: 3, NewLines: 3},
			},
		},
	})

	db := rebuildTestIndex(t, paths)
	defer db.Close()

	// Query line 3 — should find the AI edit
	matches, adjMap := queryLineBlame(db, "README.md", root, "3")
	if len(matches) == 0 {
		t.Fatal("queryLineBlame for AI line 3 returned no matches")
	}

	adj := adjMap[matches[0]]
	if adj == nil {
		t.Fatal("match has no line adjustment")
	}
	for _, expected := range []int{3, 4, 5} {
		if !adj.CurrentLines.Contains(expected) {
			t.Errorf("AI line %d missing from currentLines %v",
				expected, adj.CurrentLines.Lines())
		}
	}
}

// TestQueryLineBlame_ManualLineNoMatch verifies that querying a manually-edited
// line does NOT return an AI match when AI and manual edits share a commit.
func TestQueryLineBlame_ManualLineNoMatch(t *testing.T) {
	root := initQueryTestRepo(t)
	paths := makeTestPaths(root)

	writeTestFileAt(t, root, "README.md", generateLines(1, 20))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "initial")

	writeTestFileAt(t, root, "README.md", buildModifiedFile(20, 3, 16))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "mixed changes")
	commitSHA := gitHeadSHA(t, root)

	ls, _ := lineset.FromString("3-5")
	provenance.WriteManifest(root, paths.GitDir, provenance.Manifest{
		ID:        "m1",
		CommitSHA: commitSHA,
		Author:    "claude",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{
				File:   "README.md",
				Lines:  ls,
				Change: "AI modified lines 3-5",
				Tool:   "Edit",
				Hunk:   &record.HunkInfo{OldStart: 3, OldLines: 3, NewStart: 3, NewLines: 3},
			},
		},
	})

	db := rebuildTestIndex(t, paths)
	defer db.Close()

	// Query line 10 — a manually edited line, should NOT match any AI record
	matches, _ := queryLineBlame(db, "README.md", root, "10")
	if len(matches) > 0 {
		t.Errorf("Bug #2: queryLineBlame for manual line 10 returned %d matches, want 0",
			len(matches))
		for _, m := range matches {
			t.Logf("  unexpected match: change=%q commitSHA=%q", m.Change, m.CommitSHA)
		}
	}
}

// ── Full scenario: exact reproduction of the user's bug report ────────────────

// TestQueryIntegration_FullScenario reproduces the user's exact bug sequence:
//
//  1. AI creates file → commit 1
//  2. AI edits lines 3-5 (uncommitted)
//  3. User manually edits lines 6-16 (uncommitted)
//  4. Query line 3 → should find pending AI edit (Bug #1)
//  5. Query all file → should include pending edit (Bug #1)
//  6. Commit (AI + manual changes go into commit 2)
//  7. Query all file → AI record shows L3-5, NOT L3-16 (Bug #2)
func TestQueryIntegration_FullScenario(t *testing.T) {
	root := initQueryTestRepo(t)
	paths := makeTestPaths(root)

	// ── Step 1: AI creates the initial file and commits ──
	writeTestFileAt(t, root, "README.md", generateLines(1, 20))
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "AI creates file")
	commit1SHA := gitHeadSHA(t, root)

	ls1, _ := lineset.FromString("1-20")
	provenance.WriteManifest(root, paths.GitDir, provenance.Manifest{
		ID:        "m1",
		CommitSHA: commit1SHA,
		Author:    "claude",
		Timestamp: "2025-01-01T00:00:00Z",
		Edits: []provenance.ManifestEdit{
			{
				File:   "README.md",
				Lines:  ls1,
				Change: "AI created file with 20 lines",
				Tool:   "Write",
				Hunk:   &record.HunkInfo{OldStart: 1, OldLines: 0, NewStart: 1, NewLines: 20},
			},
		},
	})

	// ── Steps 2-3: AI edits lines 3-5, user manually edits 6-16 (uncommitted) ──
	writeTestFileAt(t, root, "README.md", buildModifiedFile(20, 3, 16))

	ls2, _ := lineset.FromString("3-5")
	provenance.WritePending(paths.GitDir, provenance.PendingEdit{
		ID:     "pending-1",
		Ts:     "2025-01-01T00:01:00Z",
		File:   "README.md",
		Lines:  ls2,
		Hunk:   &record.HunkInfo{OldStart: 3, OldLines: 3, NewStart: 3, NewLines: 3},
		Change: "AI modified lines 3-5",
		Tool:   "Edit",
		Author: "claude",
	})

	// ── Step 4: Query line 3 → should find pending AI edit (Bug #1) ──
	db := rebuildTestIndex(t, paths)

	matches, _ := queryLineBlame(db, "README.md", root, "3")
	if len(matches) == 0 {
		t.Fatal("Step 4 FAILED (Bug #1): query for line 3 returned no matches, " +
			"but pending AI edit exists for lines 3-5")
	}
	foundPending := false
	for _, m := range matches {
		if m.CommitSHA == "" && m.Change == "AI modified lines 3-5" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Error("Step 4 FAILED (Bug #1): pending edit not found in line query matches")
	}

	// ── Step 5: Query all file → should include pending edit (Bug #1) ──
	allRows := queryFileRows(t, db, "README.md")
	hasPendingRow := false
	for _, row := range allRows {
		if row.CommitSHA == "" && row.Change == "AI modified lines 3-5" {
			hasPendingRow = true
		}
	}
	if !hasPendingRow {
		t.Error("Step 5 FAILED (Bug #1): pending edit not found in file query results")
	}

	db.Close()

	// ── Step 6: Commit everything ──
	runGitCmd(t, root, "add", "README.md")
	runGitCmd(t, root, "commit", "-m", "AI + manual changes")
	commit2SHA := gitHeadSHA(t, root)

	// Clear pending, create manifest for the committed AI edit only
	provenance.ClearPending(paths.GitDir)
	provenance.WriteManifest(root, paths.GitDir, provenance.Manifest{
		ID:        "m2",
		CommitSHA: commit2SHA,
		Author:    "claude",
		Timestamp: "2025-01-01T00:01:00Z",
		Edits: []provenance.ManifestEdit{
			{
				File:   "README.md",
				Lines:  ls2,
				Change: "AI modified lines 3-5",
				Tool:   "Edit",
				Hunk:   &record.HunkInfo{OldStart: 3, OldLines: 3, NewStart: 3, NewLines: 3},
			},
		},
	})

	// ── Step 7: Query all file → AI record shows L3-5, NOT L3-16 (Bug #2) ──
	_ = os.Remove(paths.IndexDB) // Force rebuild
	db2 := rebuildTestIndex(t, paths)
	defer db2.Close()

	allRows2 := queryFileRows(t, db2, "README.md")
	adjMap := blameAdjustFile(root, "README.md", allRows2)

	for _, row := range allRows2 {
		if row.CommitSHA != commit2SHA || row.Change != "AI modified lines 3-5" {
			continue
		}

		adj := adjMap[row]
		if adj == nil {
			t.Fatal("Step 7 FAILED: no adjustment for AI record from commit 2")
		}

		currentLines := adj.CurrentLines.Lines()

		// AI record MUST show exactly lines 3-5
		if len(currentLines) != 3 {
			t.Errorf("Step 7 FAILED (Bug #2): AI record shows %d lines %v, want [3,4,5]",
				len(currentLines), currentLines)
		}
		for _, expected := range []int{3, 4, 5} {
			if !adj.CurrentLines.Contains(expected) {
				t.Errorf("Step 7 FAILED (Bug #2): AI line %d missing from currentLines",
					expected)
			}
		}

		// Manual lines 6-16 MUST NOT be attributed to AI
		for line := 6; line <= 16; line++ {
			if adj.CurrentLines.Contains(line) {
				t.Errorf("Step 7 FAILED (Bug #2): manual line %d incorrectly "+
					"attributed to AI (currentLines=%v)", line, currentLines)
			}
		}
	}
}
