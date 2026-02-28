package provenance

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BranchExists returns true if the provenance branch exists locally.
func BranchExists(root string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", BranchName)
	cmd.Dir = root
	return cmd.Run() == nil
}

// InitBranch creates the orphan provenance branch with an initial empty commit.
// Idempotent: returns nil if branch already exists.
func InitBranch(root string) error {
	if BranchExists(root) {
		return nil
	}

	// Create an empty tree
	cmd := exec.Command("git", "mktree")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader("")
	treeOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("mktree: %w", err)
	}
	treeSHA := strings.TrimSpace(string(treeOut))

	// Create root commit (no parent)
	cmd = exec.Command("git", "commit-tree", treeSHA, "-m", "blamebot: initialize provenance branch")
	cmd.Dir = root
	commitOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("commit-tree: %w", err)
	}
	commitSHA := strings.TrimSpace(string(commitOut))

	// Create the branch ref
	cmd = exec.Command("git", "update-ref", RefPath, commitSHA)
	cmd.Dir = root
	return cmd.Run()
}

// WriteBlob writes arbitrary data to a path on the provenance branch using git plumbing.
// This does NOT check out the branch or affect the working tree.
func WriteBlob(root, gitDir, branchPath string, data []byte) error {
	indexFile := filepath.Join(gitDir, "blamebot-provenance-index")
	defer os.Remove(indexFile)

	env := append(os.Environ(), "GIT_INDEX_FILE="+indexFile)

	// 1. Read existing tree into temporary index
	cmd := exec.Command("git", "read-tree", BranchName)
	cmd.Dir = root
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("read-tree: %w", err)
	}

	// 2. Hash the data as a blob
	cmd = exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(string(data))
	blobOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("hash-object: %w", err)
	}
	blobSHA := strings.TrimSpace(string(blobOut))

	// 3. Add blob to index at the specified path
	cmd = exec.Command("git", "update-index", "--add", "--cacheinfo",
		fmt.Sprintf("100644,%s,%s", blobSHA, branchPath))
	cmd.Dir = root
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update-index: %w", err)
	}

	// 4. Write tree
	cmd = exec.Command("git", "write-tree")
	cmd.Dir = root
	cmd.Env = env
	treeOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("write-tree: %w", err)
	}
	treeSHA := strings.TrimSpace(string(treeOut))

	// 5. Commit tree with parent
	parentSHA := BranchTipSHA(root)
	cmd = exec.Command("git", "commit-tree", treeSHA, "-p", parentSHA, "-m",
		fmt.Sprintf("blamebot: write %s", branchPath))
	cmd.Dir = root
	commitOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("commit-tree: %w", err)
	}
	commitSHA := strings.TrimSpace(string(commitOut))

	// 6. Update ref
	cmd = exec.Command("git", "update-ref", RefPath, commitSHA)
	cmd.Dir = root
	return cmd.Run()
}

// WriteManifest writes a manifest JSON to manifests/<id>.json on the provenance branch.
func WriteManifest(root, gitDir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := ManifestDir + "/" + m.ID + ".json"
	return WriteBlob(root, gitDir, path, append(data, '\n'))
}

