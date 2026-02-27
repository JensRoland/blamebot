package cmd

import (
	"testing"

	"github.com/jensroland/git-blamebot/internal/index"
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
