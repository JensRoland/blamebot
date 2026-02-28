package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/linemap"
	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/record"
)

func TestGroupContiguous(t *testing.T) {
	tests := []struct {
		name   string
		lines  []int
		expect [][]int
	}{
		{"empty", nil, nil},
		{"single", []int{5}, [][]int{{5}}},
		{"contiguous", []int{3, 4, 5}, [][]int{{3, 4, 5}}},
		{"two regions", []int{3, 4, 5, 349, 350, 351, 352, 353}, [][]int{{3, 4, 5}, {349, 350, 351, 352, 353}}},
		{"three regions", []int{1, 2, 10, 11, 12, 50}, [][]int{{1, 2}, {10, 11, 12}, {50}}},
		{"unsorted", []int{350, 3, 5, 349, 4, 351}, [][]int{{3, 4, 5}, {349, 350, 351}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupContiguous(tt.lines)
			if len(got) != len(tt.expect) {
				t.Fatalf("got %d regions, want %d: %v", len(got), len(tt.expect), got)
			}
			for i := range got {
				if len(got[i]) != len(tt.expect[i]) {
					t.Errorf("region %d: got %v, want %v", i, got[i], tt.expect[i])
					continue
				}
				for j := range got[i] {
					if got[i][j] != tt.expect[i][j] {
						t.Errorf("region %d[%d]: got %d, want %d", i, j, got[i][j], tt.expect[i][j])
					}
				}
			}
		})
	}
}

func TestNearestRegion(t *testing.T) {
	regions := [][]int{{3, 4, 5}, {349, 350, 351, 352, 353}}

	tests := []struct {
		name      string
		mid       float64
		expectMin int
		expectMax int
	}{
		{"near bottom (record at L347-351)", 349, 349, 353},
		{"near top (record at L2-4)", 3, 3, 5},
		{"near top (record at L2-3)", 2.5, 3, 5},
		{"exact center of top region", 4, 3, 5},
		{"midpoint favors bottom", 200, 349, 353},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nearestRegion(regions, tt.mid)
			if got[0] != tt.expectMin || got[len(got)-1] != tt.expectMax {
				t.Errorf("nearestRegion(%v, %.1f) = %d-%d, want %d-%d",
					regions, tt.mid, got[0], got[len(got)-1], tt.expectMin, tt.expectMax)
			}
		})
	}
}

func TestParseLineRange(t *testing.T) {
	tests := []struct {
		input string
		start int
		end   int
	}{
		{"42", 42, 42},
		{"10:20", 10, 20},
		{"10,20", 10, 20},
		{"1:1", 1, 1},
		{"5,5", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, end := parseLineRange(tt.input)
			if start != tt.start || end != tt.end {
				t.Errorf("parseLineRange(%q) = (%d, %d), want (%d, %d)",
					tt.input, start, end, tt.start, tt.end)
			}
		})
	}
}

func TestRecordMidpoint(t *testing.T) {
	intPtr := func(v int) *int { return &v }

	tests := []struct {
		name   string
		row    *index.ReasonRow
		expect float64
	}{
		{"nil start", &index.ReasonRow{}, 0},
		{"single line", &index.ReasonRow{LineStart: intPtr(5)}, 5},
		{"range", &index.ReasonRow{LineStart: intPtr(5), LineEnd: intPtr(11)}, 8},
		{"range L347-351", &index.ReasonRow{LineStart: intPtr(347), LineEnd: intPtr(351)}, 349},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordMidpoint(tt.row)
			if got != tt.expect {
				t.Errorf("recordMidpoint = %f, want %f", got, tt.expect)
			}
		})
	}
}

