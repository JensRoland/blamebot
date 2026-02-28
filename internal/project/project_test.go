package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewPaths(t *testing.T) {
	root := t.TempDir()
	// Create .git/ directory so resolveGitDir returns <root>/.git
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := NewPaths(root)

	if p.Root != root {
		t.Errorf("Root = %q, want %q", p.Root, root)
	}
	if want := filepath.Join(root, ".git"); p.GitDir != want {
		t.Errorf("GitDir = %q, want %q", p.GitDir, want)
	}
	if want := filepath.Join(root, ".git", "blamebot", "pending"); p.PendingDir != want {
		t.Errorf("PendingDir = %q, want %q", p.PendingDir, want)
	}
	if want := filepath.Join(root, ".git", "blamebot"); p.CacheDir != want {
		t.Errorf("CacheDir = %q, want %q", p.CacheDir, want)
	}
	if want := filepath.Join(root, ".git", "blamebot", "index.db"); p.IndexDB != want {
		t.Errorf("IndexDB = %q, want %q", p.IndexDB, want)
	}
}

func TestResolveGitDir_NormalDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveGitDir(root)
	want := filepath.Join(root, ".git")
	if got != want {
		t.Errorf("resolveGitDir() = %q, want %q", got, want)
	}
}

func TestResolveGitDir_Worktree(t *testing.T) {
	t.Run("absolute_path", func(t *testing.T) {
		root := t.TempDir()
		absTarget := "/some/path/to/gitdir"
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+absTarget+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := resolveGitDir(root)
		if got != absTarget {
			t.Errorf("resolveGitDir() = %q, want %q", got, absTarget)
		}
	})

	t.Run("relative_path", func(t *testing.T) {
		root := t.TempDir()
		relTarget := "../other-repo/.git/worktrees/my-branch"
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+relTarget+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := resolveGitDir(root)
		want := filepath.Join(root, relTarget)
		if got != want {
			t.Errorf("resolveGitDir() = %q, want %q", got, want)
		}
	})
}

func TestResolveGitDir_Missing(t *testing.T) {
	root := t.TempDir()
	// No .git at all

	got := resolveGitDir(root)
	want := filepath.Join(root, ".git")
	if got != want {
		t.Errorf("resolveGitDir() = %q, want %q (default fallback)", got, want)
	}
}

func TestIsInitialized(t *testing.T) {
	t.Run("initialized", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".blamebot"), 0o755); err != nil {
			t.Fatal(err)
		}

		if !IsInitialized(root) {
			t.Error("IsInitialized() = false, want true")
		}
	})

	t.Run("not_initialized", func(t *testing.T) {
		root := t.TempDir()

		if IsInitialized(root) {
			t.Error("IsInitialized() = true, want false")
		}
	})
}

func TestFindRoot_WithEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)

	got, err := FindRoot()
	if err != nil {
		t.Fatalf("FindRoot() error: %v", err)
	}
	if got != tmpDir {
		t.Errorf("FindRoot() = %q, want %q", got, tmpDir)
	}
}

func TestFindRoot_GitFallback(t *testing.T) {
	// Unset CLAUDE_PROJECT_DIR so FindRoot falls back to git
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	// Our test process is already in a git repo,
	// so just verify FindRoot returns a non-empty valid path.
	got, err := FindRoot()
	if err != nil {
		t.Fatalf("FindRoot() error: %v", err)
	}
	if got == "" {
		t.Error("FindRoot() returned empty string")
	}
	// Verify it's actually a directory
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("FindRoot() returned non-existent path: %s", got)
	}
	if !info.IsDir() {
		t.Errorf("FindRoot() returned non-directory: %s", got)
	}
}

func TestResolveGitDir_InvalidGitFile(t *testing.T) {
	// .git is a file but doesn't start with "gitdir: "
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("not a gitdir pointer\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveGitDir(root)
	want := filepath.Join(root, ".git")
	if got != want {
		t.Errorf("resolveGitDir() = %q, want %q (fallback for invalid content)", got, want)
	}
}

