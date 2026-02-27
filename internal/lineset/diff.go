package lineset

import "strings"

// ChangedLines compares oldStr and newStr line by line and returns the
// 1-based absolute line numbers in newStr that are actually changed or added.
// newStartLine is the 1-based line number where newStr begins in the file.
//
// Uses LCS (Longest Common Subsequence) to identify unchanged lines.
// Falls back to the full bounding range if the edit region is very large.
func ChangedLines(oldStr, newStr string, newStartLine int) LineSet {
	if oldStr == newStr {
		return LineSet{}
	}

	oldLines := splitLines(oldStr)
	newLines := splitLines(newStr)

	// All new content
	if len(oldLines) == 0 {
		return FromRange(newStartLine, newStartLine+len(newLines)-1)
	}

	// Guard: for very large edits, skip LCS and return bounding range
	if len(oldLines)*len(newLines) > 10000 {
		return FromRange(newStartLine, newStartLine+len(newLines)-1)
	}

	// Find which lines in newLines are NOT matched by the LCS
	matched := lcsMatchedNew(oldLines, newLines)

	var changed []int
	for j := 0; j < len(newLines); j++ {
		if !matched[j] {
			changed = append(changed, newStartLine+j)
		}
	}

	// Edge case: pure deletion (all new lines are matched, but strings differ)
	// The edit still happened — report the bounding range so we don't lose the record.
	if len(changed) == 0 {
		return FromRange(newStartLine, newStartLine+len(newLines)-1)
	}

	return New(changed...)
}

// lcsMatchedNew computes the LCS of a and b and returns a boolean slice
// indicating which indices in b are matched (i.e., part of the LCS).
func lcsMatchedNew(a, b []string) []bool {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to identify which new lines are matched
	matched := make([]bool, n)
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			matched[j-1] = true
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	return matched
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
