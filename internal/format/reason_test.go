package format

import (
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

	// Should display current only (no arrow) since positions didn't change
	if strings.Contains(result, "\u2192") {
		t.Errorf("should NOT contain arrow when positions unchanged, got %s", result)
	}
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

	// Should show original→current
	if !strings.Contains(result, "L10-14") {
		t.Errorf("should contain original L10-14, got %s", result)
	}
	if !strings.Contains(result, "\u2192") {
		t.Errorf("should contain arrow, got %s", result)
	}
	if !strings.Contains(result, "L15-19") {
		t.Errorf("should contain adjusted L15-19, got %s", result)
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
