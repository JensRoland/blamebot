package index

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/project"
)

// setupGitRepo creates a temp git repo and returns the directory path.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")

	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
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

func gitPaths(dir string) project.Paths {
	return project.Paths{
		Root:     dir,
		LogDir:   filepath.Join(dir, ".blamebot", "log"),
		CacheDir: filepath.Join(dir, ".git", "blamebot"),
		IndexDB:  filepath.Join(dir, ".git", "blamebot", "index.db"),
	}
}

// Test 1: AI edit matches correct lines only
// Record for lines 5-7 should only match blame for those lines, not adjacent.
func TestBlameQuery_AIEditMatchesCorrectLines(t *testing.T) {
	dir := setupGitRepo(t)
	paths := gitPaths(dir)

	// Create a 10-line file and commit (C0)
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", content)
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "initial commit")

	// AI edits lines 5-7 and we write a JSONL record, commit together (C1)
	modifiedContent := "line1\nline2\nline3\nline4\nmodified5\nmodified6\nmodified7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", modifiedContent)
	writeFile(t, dir, ".blamebot/log/session.jsonl",
		`{"file":"main.go","lines":"5-7","ts":"2025-01-01T00:00:00Z","change":"modified lines 5-7","tool":"Edit"}`+"\n")
	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit lines 5-7")

	c1SHA := git.HeadSHA(dir)

	// Rebuild index
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify commit_sha was populated
	var commitSHA string
	err = db.QueryRow("SELECT commit_sha FROM reasons WHERE file = 'main.go'").Scan(&commitSHA)
	if err != nil {
		t.Fatal(err)
	}
	if commitSHA != c1SHA {
		t.Errorf("commit_sha = %q, want %q", commitSHA, c1SHA)
	}

	// Verify blame on line 6 points to C1
	entries, err := git.BlameRange(dir, "main.go", 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	if entries[6].SHA != c1SHA {
		t.Errorf("blame for line 6: got %s, want %s", entries[6].SHA, c1SHA)
	}

	// Verify blame on line 4 does NOT point to C1 (it's from C0)
	entries, err = git.BlameRange(dir, "main.go", 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if entries[4].SHA == c1SHA {
		t.Error("blame for line 4 should NOT be C1 (it's unmodified)")
	}

	// Verify blame on line 8 does NOT point to C1
	entries, err = git.BlameRange(dir, "main.go", 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	if entries[8].SHA == c1SHA {
		t.Error("blame for line 8 should NOT be C1 (it's unmodified)")
	}
}

// Test 2: AI edit → AI edit above (with hook) → first edit shifted
// Both edits are committed with JSONL records. Git blame tracks the shift.
func TestBlameQuery_AIEditAboveShiftsFirst(t *testing.T) {
	dir := setupGitRepo(t)
	paths := gitPaths(dir)

	// Create a 10-line file and commit (C0)
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", content)
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "initial commit")

	// AI edit A: modify lines 7-8, commit as C1
	contentA := "line1\nline2\nline3\nline4\nline5\nline6\neditA7\neditA8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentA)
	writeFile(t, dir, ".blamebot/log/session.jsonl",
		`{"file":"main.go","lines":"7-8","ts":"2025-01-01T00:00:00Z","change":"edit A: lines 7-8","tool":"Edit"}`+"\n")
	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit A: lines 7-8")

	c1SHA := git.HeadSHA(dir)

	// AI edit B: insert 3 lines at line 2 (shifts everything below), commit as C2
	contentB := "line1\nnew1\nnew2\nnew3\nline2\nline3\nline4\nline5\nline6\neditA7\neditA8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentB)
	// Append record B to the JSONL
	f, err := os.OpenFile(filepath.Join(dir, ".blamebot/log/session.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"file":"main.go","lines":"2-4","ts":"2025-01-01T00:01:00Z","change":"edit B: insert 3 lines","tool":"Edit"}` + "\n")
	f.Close()

	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit B: insert 3 lines at top")

	c2SHA := git.HeadSHA(dir)

	// Rebuild index
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify record A got commit_sha = C1
	var shaA string
	err = db.QueryRow("SELECT commit_sha FROM reasons WHERE change LIKE '%edit A%'").Scan(&shaA)
	if err != nil {
		t.Fatal(err)
	}
	if shaA != c1SHA {
		t.Errorf("record A commit_sha = %q, want C1 %q", shaA, c1SHA)
	}

	// Verify record B got commit_sha = C2
	var shaB string
	err = db.QueryRow("SELECT commit_sha FROM reasons WHERE change LIKE '%edit B%'").Scan(&shaB)
	if err != nil {
		t.Fatal(err)
	}
	if shaB != c2SHA {
		t.Errorf("record B commit_sha = %q, want C2 %q", shaB, c2SHA)
	}

	// Git blame should show C1 at lines 10-11 (shifted from 7-8 by +3)
	entries, err := git.BlameFile(dir, "main.go")
	if err != nil {
		t.Fatal(err)
	}

	if entries[10].SHA != c1SHA {
		t.Errorf("line 10 blame: got %s, want C1 %s", entries[10].SHA, c1SHA)
	}
	if entries[11].SHA != c1SHA {
		t.Errorf("line 11 blame: got %s, want C1 %s", entries[11].SHA, c1SHA)
	}

	// Lines 2-4 should be from C2
	for i := 2; i <= 4; i++ {
		if entries[i].SHA != c2SHA {
			t.Errorf("line %d blame: got %s, want C2 %s", i, entries[i].SHA, c2SHA)
		}
	}
}

// Test 3: AI edit → manual edit above (no hook) → first edit shifted
// Only the AI edit has a JSONL record. Manual edit has no record.
// Git blame still correctly tracks the AI edit to its shifted position.
func TestBlameQuery_ManualEditAboveShiftsAIEdit(t *testing.T) {
	dir := setupGitRepo(t)
	paths := gitPaths(dir)

	// Create a 10-line file and commit (C0)
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", content)
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "initial commit")

	// AI edit: modify lines 7-8, commit with JSONL (C1)
	contentAI := "line1\nline2\nline3\nline4\nline5\nline6\nAI7\nAI8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentAI)
	writeFile(t, dir, ".blamebot/log/session.jsonl",
		`{"file":"main.go","lines":"7-8","ts":"2025-01-01T00:00:00Z","change":"AI edit","tool":"Edit"}`+"\n")
	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit lines 7-8")

	c1SHA := git.HeadSHA(dir)

	// Human inserts 3 lines at line 2 (NO JSONL record), commit (C2)
	contentManual := "line1\nhuman1\nhuman2\nhuman3\nline2\nline3\nline4\nline5\nline6\nAI7\nAI8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentManual)
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "manual edit: insert 3 lines at top")

	// Rebuild index
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify record has commit_sha = C1
	var commitSHA string
	err = db.QueryRow("SELECT commit_sha FROM reasons WHERE change = 'AI edit'").Scan(&commitSHA)
	if err != nil {
		t.Fatal(err)
	}
	if commitSHA != c1SHA {
		t.Errorf("commit_sha = %q, want %q", commitSHA, c1SHA)
	}

	// Git blame should show C1 at lines 10-11 (shifted from 7-8 by +3)
	entries, err := git.BlameFile(dir, "main.go")
	if err != nil {
		t.Fatal(err)
	}

	if entries[10].SHA != c1SHA {
		t.Errorf("line 10 blame: got %s, want C1 %s (AI edit shifted)", entries[10].SHA, c1SHA)
	}
	if entries[11].SHA != c1SHA {
		t.Errorf("line 11 blame: got %s, want C1 %s (AI edit shifted)", entries[11].SHA, c1SHA)
	}

	// Querying by commit_sha should find the record
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM reasons WHERE commit_sha = ?", c1SHA).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 record with commit_sha C1, got %d", count)
	}
}

