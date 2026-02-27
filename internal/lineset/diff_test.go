package lineset

import (
	"reflect"
	"strings"
	"testing"
)

func TestChangedLines_AllNew(t *testing.T) {
	ls := ChangedLines("", "a\nb\nc", 10)
	if !reflect.DeepEqual(ls.Lines(), []int{10, 11, 12}) {
		t.Errorf("all new = %v, want [10 11 12]", ls.Lines())
	}
}

func TestChangedLines_SingleLineChange(t *testing.T) {
	ls := ChangedLines("a\nb\nc", "a\nX\nc", 5)
	if !reflect.DeepEqual(ls.Lines(), []int{6}) {
		t.Errorf("single change = %v, want [6]", ls.Lines())
	}
}

func TestChangedLines_MultipleScatteredChanges(t *testing.T) {
	old := "a\nb\nc\nd\ne"
	new := "a\nX\nc\nY\ne"
	ls := ChangedLines(old, new, 10)
	if !reflect.DeepEqual(ls.Lines(), []int{11, 13}) {
		t.Errorf("scattered = %v, want [11 13]", ls.Lines())
	}
}

func TestChangedLines_Insertion(t *testing.T) {
	ls := ChangedLines("a\nc", "a\nb\nc", 5)
	// Line 6 is inserted; lines 5 and 7 match old "a" and "c"
	if !reflect.DeepEqual(ls.Lines(), []int{6}) {
		t.Errorf("insertion = %v, want [6]", ls.Lines())
	}
}

func TestChangedLines_Deletion(t *testing.T) {
	// Pure deletion: "a\nb\nc" -> "a\nc"
	// All new lines are in LCS, but strings differ, so fallback to bounding range
	ls := ChangedLines("a\nb\nc", "a\nc", 5)
	if !reflect.DeepEqual(ls.Lines(), []int{5, 6}) {
		t.Errorf("deletion = %v, want [5 6]", ls.Lines())
	}
}

func TestChangedLines_CompleteRewrite(t *testing.T) {
	ls := ChangedLines("a\nb\nc", "x\ny\nz", 1)
	if !reflect.DeepEqual(ls.Lines(), []int{1, 2, 3}) {
		t.Errorf("rewrite = %v, want [1 2 3]", ls.Lines())
	}
}

func TestChangedLines_Identical(t *testing.T) {
	ls := ChangedLines("a\nb\nc", "a\nb\nc", 1)
	if !ls.IsEmpty() {
		t.Errorf("identical = %v, want empty", ls.Lines())
	}
}

func TestChangedLines_FirstAndLastChanged(t *testing.T) {
	ls := ChangedLines("X\nb\nc\nd\nY", "A\nb\nc\nd\nZ", 10)
	if !reflect.DeepEqual(ls.Lines(), []int{10, 14}) {
		t.Errorf("first+last = %v, want [10 14]", ls.Lines())
	}
}

func TestChangedLines_LargeEditFallback(t *testing.T) {
	// Create strings large enough to trigger the guard
	old := strings.Repeat("line\n", 101) // 102 lines
	new := strings.Repeat("line\n", 101) // 102 lines but different due to trailing
	// Modify one line so they're not identical
	new = "changed\n" + new
	ls := ChangedLines(old, new, 1)
	// 101*102 > 10000, so should fall back to full range
	newLineCount := len(splitLines(new))
	if ls.Min() != 1 || ls.Max() != newLineCount {
		t.Errorf("large edit fallback: got %s (min=%d max=%d), want range 1-%d",
			ls.String(), ls.Min(), ls.Max(), newLineCount)
	}
}

func TestChangedLines_InsertionAtStart(t *testing.T) {
	ls := ChangedLines("b\nc", "a\nb\nc", 1)
	if !reflect.DeepEqual(ls.Lines(), []int{1}) {
		t.Errorf("insert at start = %v, want [1]", ls.Lines())
	}
}

func TestChangedLines_InsertionAtEnd(t *testing.T) {
	ls := ChangedLines("a\nb", "a\nb\nc", 1)
	if !reflect.DeepEqual(ls.Lines(), []int{3}) {
		t.Errorf("insert at end = %v, want [3]", ls.Lines())
	}
}
