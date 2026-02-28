package checkpoint

import (
	"fmt"
	"strings"
	"testing"
)

func TestTransformAttribution_InsertAbove(t *testing.T) {
	oldLines := []string{"line1", "line2", "line3"}
	newLines := []string{"new", "line1", "line2", "line3"}
	oldAttr := []string{"", "", ""}

	result := TransformAttribution(oldLines, newLines, oldAttr, "edit-1")

	// "new" is inserted, should be attributed to "edit-1"
	// "line1", "line2", "line3" are carried forward with ""
	expected := []string{"edit-1", "", "", ""}
	assertAttr(t, result, expected)
}

func TestTransformAttribution_InsertMiddle(t *testing.T) {
	oldLines := []string{"line1", "line2", "line3"}
	newLines := []string{"line1", "new", "line2", "line3"}
	oldAttr := []string{"", "", ""}

	result := TransformAttribution(oldLines, newLines, oldAttr, "edit-1")

	expected := []string{"", "edit-1", "", ""}
	assertAttr(t, result, expected)
}

func TestTransformAttribution_DeleteLine(t *testing.T) {
	oldLines := []string{"line1", "line2", "line3"}
	newLines := []string{"line1", "line3"}
	oldAttr := []string{"edit-a", "edit-b", "edit-c"}

	result := TransformAttribution(oldLines, newLines, oldAttr, "human")

	// line1 (edit-a) and line3 (edit-c) carry forward
	expected := []string{"edit-a", "edit-c"}
	assertAttr(t, result, expected)
}

func TestTransformAttribution_ReplaceLine(t *testing.T) {
	oldLines := []string{"line1", "line2", "line3"}
	newLines := []string{"line1", "replaced", "line3"}
	oldAttr := []string{"edit-a", "edit-b", "edit-c"}

	result := TransformAttribution(oldLines, newLines, oldAttr, "edit-2")

	// line1 and line3 carry forward, "replaced" gets "edit-2"
	expected := []string{"edit-a", "edit-2", "edit-c"}
	assertAttr(t, result, expected)
}

func TestTransformAttribution_NoChange(t *testing.T) {
	oldLines := []string{"line1", "line2", "line3"}
	newLines := []string{"line1", "line2", "line3"}
	oldAttr := []string{"edit-a", "", "edit-b"}

	result := TransformAttribution(oldLines, newLines, oldAttr, "edit-2")

	// All lines match, carry forward
	expected := []string{"edit-a", "", "edit-b"}
	assertAttr(t, result, expected)
}

func TestTransformAttribution_EmptyOld(t *testing.T) {
	oldLines := []string{}
	newLines := []string{"line1", "line2"}
	oldAttr := []string{}

	result := TransformAttribution(oldLines, newLines, oldAttr, "edit-1")

	// New file, all lines attributed to actor
	expected := []string{"edit-1", "edit-1"}
	assertAttr(t, result, expected)
}

func TestTransformAttribution_EmptyNew(t *testing.T) {
	oldLines := []string{"line1", "line2"}
	newLines := []string{}
	oldAttr := []string{"edit-a", "edit-b"}

	result := TransformAttribution(oldLines, newLines, oldAttr, "edit-1")

	if result != nil {
		t.Errorf("expected nil for deleted file, got %v", result)
	}
}

func TestComputeFileAttribution_SingleAIEdit(t *testing.T) {
	dir := t.TempDir()

	baseContent := "line1\nline2\nline3"
	afterEdit := "line1\nnew_ai_line\nline2\nline3"

	baseSHA, _ := WriteBlob(dir, baseContent)
	afterSHA, _ := WriteBlob(dir, afterEdit)

	checkpoints := []Checkpoint{
		{Kind: "pre-edit", File: "f.go", ContentSHA: baseSHA, ToolUseID: "t1", Ts: "2024-01-01T00:00:01Z"},
		{Kind: "post-edit", File: "f.go", ContentSHA: afterSHA, EditID: "edit-1", ToolUseID: "t1", Ts: "2024-01-01T00:00:02Z"},
	}

	blobReader := func(sha string) string {
		content, _ := ReadBlob(dir, sha)
		return content
	}

	// Current content is same as after edit (no subsequent manual changes)
	attr := ComputeFileAttribution(baseContent, afterEdit, checkpoints, blobReader)

	if len(attr) != 1 {
		t.Fatalf("expected 1 edit in attribution, got %d", len(attr))
	}

	ls, ok := attr["edit-1"]
	if !ok {
		t.Fatal("expected edit-1 in attribution")
	}
	// The inserted line is at position 2 (1-indexed)
	if !ls.Contains(2) {
		t.Errorf("expected line 2 to be attributed to edit-1, got %s", ls.String())
	}
	// Other lines should NOT be attributed
	if ls.Contains(1) || ls.Contains(3) || ls.Contains(4) {
		t.Errorf("only line 2 should be attributed, got %s", ls.String())
	}
}

