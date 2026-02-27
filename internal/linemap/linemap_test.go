package linemap

import (
	"reflect"
	"testing"

	"github.com/jensroland/git-blamebot/internal/index"
)

func intPtr(v int) *int { return &v }
func strPtr(v string) *string { return &v }

func TestNoSubsequentEdits(t *testing.T) {
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(14), Tool: "Edit",
			ChangedLines: strPtr("10-14"),
			OldStart: intPtr(10), OldLines: intPtr(3), NewStart: intPtr(10), NewLines: intPtr(5)},
	}
	result := AdjustLinePositions(rows)
	if result[0].Superseded {
		t.Fatal("should not be superseded")
	}
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{10, 11, 12, 13, 14}) {
		t.Fatalf("expected L10-14, got %v", result[0].CurrentLines.Lines())
	}
}

func TestEditBefore_ShiftsDown(t *testing.T) {
	// Edit A at L10-14, then Edit B inserts 3 lines at L5 (before A)
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(14), Tool: "Edit",
			ChangedLines: strPtr("10-14"),
			OldStart: intPtr(10), OldLines: intPtr(3), NewStart: intPtr(10), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(5), LineEnd: intPtr(7), Tool: "Edit",
			ChangedLines: strPtr("5-7"),
			OldStart: intPtr(5), OldLines: intPtr(0), NewStart: intPtr(5), NewLines: intPtr(3)},
	}
	result := AdjustLinePositions(rows)

	// Edit A should shift from L10-14 to L13-17 (shifted by +3)
	if result[0].Superseded {
		t.Fatal("edit A should not be superseded")
	}
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{13, 14, 15, 16, 17}) {
		t.Fatalf("expected [13 14 15 16 17], got %v", result[0].CurrentLines.Lines())
	}

	// Edit B should stay at L5-7
	if !reflect.DeepEqual(result[1].CurrentLines.Lines(), []int{5, 6, 7}) {
		t.Fatalf("expected [5 6 7], got %v", result[1].CurrentLines.Lines())
	}
}

func TestEditAfter_NoChange(t *testing.T) {
	// Edit A at L10-14, then Edit B at L50 (after A)
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(14), Tool: "Edit",
			ChangedLines: strPtr("10-14"),
			OldStart: intPtr(10), OldLines: intPtr(3), NewStart: intPtr(10), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(50), LineEnd: intPtr(52), Tool: "Edit",
			ChangedLines: strPtr("50-52"),
			OldStart: intPtr(50), OldLines: intPtr(3), NewStart: intPtr(50), NewLines: intPtr(3)},
	}
	result := AdjustLinePositions(rows)

	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{10, 11, 12, 13, 14}) {
		t.Fatalf("expected [10 11 12 13 14], got %v", result[0].CurrentLines.Lines())
	}
}

func TestFullOverwrite_Superseded(t *testing.T) {
	// Edit A at L10-14, then Edit B replaces L8-16 (fully contains A)
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(14), Tool: "Edit",
			ChangedLines: strPtr("10-14"),
			OldStart: intPtr(10), OldLines: intPtr(3), NewStart: intPtr(10), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(8), LineEnd: intPtr(10), Tool: "Edit",
			ChangedLines: strPtr("8-10"),
			OldStart: intPtr(8), OldLines: intPtr(9), NewStart: intPtr(8), NewLines: intPtr(3)},
	}
	result := AdjustLinePositions(rows)

	if !result[0].Superseded {
		t.Fatal("edit A should be superseded")
	}
	if !result[0].CurrentLines.IsEmpty() {
		t.Fatal("superseded row should have empty CurrentLines")
	}
}

