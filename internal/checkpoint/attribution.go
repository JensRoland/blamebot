package checkpoint

import (
	"strings"

	"github.com/jensroland/git-blamebot/internal/lineset"
)

// Attribution maps edit IDs to their current line ranges.
type Attribution map[string]lineset.LineSet

// ComputeFileAttribution builds a per-line attribution map from a checkpoint chain.
//
// Parameters:
//   - baseContent: file content before any edits (e.g., HEAD version). Empty for new files.
//   - currentContent: file content now (working tree or committed version).
//   - checkpoints: sorted by timestamp, for a single file.
//   - blobReader: reads blob content by SHA.
//
// Returns a map of editID → LineSet (lines in currentContent attributed to that edit).
// Lines not in any edit's set are human-authored.
func ComputeFileAttribution(baseContent, currentContent string, checkpoints []Checkpoint, blobReader func(string) string) Attribution {
	if len(checkpoints) == 0 {
		return Attribution{}
	}

	// Build the chain of (pre-edit, post-edit) pairs grouped by ToolUseID
	type editPair struct {
		preContent  string
		postContent string
		editID      string
		ts          string
	}

	// Group checkpoints by ToolUseID
	preByToolUse := make(map[string]Checkpoint)
	postByToolUse := make(map[string]Checkpoint)
	var toolUseOrder []string
	seen := make(map[string]bool)

	for _, cp := range checkpoints {
		if cp.ToolUseID == "" {
			continue
		}
		if !seen[cp.ToolUseID] {
			seen[cp.ToolUseID] = true
			toolUseOrder = append(toolUseOrder, cp.ToolUseID)
		}
		switch cp.Kind {
		case "pre-edit":
			preByToolUse[cp.ToolUseID] = cp
		case "post-edit":
			postByToolUse[cp.ToolUseID] = cp
		}
	}

	// Build ordered pairs
	var pairs []editPair
	for _, tuid := range toolUseOrder {
		pre, hasPre := preByToolUse[tuid]
		post, hasPost := postByToolUse[tuid]
		if !hasPre || !hasPost {
			continue // incomplete pair, skip
		}
		pairs = append(pairs, editPair{
			preContent:  blobReader(pre.ContentSHA),
			postContent: blobReader(post.ContentSHA),
			editID:      post.EditID,
			ts:          post.Ts,
		})
	}

	if len(pairs) == 0 {
		return Attribution{}
	}

	// Walk the chain building attribution
	baseLines := splitLines(baseContent)
	attr := make([]string, len(baseLines)) // all "" = human/base

	prevContent := baseContent

	for _, pair := range pairs {
		// Gap: prevContent → pair.preContent (human edits)
		if prevContent != pair.preContent {
			prevLines := splitLines(prevContent)
			preLines := splitLines(pair.preContent)
			attr = TransformAttribution(prevLines, preLines, attr, "")
		}

		// AI edit: pair.preContent → pair.postContent
		preLines := splitLines(pair.preContent)
		postLines := splitLines(pair.postContent)
		attr = TransformAttribution(preLines, postLines, attr, pair.editID)

		prevContent = pair.postContent
	}

	// Final gap: last post-edit → currentContent (human edits)
	if prevContent != currentContent {
		prevLines := splitLines(prevContent)
		curLines := splitLines(currentContent)
		attr = TransformAttribution(prevLines, curLines, attr, "")
	}

	// Aggregate: collect line numbers for each edit ID
	result := Attribution{}
	for i, editID := range attr {
		if editID == "" {
			continue // human line
		}
		lineNum := i + 1 // 1-indexed
		existing := result[editID]
		result[editID] = existing.Add(lineNum)
	}

	return result
}

// TransformAttribution applies a diff between old and new content to a
// per-line attribution array. Lines matched by LCS carry their old
// attribution. New/changed lines get attributed to the given actor.
//
// Returns a new attribution array for the new content.
func TransformAttribution(oldLines, newLines []string, oldAttr []string, actor string) []string {
	if len(oldLines) == 0 {
		// All new content → all attributed to actor
		result := make([]string, len(newLines))
		for i := range result {
			result[i] = actor
		}
		return result
	}

	if len(newLines) == 0 {
		return nil // file deleted
	}

	// For very large files (>5000 lines each), skip LCS to avoid
	// excessive memory usage. The DP table uses O(m*n) memory.
	if len(oldLines)*len(newLines) > 25_000_000 {
		result := make([]string, len(newLines))
		for i := range result {
			result[i] = actor
		}
		return result
	}

	// Compute LCS to find matching lines
	matchedOld, matchedNew := lcsMatching(oldLines, newLines)

	// Build new attribution
	result := make([]string, len(newLines))
	for j := 0; j < len(newLines); j++ {
		if matchedNew[j] >= 0 {
			// This new line is matched to an old line — carry attribution
			oldIdx := matchedNew[j]
			if oldIdx < len(oldAttr) {
				result[j] = oldAttr[oldIdx]
			}
		} else {
			// New/changed line — attribute to actor
			result[j] = actor
		}
	}

	// Ensure oldAttr consistency
	_ = matchedOld

	return result
}

// lcsMatching computes the LCS of a and b and returns two mapping arrays:
//   - matchedOld[i] = index in b that old line i maps to (-1 if deleted)
//   - matchedNew[j] = index in a that new line j maps to (-1 if inserted/changed)
func lcsMatching(a, b []string) (matchedOld []int, matchedNew []int) {
	m, n := len(a), len(b)

	matchedOld = make([]int, m)
	matchedNew = make([]int, n)
	for i := range matchedOld {
		matchedOld[i] = -1
	}
	for j := range matchedNew {
		matchedNew[j] = -1
	}

	// Build LCS DP table
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

	// Backtrack to find the matching
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			matchedOld[i-1] = j - 1
			matchedNew[j-1] = i - 1
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	return matchedOld, matchedNew
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