func TestComputeFileAttribution_AIEditThenHumanShift(t *testing.T) {
	dir := t.TempDir()

	baseContent := "line1\nline2\nline3"
	afterAIEdit := "line1\nai_line\nline2\nline3"
	currentContent := "human_line\nline1\nai_line\nline2\nline3"

	baseSHA, _ := WriteBlob(dir, baseContent)
	afterAISHA, _ := WriteBlob(dir, afterAIEdit)

	checkpoints := []Checkpoint{
		{Kind: "pre-edit", File: "f.go", ContentSHA: baseSHA, ToolUseID: "t1", Ts: "2024-01-01T00:00:01Z"},
		{Kind: "post-edit", File: "f.go", ContentSHA: afterAISHA, EditID: "edit-1", ToolUseID: "t1", Ts: "2024-01-01T00:00:02Z"},
	}

	blobReader := func(sha string) string {
		content, _ := ReadBlob(dir, sha)
		return content
	}

	attr := ComputeFileAttribution(baseContent, currentContent, checkpoints, blobReader)

	ls, ok := attr["edit-1"]
	if !ok {
		t.Fatal("expected edit-1 in attribution")
	}
	// AI line should now be at position 3 (shifted down by human insert)
	if !ls.Contains(3) {
		t.Errorf("expected ai_line at position 3, got %s", ls.String())
	}
	if ls.Len() != 1 {
		t.Errorf("expected exactly 1 attributed line, got %d", ls.Len())
	}
}

func TestComputeFileAttribution_AIEditThenHumanSplit(t *testing.T) {
	dir := t.TempDir()

	baseContent := "line1\nline2\nline3"
	afterAIEdit := "line1\nai_a\nai_b\nai_c\nline2\nline3"
	// Human inserts a line in the middle of the AI block
	currentContent := "line1\nai_a\nhuman_split\nai_b\nai_c\nline2\nline3"

	baseSHA, _ := WriteBlob(dir, baseContent)
	afterAISHA, _ := WriteBlob(dir, afterAIEdit)

	checkpoints := []Checkpoint{
		{Kind: "pre-edit", File: "f.go", ContentSHA: baseSHA, ToolUseID: "t1", Ts: "2024-01-01T00:00:01Z"},
		{Kind: "post-edit", File: "f.go", ContentSHA: afterAISHA, EditID: "edit-1", ToolUseID: "t1", Ts: "2024-01-01T00:00:02Z"},
	}

	blobReader := func(sha string) string {
		content, _ := ReadBlob(dir, sha)
		return content
	}

	attr := ComputeFileAttribution(baseContent, currentContent, checkpoints, blobReader)

	ls, ok := attr["edit-1"]
	if !ok {
		t.Fatal("expected edit-1 in attribution")
	}
	// AI lines are at positions 2, 4, 5 (split by human insert at 3)
	if !ls.Contains(2) || !ls.Contains(4) || !ls.Contains(5) {
		t.Errorf("expected lines 2,4,5 attributed to edit-1, got %s", ls.String())
	}
	if ls.Contains(3) {
		t.Errorf("line 3 (human_split) should not be attributed, got %s", ls.String())
	}
	if ls.Len() != 3 {
		t.Errorf("expected 3 attributed lines, got %d", ls.Len())
	}
}

