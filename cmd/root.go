package cmd

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/project"
)

// RunQuery handles the default query mode (no subcommand).
func RunQuery(args []string) {
	fs := flag.NewFlagSet("git-blamebot", flag.ExitOnError)

	line := fs.String("L", "", "Line number or range (42 or 10:20 or 10,20)")
	grepPattern := fs.String("grep", "", "Search prompts and changes")
	since := fs.String("since", "", "Show reasons since date (YYYY-MM-DD)")
	author := fs.String("author", "", "Filter by author name")
	traceID := fs.String("trace", "", "Show reasoning trace for a record ID or tool_use_id")
	explain := fs.Bool("explain", false, "Deep explanation using Sonnet")
	stats := fs.Bool("stats", false, "Summary statistics")
	rebuild := fs.Bool("rebuild", false, "Force index rebuild")
	showLog := fs.Bool("log", false, "Show debug logs")
	hookLog := fs.Bool("hook", false, "With --log: show hook log")
	dumpPayload := fs.Bool("dump-payload", false, "Show last raw payload")
	fillReasons := fs.Bool("fill-reasons", false, "Fill empty reasons using Claude Haiku")
	dryRun := fs.Bool("dry-run", false, "With --fill-reasons: show what would be generated")
	uninit := fs.Bool("uninit", false, "Remove blamebot tracking from this repo")
	verbose := fs.Bool("v", false, "Show hashes, sessions, traces, git blame")
	jsonOutput := fs.Bool("json", false, "Output results as JSON")
	includeHistory := fs.Bool("include-history", false, "Show superseded/overwritten edits too")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `blamebot: understand why AI-authored code exists.

Usage:
    git-blamebot <file>                    # all reasons for a file
    git-blamebot -L <line> <file>          # reasons for a specific line
    git-blamebot -L <start>,<end> <file>   # reasons for a line range
    git-blamebot --since <date> [<file>]   # reasons since a date
    git-blamebot --grep <text>             # search prompts & changes
    git-blamebot --author <name>           # filter by author
    git-blamebot --trace <id>              # show reasoning from transcript
    git-blamebot --explain <file|id> [-L N] # deep explanation using Sonnet
    git-blamebot --stats                   # summary statistics
    git-blamebot --include-history          # include superseded edits
    git-blamebot --json                    # machine-readable JSON output
    git-blamebot --rebuild                 # force index rebuild
    git-blamebot --log [--hook]            # show debug logs
    git-blamebot --dump-payload            # show last raw hook payload
    git-blamebot --fill-reasons            # fill empty reasons using Haiku
    git-blamebot --uninit                  # remove tracking from this repo

Subcommands:
    git-blamebot hook <prompt-submit|post-tool-use>
    git-blamebot enable [--global]
    git-blamebot disable
`)
	}

	// Go's flag package stops at the first non-flag arg.
	// Reorder so flags come before positional args, allowing
	// both "git blamebot -L 42 file" and "git blamebot file -L 42".
	fs.Parse(reorderArgs(args))

	root, err := project.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	paths := project.NewPaths(root)

	// Commands that don't need the index
	if *showLog {
		cmdLog(paths, *hookLog)
		return
	}
	if *dumpPayload {
		cmdDumpPayload(paths)
		return
	}
	if *fillReasons {
		cmdFillReasons(paths, root, *dryRun)
		return
	}
	if *uninit {
		cmdDisable(paths, root)
		return
	}

	if !project.IsInitialized(root) {
		fmt.Fprintln(os.Stderr, "No provenance branch found.")
		fmt.Fprintln(os.Stderr, "Run 'git-blamebot enable' in this repo first.")
		os.Exit(1)
	}

	db, err := index.Open(paths, *rebuild)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening index at %s: %v\n", paths.IndexDB, err)
		os.Exit(1)
	}
	defer db.Close()

	file := fs.Arg(0)

	switch {
	case *stats:
		cmdStats(db, *jsonOutput)
	case *grepPattern != "":
		cmdGrep(db, *grepPattern, root, *verbose, *jsonOutput)
	case *since != "":
		cmdSince(db, *since, file, root, *verbose, *jsonOutput)
	case *author != "":
		cmdAuthor(db, *author, root, *verbose, *jsonOutput)
	case *traceID != "":
		cmdTrace(db, *traceID, root, *jsonOutput)
	case *explain:
		if file == "" {
			fmt.Fprintln(os.Stderr, "Usage: git blamebot --explain <file|record-id> [-L <line>]")
			os.Exit(1)
		}
		cmdExplain(db, file, root, *line)
	case file != "":
		cmdFile(db, file, root, *line, *verbose, *jsonOutput, *includeHistory)
	default:
		fs.Usage()
	}
}

// queryRows is a helper that executes a query and collects ReasonRows.
func queryRows(db *sql.DB, query string, args ...interface{}) ([]*index.ReasonRow, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*index.ReasonRow
	for rows.Next() {
		row, err := index.ScanRow(rows)
		if err != nil {
			continue
		}
		results = append(results, row)
	}
	return results, nil
}

// reorderArgs moves flags before positional args so flag.Parse works
// regardless of argument order (e.g. "file -L 42" → "-L 42 file").
func reorderArgs(args []string) []string {
	var flags, positional []string
	i := 0
	for i < len(args) {
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			// Check if this flag takes a value (next arg is not a flag)
			if i+1 < len(args) && (len(args[i+1]) == 0 || args[i+1][0] != '-') {
				// Known boolean flags that don't take a value
				switch a {
				case "--stats", "--rebuild", "--log", "--hook", "--dump-payload",
					"--fill-reasons", "--dry-run", "--uninit", "-v", "--json",
					"--explain", "--include-history":
					// no value
				default:
					i++
					flags = append(flags, args[i])
				}
			}
		} else {
			positional = append(positional, a)
		}
		i++
	}
	return append(flags, positional...)
}
