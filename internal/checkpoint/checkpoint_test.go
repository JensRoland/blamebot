package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cp := Checkpoint{
		Kind:       "pre-edit",
		File:       "main.go",
		ContentSHA: "abc123",
		ToolUseID:  "tool-1",
		Ts:         "2024-01-01T00:00:00Z",
	}

	id, err := WriteCheckpoint(dir, cp)
	if err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	all, err := ReadAllCheckpoints(dir)
	if err != nil {
		t.Fatalf("ReadAllCheckpoints: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(all))
	}
	if all[0].Kind != "pre-edit" {
		t.Errorf("expected kind pre-edit, got %s", all[0].Kind)
	}
	if all[0].File != "main.go" {
		t.Errorf("expected file main.go, got %s", all[0].File)
	}
	if all[0].ContentSHA != "abc123" {
		t.Errorf("expected content_sha abc123, got %s", all[0].ContentSHA)
	}
}

func TestReadAllCheckpoints_SortedByTimestamp(t *testing.T) {
	dir := t.TempDir()

	WriteCheckpoint(dir, Checkpoint{Kind: "post-edit", Ts: "2024-01-03T00:00:00Z", File: "a.go"})
	WriteCheckpoint(dir, Checkpoint{Kind: "pre-edit", Ts: "2024-01-01T00:00:00Z", File: "a.go"})
	WriteCheckpoint(dir, Checkpoint{Kind: "post-edit", Ts: "2024-01-02T00:00:00Z", File: "a.go"})

	all, err := ReadAllCheckpoints(dir)
	if err != nil {
		t.Fatalf("ReadAllCheckpoints: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(all))
	}
	if all[0].Ts != "2024-01-01T00:00:00Z" {
		t.Errorf("expected first ts 2024-01-01, got %s", all[0].Ts)
	}
	if all[2].Ts != "2024-01-03T00:00:00Z" {
		t.Errorf("expected last ts 2024-01-03, got %s", all[2].Ts)
	}
}

func TestWriteBlob_Deduplication(t *testing.T) {
	dir := t.TempDir()

	sha1, err := WriteBlob(dir, "hello world")
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	sha2, err := WriteBlob(dir, "hello world")
	if err != nil {
		t.Fatalf("WriteBlob (second): %v", err)
	}

	if sha1 != sha2 {
		t.Errorf("expected same SHA, got %s and %s", sha1, sha2)
	}

	// Verify only one blob file
	blobDir := filepath.Join(dir, "blobs")
	entries, _ := os.ReadDir(blobDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 blob file, got %d", len(entries))
	}
}

func TestWriteBlob_DifferentContent(t *testing.T) {
	dir := t.TempDir()

	sha1, _ := WriteBlob(dir, "hello")
	sha2, _ := WriteBlob(dir, "world")

	if sha1 == sha2 {
		t.Error("expected different SHAs for different content")
	}

	blobDir := filepath.Join(dir, "blobs")
	entries, _ := os.ReadDir(blobDir)
	if len(entries) != 2 {
		t.Errorf("expected 2 blob files, got %d", len(entries))
	}
}

func TestReadBlob(t *testing.T) {
	dir := t.TempDir()

	content := "package main\n\nfunc main() {}\n"
	sha, _ := WriteBlob(dir, content)

	got, err := ReadBlob(dir, sha)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if got != content {
		t.Errorf("ReadBlob returned wrong content: %q", got)
	}
}

func TestCheckpointsForFile(t *testing.T) {
	checkpoints := []Checkpoint{
		{File: "a.go", Kind: "pre-edit"},
		{File: "b.go", Kind: "pre-edit"},
		{File: "a.go", Kind: "post-edit"},
		{File: "c.go", Kind: "pre-edit"},
	}

	aCheckpoints := CheckpointsForFile(checkpoints, "a.go")
	if len(aCheckpoints) != 2 {
		t.Errorf("expected 2 checkpoints for a.go, got %d", len(aCheckpoints))
	}

	bCheckpoints := CheckpointsForFile(checkpoints, "b.go")
	if len(bCheckpoints) != 1 {
		t.Errorf("expected 1 checkpoint for b.go, got %d", len(bCheckpoints))
	}

	dCheckpoints := CheckpointsForFile(checkpoints, "d.go")
	if len(dCheckpoints) != 0 {
		t.Errorf("expected 0 checkpoints for d.go, got %d", len(dCheckpoints))
	}
}

func TestClearAll(t *testing.T) {
	dir := t.TempDir()

	WriteCheckpoint(dir, Checkpoint{Kind: "pre-edit", Ts: "2024-01-01T00:00:00Z"})
	WriteBlob(dir, "hello")

	err := ClearAll(dir)
	if err != nil {
		t.Fatalf("ClearAll: %v", err)
	}

	// Directory should be gone
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func TestReadAllCheckpoints_EmptyDir(t *testing.T) {
	all, err := ReadAllCheckpoints("/nonexistent/path")
	if err != nil {
		t.Fatalf("ReadAllCheckpoints should not error on missing dir: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 checkpoints, got %d", len(all))
	}
}
