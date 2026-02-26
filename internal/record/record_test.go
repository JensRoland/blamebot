package record

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "simple", input: "hello world", expected: "b94d27b9934d3e08"}, // matches Python hashlib
		{name: "whitespace_normalization", input: "  hello   world  \n\t", expected: "b94d27b9934d3e08"},
		{name: "multiline", input: "line1\nline2\nline3", expected: "22f75635c73c7f4f"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContentHash(tt.input)
			if got != tt.expected {
				t.Errorf("ContentHash(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCompactChangeSummary(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		contains string
	}{
		{name: "insertion", old: "", new: "new code here", contains: "added:"},
		{name: "deletion", old: "old code here", new: "", contains: "removed:"},
		{name: "replacement", old: "foo", new: "bar", contains: "\u2192"}, // →
		{name: "long_common_prefix", old: "aaaaaaaaaaaaaaaaaaaaaaaaaaa_old", new: "aaaaaaaaaaaaaaaaaaaaaaaaaaa_new", contains: "\u2026"}, // …
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompactChangeSummary(tt.old, tt.new)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("CompactChangeSummary(%q, %q) = %q, expected to contain %q",
					tt.old, tt.new, got, tt.contains)
			}
		})
	}
}

func TestRelativizePath(t *testing.T) {
	tests := []struct {
		name       string
		absPath    string
		projectDir string
		expected   string
	}{
		{name: "absolute", absPath: "/home/user/project/src/main.go", projectDir: "/home/user/project", expected: "src/main.go"},
		{name: "empty", absPath: "", projectDir: "/home/user/project", expected: ""},
		{name: "same_dir", absPath: "/home/user/project/file.go", projectDir: "/home/user/project", expected: "file.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelativizePath(tt.absPath, tt.projectDir)
			if got != tt.expected {
				t.Errorf("RelativizePath(%q, %q) = %q, want %q",
					tt.absPath, tt.projectDir, got, tt.expected)
			}
		})
	}
}

func TestRecordJSONSerialization(t *testing.T) {
	five := NewInt(5)
	seven := NewInt(7)

	rec := Record{
		Ts:          "2025-01-01T00:00:00Z",
		File:        "src/main.go",
		Lines:       [2]NullableInt{five, seven},
		ContentHash: "abc123def456789a",
		Prompt:      "add logging",
		Reason:      "",
		Change:      "added log statement",
		Tool:        "Edit",
		Author:      "test",
		Session:     "sess-123",
		Trace:       "/path/to/transcript.jsonl#tool_use_abc",
	}

	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's compact (no spaces after separators)
	s := string(b)
	if strings.Contains(s, ": ") || strings.Contains(s, ", ") {
		t.Errorf("JSON should be compact, got: %s", s)
	}

	// Verify lines serializes correctly
	if !strings.Contains(s, `"lines":[5,7]`) {
		t.Errorf("lines should serialize as [5,7], got: %s", s)
	}

	// Verify null lines
	recNull := Record{Ts: "2025-01-01T00:00:00Z", File: "test.go"}
	b2, _ := json.Marshal(recNull)
	if !strings.Contains(string(b2), `"lines":[null,null]`) {
		t.Errorf("null lines should serialize as [null,null], got: %s", string(b2))
	}

	// Verify field order matches Python's dict order
	var m map[string]json.RawMessage
	json.Unmarshal(b, &m)
	keys := make([]string, 0, len(m))
	// json.Marshal uses struct field order, so check that ts comes first
	if !strings.HasPrefix(s, `{"ts":`) {
		t.Errorf("expected ts to be first field, got: %s", s[:30])
	}
	_ = keys
}