func TestComputeFileAttribution_TwoAIEditsWithHumanBetween(t *testing.T) {
	dir := t.TempDir()

	baseContent := "line1\nline2\nline3"
	afterEdit1 := "line1\nai1\nline2\nline3"
	beforeEdit2 := "line1\nai1\nhuman\nline2\nline3"
	afterEdit2 := "line1\nai1\nhuman\nline2\nai2\nline3"

	baseSHA, _ := WriteBlob(dir, baseContent)
	afterEdit1SHA, _ := WriteBlob(dir, afterEdit1)
	beforeEdit2SHA, _ := WriteBlob(dir, beforeEdit2)
	afterEdit2SHA, _ := WriteBlob(dir, afterEdit2)

	checkpoints := []Checkpoint{
		{Kind: "pre-edit", File: "f.go", ContentSHA: baseSHA, ToolUseID: "t1", Ts: "2024-01-01T00:00:01Z"},
		{Kind: "post-edit", File: "f.go", ContentSHA: afterEdit1SHA, EditID: "edit-1", ToolUseID: "t1", Ts: "2024-01-01T00:00:02Z"},
		{Kind: "pre-edit", File: "f.go", ContentSHA: beforeEdit2SHA, ToolUseID: "t2", Ts: "2024-01-01T00:00:03Z"},
		{Kind: "post-edit", File: "f.go", ContentSHA: afterEdit2SHA, EditID: "edit-2", ToolUseID: "t2", Ts: "2024-01-01T00:00:04Z"},
	}

	blobReader := func(sha string) string {
		content, _ := ReadBlob(dir, sha)
		return content
	}

	// Current = final post-edit content (no subsequent human changes)
	attr := ComputeFileAttribution(baseContent, afterEdit2, checkpoints, blobReader)

	if len(attr) != 2 {
		t.Fatalf("expected 2 edits in attribution, got %d", len(attr))
	}

	ls1 := attr["edit-1"]
	if !ls1.Contains(2) || ls1.Len() != 1 {
		t.Errorf("expected edit-1 at line 2, got %s", ls1.String())
	}

	ls2 := attr["edit-2"]
	if !ls2.Contains(5) || ls2.Len() != 1 {
		t.Errorf("expected edit-2 at line 5, got %s", ls2.String())
	}
}

func TestComputeFileAttribution_NewFile(t *testing.T) {
	dir := t.TempDir()

	// File didn't exist before
	afterEdit := "new_line1\nnew_line2"

	afterSHA, _ := WriteBlob(dir, afterEdit)
	// Pre-edit content is empty (file doesn't exist yet)
	emptySHA, _ := WriteBlob(dir, "")

	checkpoints := []Checkpoint{
		{Kind: "pre-edit", File: "f.go", ContentSHA: emptySHA, ToolUseID: "t1", Ts: "2024-01-01T00:00:01Z"},
		{Kind: "post-edit", File: "f.go", ContentSHA: afterSHA, EditID: "edit-1", ToolUseID: "t1", Ts: "2024-01-01T00:00:02Z"},
	}

	blobReader := func(sha string) string {
		content, _ := ReadBlob(dir, sha)
		return content
	}

	attr := ComputeFileAttribution("", afterEdit, checkpoints, blobReader)

	ls := attr["edit-1"]
	if ls.Len() != 2 {
		t.Errorf("expected 2 lines attributed to edit-1, got %d", ls.Len())
	}
	if !ls.Contains(1) || !ls.Contains(2) {
		t.Errorf("expected lines 1,2 attributed, got %s", ls.String())
	}
}

func TestComputeFileAttribution_EmptyCheckpoints(t *testing.T) {
	attr := ComputeFileAttribution("line1\nline2", "line1\nline2", nil, nil)
	if len(attr) != 0 {
		t.Errorf("expected empty attribution for no checkpoints, got %v", attr)
	}
}

func TestComputeFileAttribution_IncompletePair(t *testing.T) {
	dir := t.TempDir()

	// Only a pre-edit checkpoint, no post-edit
	baseSHA, _ := WriteBlob(dir, "line1")

	checkpoints := []Checkpoint{
		{Kind: "pre-edit", File: "f.go", ContentSHA: baseSHA, ToolUseID: "t1", Ts: "2024-01-01T00:00:01Z"},
	}

	blobReader := func(sha string) string {
		content, _ := ReadBlob(dir, sha)
		return content
	}

	attr := ComputeFileAttribution("line1", "line1", checkpoints, blobReader)
	if len(attr) != 0 {
		t.Errorf("expected empty attribution for incomplete pair, got %v", attr)
	}
}

