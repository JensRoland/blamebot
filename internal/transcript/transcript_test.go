package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- helpers ---

// makeEntry builds a JSONL line for a transcript entry.
func makeEntry(t *testing.T, role string, blocks []contentBlock) string {
	t.Helper()
	rawBlocks := make([]json.RawMessage, len(blocks))
	for i, b := range blocks {
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal block: %v", err)
		}
		rawBlocks[i] = data
	}
	entry := map[string]interface{}{
		"message": map[string]interface{}{
			"role":    role,
			"content": rawBlocks,
		},
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	return string(data)
}

// makeEntryWithType builds a JSONL line with a top-level "type" field (used when role is empty).
func makeEntryWithType(t *testing.T, typ string, blocks []contentBlock) string {
	t.Helper()
	rawBlocks := make([]json.RawMessage, len(blocks))
	for i, b := range blocks {
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal block: %v", err)
		}
		rawBlocks[i] = data
	}
	entry := map[string]interface{}{
		"type": typ,
		"message": map[string]interface{}{
			"content": rawBlocks,
		},
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	return string(data)
}

// makeToolUseBlock builds a contentBlock representing a tool_use with the given ID and input.
func makeToolUseBlock(id string, input map[string]interface{}) contentBlock {
	inputJSON, _ := json.Marshal(input)
	return contentBlock{
		Type:  "tool_use",
		ID:    id,
		Input: inputJSON,
	}
}

// writeJSONL writes lines to a temp file and returns the path.
func writeJSONL(t *testing.T, dir string, name string, lines []string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write JSONL: %v", err)
	}
	return path
}

// --- TestCleanPromptText ---

func TestCleanPromptText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips ide tags",
			input:    "hello <ide_something>metadata here</ide_something> world",
			expected: "hello world",
		},
		{
			name:     "strips system-reminder tags",
			input:    "before <system-reminder>secret stuff</system-reminder> after",
			expected: "before after",
		},
		{
			name:     "preserves normal text",
			input:    "just some plain text",
			expected: "just some plain text",
		},
		{
			name:     "handles multiple tags",
			input:    "<ide_foo>a</ide_foo> hello <ide_bar>b</ide_bar> <system-reminder>c</system-reminder> world",
			expected: "hello world",
		},
		{
			name:     "handles empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "strips multiline ide tag",
			input:    "<ide_context>\nline1\nline2\n</ide_context>\nprompt text",
			expected: "prompt text",
		},
		{
			name:     "strips multiline system-reminder",
			input:    "prompt <system-reminder>\nreminder line1\nreminder line2\n</system-reminder> text",
			expected: "prompt text",
		},
		{
			name:     "whitespace only after stripping",
			input:    "<ide_x>content</ide_x>   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanPromptText(tt.input)
			if got != tt.expected {
				t.Errorf("cleanPromptText(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- TestReadEntries ---

func TestReadEntries(t *testing.T) {
	t.Run("reads valid JSONL lines", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "hello"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "hi"}}),
		}
		path := writeJSONL(t, dir, "test.jsonl", lines)

		entries, err := readEntries(path)
		if err != nil {
			t.Fatalf("readEntries() error: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("got %d entries, want 2", len(entries))
		}
		if entries[0].Message.Role != "user" {
			t.Errorf("entry[0].Role = %q, want %q", entries[0].Message.Role, "user")
		}
		if entries[1].Message.Role != "assistant" {
			t.Errorf("entry[1].Role = %q, want %q", entries[1].Message.Role, "assistant")
		}
	})

	t.Run("skips blank lines", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "hello"}}),
			"",
			"   ",
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "hi"}}),
		}
		path := writeJSONL(t, dir, "test.jsonl", lines)

		entries, err := readEntries(path)
		if err != nil {
			t.Fatalf("readEntries() error: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("got %d entries, want 2", len(entries))
		}
	})

	t.Run("skips malformed JSON", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "hello"}}),
			"not valid json at all",
			"{bad json",
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "hi"}}),
		}
		path := writeJSONL(t, dir, "test.jsonl", lines)

		entries, err := readEntries(path)
		if err != nil {
			t.Fatalf("readEntries() error: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("got %d entries, want 2 (should skip malformed lines)", len(entries))
		}
	})

	t.Run("handles empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.jsonl")
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		entries, err := readEntries(path)
		if err != nil {
			t.Fatalf("readEntries() error: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("got %d entries, want 0 for empty file", len(entries))
		}
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		_, err := readEntries("/nonexistent/path/file.jsonl")
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})
}

