package hook

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
)

// HandlePostCommit reads the commit SHA and Blamebot-Ref trailer from HEAD
// and backfills the commit_sha field in the manifest on the provenance branch.
func HandlePostCommit() error {
	root, err := project.FindRoot()
	if err != nil {
		return err
	}
	if !project.IsInitialized(root) {
		return nil
	}
	paths := project.NewPaths(root)

	commitSHA := git.HeadSHA(root)
	if commitSHA == "" {
		return nil
	}

	manifestID := extractTrailerFromHEAD(root)
	if manifestID == "" {
		return nil
	}

	if !provenance.BranchExists(root) {
		return nil
	}

	if err := provenance.UpdateManifestCommitSHA(root, paths.GitDir, manifestID, commitSHA); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("post-commit: failed to update manifest %s: %v", manifestID, err), nil)
	}
	return nil
}

// extractTrailerFromHEAD reads the Blamebot-Ref trailer from the HEAD commit message.
func extractTrailerFromHEAD(root string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Blamebot-Ref: ") {
			return strings.TrimPrefix(line, "Blamebot-Ref: ")
		}
	}
	return ""
}