func TestWriteSupersedes(t *testing.T) {
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(14), Tool: "Edit", ChangedLines: strPtr("10-14")},
		{ID: 2, LineStart: intPtr(20), LineEnd: intPtr(25), Tool: "Edit", ChangedLines: strPtr("20-25")},
		{ID: 3, LineStart: intPtr(1), LineEnd: intPtr(100), Tool: "Write", ChangedLines: strPtr("1-100")},
	}
	result := AdjustLinePositions(rows)

	if !result[0].Superseded {
		t.Fatal("edit 1 should be superseded by Write")
	}
	if !result[1].Superseded {
		t.Fatal("edit 2 should be superseded by Write")
	}
	// The Write itself should not be superseded
	if result[2].Superseded {
		t.Fatal("the Write itself should not be superseded")
	}
}

func TestMultipleCumulativeShifts(t *testing.T) {
	// Edit A at L20-24, then two insertions before it
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(20), LineEnd: intPtr(24), Tool: "Edit",
			ChangedLines: strPtr("20-24"),
			OldStart: intPtr(20), OldLines: intPtr(3), NewStart: intPtr(20), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(5), LineEnd: intPtr(7), Tool: "Edit",
			ChangedLines: strPtr("5-7"),
			OldStart: intPtr(5), OldLines: intPtr(0), NewStart: intPtr(5), NewLines: intPtr(3)},
		{ID: 3, LineStart: intPtr(10), LineEnd: intPtr(11), Tool: "Edit",
			ChangedLines: strPtr("10-11"),
			OldStart: intPtr(10), OldLines: intPtr(0), NewStart: intPtr(10), NewLines: intPtr(2)},
	}
	result := AdjustLinePositions(rows)

	// Edit A shifts +3 (from edit B) then +2 (from edit C) = L25-29
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{25, 26, 27, 28, 29}) {
		t.Fatalf("expected [25 26 27 28 29], got %v", result[0].CurrentLines.Lines())
	}
}

func TestReplacementBefore_ShiftsByDelta(t *testing.T) {
	// Edit A at L20-24, then Edit B replaces 2 lines with 5 at L5
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(20), LineEnd: intPtr(24), Tool: "Edit",
			ChangedLines: strPtr("20-24"),
			OldStart: intPtr(20), OldLines: intPtr(3), NewStart: intPtr(20), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(5), LineEnd: intPtr(9), Tool: "Edit",
			ChangedLines: strPtr("5-9"),
			OldStart: intPtr(5), OldLines: intPtr(2), NewStart: intPtr(5), NewLines: intPtr(5)},
	}
	result := AdjustLinePositions(rows)

	// delta = 5-2 = 3, so L20-24 -> L23-27
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{23, 24, 25, 26, 27}) {
		t.Fatalf("expected [23 24 25 26 27], got %v", result[0].CurrentLines.Lines())
	}
}

func TestDeletionBefore_ShiftsUp(t *testing.T) {
	// Edit A at L20-24, then Edit B deletes 5 lines at L5
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(20), LineEnd: intPtr(24), Tool: "Edit",
			ChangedLines: strPtr("20-24"),
			OldStart: intPtr(20), OldLines: intPtr(3), NewStart: intPtr(20), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(5), LineEnd: intPtr(5), Tool: "Edit",
			ChangedLines: strPtr("5"),
			OldStart: intPtr(5), OldLines: intPtr(5), NewStart: intPtr(5), NewLines: intPtr(0)},
	}
	result := AdjustLinePositions(rows)

	// delta = 0-5 = -5, so L20-24 -> L15-19
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{15, 16, 17, 18, 19}) {
		t.Fatalf("expected [15 16 17 18 19], got %v", result[0].CurrentLines.Lines())
	}
}

func TestMissingHunkData_FallsBack(t *testing.T) {
	// Edit A has hunk data, Edit B (legacy) does not
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(14), Tool: "Edit",
			ChangedLines: strPtr("10-14"),
			OldStart: intPtr(10), OldLines: intPtr(3), NewStart: intPtr(10), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(5), LineEnd: intPtr(7), Tool: "Edit"},
	}
	result := AdjustLinePositions(rows)

	// Edit B has no hunk data, so it's skipped in adjustment.
	// Edit A stays at its stored position.
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{10, 11, 12, 13, 14}) {
		t.Fatalf("expected [10 11 12 13 14], got %v", result[0].CurrentLines.Lines())
	}
}

