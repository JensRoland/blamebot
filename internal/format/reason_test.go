package format

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/lineset"
)

func intPtr(v int) *int { return &v }

func TestFormatReason_NoAdjustment(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(10),
		LineEnd:   intPtr(14),
		Ts:        "2025-01-15T12:00:00Z",
		Change:    "a → b",
	}

	result := FormatReason(row, "/tmp/project", false, nil)

	if !strings.Contains(result, "main.go") {
		t.Error("should contain filename")
	}
	if !strings.Contains(result, "L10-14") {
		t.Errorf("should contain L10-14, got %s", result)
	}
}

func TestFormatReason_AdjustedSameAsStored(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(10),
		LineEnd:   intPtr(14),
		Ts:        "2025-01-15T12:00:00Z",
		Change:    "test",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.FromRange(10, 14),
	}

	result := FormatReason(row, "/tmp/project", false, adj)

	// Should display current lines only
	if !strings.Contains(result, "L10-14") {
		t.Errorf("should contain L10-14, got %s", result)
	}
}

func TestFormatReason_AdjustedShifted(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(10),
		LineEnd:   intPtr(14),
		Ts:        "2025-01-15T12:00:00Z",
		Change:    "test",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.FromRange(15, 19),
	}

	result := FormatReason(row, "/tmp/project", false, adj)

	// Should show only current adjusted lines (no original, no arrow)
	if !strings.Contains(result, "L15-19") {
		t.Errorf("should contain adjusted L15-19, got %s", result)
	}
	if strings.Contains(result, "L10-14") {
		t.Errorf("should NOT contain original L10-14, got %s", result)
	}
	if strings.Contains(result, "\u2192") {
		t.Errorf("should NOT contain arrow, got %s", result)
	}
}

func TestFormatReason_SparseAdjustedLines(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(10),
		LineEnd:   intPtr(18),
		Ts:        "2025-01-15T12:00:00Z",
		Change:    "test",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.New(10, 15, 18),
	}

	result := FormatReason(row, "/tmp/project", false, adj)

	// Sparse lines should show as compact notation
	if !strings.Contains(result, "L10,15,18") {
		t.Errorf("should contain L10,15,18, got %s", result)
	}
}

func TestFormatReason_Superseded(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(10),
		LineEnd:   intPtr(14),
		Ts:        "2025-01-15T12:00:00Z",
		Change:    "test",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.LineSet{}, // empty
		Superseded:   true,
	}

	result := FormatReason(row, "/tmp/project", false, adj)

	// When superseded with empty CurrentLines, should fall back to stored lines
	// (the adj path is only taken when !adj.CurrentLines.IsEmpty())
	if !strings.Contains(result, "L10-14") {
		t.Errorf("should contain stored L10-14, got %s", result)
	}
}

func TestFormatReason_NoLineInfo(t *testing.T) {
	row := &index.ReasonRow{
		ID:     1,
		File:   "main.go",
		Ts:     "2025-01-15T12:00:00Z",
		Change: "test",
	}

	result := FormatReason(row, "/tmp/project", false, nil)

	// Should not contain any "L" line reference
	if strings.Contains(result, " L") {
		t.Errorf("should not contain line reference, got %s", result)
	}
}

func TestFormatReason_SingleLine(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(5),
		LineEnd:   intPtr(5),
		Ts:        "2025-01-15T12:00:00Z",
		Change:    "test",
	}

	result := FormatReason(row, "/tmp/project", false, nil)

	// Should show "L5" not "L5-5"
	if strings.Contains(result, "L5-5") {
		t.Errorf("should show L5 not L5-5, got %s", result)
	}
	if !strings.Contains(result, "L5") {
		t.Errorf("should contain L5, got %s", result)
	}
}

func TestFormatReason_ShowsPromptAndChange(t *testing.T) {
	row := &index.ReasonRow{
		ID:     1,
		File:   "main.go",
		Ts:     "2025-01-15T12:00:00Z",
		Prompt: "fix the bug",
		Change: "a → b",
	}

	result := FormatReason(row, "/tmp/project", false, nil)

	if !strings.Contains(result, "fix the bug") {
		t.Error("should contain prompt")
	}
	if !strings.Contains(result, "a → b") {
		t.Error("should contain change")
	}
}

