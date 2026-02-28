package provenance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/record"
)

// initTestRepo creates a git repo in a temp dir and returns the root path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	// Need at least one commit so HEAD exists
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestInitBranch(t *testing.T) {
	root := initTestRepo(t)

	if BranchExists(root) {
		t.Fatal("branch should not exist before init")
	}

	if err := InitBranch(root); err != nil {
		t.Fatalf("InitBranch: %v", err)
	}

	if !BranchExists(root) {
		t.Fatal("branch should exist after init")
	}

	// Idempotent
	if err := InitBranch(root); err != nil {
		t.Fatalf("InitBranch (idempotent): %v", err)
	}
}

func TestBranchTipSHA(t *testing.T) {
	root := initTestRepo(t)

	if sha := BranchTipSHA(root); sha != "" {
		t.Fatalf("expected empty SHA before init, got %s", sha)
	}

	InitBranch(root)

	sha := BranchTipSHA(root)
	if sha == "" {
		t.Fatal("expected non-empty SHA after init")
	}
	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %d: %s", len(sha), sha)
	}
}

func TestWriteAndReadManifest(t *testing.T) {
	root := initTestRepo(t)
	gitDir := filepath.Join(root, ".git")
	InitBranch(root)

	m := Manifest{
		ID:        "test-uuid-1234",
		Author:    "Test User",
		Timestamp: "2026-02-28T12:00:00Z",
		Edits: []ManifestEdit{
			{
				File:        "src/main.go",
				Lines:       lineset.FromRange(5, 7),
				Hunk:        &record.HunkInfo{OldStart: 5, OldLines: 3, NewStart: 5, NewLines: 4},
				ContentHash: "abc123",
				Prompt:      "fix the bug",
				Reason:      "Fixed off-by-one error",
				Change:      "i <= n → i < n",
				Tool:        "Edit",
				Session:     "session-1",
				Trace:       "/tmp/transcript.jsonl#toolu_123",
			},
		},
	}

	if err := WriteManifest(root, gitDir, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Read back
	got, err := ReadManifest(root, "test-uuid-1234")
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}

	if got.ID != m.ID {
		t.Errorf("ID: got %s, want %s", got.ID, m.ID)
	}
	if got.Author != m.Author {
		t.Errorf("Author: got %s, want %s", got.Author, m.Author)
	}
	if len(got.Edits) != 1 {
		t.Fatalf("Edits: got %d, want 1", len(got.Edits))
	}
	if got.Edits[0].File != "src/main.go" {
		t.Errorf("Edit.File: got %s, want src/main.go", got.Edits[0].File)
	}
	if got.Edits[0].Reason != "Fixed off-by-one error" {
		t.Errorf("Edit.Reason: got %s, want Fixed off-by-one error", got.Edits[0].Reason)
	}
}

func TestListAndReadAllManifests(t *testing.T) {
	root := initTestRepo(t)
	gitDir := filepath.Join(root, ".git")
	InitBranch(root)

	// Write two manifests
	for _, id := range []string{"aaa-111", "bbb-222"} {
		m := Manifest{
			ID:        id,
			Author:    "Test",
			Timestamp: "2026-02-28T12:00:00Z",
			Edits: []ManifestEdit{
				{File: "file-" + id + ".go", Change: "edit " + id},
			},
		}
		if err := WriteManifest(root, gitDir, m); err != nil {
			t.Fatalf("WriteManifest(%s): %v", id, err)
		}
	}

	ids, err := ListManifests(root)
	if err != nil {
		t.Fatalf("ListManifests: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ListManifests: got %d, want 2", len(ids))
	}

	manifests, err := ReadAllManifests(root)
	if err != nil {
		t.Fatalf("ReadAllManifests: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("ReadAllManifests: got %d, want 2", len(manifests))
	}
}

func TestUpdateManifestCommitSHA(t *testing.T) {
	root := initTestRepo(t)
	gitDir := filepath.Join(root, ".git")
	InitBranch(root)

	m := Manifest{
		ID:        "update-test",
		Author:    "Test",
		Timestamp: "2026-02-28T12:00:00Z",
		Edits:     []ManifestEdit{{File: "test.go"}},
	}
	WriteManifest(root, gitDir, m)

	if err := UpdateManifestCommitSHA(root, gitDir, "update-test", "abc123def456"); err != nil {
		t.Fatalf("UpdateManifestCommitSHA: %v", err)
	}

	got, _ := ReadManifest(root, "update-test")
	if got.CommitSHA != "abc123def456" {
		t.Errorf("CommitSHA: got %s, want abc123def456", got.CommitSHA)
	}
}

func TestWriteAndReadTrace(t *testing.T) {
	root := initTestRepo(t)
	gitDir := filepath.Join(root, ".git")
	InitBranch(root)

	contexts := map[string]string{
		"toolu_123": "thinking about the change...",
		"toolu_456": "decided to refactor...",
	}

	if err := WriteTrace(root, gitDir, "session-abc", contexts); err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}

	got, err := ReadTrace(root, "session-abc")
	if err != nil {
		t.Fatalf("ReadTrace: %v", err)
	}
	if got["toolu_123"] != "thinking about the change..." {
		t.Errorf("trace toolu_123: got %q", got["toolu_123"])
	}

	// Test merge behavior — write more contexts to same session
	moreContexts := map[string]string{
		"toolu_789": "another context",
	}
	if err := WriteTrace(root, gitDir, "session-abc", moreContexts); err != nil {
		t.Fatalf("WriteTrace (merge): %v", err)
	}

	got, _ = ReadTrace(root, "session-abc")
	if len(got) != 3 {
		t.Fatalf("expected 3 traces after merge, got %d", len(got))
	}
}