func TestNilLineStart_Skipped(t *testing.T) {
	rows := []*index.ReasonRow{
		{ID: 1, Tool: "Edit"},
	}
	result := AdjustLinePositions(rows)

	if !result[0].CurrentLines.IsEmpty() {
		t.Fatal("expected empty CurrentLines for record with no line info")
	}
}

func TestPartialOverwrite_RemainingLines(t *testing.T) {
	// Edit A changed lines 10,12,15 (sparse), then Edit B overwrites L11-13
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(15), Tool: "Edit",
			ChangedLines: strPtr("10,12,15"),
			OldStart: intPtr(10), OldLines: intPtr(4), NewStart: intPtr(10), NewLines: intPtr(6)},
		{ID: 2, LineStart: intPtr(11), LineEnd: intPtr(13), Tool: "Edit",
			ChangedLines: strPtr("11-13"),
			OldStart: intPtr(11), OldLines: intPtr(3), NewStart: intPtr(11), NewLines: intPtr(3)},
	}
	result := AdjustLinePositions(rows)

	// Line 10 is before edit (unchanged), line 12 is overwritten (removed),
	// line 15 is after edit (delta=0, unchanged). Result: [10, 15]
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{10, 15}) {
		t.Fatalf("expected [10 15], got %v", result[0].CurrentLines.Lines())
	}
}

func TestLegacyFallback_BoundingRange(t *testing.T) {
	// Legacy record with no ChangedLines — falls back to LineStart/LineEnd range
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(12), Tool: "Edit"},
	}
	result := AdjustLinePositions(rows)

	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{10, 11, 12}) {
		t.Fatalf("expected [10 11 12], got %v", result[0].CurrentLines.Lines())
	}
}

func TestZeroDeltaEdit_NoShift(t *testing.T) {
	// Edit A at L10-14, then Edit B replaces 3 lines with 3 lines at L5
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(14), Tool: "Edit",
			ChangedLines: strPtr("10-14"),
			OldStart: intPtr(10), OldLines: intPtr(3), NewStart: intPtr(10), NewLines: intPtr(5)},
		{ID: 2, LineStart: intPtr(5), LineEnd: intPtr(7), Tool: "Edit",
			ChangedLines: strPtr("5-7"),
			OldStart: intPtr(5), OldLines: intPtr(3), NewStart: intPtr(5), NewLines: intPtr(3)},
	}
	result := AdjustLinePositions(rows)

	// delta = 0, so no shift
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{10, 11, 12, 13, 14}) {
		t.Fatalf("expected [10 11 12 13 14], got %v", result[0].CurrentLines.Lines())
	}
}

func TestInsertionWithinRange_Shifts(t *testing.T) {
	// Edit A has lines 10,12,15, then Edit B inserts 3 lines at L13 (within A's range)
	rows := []*index.ReasonRow{
		{ID: 1, LineStart: intPtr(10), LineEnd: intPtr(15), Tool: "Edit",
			ChangedLines: strPtr("10,12,15"),
			OldStart: intPtr(10), OldLines: intPtr(4), NewStart: intPtr(10), NewLines: intPtr(6)},
		{ID: 2, LineStart: intPtr(13), LineEnd: intPtr(15), Tool: "Edit",
			ChangedLines: strPtr("13-15"),
			OldStart: intPtr(13), OldLines: intPtr(0), NewStart: intPtr(13), NewLines: intPtr(3)},
	}
	result := AdjustLinePositions(rows)

	// Lines 10,12 are before insertion point (unchanged)
	// Line 15 is at/after insertion point: shifted by +3 -> 18
	if !reflect.DeepEqual(result[0].CurrentLines.Lines(), []int{10, 12, 18}) {
		t.Fatalf("expected [10 12 18], got %v", result[0].CurrentLines.Lines())
	}
}
