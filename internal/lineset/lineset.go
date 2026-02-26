package lineset

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// LineSet represents a set of 1-based line numbers, stored as a sorted,
// deduplicated slice. It serializes to compact notation like "5,7-8,12".
type LineSet struct {
	lines []int
}

// New creates a LineSet from individual line numbers.
func New(lines ...int) LineSet {
	return LineSet{lines: dedupSorted(lines)}
}

// FromRange creates a LineSet covering a contiguous range [start, end].
func FromRange(start, end int) LineSet {
	if start <= 0 || end < start {
		return LineSet{}
	}
	lines := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		lines = append(lines, i)
	}
	return LineSet{lines: lines}
}

// FromString parses compact notation like "5", "5-7", or "5,7-8,12".
func FromString(s string) (LineSet, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return LineSet{}, nil
	}

	var lines []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "-"); idx >= 0 {
			start, err := strconv.Atoi(strings.TrimSpace(part[:idx]))
			if err != nil {
				return LineSet{}, fmt.Errorf("invalid range start %q: %w", part[:idx], err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(part[idx+1:]))
			if err != nil {
				return LineSet{}, fmt.Errorf("invalid range end %q: %w", part[idx+1:], err)
			}
			if end < start {
				return LineSet{}, fmt.Errorf("invalid range %d-%d", start, end)
			}
			for i := start; i <= end; i++ {
				lines = append(lines, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return LineSet{}, fmt.Errorf("invalid line number %q: %w", part, err)
			}
			lines = append(lines, n)
		}
	}

	return LineSet{lines: dedupSorted(lines)}, nil
}

// String returns the compact notation: "5,7-8,12".
func (ls LineSet) String() string {
	if len(ls.lines) == 0 {
		return ""
	}

	var parts []string
	i := 0
	for i < len(ls.lines) {
		start := ls.lines[i]
		end := start
		for i+1 < len(ls.lines) && ls.lines[i+1] == end+1 {
			end = ls.lines[i+1]
			i++
		}
		if start == end {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, end))
		}
		i++
	}
	return strings.Join(parts, ",")
}

// IsEmpty returns true if the set contains no lines.
func (ls LineSet) IsEmpty() bool {
	return len(ls.lines) == 0
}

// Lines returns the sorted line numbers.
func (ls LineSet) Lines() []int {
	return ls.lines
}

// Len returns the number of lines in the set.
func (ls LineSet) Len() int {
	return len(ls.lines)
}

// Min returns the smallest line number, or 0 if empty.
func (ls LineSet) Min() int {
	if len(ls.lines) == 0 {
		return 0
	}
	return ls.lines[0]
}

// Max returns the largest line number, or 0 if empty.
func (ls LineSet) Max() int {
	if len(ls.lines) == 0 {
		return 0
	}
	return ls.lines[len(ls.lines)-1]
}

// Contains returns true if the given line number is in the set.
func (ls LineSet) Contains(line int) bool {
	i := sort.SearchInts(ls.lines, line)
	return i < len(ls.lines) && ls.lines[i] == line
}

// Overlaps returns true if any line in [start, end] is in the set.
func (ls LineSet) Overlaps(start, end int) bool {
	if len(ls.lines) == 0 {
		return false
	}
	// Find first line >= start
	i := sort.SearchInts(ls.lines, start)
	return i < len(ls.lines) && ls.lines[i] <= end
}

// MarshalJSON serializes as a JSON string in compact notation.
func (ls LineSet) MarshalJSON() ([]byte, error) {
	s := ls.String()
	if s == "" {
		return []byte("null"), nil
	}
	return json.Marshal(s)
}

// UnmarshalJSON handles both the new string format ("5,7-8,12")
// and the legacy two-element array format ([5,12]).
func (ls *LineSet) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" || s == "" {
		ls.lines = nil
		return nil
	}

	// New format: JSON string
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		parsed, err := FromString(str)
		if err != nil {
			return err
		}
		ls.lines = parsed.lines
		return nil
	}

	// Legacy format: JSON array [start, end] or [null, null]
	if s[0] == '[' {
		var arr [2]*int
		// Use custom parsing to handle null entries
		var raw [2]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		for i, r := range raw {
			rs := strings.TrimSpace(string(r))
			if rs == "null" || rs == "" {
				continue
			}
			var v int
			if err := json.Unmarshal(r, &v); err != nil {
				return err
			}
			arr[i] = &v
		}
		if arr[0] != nil && arr[1] != nil {
			*ls = FromRange(*arr[0], *arr[1])
		} else if arr[0] != nil {
			*ls = New(*arr[0])
		} else {
			ls.lines = nil
		}
		return nil
	}

	return fmt.Errorf("unexpected JSON for LineSet: %s", s)
}

func dedupSorted(nums []int) []int {
	if len(nums) == 0 {
		return nil
	}
	sort.Ints(nums)
	result := []int{nums[0]}
	for i := 1; i < len(nums); i++ {
		if nums[i] != nums[i-1] {
			result = append(result, nums[i])
		}
	}
	return result
}
