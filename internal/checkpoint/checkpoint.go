package checkpoint

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Checkpoint represents a file snapshot at a known point in time.
type Checkpoint struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`        // "pre-edit" or "post-edit"
	File       string `json:"file"`        // project-relative path
	ContentSHA string `json:"content_sha"` // SHA256 of file content blob
	EditID     string `json:"edit_id"`     // links to PendingEdit (post-edit only)
	ToolUseID  string `json:"tool_use_id"` // links pre/post pairs
	Ts         string `json:"ts"`
}

// WriteCheckpoint writes a checkpoint metadata file to the checkpoint directory.
// Returns the generated checkpoint ID.
func WriteCheckpoint(dir string, cp Checkpoint) (string, error) {
	if cp.ID == "" {
		cp.ID = uuid.New().String()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, cp.ID+".json")
	return cp.ID, os.WriteFile(path, data, 0o644)
}

// WriteBlob writes file content to the blob directory, deduplicated by SHA256.
// Returns the content SHA256 hash.
func WriteBlob(dir string, content string) (string, error) {
	h := sha256.Sum256([]byte(content))
	sha := fmt.Sprintf("%x", h)

	blobDir := filepath.Join(dir, "blobs")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(blobDir, sha)
	if _, err := os.Stat(path); err == nil {
		return sha, nil // already exists, dedup
	}
	return sha, os.WriteFile(path, []byte(content), 0o644)
}

// ReadBlob reads file content from the blob directory by SHA256.
func ReadBlob(dir, sha string) (string, error) {
	path := filepath.Join(dir, "blobs", sha)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadAllCheckpoints reads all checkpoint metadata files and returns them
// sorted by timestamp.
func ReadAllCheckpoints(dir string) ([]Checkpoint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var checkpoints []Checkpoint
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			continue
		}
		checkpoints = append(checkpoints, cp)
	}

	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].Ts < checkpoints[j].Ts
	})
	return checkpoints, nil
}

// CheckpointsForFile returns checkpoints filtered to a specific file.
func CheckpointsForFile(checkpoints []Checkpoint, file string) []Checkpoint {
	var result []Checkpoint
	for _, cp := range checkpoints {
		if cp.File == file {
			result = append(result, cp)
		}
	}
	return result
}

// ClearAll removes all checkpoints and blobs from the checkpoint directory.
func ClearAll(dir string) error {
	return os.RemoveAll(dir)
}