func TestFormatReason_ReasonOverChange(t *testing.T) {
	row := &index.ReasonRow{
		ID:     1,
		File:   "main.go",
		Ts:     "2025-01-15T12:00:00Z",
		Reason: "added error handling",
		Change: "a → b",
	}

	result := FormatReason(row, "/tmp/project", false, nil)

	// When reason exists, it should be shown (not change)
	if !strings.Contains(result, "added error handling") {
		t.Error("should contain reason")
	}
}

func TestRowToJSON_NoAdjustment(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(10),
		LineEnd:   intPtr(14),
		Ts:        "2025-01-15T12:00:00Z",
	}

	d := RowToJSON(row, "/tmp/project", nil)

	if d["file"] != "main.go" {
		t.Errorf("file = %v", d["file"])
	}
	if _, ok := d["current_lines"]; ok {
		t.Error("should not have current_lines without adjustment")
	}
	if _, ok := d["superseded"]; ok {
		t.Error("should not have superseded without adjustment")
	}
}

func TestRowToJSON_WithAdjustment(t *testing.T) {
	row := &index.ReasonRow{
		ID:          1,
		File:        "main.go",
		LineStart:   intPtr(10),
		LineEnd:     intPtr(14),
		Ts:          "2025-01-15T12:00:00Z",
		ContentHash: "",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.New(15, 17, 19),
	}

	d := RowToJSON(row, "/tmp/project", adj)

	cl, ok := d["current_lines"].(string)
	if !ok {
		t.Fatalf("current_lines should be a string, got %T", d["current_lines"])
	}
	if cl != "15,17,19" {
		t.Errorf("current_lines = %q, want %q", cl, "15,17,19")
	}
}

func TestRowToJSON_Superseded(t *testing.T) {
	row := &index.ReasonRow{
		ID:   1,
		File: "main.go",
		Ts:   "2025-01-15T12:00:00Z",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.New(10), // non-empty so adj path is taken
		Superseded:   true,
	}

	d := RowToJSON(row, "/tmp/project", adj)

	if d["superseded"] != true {
		t.Errorf("superseded = %v, want true", d["superseded"])
	}
}

func TestRowToJSON_EmptyAdjustment(t *testing.T) {
	row := &index.ReasonRow{
		ID:        1,
		File:      "main.go",
		LineStart: intPtr(5),
		LineEnd:   intPtr(7),
		Ts:        "2025-01-15T12:00:00Z",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.LineSet{}, // empty
	}

	d := RowToJSON(row, "/tmp/project", adj)

	// With empty CurrentLines, should NOT include current_lines
	if _, ok := d["current_lines"]; ok {
		t.Error("should not have current_lines with empty adjustment")
	}
}

func expectedHash(content string) string {
	normalized := strings.Join(strings.Fields(content), " ")
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h)[:16]
}

func TestFormatReason_Basic(t *testing.T) {
	row := &index.ReasonRow{
		ID:     1,
		File:   "cmd/root.go",
		Ts:     "2025-03-20T10:30:00Z",
		Prompt: "Add error handling to main",
		Reason: "Wrapped return values with fmt.Errorf for better context",
		Change: "err → fmt.Errorf(...)",
	}

	result := FormatReason(row, "/tmp", false, nil)

	if !strings.Contains(result, "cmd/root.go") {
		t.Errorf("should contain file name, got %s", result)
	}
	if !strings.Contains(result, "2025-03-20") {
		t.Errorf("should contain date, got %s", result)
	}
	if !strings.Contains(result, "Add error handling to main") {
		t.Errorf("should contain prompt, got %s", result)
	}
	if !strings.Contains(result, "Wrapped return values") {
		t.Errorf("should contain reason, got %s", result)
	}
	// Verbose-only fields should NOT appear
	for _, field := range []string{"Tool:", "Hash:", "Session:", "Trace:", "Source:"} {
		if strings.Contains(result, field) {
			t.Errorf("non-verbose output should not contain %q, got %s", field, result)
		}
	}
}

