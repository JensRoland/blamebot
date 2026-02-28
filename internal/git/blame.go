package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// BlameEntry holds parsed git blame data for a single line.
type BlameEntry struct {
	SHA      string // 40-char commit SHA (0000... for uncommitted)
	Line     int    // 1-based line number in current file
	OrigLine int    // 1-based line number in the original commit
}

// IsUncommitted returns true if the blame entry is for uncommitted content.
func (e BlameEntry) IsUncommitted() bool {
	return strings.TrimLeft(e.SHA, "0") == ""
}

// BlameFile runs git blame on an entire file and returns entries keyed by line number.
func BlameFile(root, file string) (map[int]BlameEntry, error) {
	cmd := exec.Command("git", "blame", "--porcelain", file)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git blame %s: %w", file, err)
	}
	return parsePorcelainBlame(out), nil
}

// BlameRange runs git blame -L start,end on a file.
func BlameRange(root, file string, start, end int) (map[int]BlameEntry, error) {
	cmd := exec.Command("git", "blame", "-L", fmt.Sprintf("%d,%d", start, end), "--porcelain", file)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git blame -L %d,%d %s: %w", start, end, file, err)
	}
	return parsePorcelainBlame(out), nil
}

// parsePorcelainBlame parses git blame --porcelain output.
//
// Porcelain format:
//
//	<40-byte SHA> <orig-line> <final-line> [<num-lines>]
//	header lines...
//	\t<actual line content>
//
// The first line for each blame group starts with the SHA.
// Subsequent lines in the same group reuse the SHA.
// Each group ends with a tab-prefixed content line.
func parsePorcelainBlame(out []byte) map[int]BlameEntry {
	entries := make(map[int]BlameEntry)
	lines := strings.Split(string(out), "\n")

	var currentSHA string
	for _, line := range lines {
		if line == "" {
			continue
		}

		// Tab-prefixed lines are content — skip
		if strings.HasPrefix(line, "\t") {
			continue
		}

		// Header lines (author, committer, summary, etc.)
		if strings.HasPrefix(line, "author") ||
			strings.HasPrefix(line, "committer") ||
			strings.HasPrefix(line, "summary") ||
			strings.HasPrefix(line, "previous") ||
			strings.HasPrefix(line, "filename") ||
			strings.HasPrefix(line, "boundary") {
			continue
		}

		// SHA line: <40-char sha> <orig-line> <final-line> [<num-lines>]
		fields := strings.Fields(line)
		if len(fields) >= 3 && len(fields[0]) == 40 {
			currentSHA = fields[0]
			var origLine, finalLine int
			_, _ = fmt.Sscanf(fields[1], "%d", &origLine)
			_, _ = fmt.Sscanf(fields[2], "%d", &finalLine)
			if finalLine > 0 {
				entries[finalLine] = BlameEntry{
					SHA:      currentSHA,
					Line:     finalLine,
					OrigLine: origLine,
				}
			}
		}
	}

	return entries
}

// HeadSHA returns the current HEAD commit SHA.
func HeadSHA(root string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
