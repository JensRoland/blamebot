package record

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/lineset"
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
	rec := Record{
		Ts:          "2025-01-01T00:00:00Z",
		File:        "src/main.go",
		Lines:       lineset.New(5, 6, 7),
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

	s := string(b)

	// Verify compact JSON (no spaces after separators)
	if strings.Contains(s, ": ") || strings.Contains(s, ", ") {
		t.Errorf("JSON should be compact, got: %s", s)
	}

	// Verify lines serializes as compact string notation
	if !strings.Contains(s, `"lines":"5-7"`) {
		t.Errorf("lines should serialize as \"5-7\", got: %s", s)
	}

	// Verify null lines (empty LineSet)
	recNull := Record{Ts: "2025-01-01T00:00:00Z", File: "test.go"}
	b2, _ := json.Marshal(recNull)
	if !strings.Contains(string(b2), `"lines":null`) {
		t.Errorf("empty lines should serialize as null, got: %s", string(b2))
	}

	// Verify non-contiguous lines
	recScattered := Record{
		Ts:    "2025-01-01T00:00:00Z",
		File:  "test.go",
		Lines: lineset.New(5, 7, 8, 12),
	}
	b3, _ := json.Marshal(recScattered)
	if !strings.Contains(string(b3), `"lines":"5,7-8,12"`) {
		t.Errorf("scattered lines should serialize as \"5,7-8,12\", got: %s", string(b3))
	}

	// Verify field order (ts first)
	if !strings.HasPrefix(s, `{"ts":`) {
		t.Errorf("expected ts to be first field, got: %s", s[:30])
	}
}

func TestRecordJSONDeserialization_Legacy(t *testing.T) {
	jsonStr := `{"ts":"2025-01-01T00:00:00Z","file":"test.go","lines":[5,7],"content_hash":"","prompt":"","reason":"","change":"","tool":"","author":"","session":"","trace":""}`
	var rec Record
	err := json.Unmarshal([]byte(jsonStr), &rec)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Lines.Min() != 5 || rec.Lines.Max() != 7 || rec.Lines.Len() != 3 {
		t.Errorf("legacy [5,7] should produce range 5-7 (3 lines), got %s (len %d)", rec.Lines.String(), rec.Lines.Len())
	}
}

func TestRecordJSONDeserialization_LegacyNull(t *testing.T) {
	jsonStr := `{"ts":"2025-01-01T00:00:00Z","file":"test.go","lines":[null,null],"content_hash":"","prompt":"","reason":"","change":"","tool":"","author":"","session":"","trace":""}`
	var rec Record
	err := json.Unmarshal([]byte(jsonStr), &rec)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Lines.IsEmpty() {
		t.Errorf("legacy [null,null] should be empty, got %s", rec.Lines.String())
	}
}

func TestRecordJSONDeserialization_New(t *testing.T) {
	jsonStr := `{"ts":"2025-01-01T00:00:00Z","file":"test.go","lines":"5,7-8,12","content_hash":"","prompt":"","reason":"","change":"","tool":"","author":"","session":"","trace":""}`
	var rec Record
	err := json.Unmarshal([]byte(jsonStr), &rec)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Lines.Min() != 5 || rec.Lines.Max() != 12 || rec.Lines.Len() != 4 {
		t.Errorf("new format should produce {5,7,8,12}, got %s (len %d)", rec.Lines.String(), rec.Lines.Len())
	}
}

func TestRecordWithHunk(t *testing.T) {
	rec := Record{
		Ts:   "2025-01-01T00:00:00Z",
		File: "test.go",
		Lines: lineset.New(10, 12),
		Hunk: &HunkInfo{OldStart: 10, OldLines: 3, NewStart: 10, NewLines: 5},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"hunk":{"old_start":10`) {
		t.Errorf("hunk should be present, got: %s", s)
	}

	// Verify nil hunk is omitted
	recNoHunk := Record{Ts: "2025-01-01T00:00:00Z", File: "test.go"}
	b2, _ := json.Marshal(recNoHunk)
	if strings.Contains(string(b2), "hunk") {
		t.Errorf("nil hunk should be omitted, got: %s", string(b2))
	}
}