func TestFormatReason_Verbose(t *testing.T) {
	row := &index.ReasonRow{
		ID:          2,
		File:        "internal/index/index.go",
		LineStart:   intPtr(10),
		LineEnd:     intPtr(20),
		Ts:          "2025-04-01T08:00:00Z",
		Prompt:      "Refactor query logic",
		Reason:      "Extracted common query builder into helper function",
		Change:      "inline SQL → buildQuery()",
		Tool:        "Edit",
		ContentHash: "abcdef1234567890",
		Session:     "sess-abc-123-def-456",
		Trace:       "traces/sess-abc.jsonl#tool_use_42",
		SourceFile:  "sess-abc-123.jsonl",
		Author:      "dev@example.com",
	}

	// Use nonexistent root so git blame fails silently
	result := FormatReason(row, "/tmp/nonexistent", true, nil)

	// Verbose fields should appear
	if !strings.Contains(result, "inline SQL") {
		t.Errorf("verbose output should contain Diff (the Change field), got %s", result)
	}
	if !strings.Contains(result, "Edit") {
		t.Errorf("verbose output should contain Tool, got %s", result)
	}
	if !strings.Contains(result, "abcdef1234567890") {
		t.Errorf("verbose output should contain Hash, got %s", result)
	}
	if !strings.Contains(result, "sess-abc-123-def-456") {
		t.Errorf("verbose output should contain Session, got %s", result)
	}
	if !strings.Contains(result, "sess-abc-123.jsonl") {
		t.Errorf("verbose output should contain Source, got %s", result)
	}
	// Trace with "#" should show only the part after "#"
	if !strings.Contains(result, "tool_use_42") {
		t.Errorf("verbose output should contain trace tool id, got %s", result)
	}
}

func TestFormatReason_WithAdjustment(t *testing.T) {
	row := &index.ReasonRow{
		ID:        3,
		File:      "main.go",
		LineStart: intPtr(1),
		LineEnd:   intPtr(3),
		Ts:        "2025-05-01T00:00:00Z",
		Change:    "refactor",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.New(5, 6, 7),
	}

	result := FormatReason(row, "/tmp/nonexistent", false, adj)

	if !strings.Contains(result, "L5-7") {
		t.Errorf("should contain adjusted lines L5-7, got %s", result)
	}
	// Original lines should NOT appear
	if strings.Contains(result, "L1-3") {
		t.Errorf("should NOT contain original lines L1-3, got %s", result)
	}
}

func TestFormatReason_SupersededAdj(t *testing.T) {
	row := &index.ReasonRow{
		ID:        4,
		File:      "util.go",
		LineStart: intPtr(10),
		LineEnd:   intPtr(12),
		Ts:        "2025-06-01T00:00:00Z",
		Change:    "old change",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.LineSet{}, // empty
		Superseded:   true,
	}

	// Should not crash; falls back to stored lines since CurrentLines is empty
	result := FormatReason(row, "/tmp/nonexistent", false, adj)

	if !strings.Contains(result, "util.go") {
		t.Errorf("should contain file name, got %s", result)
	}
	if !strings.Contains(result, "L10-12") {
		t.Errorf("should fall back to stored lines L10-12, got %s", result)
	}
}

func TestFormatReason_LongPrompt(t *testing.T) {
	longPrompt := strings.Repeat("x", 150)
	row := &index.ReasonRow{
		ID:     5,
		File:   "main.go",
		Ts:     "2025-01-01T00:00:00Z",
		Prompt: longPrompt,
		Change: "a → b",
	}

	result := FormatReason(row, "/tmp", false, nil)

	// Should be truncated: first 117 chars + "..."
	expected := strings.Repeat("x", 117) + "..."
	if !strings.Contains(result, expected) {
		t.Errorf("long prompt should be truncated to 117+..., got %s", result)
	}
	// Full prompt should NOT appear
	if strings.Contains(result, longPrompt) {
		t.Errorf("full long prompt should not appear in output")
	}
}