func TestComputeFileAttribution_LargeFile(t *testing.T) {
	dir := t.TempDir()

	// Build a 350-line file, replace 3 lines in the middle, then insert 2 lines
	// (splits the AI block). This tests that LCS works on real-sized files.
	var baseLines []string
	for i := 1; i <= 350; i++ {
		baseLines = append(baseLines, fmt.Sprintf("original-line-%d", i))
	}
	baseContent := strings.Join(baseLines, "\n")

	// AI edit: replace lines 100-102
	afterEditLines := make([]string, len(baseLines))
	copy(afterEditLines, baseLines)
	afterEditLines[99] = "ai-replaced-100"
	afterEditLines[100] = "ai-replaced-101"
	afterEditLines[101] = "ai-replaced-102"
	afterEditContent := strings.Join(afterEditLines, "\n")

	// Human insert at line 101 (splits the AI block)
	var currentLines []string
	currentLines = append(currentLines, afterEditLines[:100]...)
	currentLines = append(currentLines, "human-inserted")
	currentLines = append(currentLines, afterEditLines[100:]...)
	currentContent := strings.Join(currentLines, "\n")

	baseSHA, _ := WriteBlob(dir, baseContent)
	afterSHA, _ := WriteBlob(dir, afterEditContent)

	checkpoints := []Checkpoint{
		{Kind: "pre-edit", File: "big.go", ContentSHA: baseSHA, ToolUseID: "t1", Ts: "2024-01-01T00:00:01Z"},
		{Kind: "post-edit", File: "big.go", ContentSHA: afterSHA, EditID: "edit-1", ToolUseID: "t1", Ts: "2024-01-01T00:00:02Z"},
	}

	blobReader := func(sha string) string {
		content, _ := ReadBlob(dir, sha)
		return content
	}

	attr := ComputeFileAttribution(baseContent, currentContent, checkpoints, blobReader)

	ls, ok := attr["edit-1"]
	if !ok {
		t.Fatal("expected edit-1 in attribution")
	}
	// AI lines should be at 100, 102, 103 (shifted by human insert at 101)
	if !ls.Contains(100) {
		t.Errorf("expected line 100 attributed, got %s", ls.String())
	}
	if !ls.Contains(102) {
		t.Errorf("expected line 102 attributed, got %s", ls.String())
	}
	if !ls.Contains(103) {
		t.Errorf("expected line 103 attributed, got %s", ls.String())
	}
	if ls.Contains(101) {
		t.Errorf("line 101 (human insert) should not be attributed, got %s", ls.String())
	}
	if ls.Len() != 3 {
		t.Errorf("expected 3 attributed lines, got %d: %s", ls.Len(), ls.String())
	}
}

func TestLcsMatching(t *testing.T) {
	a := strings.Split("A\nB\nC\nD", "\n")
	b := strings.Split("A\nX\nB\nD", "\n")

	matchedOld, matchedNew := lcsMatching(a, b)

	// A matches A, B matches B, D matches D, C is deleted, X is inserted
	if matchedOld[0] != 0 { // A → A
		t.Errorf("expected A(0) to match 0, got %d", matchedOld[0])
	}
	if matchedOld[1] != 2 { // B → B (at index 2 in new)
		t.Errorf("expected B(1) to match 2, got %d", matchedOld[1])
	}
	if matchedOld[2] != -1 { // C is deleted
		t.Errorf("expected C(2) to be -1, got %d", matchedOld[2])
	}
	if matchedOld[3] != 3 { // D → D
		t.Errorf("expected D(3) to match 3, got %d", matchedOld[3])
	}
	if matchedNew[1] != -1 { // X is inserted
		t.Errorf("expected X(1) to be -1, got %d", matchedNew[1])
	}
}

func assertAttr(t *testing.T, got, expected []string) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("attribution length mismatch: got %d, want %d\ngot:      %v\nexpected: %v", len(got), len(expected), got, expected)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Errorf("attribution[%d]: got %q, want %q\ngot:      %v\nexpected: %v", i, got[i], expected[i], got, expected)
			return
		}
	}
}