func TestPendingCRUD(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	os.MkdirAll(gitDir, 0o755)

	// Initially empty
	if HasPending(gitDir) {
		t.Fatal("should not have pending initially")
	}

	// Write two pending edits
	pe1 := PendingEdit{ID: "edit-1", Ts: "2026-02-28T12:00:00Z", File: "a.go", Author: "Test"}
	pe2 := PendingEdit{ID: "edit-2", Ts: "2026-02-28T12:01:00Z", File: "b.go", Author: "Test"}

	if err := WritePending(gitDir, pe1); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := WritePending(gitDir, pe2); err != nil {
		t.Fatalf("WritePending: %v", err)
	}

	if !HasPending(gitDir) {
		t.Fatal("should have pending after write")
	}

	// Read all — should be sorted by timestamp
	edits, err := ReadAllPending(gitDir)
	if err != nil {
		t.Fatalf("ReadAllPending: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}
	if edits[0].ID != "edit-1" || edits[1].ID != "edit-2" {
		t.Errorf("unexpected order: %s, %s", edits[0].ID, edits[1].ID)
	}

	// Clear
	if err := ClearPending(gitDir); err != nil {
		t.Fatalf("ClearPending: %v", err)
	}
	if HasPending(gitDir) {
		t.Fatal("should not have pending after clear")
	}
}

func TestWorkingTreeNotAffected(t *testing.T) {
	root := initTestRepo(t)
	gitDir := filepath.Join(root, ".git")
	InitBranch(root)

	// Create a file in the working tree
	os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o644)

	// Write a manifest — should NOT affect working tree
	m := Manifest{
		ID:        "wt-test",
		Author:    "Test",
		Timestamp: "2026-02-28T12:00:00Z",
		Edits:     []ManifestEdit{{File: "test.go"}},
	}
	WriteManifest(root, gitDir, m)

	// Verify hello.txt still exists and is unchanged
	data, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("hello.txt should still exist: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("hello.txt content changed: %s", data)
	}

	// Verify no manifests directory in working tree
	if _, err := os.Stat(filepath.Join(root, "manifests")); !os.IsNotExist(err) {
		t.Fatal("manifests/ should NOT appear in working tree")
	}

	// Verify HEAD is still on the original branch
	cmd := exec.Command("git", "symbolic-ref", "HEAD")
	cmd.Dir = root
	out, _ := cmd.Output()
	ref := strings.TrimSpace(string(out))
	if ref == RefPath {
		t.Fatal("HEAD should NOT be on the provenance branch")
	}
}

func TestReadBlobNonexistent(t *testing.T) {
	root := initTestRepo(t)
	InitBranch(root)

	_, err := ReadBlob(root, "manifests/does-not-exist.json")
	if err == nil {
		t.Fatal("expected error reading nonexistent blob")
	}
}

func TestReadManifestNonexistent(t *testing.T) {
	root := initTestRepo(t)
	InitBranch(root)

	_, err := ReadManifest(root, "nonexistent")
	if err == nil {
		t.Fatal("expected error reading nonexistent manifest")
	}
}

func TestListManifestsEmptyBranch(t *testing.T) {
	root := initTestRepo(t)
	InitBranch(root)

	ids, err := ListManifests(root)
	if err != nil {
		t.Fatalf("ListManifests on empty branch: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 manifests on empty branch, got %d", len(ids))
	}
}

func TestListManifestsNoBranch(t *testing.T) {
	root := initTestRepo(t)

	ids, err := ListManifests(root)
	if err != nil {
		t.Fatalf("ListManifests with no branch: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 manifests with no branch, got %d", len(ids))
	}
}
