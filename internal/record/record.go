package record

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jensroland/git-blamebot/internal/lineset"
)

// HunkInfo stores the raw unified-diff hunk metadata for an edit.
// This enables line-number adjustment across subsequent edits.
type HunkInfo struct {
	OldStart int `json:"old_start"`
	OldLines int `json:"old_lines"`
	NewStart int `json:"new_start"`
	NewLines int `json:"new_lines"`
}

// Record is a single blamebot JSONL entry.
type Record struct {
	Ts          string         `json:"ts"`
	File        string         `json:"file"`
	Lines       lineset.LineSet `json:"lines"`
	Hunk        *HunkInfo      `json:"hunk,omitempty"`
	ContentHash string         `json:"content_hash"`
	Prompt      string         `json:"prompt"`
	Reason      string         `json:"reason"`
	Change      string         `json:"change"`
	Tool        string         `json:"tool"`
	Author      string         `json:"author"`
	Session     string         `json:"session"`
	Trace       string         `json:"trace"`
}

// ContentHash produces a 16-char hex hash of whitespace-normalized text.
// Matches Python: hashlib.sha256(" ".join(text.split()).encode()).hexdigest()[:16]
func ContentHash(text string) string {
	if text == "" {
		return ""
	}
	normalized := strings.Join(strings.Fields(text), " ")
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h)[:16]
}

// RelativizePath converts an absolute path to a project-relative path.
// Always uses forward slashes for portability.
func RelativizePath(absPath, projectDir string) string {
	if absPath == "" {
		return ""
	}
	rel, err := filepath.Rel(projectDir, absPath)
	if err != nil {
		return absPath
	}
	return filepath.ToSlash(rel)
}

// CompactChangeSummary generates a human-readable summary of what changed.
func CompactChangeSummary(oldStr, newStr string) string {
	const maxLen = 200

	if oldStr == "" && newStr != "" {
		preview := strings.ReplaceAll(newStr, "\n", " ")
		if len(preview) > maxLen {
			preview = preview[:maxLen]
		}
		return "added: " + preview
	}

	if oldStr != "" && newStr == "" {
		preview := strings.ReplaceAll(oldStr, "\n", " ")
		if len(preview) > maxLen {
			preview = preview[:maxLen]
		}
		return "removed: " + preview
	}

	// Normalize to single-line for display
	oldFlat := strings.TrimSpace(strings.ReplaceAll(oldStr, "\n", " "))
	newFlat := strings.TrimSpace(strings.ReplaceAll(newStr, "\n", " "))

	// Find common prefix length
	common := 0
	minLen := len(oldFlat)
	if len(newFlat) < minLen {
		minLen = len(newFlat)
	}
	for i := 0; i < minLen; i++ {
		if oldFlat[i] == newFlat[i] {
			common++
		} else {
			break
		}
	}

	var oldDisplay, newDisplay string
	if common > 20 {
		offset := common - 10
		if offset < 0 {
			offset = 0
		}
		oldDisplay = "\u2026" + oldFlat[offset:]
		newDisplay = "\u2026" + newFlat[offset:]
	} else {
		oldDisplay = oldFlat
		newDisplay = newFlat
	}

	if len(oldDisplay) > maxLen {
		oldDisplay = oldDisplay[:maxLen] + "\u2026"
	}
	if len(newDisplay) > maxLen {
		newDisplay = newDisplay[:maxLen] + "\u2026"
	}

	return oldDisplay + " \u2192 " + newDisplay
}
