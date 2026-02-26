package format

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// FormatSideBySideDiff renders a side-by-side diff with box-drawing borders.
func FormatSideBySideDiff(oldText, newText string) string {
	termWidth := TermWidth()
	colW := (termWidth - 7) / 2
	if colW < 20 {
		colW = 20
	}

	oldLines := expandTabs(oldText)
	newLines := expandTabs(newText)

	if len(oldLines) == 0 {
		oldLines = []string{""}
	}
	if len(newLines) == 0 {
		newLines = []string{""}
	}

	// Compute line-level diff using diffmatchpatch on joined text
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"), true)
	diffs = dmp.DiffCleanupSemantic(diffs)

	// Convert character diffs to line-level rows
	type diffRow struct {
		tag   string // "equal", "delete", "insert", "replace"
		left  *string
		right *string
	}

	// Reconstruct line-level operations
	var rows []diffRow
	var oldBuf, newBuf []string

	flushBuf := func() {
		maxLen := len(oldBuf)
		if len(newBuf) > maxLen {
			maxLen = len(newBuf)
		}
		for i := 0; i < maxLen; i++ {
			var o, n *string
			tag := "replace"
			if i < len(oldBuf) {
				o = &oldBuf[i]
			}
			if i < len(newBuf) {
				n = &newBuf[i]
			}
			if o == nil {
				tag = "insert"
			} else if n == nil {
				tag = "delete"
			}
			rows = append(rows, diffRow{tag: tag, left: o, right: n})
		}
		oldBuf = nil
		newBuf = nil
	}

	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flushBuf()
			for _, l := range lines {
				lCopy := l
				rows = append(rows, diffRow{tag: "equal", left: &lCopy, right: &lCopy})
			}
		case diffmatchpatch.DiffDelete:
			for _, l := range lines {
				lCopy := l
				oldBuf = append(oldBuf, lCopy)
			}
		case diffmatchpatch.DiffInsert:
			for _, l := range lines {
				lCopy := l
				newBuf = append(newBuf, lCopy)
			}
		}
	}
	flushBuf()

	totalRows := len(rows)
	maxDisplay := 40
	truncated := totalRows > maxDisplay
	if truncated {
		rows = rows[:maxDisplay]
	}

	var output []string

	// Top border with labels
	lblL := "\u2500 Before "
	lblR := "\u2500 After "
	output = append(output, fmt.Sprintf("\u250c%s%s\u252c%s%s\u2510",
		lblL, strings.Repeat("\u2500", colW+2-runeLen(lblL)),
		lblR, strings.Repeat("\u2500", colW+2-runeLen(lblR))))

	for _, r := range rows {
		left := padOrTrunc("", colW)
		right := padOrTrunc("", colW)
		if r.left != nil {
			left = padOrTrunc(*r.left, colW)
		}
		if r.right != nil {
			right = padOrTrunc(*r.right, colW)
		}

		switch r.tag {
		case "equal":
			output = append(output, fmt.Sprintf("\u2502 %s%s%s \u2502 %s%s%s \u2502",
				Dim, left, Reset, Dim, right, Reset))
		case "delete":
			output = append(output, fmt.Sprintf("\u2502 %s%s%s \u2502 %s \u2502",
				Red, left, Reset, strings.Repeat(" ", colW)))
		case "insert":
			output = append(output, fmt.Sprintf("\u2502 %s \u2502 %s%s%s \u2502",
				strings.Repeat(" ", colW), Green, right, Reset))
		case "replace":
			l := strings.Repeat(" ", colW)
			r2 := strings.Repeat(" ", colW)
			if r.left != nil {
				l = Red + left + Reset
			}
			if r.right != nil {
				r2 = Green + right + Reset
			}
			output = append(output, fmt.Sprintf("\u2502 %s \u2502 %s \u2502", l, r2))
		}
	}

	// Bottom border
	output = append(output, fmt.Sprintf("\u2514%s\u2534%s\u2518",
		strings.Repeat("\u2500", colW+2), strings.Repeat("\u2500", colW+2)))

	if truncated {
		output = append(output, fmt.Sprintf("  %s\u2026 %d more lines not shown%s",
			Dim, totalRows-maxDisplay, Reset))
	}

	return strings.Join(output, "\n")
}

func expandTabs(text string) []string {
	if text == "" {
		return nil
	}
	expanded := strings.ReplaceAll(text, "\t", "    ")
	return strings.Split(expanded, "\n")
}

func padOrTrunc(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w])
	}
	return s + strings.Repeat(" ", w-len(r))
}

func runeLen(s string) int {
	return len([]rune(s))
}