// --- TestWalkBackwards ---

func TestWalkBackwards(t *testing.T) {
	// Helper to parse entries from JSONL lines
	parseEntries := func(t *testing.T, lines []string) []transcriptEntry {
		t.Helper()
		dir := t.TempDir()
		path := writeJSONL(t, dir, "test.jsonl", lines)
		entries, err := readEntries(path)
		if err != nil {
			t.Fatalf("readEntries: %v", err)
		}
		return entries
	}

	t.Run("finds thinking block before target", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "do something"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "Let me think about this"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 2)
		if !strings.Contains(result, "[Thinking]") {
			t.Errorf("expected [Thinking] prefix, got: %s", result)
		}
		if !strings.Contains(result, "Let me think about this") {
			t.Errorf("expected thinking content, got: %s", result)
		}
	})

	t.Run("finds text block before target", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "do something"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "I will edit this file"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 2)
		if !strings.Contains(result, "[Response]") {
			t.Errorf("expected [Response] prefix, got: %s", result)
		}
		if !strings.Contains(result, "I will edit this file") {
			t.Errorf("expected text content, got: %s", result)
		}
	})

	t.Run("stops at real user message", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "old thinking"}}),
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "new request"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "new thinking"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 3)
		if strings.Contains(result, "old thinking") {
			t.Errorf("should not include thinking from before user message, got: %s", result)
		}
		if !strings.Contains(result, "new thinking") {
			t.Errorf("should include thinking after user message, got: %s", result)
		}
	})

	t.Run("continues past tool_result user messages", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "user request"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "reasoning about it"}}),
			makeEntry(t, "user", []contentBlock{{Type: "tool_result"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 3)
		// Should continue past the tool_result user message and find the thinking block
		if !strings.Contains(result, "reasoning about it") {
			t.Errorf("should continue past tool_result and find thinking, got: %s", result)
		}
	})

	t.Run("truncates long thinking", func(t *testing.T) {
		longThinking := strings.Repeat("x", 2000)
		entries := parseEntries(t, []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "do something"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: longThinking}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 2)
		// Thinking is truncated to last 1500 chars + "[Thinking]\n" prefix
		thinkingContent := strings.TrimPrefix(result, "[Thinking]\n")
		if len(thinkingContent) != 1500 {
			t.Errorf("thinking should be truncated to 1500 chars, got %d", len(thinkingContent))
		}
		// Should contain the tail of the long string
		if !strings.HasSuffix(thinkingContent, "xxx") {
			t.Errorf("truncated thinking should contain tail chars")
		}
	})

	t.Run("truncates long text", func(t *testing.T) {
		longText := strings.Repeat("y", 800)
		entries := parseEntries(t, []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "do something"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: longText}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 2)
		// Text is truncated to last 500 chars + "[Response]\n" prefix
		textContent := strings.TrimPrefix(result, "[Response]\n")
		if len(textContent) != 500 {
			t.Errorf("text should be truncated to 500 chars, got %d", len(textContent))
		}
	})

	t.Run("returns message when no blocks found", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "user request"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 1)
		if !strings.Contains(result, "no thinking/text blocks found") {
			t.Errorf("expected 'no thinking/text blocks found' message, got: %s", result)
		}
		if !strings.Contains(result, "entry 1") {
			t.Errorf("expected entry index in message, got: %s", result)
		}
	})

	t.Run("reverses context parts for chronological order", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "user request"}}),
			makeEntry(t, "assistant", []contentBlock{
				{Type: "thinking", Thinking: "FIRST_THINKING"},
				{Type: "text", Text: "SECOND_TEXT"},
			}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 2)
		// After reversal: text (appended last) should appear after thinking (appended first)
		// walkBackwards iterates content blocks in order: thinking first, text second
		// So contextParts = ["[Thinking]\nFIRST_THINKING", "[Response]\nSECOND_TEXT"]
		// After reversal: ["[Response]\nSECOND_TEXT", "[Thinking]\nFIRST_THINKING"]
		textIdx := strings.Index(result, "[Response]")
		thinkingIdx := strings.Index(result, "[Thinking]")
		if textIdx < 0 || thinkingIdx < 0 {
			t.Fatalf("expected both [Response] and [Thinking], got: %s", result)
		}
		if textIdx > thinkingIdx {
			t.Errorf("[Response] should come before [Thinking] after reversal, got: %s", result)
		}
	})

	t.Run("returns message for target at index 0", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 0)
		if !strings.Contains(result, "no thinking/text blocks found") {
			t.Errorf("expected 'no thinking/text blocks found' message, got: %s", result)
		}
	})

	t.Run("uses type field when role is empty", func(t *testing.T) {
		entries := parseEntries(t, []string{
			makeEntryWithType(t, "user", []contentBlock{{Type: "text", Text: "user request"}}),
			makeEntryWithType(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "reasoning"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_123", map[string]interface{}{"file": "test.go"})}),
		})

		result := walkBackwards(entries, 2)
		if !strings.Contains(result, "reasoning") {
			t.Errorf("should find thinking from entry with type field, got: %s", result)
		}
	})
}