func TestFormatReason_LongReason(t *testing.T) {
	longReason := strings.Repeat("r", 250)
	row := &index.ReasonRow{
		ID:     6,
		File:   "main.go",
		Ts:     "2025-01-01T00:00:00Z",
		Reason: longReason,
	}

	result := FormatReason(row, "/tmp", false, nil)

	// Should be truncated: first 197 chars + "..."
	expected := strings.Repeat("r", 197) + "..."
	if !strings.Contains(result, expected) {
		t.Errorf("long reason should be truncated to 197+..., got %s", result)
	}
	// Full reason should NOT appear
	if strings.Contains(result, longReason) {
		t.Errorf("full long reason should not appear in output")
	}
}

func TestFormatReason_NoPromptWithChange(t *testing.T) {
	row := &index.ReasonRow{
		ID:     7,
		File:   "main.go",
		Ts:     "2025-01-01T00:00:00Z",
		Prompt: "",
		Reason: "",
		Change: "added new function",
	}

	result := FormatReason(row, "/tmp", false, nil)

	// With empty Prompt and empty Reason but non-empty Change, should show "Change:"
	if !strings.Contains(result, "Change:") {
		t.Errorf("should show Change: label when no prompt/reason, got %s", result)
	}
	if !strings.Contains(result, "added new function") {
		t.Errorf("should contain the change text, got %s", result)
	}
	if strings.Contains(result, "Reason:") {
		t.Errorf("should NOT show Reason: label, got %s", result)
	}
}

func TestRowToJSON_Basic(t *testing.T) {
	row := &index.ReasonRow{
		ID:        10,
		File:      "pkg/util.go",
		LineStart: intPtr(5),
		LineEnd:   intPtr(8),
		Ts:        "2025-02-15T14:00:00Z",
		Prompt:    "add helper",
		Reason:    "extracted common logic",
		Change:    "inline → helper()",
		Tool:      "Write",
		Author:    "dev@test.com",
		Session:   "session-xyz",
		Trace:     "traces/session-xyz.jsonl",
		SourceFile: "session-xyz.jsonl",
	}

	d := RowToJSON(row, "/tmp/nonexistent", nil)

	if d["file"] != "pkg/util.go" {
		t.Errorf("file = %v, want pkg/util.go", d["file"])
	}
	if d["ts"] != "2025-02-15T14:00:00Z" {
		t.Errorf("ts = %v", d["ts"])
	}
	if d["prompt"] != "add helper" {
		t.Errorf("prompt = %v", d["prompt"])
	}
	if d["reason"] != "extracted common logic" {
		t.Errorf("reason = %v", d["reason"])
	}
	if d["change"] != "inline → helper()" {
		t.Errorf("change = %v", d["change"])
	}
	if d["tool"] != "Write" {
		t.Errorf("tool = %v", d["tool"])
	}
	if d["author"] != "dev@test.com" {
		t.Errorf("author = %v", d["author"])
	}
	if d["session"] != "session-xyz" {
		t.Errorf("session = %v", d["session"])
	}
	if d["trace"] != "traces/session-xyz.jsonl" {
		t.Errorf("trace = %v", d["trace"])
	}
	if d["source_file"] != "session-xyz.jsonl" {
		t.Errorf("source_file = %v", d["source_file"])
	}
	// lines should be an array-like value
	lines, ok := d["lines"].([2]interface{})
	if !ok {
		t.Fatalf("lines should be [2]interface{}, got %T", d["lines"])
	}
	if *lines[0].(*int) != 5 {
		t.Errorf("lines[0] = %v, want 5", lines[0])
	}
	if *lines[1].(*int) != 8 {
		t.Errorf("lines[1] = %v, want 8", lines[1])
	}
	// Should NOT have adjustment-related keys
	if _, ok := d["current_lines"]; ok {
		t.Error("should not have current_lines without adjustment")
	}
	if _, ok := d["match"]; ok {
		t.Error("should not have match without content hash")
	}
}

