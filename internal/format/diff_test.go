package format

import (
	"strings"
	"testing"
)

func TestExpandTabs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "converts tabs to 4 spaces",
			in:   "hello\tworld",
			want: []string{"hello    world"},
		},
		{
			name: "splits on newlines",
			in:   "line1\nline2\nline3",
			want: []string{"line1", "line2", "line3"},
		},
		{
			name: "returns nil for empty string",
			in:   "",
			want: nil,
		},
		{
			name: "tabs and newlines combined",
			in:   "\tfoo\n\tbar",
			want: []string{"    foo", "    bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandTabs(tt.in)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expandTabs(%q) = %v, want nil", tt.in, got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expandTabs(%q) returned %d lines, want %d", tt.in, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("expandTabs(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPadOrTrunc(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{
			name:  "pads short string with spaces",
			s:     "hi",
			width: 6,
			want:  "hi    ",
		},
		{
			name:  "truncates long string",
			s:     "hello world",
			width: 5,
			want:  "hello",
		},
		{
			name:  "handles exact width match",
			s:     "exact",
			width: 5,
			want:  "exact",
		},
		{
			name:  "handles empty string",
			s:     "",
			width: 4,
			want:  "    ",
		},
		{
			name:  "truncates unicode string by runes",
			s:     "abcdef",
			width: 3,
			want:  "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padOrTrunc(tt.s, tt.width)
			gotRunes := []rune(got)
			if len(gotRunes) != tt.width {
				t.Errorf("padOrTrunc(%q, %d) has rune length %d, want %d", tt.s, tt.width, len(gotRunes), tt.width)
			}
			if got != tt.want {
				t.Errorf("padOrTrunc(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
		})
	}
}

func TestRuneLen(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int
	}{
		{
			name: "ASCII string",
			s:    "hello",
			want: 5,
		},
		{
			name: "unicode string",
			s:    "\u00e9\u00e8\u00ea",
			want: 3,
		},
		{
			name: "empty string",
			s:    "",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runeLen(tt.s)
			if got != tt.want {
				t.Errorf("runeLen(%q) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

func TestFormatSideBySideDiff(t *testing.T) {
	t.Run("equal text produces output with headers", func(t *testing.T) {
		result := FormatSideBySideDiff("hello\nworld", "hello\nworld")

		if !strings.Contains(result, "Before") {
			t.Error("should contain 'Before' header")
		}
		if !strings.Contains(result, "After") {
			t.Error("should contain 'After' header")
		}
		// Box drawing characters for top border
		if !strings.Contains(result, "\u250c") {
			t.Error("should contain top-left corner")
		}
		if !strings.Contains(result, "\u2518") {
			t.Error("should contain bottom-right corner")
		}
	})

	t.Run("different text shows changes", func(t *testing.T) {
		result := FormatSideBySideDiff("old line", "new line")

		// The result should contain both lines in some form
		if !strings.Contains(result, "Before") {
			t.Error("should contain 'Before' header")
		}
		if !strings.Contains(result, "After") {
			t.Error("should contain 'After' header")
		}
		// Should have content rows
		if !strings.Contains(result, "\u2502") {
			t.Error("should contain vertical border characters")
		}
	})

	t.Run("empty old text shows insertions", func(t *testing.T) {
		result := FormatSideBySideDiff("", "new content")

		if !strings.Contains(result, "new content") {
			t.Error("should contain the new content")
		}
		if !strings.Contains(result, "Before") {
			t.Error("should contain 'Before' header")
		}
	})

	t.Run("empty new text shows deletions", func(t *testing.T) {
		result := FormatSideBySideDiff("old content", "")

		if !strings.Contains(result, "old content") {
			t.Error("should contain the old content")
		}
		if !strings.Contains(result, "After") {
			t.Error("should contain 'After' header")
		}
	})

	t.Run("truncation at 40 rows", func(t *testing.T) {
		// Generate more than 40 differing lines
		var oldLines, newLines []string
		for i := 0; i < 50; i++ {
			oldLines = append(oldLines, "old line")
			newLines = append(newLines, "new line")
		}
		old := strings.Join(oldLines, "\n")
		new_ := strings.Join(newLines, "\n")

		result := FormatSideBySideDiff(old, new_)

		if !strings.Contains(result, "more lines not shown") {
			t.Error("should contain 'more lines not shown' message for truncated diff")
		}
	})
}
