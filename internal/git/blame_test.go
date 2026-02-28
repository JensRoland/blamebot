package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupGitRepo creates a temp git repo with an initial commit containing a file.
func setupGitRepo(t *testing.T, fileName string, content string) string {
	t.Helper()
	dir := t.TempDir()

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

	filePath := filepath.Join(dir, fileName)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", fileName)
	run("commit", "-m", "initial commit")

	return dir
}

func TestBlameFile(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5\n"
	dir := setupGitRepo(t, "test.txt", content)

	entries, err := BlameFile(dir, "test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}

	// All lines should have the same commit SHA (initial commit)
	var sha string
	for _, entry := range entries {
		if sha == "" {
			sha = entry.SHA
		}
		if entry.SHA != sha {
			t.Errorf("expected all lines to have SHA %s, got %s", sha, entry.SHA)
		}
		if entry.IsUncommitted() {
			t.Error("expected committed entry")
		}
	}
}

func TestBlameRange(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5\n"
	dir := setupGitRepo(t, "test.txt", content)

	entries, err := BlameRange(dir, "test.txt", 2, 4)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	for line, entry := range entries {
		if line < 2 || line > 4 {
			t.Errorf("unexpected line %d in range result", line)
		}
		if entry.Line != line {
			t.Errorf("entry.Line = %d, want %d", entry.Line, line)
		}
	}
}

func TestBlameFile_MultipleCommits(t *testing.T) {
	dir := setupGitRepo(t, "test.txt", "line1\nline2\nline3\n")

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

	// Modify line 2 in a second commit
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("line1\nmodified\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "test.txt")
	run("commit", "-m", "modify line 2")

	entries, err := BlameFile(dir, "test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	// Lines 1 and 3 should have the initial commit, line 2 should have the second commit
	sha1 := entries[1].SHA
	sha2 := entries[2].SHA
	sha3 := entries[3].SHA

	if sha1 != sha3 {
		t.Errorf("lines 1 and 3 should have same SHA: %s vs %s", sha1, sha3)
	}
	if sha2 == sha1 {
		t.Error("line 2 should have different SHA from line 1")
	}
}

func TestBlameFile_Uncommitted(t *testing.T) {
	dir := setupGitRepo(t, "test.txt", "line1\nline2\nline3\n")

	// Modify line 2 without committing
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("line1\nuncommitted\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := BlameFile(dir, "test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	// Line 2 should be uncommitted
	if !entries[2].IsUncommitted() {
		t.Errorf("line 2 should be uncommitted, got SHA %s", entries[2].SHA)
	}

	// Lines 1 and 3 should be committed
	if entries[1].IsUncommitted() {
		t.Error("line 1 should be committed")
	}
	if entries[3].IsUncommitted() {
		t.Error("line 3 should be committed")
	}
}

func TestHeadSHA(t *testing.T) {
	dir := setupGitRepo(t, "test.txt", "hello\n")

	sha := HeadSHA(dir)
	if sha == "" {
		t.Error("expected non-empty HEAD SHA")
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %d chars: %s", len(sha), sha)
	}
}

func TestBlameFile_LineShift(t *testing.T) {
	// Test that git blame correctly tracks lines through insertions
	dir := setupGitRepo(t, "test.txt", "line1\nline2\nline3\n")

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

	// Get initial commit SHA
	initSHA := HeadSHA(dir)

	// Insert lines at the beginning (shifting everything down)
	if err := os.WriteFile(filepath.Join(dir, "test.txt"),
		[]byte("new1\nnew2\nnew3\nline1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "test.txt")
	run("commit", "-m", "insert 3 lines at top")

	insertSHA := HeadSHA(dir)

	entries, err := BlameFile(dir, "test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(entries))
	}

	// Lines 1-3 should be from the insert commit
	for i := 1; i <= 3; i++ {
		if entries[i].SHA != insertSHA {
			t.Errorf("line %d: expected SHA %s (insert), got %s", i, insertSHA, entries[i].SHA)
		}
	}

	// Lines 4-6 should be from the initial commit (shifted from 1-3)
	for i := 4; i <= 6; i++ {
		if entries[i].SHA != initSHA {
			t.Errorf("line %d: expected SHA %s (initial), got %s", i, initSHA, entries[i].SHA)
		}
	}
}

func TestParsePorcelainBlame(t *testing.T) {
	// Minimal porcelain output for 2 lines from different commits
	out := fmt.Sprintf(
		"%s 1 1 1\nauthor Test\ncommitter Test\nsummary commit 1\nfilename test.txt\n\tline1\n"+
			"%s 1 2 1\nauthor Test\ncommitter Test\nsummary commit 2\nfilename test.txt\n\tline2\n",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)

	entries := parsePorcelainBlame([]byte(out))

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[1].SHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("line 1 SHA = %s", entries[1].SHA)
	}
	if entries[2].SHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("line 2 SHA = %s", entries[2].SHA)
	}
}