func TestReorderArgs(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{
			"file before flag with value",
			[]string{"file.go", "-L", "42"},
			[]string{"-L", "42", "file.go"},
		},
		{
			"flags already before file",
			[]string{"-L", "42", "file.go"},
			[]string{"-L", "42", "file.go"},
		},
		{
			"boolean flag only",
			[]string{"--stats"},
			[]string{"--stats"},
		},
		{
			"file before grep flag",
			[]string{"file.go", "--grep", "bug"},
			[]string{"--grep", "bug", "file.go"},
		},
		{
			"empty args",
			[]string{},
			[]string{},
		},
		{
			"file only",
			[]string{"file.go"},
			[]string{"file.go"},
		},
		{
			"mixed boolean and value flags with file",
			[]string{"--json", "file.go", "-L", "5"},
			[]string{"--json", "-L", "5", "file.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderArgs(tt.input)
			if len(got) == 0 && len(tt.expect) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.expect) {
				t.Errorf("reorderArgs(%v) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

func TestRelativePath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		root     string
		expect   string
	}{
		{
			"absolute path made relative",
			"/foo/bar/baz.go",
			"/foo/bar",
			"baz.go",
		},
		{
			"already relative",
			"baz.go",
			"/foo/bar",
			"baz.go",
		},
		{
			"path that cannot be made relative returns original",
			"/completely/different/path.go",
			"/foo/bar",
			// filepath.Rel will produce a relative path like "../../completely/different/path.go"
			// which the function still returns (it doesn't error out)
			"../../completely/different/path.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relativePath(tt.filePath, tt.root)
			if got != tt.expect {
				t.Errorf("relativePath(%q, %q) = %q, want %q", tt.filePath, tt.root, got, tt.expect)
			}
		})
	}
}

func TestSortNewestFirst(t *testing.T) {
	rows := []*index.ReasonRow{
		{ID: 1, Ts: "2025-01-01T00:00:00Z"},
		{ID: 2, Ts: "2025-01-02T00:00:00Z"},
		{ID: 3, Ts: "2025-01-03T00:00:00Z"},
	}

	sortNewestFirst(rows)

	// After reversing, newest (ID=3) should be first
	if rows[0].ID != 3 {
		t.Errorf("expected first row ID=3, got ID=%d", rows[0].ID)
	}
	if rows[1].ID != 2 {
		t.Errorf("expected second row ID=2, got ID=%d", rows[1].ID)
	}
	if rows[2].ID != 1 {
		t.Errorf("expected third row ID=1, got ID=%d", rows[2].ID)
	}
}

func TestSortOldestFirst(t *testing.T) {
	rows := []*index.ReasonRow{
		{ID: 3, Ts: "2025-01-03T00:00:00Z"},
		{ID: 1, Ts: "2025-01-01T00:00:00Z"},
		{ID: 2, Ts: "2025-01-02T00:00:00Z"},
	}

	sortOldestFirst(rows)

	// After sorting by Ts ascending, oldest (ID=1) should be first
	if rows[0].ID != 1 {
		t.Errorf("expected first row ID=1, got ID=%d (Ts=%s)", rows[0].ID, rows[0].Ts)
	}
	if rows[1].ID != 2 {
		t.Errorf("expected second row ID=2, got ID=%d (Ts=%s)", rows[1].ID, rows[1].Ts)
	}
	if rows[2].ID != 3 {
		t.Errorf("expected third row ID=3, got ID=%d (Ts=%s)", rows[2].ID, rows[2].Ts)
	}
}

func TestBlameLinesForSHA(t *testing.T) {
	entries := map[int]git.BlameEntry{
		1: {SHA: "aaaa", Line: 1},
		2: {SHA: "bbbb", Line: 2},
		3: {SHA: "aaaa", Line: 3},
		4: {SHA: "cccc", Line: 4},
		5: {SHA: "aaaa", Line: 5},
	}

	result := blameLinesForSHA(entries, "aaaa")
	lines := result.Lines()

	if result.Len() != 3 {
		t.Fatalf("expected 3 lines for SHA 'aaaa', got %d", result.Len())
	}

	// Lines should be sorted
	expected := []int{1, 3, 5}
	for i, l := range lines {
		if l != expected[i] {
			t.Errorf("line[%d] = %d, want %d", i, l, expected[i])
		}
	}

	// Test with a SHA that has no entries
	empty := blameLinesForSHA(entries, "zzzz")
	if !empty.IsEmpty() {
		t.Errorf("expected empty result for unknown SHA, got %d lines", empty.Len())
	}
}

func TestRegionCenter(t *testing.T) {
	tests := []struct {
		name   string
		region []int
		expect float64
	}{
		{"three elements", []int{1, 2, 3}, 2.0},
		{"single element", []int{5}, 5.0},
		{"empty", []int{}, 0.0},
		{"two elements", []int{10, 20}, 15.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := regionCenter(tt.region)
			if got != tt.expect {
				t.Errorf("regionCenter(%v) = %f, want %f", tt.region, got, tt.expect)
			}
		})
	}
}