// --- TestExtractSessionPrompts ---

func TestExtractSessionPrompts(t *testing.T) {
	t.Run("extracts user text blocks", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "first prompt"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "response"}}),
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "second prompt"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		prompts := ExtractSessionPrompts(path)
		if len(prompts) != 2 {
			t.Fatalf("got %d prompts, want 2", len(prompts))
		}
		if prompts[0] != "first prompt" {
			t.Errorf("prompts[0] = %q, want %q", prompts[0], "first prompt")
		}
		if prompts[1] != "second prompt" {
			t.Errorf("prompts[1] = %q, want %q", prompts[1], "second prompt")
		}
	})

	t.Run("skips tool_result-only user entries", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "real prompt"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "response"}}),
			makeEntry(t, "user", []contentBlock{{Type: "tool_result"}}),
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "another prompt"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		prompts := ExtractSessionPrompts(path)
		if len(prompts) != 2 {
			t.Fatalf("got %d prompts, want 2 (should skip tool_result)", len(prompts))
		}
		if prompts[0] != "real prompt" {
			t.Errorf("prompts[0] = %q, want %q", prompts[0], "real prompt")
		}
		if prompts[1] != "another prompt" {
			t.Errorf("prompts[1] = %q, want %q", prompts[1], "another prompt")
		}
	})

	t.Run("cleans IDE tags from prompts", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "<ide_context>metadata</ide_context> actual prompt"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		prompts := ExtractSessionPrompts(path)
		if len(prompts) != 1 {
			t.Fatalf("got %d prompts, want 1", len(prompts))
		}
		if prompts[0] != "actual prompt" {
			t.Errorf("prompts[0] = %q, want %q", prompts[0], "actual prompt")
		}
	})

	t.Run("returns nil for missing file", func(t *testing.T) {
		prompts := ExtractSessionPrompts("/nonexistent/path/session.jsonl")
		if prompts != nil {
			t.Errorf("expected nil for missing file, got %v", prompts)
		}
	})

	t.Run("handles empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.jsonl")
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		prompts := ExtractSessionPrompts(path)
		if prompts != nil {
			t.Errorf("expected nil for empty file, got %v", prompts)
		}
	})

	t.Run("skips user entries where text is empty after cleaning", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "<ide_context>only metadata</ide_context>"}}),
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "real prompt"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		prompts := ExtractSessionPrompts(path)
		if len(prompts) != 1 {
			t.Fatalf("got %d prompts, want 1 (should skip empty-after-clean)", len(prompts))
		}
		if prompts[0] != "real prompt" {
			t.Errorf("prompts[0] = %q, want %q", prompts[0], "real prompt")
		}
	})
}

// --- TestReadTraceContext ---

