package lineset

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		{name: "empty", input: "", want: nil},
		{name: "single", input: "5", want: []int{5}},
		{name: "range", input: "5-7", want: []int{5, 6, 7}},
		{name: "mixed", input: "5,7-8,12", want: []int{5, 7, 8, 12}},
		{name: "single_line_range", input: "3-3", want: []int{3}},
		{name: "whitespace", input: " 5 , 7 - 8 , 12 ", want: []int{5, 7, 8, 12}},
		{name: "invalid_number", input: "abc", wantErr: true},
		{name: "invalid_range", input: "5-3", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got.Lines(), tt.want) {
				t.Errorf("FromString(%q) = %v, want %v", tt.input, got.Lines(), tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name  string
		lines []int
		want  string
	}{
		{name: "empty", lines: nil, want: ""},
		{name: "single", lines: []int{5}, want: "5"},
		{name: "range", lines: []int{5, 6, 7}, want: "5-7"},
		{name: "mixed", lines: []int{5, 7, 8, 12}, want: "5,7-8,12"},
		{name: "all_separate", lines: []int{1, 3, 5}, want: "1,3,5"},
		{name: "two_ranges", lines: []int{1, 2, 3, 7, 8, 9}, want: "1-3,7-9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ls := New(tt.lines...)
			if got := ls.String(); got != tt.want {
				t.Errorf("New(%v).String() = %q, want %q", tt.lines, got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	inputs := []string{"5", "5-7", "5,7-8,12", "1,3,5", "1-3,7-9"}
	for _, s := range inputs {
		ls, err := FromString(s)
		if err != nil {
			t.Fatalf("FromString(%q) error: %v", s, err)
		}
		if got := ls.String(); got != s {
			t.Errorf("round-trip failed: FromString(%q).String() = %q", s, got)
		}
	}
}

func TestBoundingRange(t *testing.T) {
	ls := New(5, 7, 8, 12)
	min, max := ls.Min(), ls.Max()
	if min != 5 || max != 12 {
		t.Errorf("Min/Max = %d/%d, want 5/12", min, max)
	}

	empty := LineSet{}
	if empty.Min() != 0 || empty.Max() != 0 {
		t.Errorf("empty Min/Max = %d/%d, want 0/0", empty.Min(), empty.Max())
	}
}

func TestContains(t *testing.T) {
	ls := New(5, 7, 8, 12)
	for _, n := range []int{5, 7, 8, 12} {
		if !ls.Contains(n) {
			t.Errorf("Contains(%d) = false, want true", n)
		}
	}
	for _, n := range []int{1, 6, 9, 11, 13} {
		if ls.Contains(n) {
			t.Errorf("Contains(%d) = true, want false", n)
		}
	}
}

func TestOverlaps(t *testing.T) {
	ls := New(5, 7, 8, 12)

	tests := []struct {
		start, end int
		want       bool
	}{
		{1, 4, false},
		{1, 5, true},
		{6, 6, false},
		{7, 8, true},
		{9, 11, false},
		{12, 15, true},
		{13, 20, false},
	}
	for _, tt := range tests {
		if got := ls.Overlaps(tt.start, tt.end); got != tt.want {
			t.Errorf("Overlaps(%d, %d) = %v, want %v", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestMarshalJSON(t *testing.T) {
	ls := New(5, 7, 8, 12)
	b, err := json.Marshal(ls)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"5,7-8,12"` {
		t.Errorf("MarshalJSON = %s, want %q", b, "5,7-8,12")
	}

	// Empty set marshals as null
	empty := LineSet{}
	b, _ = json.Marshal(empty)
	if string(b) != "null" {
		t.Errorf("empty MarshalJSON = %s, want null", b)
	}
}

func TestUnmarshalJSON_NewFormat(t *testing.T) {
	var ls LineSet
	err := json.Unmarshal([]byte(`"5,7-8,12"`), &ls)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ls.Lines(), []int{5, 7, 8, 12}) {
		t.Errorf("UnmarshalJSON new format = %v, want [5 7 8 12]", ls.Lines())
	}
}

func TestUnmarshalJSON_LegacyArray(t *testing.T) {
	var ls LineSet
	err := json.Unmarshal([]byte(`[5,12]`), &ls)
	if err != nil {
		t.Fatal(err)
	}
	// Legacy [5,12] means range 5-12
	if ls.Min() != 5 || ls.Max() != 12 || ls.Len() != 8 {
		t.Errorf("UnmarshalJSON legacy = %v (len %d), want range 5-12 (len 8)", ls.Lines(), ls.Len())
	}
}

func TestUnmarshalJSON_LegacyNull(t *testing.T) {
	var ls LineSet
	err := json.Unmarshal([]byte(`[null,null]`), &ls)
	if err != nil {
		t.Fatal(err)
	}
	if !ls.IsEmpty() {
		t.Errorf("UnmarshalJSON [null,null] should be empty, got %v", ls.Lines())
	}
}

func TestUnmarshalJSON_Null(t *testing.T) {
	var ls LineSet
	err := json.Unmarshal([]byte(`null`), &ls)
	if err != nil {
		t.Fatal(err)
	}
	if !ls.IsEmpty() {
		t.Errorf("UnmarshalJSON null should be empty, got %v", ls.Lines())
	}
}

func TestFromRange(t *testing.T) {
	ls := FromRange(3, 6)
	if !reflect.DeepEqual(ls.Lines(), []int{3, 4, 5, 6}) {
		t.Errorf("FromRange(3,6) = %v, want [3 4 5 6]", ls.Lines())
	}

	empty := FromRange(5, 3) // invalid
	if !empty.IsEmpty() {
		t.Errorf("FromRange(5,3) should be empty, got %v", empty.Lines())
	}

	empty2 := FromRange(0, 5) // start <= 0
	if !empty2.IsEmpty() {
		t.Errorf("FromRange(0,5) should be empty, got %v", empty2.Lines())
	}
}