func TestConstrainBySimulation(t *testing.T) {
	tests := []struct {
		name       string
		sim        *linemap.AdjustedRow
		blameLines lineset.LineSet
		expect     string
	}{
		{
			name:       "intersection narrows blame to AI lines only",
			sim:        &linemap.AdjustedRow{CurrentLines: lineset.New(3, 4, 5)},
			blameLines: lineset.New(3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16),
			expect:     "3-5",
		},
		{
			name:       "no intersection falls back to blame lines",
			sim:        &linemap.AdjustedRow{CurrentLines: lineset.New(100, 101)},
			blameLines: lineset.New(3, 4, 5),
			expect:     "3-5",
		},
		{
			name:       "nil sim returns blame lines",
			sim:        nil,
			blameLines: lineset.New(3, 4, 5),
			expect:     "3-5",
		},
		{
			name:       "superseded sim returns blame lines",
			sim:        &linemap.AdjustedRow{Superseded: true, CurrentLines: lineset.New(3, 4, 5)},
			blameLines: lineset.New(3, 4, 5, 6, 7),
			expect:     "3-7",
		},
		{
			name:       "empty sim lines returns blame lines",
			sim:        &linemap.AdjustedRow{},
			blameLines: lineset.New(3, 4, 5),
			expect:     "3-5",
		},
		{
			name:       "exact match returns same lines",
			sim:        &linemap.AdjustedRow{CurrentLines: lineset.New(3, 4, 5)},
			blameLines: lineset.New(3, 4, 5),
			expect:     "3-5",
		},
		{
			name:       "partial overlap returns intersection",
			sim:        &linemap.AdjustedRow{CurrentLines: lineset.New(3, 4, 5, 6, 7)},
			blameLines: lineset.New(5, 6, 7, 8, 9),
			expect:     "5-7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := constrainBySimulation(tt.sim, tt.blameLines)
			if got.String() != tt.expect {
				t.Errorf("constrainBySimulation = %q, want %q", got.String(), tt.expect)
			}
		})
	}
}

func TestHashOfLines(t *testing.T) {
	fileLines := strings.Split("line 1\nline 2\nline 3\nline 4\nline 5", "\n")

	t.Run("matches ContentHash of same text", func(t *testing.T) {
		got := hashOfLines(fileLines, 2, 4)
		want := record.ContentHash("line 2\nline 3\nline 4")
		if got != want {
			t.Errorf("hashOfLines = %q, want %q", got, want)
		}
	})

	t.Run("out of bounds returns empty", func(t *testing.T) {
		if got := hashOfLines(fileLines, 0, 2); got != "" {
			t.Errorf("start < 1 should return empty, got %q", got)
		}
		if got := hashOfLines(fileLines, 4, 6); got != "" {
			t.Errorf("end > len should return empty, got %q", got)
		}
		if got := hashOfLines(fileLines, 3, 2); got != "" {
			t.Errorf("start > end should return empty, got %q", got)
		}
	})
}