func TestReadTraceContext(t *testing.T) {
	t.Run("returns empty for empty traceRef", func(t *testing.T) {
		result := ReadTraceContext("", "")
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("reads from committed traces JSON file", func(t *testing.T) {
		dir := t.TempDir()
		tracesDir := filepath.Join(dir, ".blamebot", "traces")
		if err := os.MkdirAll(tracesDir, 0755); err != nil {
			t.Fatal(err)
		}

		traces := map[string]string{
			"tool_abc": "[Thinking]\nSome reasoning about the edit",
		}
		data, _ := json.Marshal(traces)
		if err := os.WriteFile(filepath.Join(tracesDir, "mysession.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		traceRef := "/some/path/mysession.jsonl#tool_abc"
		result := ReadTraceContext(traceRef, dir)
		if result != "[Thinking]\nSome reasoning about the edit" {
			t.Errorf("expected trace from committed file, got %q", result)
		}
	})

	t.Run("falls back to local transcript when no committed trace", func(t *testing.T) {
		dir := t.TempDir()
		// No traces dir -- will fall back to transcript

		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "fix the bug"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "I need to fix the null check"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_xyz", map[string]interface{}{"file": "main.go"})}),
		}
		transcriptPath := writeJSONL(t, dir, "session.jsonl", lines)

		traceRef := transcriptPath + "#tool_xyz"
		result := ReadTraceContext(traceRef, dir)
		if !strings.Contains(result, "I need to fix the null check") {
			t.Errorf("expected thinking from transcript fallback, got %q", result)
		}
	})

	t.Run("returns message when toolUseID not found in transcript", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "do something"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "ok"}}),
		}
		transcriptPath := writeJSONL(t, dir, "session.jsonl", lines)

		traceRef := transcriptPath + "#nonexistent_id"
		result := ReadTraceContext(traceRef, "")
		if !strings.Contains(result, "not found") {
			t.Errorf("expected 'not found' message, got %q", result)
		}
		if !strings.Contains(result, "nonexistent_id") {
			t.Errorf("expected tool_use_id in message, got %q", result)
		}
	})

	t.Run("returns message when no toolUseID provided", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "do something"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "ok"}}),
		}
		transcriptPath := writeJSONL(t, dir, "session.jsonl", lines)

		// traceRef is just the path, no #id
		result := ReadTraceContext(transcriptPath, "")
		if !strings.Contains(result, "no tool_use_id") {
			t.Errorf("expected 'no tool_use_id' message, got %q", result)
		}
		if !strings.Contains(result, "2 entries") {
			t.Errorf("expected entry count in message, got %q", result)
		}
	})

	t.Run("returns empty for missing transcript file", func(t *testing.T) {
		result := ReadTraceContext("/nonexistent/file.jsonl#tool_id", "")
		if result != "" {
			t.Errorf("expected empty string for missing file, got %q", result)
		}
	})

	t.Run("prefers committed trace over transcript", func(t *testing.T) {
		dir := t.TempDir()

		// Create committed traces with specific context
		tracesDir := filepath.Join(dir, ".blamebot", "traces")
		if err := os.MkdirAll(tracesDir, 0755); err != nil {
			t.Fatal(err)
		}
		traces := map[string]string{
			"tool_abc": "committed trace context",
		}
		data, _ := json.Marshal(traces)
		if err := os.WriteFile(filepath.Join(tracesDir, "sess.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		// Also create a transcript with different context
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "do something"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "transcript thinking"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_abc", map[string]interface{}{"file": "test.go"})}),
		}
		transcriptPath := writeJSONL(t, dir, "sess.jsonl", lines)

		traceRef := transcriptPath + "#tool_abc"
		result := ReadTraceContext(traceRef, dir)
		if result != "committed trace context" {
			t.Errorf("should prefer committed trace, got %q", result)
		}
	})
}

// --- TestExtractTraceContexts ---

func TestExtractTraceContexts(t *testing.T) {
	t.Run("extracts multiple tool_use_ids", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "first request"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "thinking for tool_1"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_1", map[string]interface{}{"file": "a.go"})}),
			makeEntry(t, "user", []contentBlock{{Type: "tool_result"}}),
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "second request"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "thinking for tool_2"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_2", map[string]interface{}{"file": "b.go"})}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		results := ExtractTraceContexts(path, []string{"tool_1", "tool_2"})
		if len(results) != 2 {
			t.Fatalf("got %d results, want 2", len(results))
		}
		if !strings.Contains(results["tool_1"], "thinking for tool_1") {
			t.Errorf("tool_1 context = %q, expected thinking for tool_1", results["tool_1"])
		}
		if !strings.Contains(results["tool_2"], "thinking for tool_2") {
			t.Errorf("tool_2 context = %q, expected thinking for tool_2", results["tool_2"])
		}
	})

	t.Run("returns nil for empty IDs list", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "prompt"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		results := ExtractTraceContexts(path, []string{})
		if results != nil {
			t.Errorf("expected nil for empty IDs, got %v", results)
		}
	})

	t.Run("returns nil for nil IDs list", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "prompt"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		results := ExtractTraceContexts(path, nil)
		if results != nil {
			t.Errorf("expected nil for nil IDs, got %v", results)
		}
	})

	t.Run("returns nil for missing file", func(t *testing.T) {
		results := ExtractTraceContexts("/nonexistent/path.jsonl", []string{"tool_1"})
		if results != nil {
			t.Errorf("expected nil for missing file, got %v", results)
		}
	})

	t.Run("returns nil for empty path", func(t *testing.T) {
		results := ExtractTraceContexts("", []string{"tool_1"})
		if results != nil {
			t.Errorf("expected nil for empty path, got %v", results)
		}
	})

	t.Run("skips IDs not found in transcript", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "user", []contentBlock{{Type: "text", Text: "request"}}),
			makeEntry(t, "assistant", []contentBlock{{Type: "thinking", Thinking: "thinking"}}),
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_1", map[string]interface{}{"file": "a.go"})}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		results := ExtractTraceContexts(path, []string{"tool_1", "tool_missing"})
		if len(results) != 1 {
			t.Fatalf("got %d results, want 1", len(results))
		}
		if _, ok := results["tool_missing"]; ok {
			t.Error("should not have result for missing tool ID")
		}
	})
}

