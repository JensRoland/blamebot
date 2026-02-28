package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCleanPrompt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips_ide_tags",
			input:    "<ide_something>content</ide_something>fix the bug",
			expected: "fix the bug",
		},
		{
			name:     "preserves_text_between_tags",
			input:    "<ide_foo>bar</ide_foo>hello world<ide_baz>qux</ide_baz>",
			expected: "hello world",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "multiple_ide_tags",
			input:    "<ide_a>1</ide_a><ide_b>2</ide_b><ide_c>3</ide_c>actual prompt",
			expected: "actual prompt",
		},
		{
			name:     "no_ide_tags",
			input:    "just a normal prompt",
			expected: "just a normal prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanPrompt(tt.input)
			if got != tt.expected {
				t.Errorf("cleanPrompt(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRandomString(t *testing.T) {
	t.Run("returns_correct_length", func(t *testing.T) {
		for _, n := range []int{0, 1, 6, 20} {
			got := randomString(n)
			if len(got) != n {
				t.Errorf("randomString(%d) length = %d, want %d", n, len(got), n)
			}
		}
	})

	t.Run("contains_only_alphanumeric", func(t *testing.T) {
		re := regexp.MustCompile(`^[a-z0-9]*$`)
		for i := 0; i < 100; i++ {
			got := randomString(10)
			if !re.MatchString(got) {
				t.Errorf("randomString(10) = %q, contains non-alphanumeric characters", got)
			}
		}
	})

	t.Run("produces_different_strings", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 20; i++ {
			seen[randomString(10)] = true
		}
		// With 36^10 possible strings, 20 calls should produce at least 2 distinct values
		if len(seen) < 2 {
			t.Errorf("randomString(10) produced only %d distinct values in 20 calls", len(seen))
		}
	})
}

func TestHandlePromptSubmit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)

	// Set up required directories
	if err := os.MkdirAll(filepath.Join(tmpDir, ".blamebot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git", "blamebot"), 0o755); err != nil {
		t.Fatal(err)
	}

	payload := map[string]interface{}{
		"prompt":          "<ide_tag>stuff</ide_tag>fix the bug",
		"session_id":      "sess-1",
		"transcript_path": "/path/to/transcript",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	err = HandlePromptSubmit(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("HandlePromptSubmit() error: %v", err)
	}

	// Verify current_prompt.json was created
	stateFile := filepath.Join(tmpDir, ".git", "blamebot", "current_prompt.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read current_prompt.json: %v", err)
	}

	var state promptState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse current_prompt.json: %v", err)
	}

	// Prompt should be cleaned (IDE tag stripped)
	if state.Prompt != "fix the bug" {
		t.Errorf("Prompt = %q, want %q", state.Prompt, "fix the bug")
	}
	if state.PromptRaw != "<ide_tag>stuff</ide_tag>fix the bug" {
		t.Errorf("PromptRaw = %q, want original with IDE tags", state.PromptRaw)
	}
	if state.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", state.SessionID, "sess-1")
	}
	if state.TranscriptPath != "/path/to/transcript" {
		t.Errorf("TranscriptPath = %q, want %q", state.TranscriptPath, "/path/to/transcript")
	}
	if state.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
	if state.SessionFile == "" {
		t.Error("SessionFile should not be empty")
	}
}

func TestHandlePromptSubmit_NotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)

	// No .blamebot/ directory — not initialized

	payload := map[string]interface{}{
		"prompt":     "fix the bug",
		"session_id": "sess-1",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	err = HandlePromptSubmit(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("HandlePromptSubmit() error: %v, want nil (silent exit)", err)
	}

	// Verify no files were created
	stateFile := filepath.Join(tmpDir, ".git", "blamebot", "current_prompt.json")
	if _, err := os.Stat(stateFile); err == nil {
		t.Error("current_prompt.json should not exist when not initialized")
	}
}