// Test 4: Overlapping edits with supersession
// A edits L3-6, B edits L4-7 (overwrites part of A).
// After B, line 3 → C1 (A's surviving line), lines 4-7 → C2 (B).
func TestBlameQuery_OverlappingEdits(t *testing.T) {
	dir := setupGitRepo(t)
	paths := gitPaths(dir)

	// Create a 10-line file and commit (C0)
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", content)
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "initial commit")

	// AI edit A: modify lines 3-6, commit (C1)
	contentA := "line1\nline2\nA3\nA4\nA5\nA6\nline7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentA)
	writeFile(t, dir, ".blamebot/log/session.jsonl",
		`{"file":"main.go","lines":"3-6","ts":"2025-01-01T00:00:00Z","change":"edit A","tool":"Edit"}`+"\n")
	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit A: lines 3-6")

	c1SHA := git.HeadSHA(dir)

	// AI edit B: modify lines 4-7 (overwrites part of A), commit (C2)
	contentB := "line1\nline2\nA3\nB4\nB5\nB6\nB7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentB)
	f, err := os.OpenFile(filepath.Join(dir, ".blamebot/log/session.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"file":"main.go","lines":"4-7","ts":"2025-01-01T00:01:00Z","change":"edit B","tool":"Edit"}` + "\n")
	f.Close()

	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit B: lines 4-7")

	c2SHA := git.HeadSHA(dir)

	// Rebuild index
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Git blame: line 3 → C1 (A's surviving line)
	entries, err := git.BlameFile(dir, "main.go")
	if err != nil {
		t.Fatal(err)
	}

	if entries[3].SHA != c1SHA {
		t.Errorf("line 3 blame: got %s, want C1 %s", entries[3].SHA, c1SHA)
	}

	// Lines 4-7 → C2 (B's edits)
	for i := 4; i <= 7; i++ {
		if entries[i].SHA != c2SHA {
			t.Errorf("line %d blame: got %s, want C2 %s", i, entries[i].SHA, c2SHA)
		}
	}

	// Querying line 3 should find record A via C1
	var changeA string
	err = db.QueryRow("SELECT change FROM reasons WHERE commit_sha = ?", c1SHA).Scan(&changeA)
	if err != nil {
		t.Fatal(err)
	}
	if changeA != "edit A" {
		t.Errorf("record for C1 change = %q, want 'edit A'", changeA)
	}

	// Querying line 5 should find record B via C2
	var changeB string
	err = db.QueryRow("SELECT change FROM reasons WHERE commit_sha = ?", c2SHA).Scan(&changeB)
	if err != nil {
		t.Fatal(err)
	}
	if changeB != "edit B" {
		t.Errorf("record for C2 change = %q, want 'edit B'", changeB)
	}
}

// Test 5: Overlapping edits + manual shift
// Same as test 4, then human inserts 2 lines at line 1.
// Line 5 → C1 (was L3, shifted +2), lines 6-9 → C2 (was L4-7, shifted +2).
func TestBlameQuery_OverlappingEditsWithManualShift(t *testing.T) {
	dir := setupGitRepo(t)
	paths := gitPaths(dir)

	// Create a 10-line file and commit (C0)
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", content)
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "initial commit")

	// AI edit A: modify lines 3-6, commit (C1)
	contentA := "line1\nline2\nA3\nA4\nA5\nA6\nline7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentA)
	writeFile(t, dir, ".blamebot/log/session.jsonl",
		`{"file":"main.go","lines":"3-6","ts":"2025-01-01T00:00:00Z","change":"edit A","tool":"Edit"}`+"\n")
	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit A: lines 3-6")

	c1SHA := git.HeadSHA(dir)

	// AI edit B: modify lines 4-7 (overwrites part of A), commit (C2)
	contentB := "line1\nline2\nA3\nB4\nB5\nB6\nB7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentB)
	f, err := os.OpenFile(filepath.Join(dir, ".blamebot/log/session.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"file":"main.go","lines":"4-7","ts":"2025-01-01T00:01:00Z","change":"edit B","tool":"Edit"}` + "\n")
	f.Close()

	gitRun(t, dir, "add", "main.go", ".blamebot/log/session.jsonl")
	gitRun(t, dir, "commit", "-m", "AI edit B: lines 4-7")

	c2SHA := git.HeadSHA(dir)

	// Human inserts 2 lines at the very beginning (NO JSONL), commit (C3)
	contentManual := "human1\nhuman2\nline1\nline2\nA3\nB4\nB5\nB6\nB7\nline8\nline9\nline10\n"
	writeFile(t, dir, "main.go", contentManual)
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "manual: insert 2 lines at top")

	// Rebuild index
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Git blame: line 5 → C1 (was L3, shifted +2)
	entries, err := git.BlameFile(dir, "main.go")
	if err != nil {
		t.Fatal(err)
	}

	if entries[5].SHA != c1SHA {
		t.Errorf("line 5 blame: got %s, want C1 %s (A3 shifted +2)", entries[5].SHA, c1SHA)
	}

	// Lines 6-9 → C2 (was L4-7, shifted +2)
	for i := 6; i <= 9; i++ {
		if entries[i].SHA != c2SHA {
			t.Errorf("line %d blame: got %s, want C2 %s (B shifted +2)", i, entries[i].SHA, c2SHA)
		}
	}

	// The commit_sha linkage still works — querying C1 finds record A
	var changeA string
	err = db.QueryRow("SELECT change FROM reasons WHERE commit_sha = ?", c1SHA).Scan(&changeA)
	if err != nil {
		t.Fatal(err)
	}
	if changeA != "edit A" {
		t.Errorf("C1 record change = %q, want 'edit A'", changeA)
	}

	// Querying C2 finds record B
	var changeB string
	err = db.QueryRow("SELECT change FROM reasons WHERE commit_sha = ?", c2SHA).Scan(&changeB)
	if err != nil {
		t.Fatal(err)
	}
	if changeB != "edit B" {
		t.Errorf("C2 record change = %q, want 'edit B'", changeB)
	}
}

// Test: HEAD staleness detection triggers rebuild
func TestHeadSHAChanged_TriggersRebuild(t *testing.T) {
	dir := setupGitRepo(t)
	paths := gitPaths(dir)

	// Create initial content and commit
	writeFile(t, dir, "main.go", "hello\n")
	writeFile(t, dir, ".blamebot/log/session.jsonl",
		`{"file":"main.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"test"}`+"\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")

	// Build index (stores HEAD SHA in meta table)
	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Index should NOT be stale yet
	if IsStale(paths) {
		t.Error("index should not be stale immediately after rebuild")
	}

	// Make another commit (changes HEAD)
	writeFile(t, dir, "main.go", "hello world\n")
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "second commit")

	// Now index should be stale (HEAD changed)
	if !IsStale(paths) {
		t.Error("index should be stale after HEAD changed")
	}
}

// Test: commit_sha populated correctly for multi-line JSONL
func TestRebuild_CommitSHAPopulated(t *testing.T) {
	dir := setupGitRepo(t)
	paths := gitPaths(dir)

	// Create file and JSONL with 2 records, commit together
	writeFile(t, dir, "main.go", "a\nb\n")
	writeFile(t, dir, ".blamebot/log/session.jsonl",
		`{"file":"main.go","lines":"1","ts":"2025-01-01T00:00:00Z","change":"first"}`+"\n"+
			`{"file":"main.go","lines":"2","ts":"2025-01-01T00:01:00Z","change":"second"}`+"\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")

	sha := git.HeadSHA(dir)

	db, err := Rebuild(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Both records should have the same commit_sha (same commit)
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
		if s != sha {
			t.Errorf("record %d commit_sha = %q, want %q", i+1, s, sha)
		}
	}
}
