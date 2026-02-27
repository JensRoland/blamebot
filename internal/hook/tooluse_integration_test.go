package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/record"
)

// TestHandlePostToolUse_EndToEnd tests the full pipeline:
// construct a Claude Code hook payload → HandlePostToolUse() → verify JSONL output.
func TestHandlePostToolUse_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()

	// Create the required directory structure
	if err := os.MkdirAll(filepath.Join(tmpDir, ".blamebot", "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git", "blamebot"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write current_prompt.json with session state
	ps := promptState{
		Prompt:         "fix the bug in handler",
		SessionFile:    "test-session.jsonl",
		Author:         "claude-test",
		SessionID:      "session-abc",
		TranscriptPath: "/transcript/path",
	}
	psBytes, _ := json.Marshal(ps)
	if err := os.WriteFile(filepath.Join(tmpDir, ".git", "blamebot", "current_prompt.json"), psBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Set CLAUDE_PROJECT_DIR so FindRoot() uses our temp dir
	old := os.Getenv("CLAUDE_PROJECT_DIR")
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)
	defer func() { _ = os.Setenv("CLAUDE_PROJECT_DIR", old) }()

	// Build an Edit tool payload
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
	err := HandlePostToolUse(bytes.NewReader(payloadBytes))
	if err != nil {
		t.Fatal(err)
	}

	// Read the output JSONL file
	sessionPath := filepath.Join(tmpDir, ".blamebot", "log", "test-session.jsonl")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("session file not created: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 record, got %d", len(lines))
	}

	// Parse the output record
	var rec record.Record
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("failed to parse record: %v", err)
	}

	// Verify file path is relativized
	if rec.File != "src/main.go" {
		t.Errorf("file = %q, want %q", rec.File, "src/main.go")
	}

	// Verify lines — LCS should identify only line 11 as changed
	// (second line of the 2-line edit at newStart=10)
	if rec.Lines.IsEmpty() {
		t.Fatal("lines should not be empty")
	}
	gotLines := rec.Lines.Lines()
	if len(gotLines) != 1 || gotLines[0] != 11 {
		t.Errorf("lines = %v, want [11]", gotLines)
	}

	// Verify hunk metadata
	if rec.Hunk == nil {
		t.Fatal("hunk should not be nil")
	}
	if rec.Hunk.OldStart != 10 || rec.Hunk.OldLines != 2 {
		t.Errorf("hunk = %+v, want OldStart=10 OldLines=2", *rec.Hunk)
	}
	if rec.Hunk.NewStart != 10 || rec.Hunk.NewLines != 2 {
		t.Errorf("hunk = %+v, want NewStart=10 NewLines=2", *rec.Hunk)
	}

	// Verify content hash is set
	if rec.ContentHash == "" {
		t.Error("content_hash should not be empty")
	}

	// Verify prompt state was carried through
	if rec.Prompt != "fix the bug in handler" {
		t.Errorf("prompt = %q", rec.Prompt)
	}
	if rec.Author != "claude-test" {
		t.Errorf("author = %q", rec.Author)
	}
	if rec.Session != "session-abc" {
		t.Errorf("session = %q", rec.Session)
	}

	// Verify trace reference includes tool_use_id
	if rec.Trace != "/transcript/path#tool-123" {
		t.Errorf("trace = %q, want %q", rec.Trace, "/transcript/path#tool-123")
	}

	// Verify tool name
	if rec.Tool != "Edit" {
		t.Errorf("tool = %q", rec.Tool)
	}

	// Verify change summary is not empty
	if rec.Change == "" {
		t.Error("change should not be empty")
	}

	// Verify timestamp is set
	if rec.Ts == "" {
		t.Error("ts should not be empty")
	}
}

// TestHandlePostToolUse_WriteCreatesFullRange tests that a Write tool
// produces a full-file LineSet and correct hunk metadata.
func TestHandlePostToolUse_WriteCreatesFullRange(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(tmpDir, ".blamebot", "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git", "blamebot"), 0o755); err != nil {
		t.Fatal(err)
	}

	ps := promptState{
		SessionFile: "write-session.jsonl",
		Author:      "test",
	}
	psBytes, _ := json.Marshal(ps)
	if err := os.WriteFile(filepath.Join(tmpDir, ".git", "blamebot", "current_prompt.json"), psBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Getenv("CLAUDE_PROJECT_DIR")
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)
	defer func() { _ = os.Setenv("CLAUDE_PROJECT_DIR", old) }()

	payload := map[string]interface{}{
		"tool_name": "Write",
		"tool_input": map[string]interface{}{
			"file_path": filepath.Join(tmpDir, "new_file.go"),
			"content":   "package main\n\nfunc main() {\n}\n",
		},
	}

	payloadBytes, _ := json.Marshal(payload)
	err := HandlePostToolUse(bytes.NewReader(payloadBytes))
	if err != nil {
		t.Fatal(err)
	}

	sessionPath := filepath.Join(tmpDir, ".blamebot", "log", "write-session.jsonl")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("session file not created: %v", err)
	}

	var rec record.Record
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("failed to parse record: %v", err)
	}

	// Write creates full range: 5 lines (4 newlines + 1)
	gotLines := rec.Lines.Lines()
	if len(gotLines) != 5 {
		t.Errorf("lines = %v, want [1 2 3 4 5]", gotLines)
	}
	if len(gotLines) > 0 && (gotLines[0] != 1 || gotLines[len(gotLines)-1] != 5) {
		t.Errorf("lines range = %d-%d, want 1-5", gotLines[0], gotLines[len(gotLines)-1])
	}

	// Write should have hunk with OldLines=0
	if rec.Hunk == nil {
		t.Fatal("hunk should not be nil for Write")
	}
	if rec.Hunk.OldLines != 0 || rec.Hunk.NewLines != 5 {
		t.Errorf("hunk = %+v, want OldLines=0 NewLines=5", *rec.Hunk)
	}
}

// TestHandlePostToolUse_NotInitialized verifies that HandlePostToolUse
// returns nil without writing anything when .blamebot/ doesn't exist.
func TestHandlePostToolUse_NotInitialized(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .git/blamebot but NOT .blamebot/
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git", "blamebot"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := os.Getenv("CLAUDE_PROJECT_DIR")
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)
	defer func() { _ = os.Setenv("CLAUDE_PROJECT_DIR", old) }()

	payload := `{"tool_name":"Edit","tool_input":{"file_path":"x.go","old_string":"a","new_string":"b"}}`
	err := HandlePostToolUse(bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// No .blamebot/log should be created
	_, err = os.Stat(filepath.Join(tmpDir, ".blamebot", "log"))
	if err == nil {
		t.Error(".blamebot/log should not exist when not initialized")
	}
}
