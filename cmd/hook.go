package cmd

import (
	"fmt"
	"os"

	"github.com/jensroland/git-blamebot/internal/debug"
	"github.com/jensroland/git-blamebot/internal/hook"
	"github.com/jensroland/git-blamebot/internal/project"
)

// RunHook dispatches hook subcommands.
func RunHook(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: git-blamebot hook <prompt-submit|post-tool-use>")
		os.Exit(1)
	}

	var err error
	switch args[0] {
	case "prompt-submit":
		err = hook.HandlePromptSubmit(os.Stdin)
	case "post-tool-use":
		err = hook.HandlePostToolUse(os.Stdin)
	default:
		fmt.Fprintf(os.Stderr, "Unknown hook type: %s\n", args[0])
		os.Exit(1)
	}

	if err != nil {
		// Log error but never fail -- hooks must not block Claude Code
		if root, e := project.FindRoot(); e == nil {
			paths := project.NewPaths(root)
			debug.Log(paths.CacheDir, "hook.log", fmt.Sprintf("Fatal error: %v", err), nil)
		}
	}
	// Always exit 0
}