// WriteTrace writes trace contexts to traces/<sessionID>.json on the provenance branch.
// Merges with existing trace content if the file already exists.
func WriteTrace(root, gitDir string, sessionID string, contexts map[string]string) error {
	path := TracesDir + "/" + sessionID + ".json"

	// Try to read existing trace and merge
	existing := make(map[string]string)
	if data, err := ReadBlob(root, path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	for k, v := range contexts {
		existing[k] = v
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return WriteBlob(root, gitDir, path, append(data, '\n'))
}

// UpdateManifestCommitSHA reads a manifest, sets its commit_sha, and rewrites it.
func UpdateManifestCommitSHA(root, gitDir string, manifestID, commitSHA string) error {
	m, err := ReadManifest(root, manifestID)
	if err != nil {
		return err
	}
	m.CommitSHA = commitSHA
	return WriteManifest(root, gitDir, *m)
}

// ReadBlob reads a file from the provenance branch without checkout.
func ReadBlob(root, branchPath string) ([]byte, error) {
	cmd := exec.Command("git", "show", BranchName+":"+branchPath)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", branchPath, err)
	}
	return out, nil
}

// ReadManifest reads a single manifest by UUID from the provenance branch.
func ReadManifest(root string, manifestID string) (*Manifest, error) {
	path := ManifestDir + "/" + manifestID + ".json"
	data, err := ReadBlob(root, path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", manifestID, err)
	}
	return &m, nil
}

// ListManifests returns all manifest UUIDs on the provenance branch.
func ListManifests(root string) ([]string, error) {
	if !BranchExists(root) {
		return nil, nil
	}
	cmd := exec.Command("git", "ls-tree", "--name-only", BranchName, ManifestDir+"/")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // directory may not exist yet
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		// line is "manifests/<uuid>.json"
		name := filepath.Base(line)
		id := strings.TrimSuffix(name, ".json")
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ReadAllManifests reads all manifests from the provenance branch.
func ReadAllManifests(root string) ([]Manifest, error) {
	ids, err := ListManifests(root)
	if err != nil {
		return nil, err
	}
	var manifests []Manifest
	for _, id := range ids {
		m, err := ReadManifest(root, id)
		if err != nil {
			continue // skip unreadable manifests
		}
		manifests = append(manifests, *m)
	}
	return manifests, nil
}

// ReadTrace reads a trace file from the provenance branch.
func ReadTrace(root string, sessionID string) (map[string]string, error) {
	path := TracesDir + "/" + sessionID + ".json"
	data, err := ReadBlob(root, path)
	if err != nil {
		return nil, err
	}
	var traces map[string]string
	if err := json.Unmarshal(data, &traces); err != nil {
		return nil, err
	}
	return traces, nil
}

// BranchTipSHA returns the current tip commit SHA of the provenance branch.
func BranchTipSHA(root string) string {
	cmd := exec.Command("git", "rev-parse", BranchName)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// PushBranch pushes the provenance branch to the remote.
// Handles concurrent push with fetch-rebuild-retry logic.
func PushBranch(root string, remote string, maxRetries int) error {
	if remote == "" {
		remote = "origin"
	}

	// Check if remote exists
	cmd := exec.Command("git", "remote", "get-url", remote)
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		return nil // no remote configured, silently skip
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		cmd = exec.Command("git", "push", remote, BranchName)
		cmd.Dir = root
		if err := cmd.Run(); err == nil {
			return nil
		}

		// Push failed — fetch and rebuild our local branch on top of remote
		cmd = exec.Command("git", "fetch", remote, BranchName)
		cmd.Dir = root
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("fetch %s: %w", remote, err)
		}

		// Merge remote into local by rebasing local manifests on top
		if err := mergeRemoteBranch(root); err != nil {
			return fmt.Errorf("merge remote: %w", err)
		}
	}
	return fmt.Errorf("push failed after %d retries", maxRetries)
}

// mergeRemoteBranch merges the remote provenance branch into the local one.
// Since we only ever add files with UUID names, there are no content conflicts.
// We create a merge commit that combines both trees.
func mergeRemoteBranch(root string) error {
	localSHA := BranchTipSHA(root)

	cmd := exec.Command("git", "rev-parse", "FETCH_HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("FETCH_HEAD: %w", err)
	}
	remoteSHA := strings.TrimSpace(string(out))

	if localSHA == remoteSHA {
		return nil
	}

	// Use read-tree -m to merge the two trees
	// Since files are UUID-named and never conflict, this is always clean
	indexFile := filepath.Join(root, ".git", "blamebot-merge-index")
	defer os.Remove(indexFile)

	env := append(os.Environ(), "GIT_INDEX_FILE="+indexFile)

	// Three-way merge: find merge base first
	cmd = exec.Command("git", "merge-base", localSHA, remoteSHA)
	cmd.Dir = root
	baseOut, err := cmd.Output()
	if err != nil {
		// No common ancestor — use remote as base
		cmd = exec.Command("git", "read-tree", remoteSHA)
		cmd.Dir = root
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			return err
		}
	} else {
		baseSHA := strings.TrimSpace(string(baseOut))
		cmd = exec.Command("git", "read-tree", "-m", baseSHA, localSHA, remoteSHA)
		cmd.Dir = root
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	cmd = exec.Command("git", "write-tree")
	cmd.Dir = root
	cmd.Env = env
	treeOut, err := cmd.Output()
	if err != nil {
		return err
	}
	treeSHA := strings.TrimSpace(string(treeOut))

	cmd = exec.Command("git", "commit-tree", treeSHA,
		"-p", localSHA, "-p", remoteSHA,
		"-m", "blamebot: merge provenance")
	cmd.Dir = root
	commitOut, err := cmd.Output()
	if err != nil {
		return err
	}
	commitSHA := strings.TrimSpace(string(commitOut))

	cmd = exec.Command("git", "update-ref", RefPath, commitSHA)
	cmd.Dir = root
	return cmd.Run()
}
