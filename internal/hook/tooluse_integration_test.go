package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jensroland/git-blamebot/internal/provenance"
)

// TestHandlePostToolUse_EndToEnd tests the full pipeline:
// construct a Claude Code hook payload → HandlePostToolUse() → verify pending edit output.
func TestHandlePostToolUse_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Write current_prompt.json with session state
	ps := promptState{
		Prompt:         "fix the bug in handler",
		Author:         "claude-test",
		SessionID:      "session-abc",
		TranscriptPath: "/transcript/path",
	}
	psBytes, _ := json.Marshal(ps)
	_ = os.WriteFile(filepath.Join(tmpDir, ".git", "blamebot", "current_prompt.json"), psBytes, 0o644)

	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)

	payload := map[string]interface{}{
		"tool_name": "Edit",
		"tool_input": map[string]interface{}{
			"file_path":  filepath.Join(tmpDir, "src", "main.go"),
			"old_string": "fmt.Println(\"hello\")\nfmt.Println(\"world\")",
			"new_string": "fmt.Println(\"hello\")\nfmt.Println(\"goodbye\")",
		},
		"tool_response": map[string]interface{}{
			"structuredPatch": []interface{}{
				map[string]interface{}{
					"oldStart": float64(10),
					"oldLines": float64(2),
					"newStart": float64(10),
					"newLines": float64(2),
				},
			},
		},
		"tool_use_id": "tool-123",
	}

	payloadBytes, _ := json.Marshal(payload)
	if err := HandlePostToolUse(bytes.NewReader(payloadBytes)); err != nil {
		t.Fatal(err)
	}

	edits, err := provenance.ReadAllPending(filepath.Join(tmpDir, ".git"))
	if err != nil || len(edits) != 1 {
		t.Fatalf("expected 1 pending edit, got %d (err=%v)", len(edits), err)
	}

	pe := edits[0]

	if pe.File != "src/main.go" {
		t.Errorf("file = %q, want %q", pe.File, "src/main.go")
	}
	if pe.Lines.IsEmpty() {
		t.Fatal("lines should not be empty")
	}
	gotLines := pe.Lines.Lines()
	if len(gotLines) != 1 || gotLines[0] != 12 {
		t.Errorf("lines = %v, want [12]", gotLines)
	}
	if pe.Hunk == nil {
		t.Fatal("hunk should not be nil")
	}
	if pe.Hunk.OldStart != 11 || pe.Hunk.OldLines != 2 {
		t.Errorf("hunk = %+v, want OldStart=11 OldLines=2", *pe.Hunk)
	}
	if pe.ContentHash == "" {
		t.Error("content_hash should not be empty")
	}
	if pe.Prompt != "fix the bug in handler" {
		t.Errorf("prompt = %q", pe.Prompt)
	}
	if pe.Author != "claude-test" {
		t.Errorf("author = %q", pe.Author)
	}
	if pe.Session != "session-abc" {
		t.Errorf("session = %q", pe.Session)
	}
	if pe.Trace != "/transcript/path#tool-123" {
		t.Errorf("trace = %q, want %q", pe.Trace, "/transcript/path#tool-123")
	}
	if pe.Tool != "Edit" {
		t.Errorf("tool = %q", pe.Tool)
	}
	if pe.Change == "" {
		t.Error("change should not be empty")
	}
}

// TestHandlePostToolUse_WriteCreatesFullRange tests that a Write tool
// produces a full-file LineSet and correct hunk metadata.
func TestHandlePostToolUse_WriteCreatesFullRange(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	ps := promptState{Author: "test"}
	psBytes, _ := json.Marshal(ps)
	_ = os.WriteFile(filepath.Join(tmpDir, ".git", "blamebot", "current_prompt.json"), psBytes, 0o644)

	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)

	payload := map[string]interface{}{
		"tool_name": "Write",
		"tool_input": map[string]interface{}{
			"file_path": filepath.Join(tmpDir, "new_file.go"),
			"content":   "package main\n\nfunc main() {\n}\n",
		},
	}

	payloadBytes, _ := json.Marshal(payload)
	if err := HandlePostToolUse(bytes.NewReader(payloadBytes)); err != nil {
		t.Fatal(err)
	}

	edits, err := provenance.ReadAllPending(filepath.Join(tmpDir, ".git"))
	if err != nil || len(edits) != 1 {
		t.Fatalf("expected 1 pending edit, got %d", len(edits))
	}

	pe := edits[0]
	gotLines := pe.Lines.Lines()
	if len(gotLines) != 5 {
		t.Errorf("lines = %v, want [1 2 3 4 5]", gotLines)
	}
	if pe.Hunk == nil {
		t.Fatal("hunk should not be nil for Write")
	}
	if pe.Hunk.OldLines != 0 || pe.Hunk.NewLines != 5 {
		t.Errorf("hunk = %+v, want OldLines=0 NewLines=5", *pe.Hunk)
	}
}

// TestHandlePostToolUse_NotInitialized verifies that HandlePostToolUse
// returns nil without writing anything when not initialized.
func TestHandlePostToolUse_NotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	// Create bare .git but NO provenance branch and no .blamebot/
	_ = os.MkdirAll(filepath.Join(tmpDir, ".git", "blamebot"), 0o755)

	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)

	payload := `{"tool_name":"Edit","tool_input":{"file_path":"x.go","old_string":"a","new_string":"b"}}`
	if err := HandlePostToolUse(bytes.NewReader([]byte(payload))); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if provenance.HasPending(filepath.Join(tmpDir, ".git")) {
		t.Error("pending edits should not exist when not initialized")
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")
	if err := provenance.InitBranch(dir); err != nil {
		t.Fatalf("InitBranch: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(dir, ".git", "blamebot"), 0o755)
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