func TestCorrectByContentHash(t *testing.T) {
	// Helper to create a temp file with content and return its project root
	setup := func(t *testing.T, content string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	newLines := func(n int) *int { return &n }
	lineStart := func(n int) *int { return &n }

	t.Run("position correct — no change", func(t *testing.T) {
		// AI content is at lines 3-5 as simulated
		content := "line 1\nline 2\nAI one\nAI two\nAI three\nline 6\n"
		root := setup(t, content)
		aiContent := "AI one\nAI two\nAI three"
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash(aiContent),
			NewLines:    newLines(3),
			Tool:        "Edit",
		}
		got := correctByContentHash(root, row, lineset.FromRange(3, 5))
		if got.String() != "3-5" {
			t.Errorf("expected 3-5, got %s", got.String())
		}
	})

	t.Run("manual insert above shifts AI content down", func(t *testing.T) {
		// AI wrote lines 3-5 but 3 manual lines were inserted at top
		// AI content is now at lines 6-8
		content := "manual 1\nmanual 2\nmanual 3\nline 1\nline 2\nAI one\nAI two\nAI three\nline 6\n"
		root := setup(t, content)
		aiContent := "AI one\nAI two\nAI three"
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash(aiContent),
			NewLines:    newLines(3),
			Tool:        "Edit",
		}
		// Simulation thinks lines 3-5, but actual is 6-8
		got := correctByContentHash(root, row, lineset.FromRange(3, 5))
		if got.String() != "6-8" {
			t.Errorf("expected 6-8, got %s", got.String())
		}
	})

	t.Run("manual delete above shifts AI content up", func(t *testing.T) {
		// AI wrote lines 10-12 but 3 lines were deleted above
		// AI content is now at lines 7-9
		content := "line 1\nline 2\nline 3\nline 7\nline 8\nline 9\nAI one\nAI two\nAI three\nline 13\n"
		root := setup(t, content)
		aiContent := "AI one\nAI two\nAI three"
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash(aiContent),
			NewLines:    newLines(3),
			Tool:        "Edit",
		}
		// Simulation thinks lines 10-12, but actual is 7-9
		got := correctByContentHash(root, row, lineset.FromRange(10, 12))
		if got.String() != "7-9" {
			t.Errorf("expected 7-9, got %s", got.String())
		}
	})

	t.Run("content modified — returns empty (superseded)", func(t *testing.T) {
		// AI wrote "AI one\nAI two\nAI three" but user modified it
		content := "line 1\nline 2\nmodified one\nmodified two\nmodified three\nline 6\n"
		root := setup(t, content)
		aiContent := "AI one\nAI two\nAI three"
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash(aiContent),
			NewLines:    newLines(3),
			Tool:        "Edit",
		}
		got := correctByContentHash(root, row, lineset.FromRange(3, 5))
		// Content not found anywhere — should return empty (superseded)
		if !got.IsEmpty() {
			t.Errorf("expected empty (superseded), got %s", got.String())
		}
	})

	t.Run("empty ContentHash — no correction", func(t *testing.T) {
		root := setup(t, "line 1\nline 2\n")
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: "",
			NewLines:    newLines(1),
			Tool:        "Edit",
		}
		got := correctByContentHash(root, row, lineset.FromRange(1, 1))
		if got.String() != "1" {
			t.Errorf("expected 1, got %s", got.String())
		}
	})

	t.Run("Write tool — skipped", func(t *testing.T) {
		root := setup(t, "line 1\nline 2\n")
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash("line 1"),
			NewLines:    newLines(1),
			Tool:        "Write",
		}
		got := correctByContentHash(root, row, lineset.FromRange(5, 5))
		if got.String() != "5" {
			t.Errorf("expected 5 (no correction for Write), got %s", got.String())
		}
	})

	t.Run("nil NewLines — no correction", func(t *testing.T) {
		root := setup(t, "line 1\nline 2\n")
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash("line 1"),
			Tool:        "Edit",
		}
		got := correctByContentHash(root, row, lineset.FromRange(1, 1))
		if got.String() != "1" {
			t.Errorf("expected 1, got %s", got.String())
		}
	})

	t.Run("file deleted — returns empty (superseded)", func(t *testing.T) {
		row := &index.ReasonRow{
			File:        "nonexistent.txt",
			ContentHash: record.ContentHash("test"),
			NewLines:    newLines(1),
			Tool:        "Edit",
		}
		got := correctByContentHash("/no/such/dir", row, lineset.FromRange(1, 1))
		if !got.IsEmpty() {
			t.Errorf("expected empty (superseded), got %s", got.String())
		}
	})

	t.Run("AI lines deleted from file — returns empty (superseded)", func(t *testing.T) {
		// AI wrote 3 lines, user deleted them entirely
		content := "line 1\nline 2\nline 6\nline 7\n"
		root := setup(t, content)
		aiContent := "AI one\nAI two\nAI three"
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash(aiContent),
			NewLines:    newLines(3),
			Tool:        "Edit",
		}
		got := correctByContentHash(root, row, lineset.FromRange(3, 5))
		if !got.IsEmpty() {
			t.Errorf("expected empty (superseded) for deleted content, got %s", got.String())
		}
	})

	t.Run("empty simLines with LineStart — searches from original", func(t *testing.T) {
		content := "line 1\nline 2\nAI one\nAI two\nline 5\n"
		root := setup(t, content)
		aiContent := "AI one\nAI two"
		row := &index.ReasonRow{
			File:        "test.txt",
			ContentHash: record.ContentHash(aiContent),
			NewLines:    newLines(2),
			LineStart:   lineStart(3),
			Tool:        "Edit",
		}
		got := correctByContentHash(root, row, lineset.LineSet{})
		if got.String() != "3-4" {
			t.Errorf("expected 3-4, got %s", got.String())
		}
	})
}
