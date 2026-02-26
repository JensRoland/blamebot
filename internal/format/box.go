package format

import (
	"fmt"
	"strings"
)

// FormatBorderedText renders text inside a bordered box with word wrapping.
func FormatBorderedText(text, title string) string {
	termWidth := TermWidth()
	innerW := termWidth - 4
	if innerW < 30 {
		innerW = 30
	}

	var wrapped []string
	for _, paragraph := range strings.Split(text, "\n") {
		if strings.TrimSpace(paragraph) == "" {
			wrapped = append(wrapped, "")
			continue
		}
		wrapped = append(wrapped, wordWrap(paragraph, innerW)...)
	}

	var output []string

	// Top border with optional title
	if title != "" {
		lbl := fmt.Sprintf("\u2500 %s ", title)
		output = append(output, fmt.Sprintf("\u250c%s%s\u2510",
			lbl, strings.Repeat("\u2500", innerW+2-runeLen(lbl))))
	} else {
		output = append(output, fmt.Sprintf("\u250c%s\u2510",
			strings.Repeat("\u2500", innerW+2)))
	}

	for _, line := range wrapped {
		padded := padOrTrunc(line, innerW)
		output = append(output, fmt.Sprintf("\u2502 %s \u2502", padded))
	}

	output = append(output, fmt.Sprintf("\u2514%s\u2518",
		strings.Repeat("\u2500", innerW+2)))

	return strings.Join(output, "\n")
}

// wordWrap wraps text to the given width, breaking at word boundaries.
func wordWrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	current := words[0]

	for _, word := range words[1:] {
		if len(current)+1+len(word) <= width {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	lines = append(lines, current)
	return lines
}
