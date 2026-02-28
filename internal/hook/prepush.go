package hook

import (
	"fmt"

	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
)

// HandlePrePush pushes the provenance branch alongside the code branch.
// Errors are logged but never returned — must not block the user's code push.
func HandlePrePush() error {
	root, err := project.FindRoot()
	if err != nil {
		return nil
	}
	if !project.IsInitialized(root) {
		return nil
	}
	if !provenance.BranchExists(root) {
		return nil
	}
	paths := project.NewPaths(root)

	if err := provenance.PushBranch(root, "origin", 3); err != nil {
		debug.Log(paths.CacheDir, "hook.log",
			fmt.Sprintf("pre-push: failed to push provenance branch: %v", err), nil)
	}
	return nil
}
