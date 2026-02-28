package format

import (
	"strings"
	"testing"
)

func TestWordWrap(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{
			name:  "text shorter than width stays on one line",
			text:  "hello world",
			width: 80,
			want:  []string{"hello world"},
		},
		{
			name:  "text wraps at word boundaries",
			text:  "the quick brown fox jumps over the lazy dog",
			width: 20,
			want:  []string{"the quick brown fox", "jumps over the lazy", "dog"},
		},
		{
			name:  "empty string returns single empty string",
			text:  "",
			width: 40,
			want:  []string{""},
		},
		{
			name:  "single very long word",
			text:  "superlongwordthatexceedswidth",
			width: 10,
			want:  []string{"superlongwordthatexceedswidth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wordWrap(tt.text, tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("wordWrap(%q, %d) returned %d lines, want %d\ngot:  %v\nwant: %v",
					tt.text, tt.width, len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("wordWrap(%q, %d)[%d] = %q, want %q", tt.text, tt.width, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFormatBorderedText(t *testing.T) {
	t.Run("text with title shows title in top border", func(t *testing.T) {
		result := FormatBorderedText("Some text here", "My Title")

		if !strings.Contains(result, "My Title") {
			t.Error("should contain title")
		}
		// Top border should have the title embedded
		lines := strings.Split(result, "\n")
		if len(lines) < 3 {
			t.Fatal("output should have at least 3 lines (top border, content, bottom border)")
		}
		if !strings.Contains(lines[0], "My Title") {
			t.Error("title should appear in top border line")
		}
		if !strings.Contains(lines[0], "\u250c") {
			t.Error("top border should start with top-left corner")
		}
	})

	t.Run("text without title shows plain border", func(t *testing.T) {
		result := FormatBorderedText("Some text here", "")

		lines := strings.Split(result, "\n")
		if len(lines) < 3 {
			t.Fatal("output should have at least 3 lines")
		}
		// Top border should be plain (no title text, just box-drawing chars)
		topBorder := lines[0]
		if !strings.HasPrefix(topBorder, "\u250c") {
			t.Error("top border should start with top-left corner")
		}
		if !strings.HasSuffix(topBorder, "\u2510") {
			t.Error("top border should end with top-right corner")
		}
		// The border should be just corner + horizontal lines + corner
		inner := topBorder[len("\u250c") : len(topBorder)-len("\u2510")]
		cleaned := strings.ReplaceAll(inner, "\u2500", "")
		if cleaned != "" {
			t.Errorf("plain top border should only contain horizontal lines, but found extra: %q", cleaned)
		}
	})

	t.Run("multi-paragraph text", func(t *testing.T) {
		result := FormatBorderedText("First paragraph.\n\nSecond paragraph.", "")

		// Should contain both paragraphs
		if !strings.Contains(result, "First paragraph.") {
			t.Error("should contain first paragraph")
		}
		if !strings.Contains(result, "Second paragraph.") {
			t.Error("should contain second paragraph")
		}
		// Should have a blank line between paragraphs (empty content row)
		lines := strings.Split(result, "\n")
		foundEmpty := false
		for _, line := range lines[1 : len(lines)-1] { // skip borders
			// An empty content row would be: "| <spaces> |"
			trimmed := strings.TrimPrefix(line, "\u2502 ")
			trimmed = strings.TrimSuffix(trimmed, " \u2502")
			if strings.TrimSpace(trimmed) == "" && strings.Contains(line, "\u2502") {
				foundEmpty = true
				break
			}
		}
		if !foundEmpty {
			t.Error("should have an empty row between paragraphs")
		}
	})

	t.Run("short text fits in box", func(t *testing.T) {
		result := FormatBorderedText("Hi", "")

		if !strings.Contains(result, "Hi") {
			t.Error("should contain text")
		}
		// Should have proper box structure
		lines := strings.Split(result, "\n")
		if len(lines) != 3 {
			t.Errorf("short text should produce 3 lines (top, content, bottom), got %d", len(lines))
		}
		// Bottom border
		lastLine := lines[len(lines)-1]
		if !strings.HasPrefix(lastLine, "\u2514") {
			t.Error("bottom border should start with bottom-left corner")
		}
		if !strings.HasSuffix(lastLine, "\u2518") {
			t.Error("bottom border should end with bottom-right corner")
		}
	})
}
