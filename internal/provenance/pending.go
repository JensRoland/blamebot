package provenance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PendingDir returns the path to .git/blamebot/pending/.
func PendingDir(gitDir string) string {
	return filepath.Join(gitDir, "blamebot", "pending")
}

// WritePending writes a PendingEdit to .git/blamebot/pending/<id>.json.
func WritePending(gitDir string, edit PendingEdit) error {
	dir := PendingDir(gitDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(edit, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, edit.ID+".json"), append(data, '\n'), 0o644)
}

// ReadAllPending reads all pending edit files and returns them sorted by timestamp.
func ReadAllPending(gitDir string) ([]PendingEdit, error) {
	dir := PendingDir(gitDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var edits []PendingEdit
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var pe PendingEdit
		if err := json.Unmarshal(data, &pe); err != nil {
			continue
		}
		edits = append(edits, pe)
	}

	sort.Slice(edits, func(i, j int) bool {
		return edits[i].Ts < edits[j].Ts
	})
	return edits, nil
}

// ClearPending removes all files from .git/blamebot/pending/.
func ClearPending(gitDir string) error {
	dir := PendingDir(gitDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	return nil
}

// HasPending returns true if there are any pending edit files.
func HasPending(gitDir string) bool {
	dir := PendingDir(gitDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			return true
		}
	}
	return false
}
