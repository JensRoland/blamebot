package provenance

import (
	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/record"
)

const (
	BranchName  = "blamebot-provenance"
	RefPath     = "refs/heads/" + BranchName
	ManifestDir = "manifests"
	TracesDir   = "traces"
)

// Manifest represents a bundle of edits associated with a single code commit.
type Manifest struct {
	ID           string                      `json:"id"`
	CommitSHA    string                      `json:"commit_sha,omitempty"`
	Author       string                      `json:"author"`
	Timestamp    string                      `json:"timestamp"`
	Edits        []ManifestEdit              `json:"edits"`
	Attributions map[string]FileAttribution  `json:"attributions,omitempty"`
}

// FileAttribution maps manifest edit indices to their line ranges at commit time.
type FileAttribution struct {
	EditLines map[int]lineset.LineSet `json:"edit_lines"`
}

// ManifestEdit is a single edit record within a manifest.
type ManifestEdit struct {
	File        string          `json:"file"`
	Lines       lineset.LineSet `json:"lines"`
	Hunk        *record.HunkInfo `json:"hunk,omitempty"`
	ContentHash string          `json:"content_hash"`
	Prompt      string          `json:"prompt"`
	Reason      string          `json:"reason"`
	Change      string          `json:"change"`
	Tool        string          `json:"tool"`
	Session     string          `json:"session"`
	Trace       string          `json:"trace"`
}

// PendingEdit is an individual edit stored in .git/blamebot/pending/ before commit.
type PendingEdit struct {
	ID          string          `json:"id"`
	Ts          string          `json:"ts"`
	File        string          `json:"file"`
	Lines       lineset.LineSet `json:"lines"`
	Hunk        *record.HunkInfo `json:"hunk,omitempty"`
	ContentHash string          `json:"content_hash"`
	Prompt      string          `json:"prompt"`
	Change      string          `json:"change"`
	Tool        string          `json:"tool"`
	Author      string          `json:"author"`
	Session     string          `json:"session"`
	Trace       string          `json:"trace"`
}