// --- TestExtractDiffFromTrace ---

func TestExtractDiffFromTrace(t *testing.T) {
	t.Run("extracts old_string/new_string for Edit tool", func(t *testing.T) {
		dir := t.TempDir()
		input := map[string]interface{}{
			"file_path":  "/path/to/file.go",
			"old_string": "func old() {}",
			"new_string": "func new() {}",
		}
		lines := []string{
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_edit", input)}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		oldStr, newStr, ok := ExtractDiffFromTrace(path + "#tool_edit")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if oldStr != "func old() {}" {
			t.Errorf("oldStr = %q, want %q", oldStr, "func old() {}")
		}
		if newStr != "func new() {}" {
			t.Errorf("newStr = %q, want %q", newStr, "func new() {}")
		}
	})

	t.Run("extracts content for Write tool", func(t *testing.T) {
		dir := t.TempDir()
		input := map[string]interface{}{
			"file_path": "/path/to/file.go",
			"content":   "package main\n\nfunc main() {}\n",
		}
		lines := []string{
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_write", input)}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		oldStr, newStr, ok := ExtractDiffFromTrace(path + "#tool_write")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if oldStr != "" {
			t.Errorf("oldStr = %q, want empty for Write tool", oldStr)
		}
		if newStr != "package main\n\nfunc main() {}\n" {
			t.Errorf("newStr = %q, want file content", newStr)
		}
	})

	t.Run("returns false for empty traceRef", func(t *testing.T) {
		_, _, ok := ExtractDiffFromTrace("")
		if ok {
			t.Error("expected ok=false for empty traceRef")
		}
	})

	t.Run("returns false for missing transcript file", func(t *testing.T) {
		_, _, ok := ExtractDiffFromTrace("/nonexistent/file.jsonl#tool_id")
		if ok {
			t.Error("expected ok=false for missing file")
		}
	})

	t.Run("returns false when toolUseID not found", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "just text"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		_, _, ok := ExtractDiffFromTrace(path + "#nonexistent_tool")
		if ok {
			t.Error("expected ok=false for missing tool ID")
		}
	})

	t.Run("returns false for traceRef without hash", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			makeEntry(t, "assistant", []contentBlock{{Type: "text", Text: "text"}}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		_, _, ok := ExtractDiffFromTrace(path)
		if ok {
			t.Error("expected ok=false for traceRef without #id")
		}
	})

	t.Run("returns false when input has no old/new/content", func(t *testing.T) {
		dir := t.TempDir()
		input := map[string]interface{}{
			"command": "ls -la",
		}
		lines := []string{
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_bash", input)}),
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		_, _, ok := ExtractDiffFromTrace(path + "#tool_bash")
		if ok {
			t.Error("expected ok=false for input without old_string/new_string/content")
		}
	})

	t.Run("skips blank lines in transcript", func(t *testing.T) {
		dir := t.TempDir()
		input := map[string]interface{}{
			"old_string": "old",
			"new_string": "new",
		}
		lines := []string{
			"",
			makeEntry(t, "assistant", []contentBlock{makeToolUseBlock("tool_1", input)}),
			"",
		}
		path := writeJSONL(t, dir, "session.jsonl", lines)

		oldStr, newStr, ok := ExtractDiffFromTrace(path + "#tool_1")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if oldStr != "old" || newStr != "new" {
			t.Errorf("got (%q, %q), want (\"old\", \"new\")", oldStr, newStr)
		}
	})
}