func TestRowToJSON_WithAdjustmentAndHash(t *testing.T) {
	row := &index.ReasonRow{
		ID:          11,
		File:        "main.go",
		LineStart:   intPtr(10),
		LineEnd:     intPtr(12),
		Ts:          "2025-01-01T00:00:00Z",
		ContentHash: "deadbeef12345678",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.New(15, 16, 17),
	}

	// File doesn't exist so hash check returns empty → "unknown"
	d := RowToJSON(row, "/tmp/nonexistent", adj)

	cl, ok := d["current_lines"].(string)
	if !ok {
		t.Fatalf("current_lines should be string, got %T", d["current_lines"])
	}
	if cl != "15-17" {
		t.Errorf("current_lines = %q, want %q", cl, "15-17")
	}
	if d["match"] != "unknown" {
		t.Errorf("match = %v, want unknown (file doesn't exist)", d["match"])
	}
}

func TestRowToJSON_WithAdjustmentSuperseded(t *testing.T) {
	row := &index.ReasonRow{
		ID:   12,
		File: "main.go",
		Ts:   "2025-01-01T00:00:00Z",
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.New(1), // non-empty so adj path is taken
		Superseded:   true,
	}

	d := RowToJSON(row, "/tmp/nonexistent", adj)

	if d["superseded"] != true {
		t.Errorf("superseded = %v, want true", d["superseded"])
	}
	if _, ok := d["current_lines"]; !ok {
		t.Error("should have current_lines when adj has non-empty lines")
	}
}

func TestRowToJSON_WithMatchingHash(t *testing.T) {
	dir := t.TempDir()
	// Write a file with known content
	fp := filepath.Join(dir, "main.go")
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Compute expected hash for lines 2-4: "line2\nline3\nline4"
	hash := expectedHash("line2\nline3\nline4")

	row := &index.ReasonRow{
		ID:          13,
		File:        "main.go",
		LineStart:   intPtr(2),
		LineEnd:     intPtr(4),
		Ts:          "2025-01-01T00:00:00Z",
		ContentHash: hash,
	}
	adj := &LineAdjustment{
		CurrentLines: lineset.FromRange(2, 4),
	}

	d := RowToJSON(row, dir, adj)

	if d["match"] != "exact" {
		t.Errorf("match = %v, want exact", d["match"])
	}
	cl, ok := d["current_lines"].(string)
	if !ok {
		t.Fatalf("current_lines should be string, got %T", d["current_lines"])
	}
	if cl != "2-4" {
		t.Errorf("current_lines = %q, want %q", cl, "2-4")
	}
}

func TestCurrentContentHash(t *testing.T) {
	t.Run("known content line range", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "test.go")
		content := "line1\nline2\nline3\nline4\nline5\n"
		if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		// Hash lines 2-4 (1-indexed): "line2\nline3\nline4"
		got := currentContentHash(fp, 2, 4)
		want := expectedHash("line2\nline3\nline4")
		if got != want {
			t.Errorf("currentContentHash(lines 2-4) = %q, want %q", got, want)
		}
	})

	t.Run("full file hash with zero line range", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "test.go")
		content := "alpha\nbeta\ngamma\n"
		if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		// lineStart=0, lineEnd=0 means full file
		got := currentContentHash(fp, 0, 0)
		want := expectedHash(content)
		if got != want {
			t.Errorf("currentContentHash(full file) = %q, want %q", got, want)
		}
	})

	t.Run("returns empty string for missing file", func(t *testing.T) {
		got := currentContentHash("/nonexistent/path/file.go", 1, 5)
		if got != "" {
			t.Errorf("currentContentHash(missing file) = %q, want empty string", got)
		}
	})

	t.Run("whitespace normalization produces same hash", func(t *testing.T) {
		dir := t.TempDir()
		fp1 := filepath.Join(dir, "a.go")
		fp2 := filepath.Join(dir, "b.go")

		// Same tokens, different whitespace
		content1 := "func  main()  {\n\tfmt.Println(\"hi\")\n}\n"
		content2 := "func main() {\n    fmt.Println(\"hi\")\n}\n"
		if err := os.WriteFile(fp1, []byte(content1), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp2, []byte(content2), 0644); err != nil {
			t.Fatal(err)
		}

		hash1 := currentContentHash(fp1, 0, 0)
		hash2 := currentContentHash(fp2, 0, 0)
		if hash1 != hash2 {
			t.Errorf("whitespace-different files should produce same hash, got %q vs %q", hash1, hash2)
		}
	})
}
