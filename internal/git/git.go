package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Author returns the git user.name config value.
func Author() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return "unknown"
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "unknown"
	}
	return name
}

// RevParseTopLevel returns the git repo root.
func RevParseTopLevel() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// BlameInfo holds parsed git blame data for a single line.
type BlameInfo struct {
	SHA     string
	Author  string
	Summary string
}

// BlameForLine runs git blame --porcelain for a single line.
func BlameForLine(projectRoot, filePath string, line int) (*BlameInfo, error) {
	cmd := exec.Command("git", "blame", "-L", fmt.Sprintf("%d,%d", line, line), "--porcelain", filePath)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	info := &BlameInfo{}
	for _, bline := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(bline, "author ") {
			info.Author = bline[7:]
		} else if strings.HasPrefix(bline, "summary ") {
			info.Summary = bline[8:]
		} else if info.SHA == "" && strings.Contains(bline, " ") {
			parts := strings.Fields(bline)
			if len(parts) >= 1 && len(parts[0]) == 40 {
				info.SHA = parts[0]
			}
		}
	}

	if info.SHA == "" && info.Author == "" {
		return nil, nil
	}
	return info, nil
}

// StagedJSONLFiles returns staged .blamebot/log/*.jsonl file paths.
func StagedJSONLFiles(projectRoot string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--", ".blamebot/log/*.jsonl")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// StageFile runs git add for a file.
func StageFile(projectRoot, relPath string) error {
	cmd := exec.Command("git", "add", relPath)
	cmd.Dir = projectRoot
	return cmd.Run()
}
