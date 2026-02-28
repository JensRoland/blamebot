package cmd

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jensroland/git-blamebot/internal/checkpoint"
	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
	"github.com/jensroland/git-blamebot/internal/record"
)

// RunDebug dispatches debug subcommands.
func RunDebug(args []string) {
	if len(args) == 0 {
		debugUsage()
		os.Exit(1)
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "insert":
		debugEditCmd("insert", rest)
	case "replace":
		debugEditCmd("replace", rest)
	case "remove":
		debugEditCmd("remove", rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown debug command: %s\n\n", sub)
		debugUsage()
		os.Exit(1)
	}
}

func debugUsage() {
	fmt.Fprintf(os.Stderr, `Usage: git-blamebot debug <command> <file> [flags]

Commands:
    insert   Insert N new lines before line M
    replace  Replace N lines starting at line M
    remove   Remove N lines starting at line M

Flags:
    -m <line>     Line number (1-indexed)
    -n <count>    Number of lines (default: 1)
    --as-agent    Register the edit as an AI edit (writes pending provenance)

Examples:
    git-blamebot debug insert README.md -m 5 -n 3 --as-agent
    git-blamebot debug replace src/main.go -m 10 -n 2
    git-blamebot debug remove README.md -m 3 -n 1
`)
}

func debugEditCmd(op string, args []string) {
	fs := flag.NewFlagSet("debug "+op, flag.ExitOnError)
	lineNum := fs.Int("m", 0, "Line number (1-indexed)")
	count := fs.Int("n", 1, "Number of lines")
	asAgent := fs.Bool("as-agent", false, "Register as AI edit")
	// Reorder so flags come before the file argument, allowing
	// both "replace README.md -m 3" and "replace -m 3 README.md"
	fs.Parse(reorderDebugArgs(args))

	if fs.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "Error: file argument required\n")
		os.Exit(1)
	}
	file := fs.Arg(0)

	if *lineNum < 1 {
		fmt.Fprintf(os.Stderr, "Error: -m must be >= 1\n")
		os.Exit(1)
	}
	if *count < 1 {
		fmt.Fprintf(os.Stderr, "Error: -n must be >= 1\n")
		os.Exit(1)
	}

	root, err := project.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	// Resolve to absolute path, then make relative to project root
	absFile := file
	if !filepath.IsAbs(file) {
		absFile = filepath.Join(root, file)
	}
	relFile := record.RelativizePath(absFile, root)

	switch op {
	case "insert":
		debugInsert(root, absFile, relFile, *lineNum, *count, *asAgent)
	case "replace":
		debugReplace(root, absFile, relFile, *lineNum, *count, *asAgent)
	case "remove":
		debugRemove(root, absFile, relFile, *lineNum, *count, *asAgent)
	}
}

func debugInsert(root, absFile, relFile string, line, count int, asAgent bool) {
	lines, err := readAllLines(absFile)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	// For insert, line can be 1 past the end (append)
	if line > len(lines)+1 {
		fmt.Fprintf(os.Stderr, "Error: line %d out of range (file has %d lines)\n", line, len(lines))
		os.Exit(1)
	}

	// Generate new lines (random suffix ensures unique content hashes)
	suffix := ""
	if asAgent {
		suffix = "-as-agent"
	}
	var newContent []string
	for i := 0; i < count; i++ {
		newContent = append(newContent, fmt.Sprintf("debug-insert-L%d-%d%s", line+i, rand.Intn(100000), suffix))
	}

	// Splice in
	result := make([]string, 0, len(lines)+count)
	result = append(result, lines[:line-1]...)
	result = append(result, newContent...)
	result = append(result, lines[line-1:]...)

	if err := writeFileLines(absFile, result); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	fmt.Printf("Inserted %d line(s) at L%d in %s\n", count, line, relFile)

	if asAgent {
		hunk := record.HunkInfo{
			OldStart: line,
			OldLines: 0,
			NewStart: line,
			NewLines: count,
		}
		newLines := lineset.FromRange(line, line+count-1)
		preContent := strings.Join(lines, "\n") + "\n"
		if len(lines) == 0 {
			preContent = ""
		}
		postContent := strings.Join(result, "\n") + "\n"
		registerAsAgent(root, relFile, "insert", hunk, newLines, strings.Join(newContent, "\n"), preContent, postContent)
	}
}

func debugReplace(root, absFile, relFile string, line, count int, asAgent bool) {
	lines, err := readAllLines(absFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	if line+count-1 > len(lines) {
		fmt.Fprintf(os.Stderr, "Error: lines %d-%d out of range (file has %d lines)\n",
			line, line+count-1, len(lines))
		os.Exit(1)
	}

	// Generate replacement lines (random suffix ensures unique content hashes)
	suffix := ""
	if asAgent {
		suffix = "-as-agent"
	}
	var newContent []string
	for i := 0; i < count; i++ {
		newContent = append(newContent, fmt.Sprintf("debug-replace-L%d-%d%s", line+i, rand.Intn(100000), suffix))
	}

	// Capture pre-edit content before replacing
	preContent := strings.Join(lines, "\n") + "\n"

	// Replace in-place
	for i := 0; i < count; i++ {
		lines[line-1+i] = newContent[i]
	}

	if err := writeFileLines(absFile, lines); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	fmt.Printf("Replaced %d line(s) at L%d-%d in %s\n", count, line, line+count-1, relFile)

	if asAgent {
		hunk := record.HunkInfo{
			OldStart: line,
			OldLines: count,
			NewStart: line,
			NewLines: count,
		}
		newLines := lineset.FromRange(line, line+count-1)
		postContent := strings.Join(lines, "\n") + "\n"
		registerAsAgent(root, relFile, "replace", hunk, newLines, strings.Join(newContent, "\n"), preContent, postContent)
	}
}

func debugRemove(root, absFile, relFile string, line, count int, asAgent bool) {
	lines, err := readAllLines(absFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	if line+count-1 > len(lines) {
		fmt.Fprintf(os.Stderr, "Error: lines %d-%d out of range (file has %d lines)\n",
			line, line+count-1, len(lines))
		os.Exit(1)
	}

	// Remove lines
	result := make([]string, 0, len(lines)-count)
	result = append(result, lines[:line-1]...)
	result = append(result, lines[line+count-1:]...)

	if err := writeFileLines(absFile, result); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	fmt.Printf("Removed %d line(s) at L%d-%d in %s\n", count, line, line+count-1, relFile)

	if asAgent {
		hunk := record.HunkInfo{
			OldStart: line,
			OldLines: count,
			NewStart: line,
			NewLines: 0,
		}
		preContent := strings.Join(lines, "\n") + "\n"
		postContent := strings.Join(result, "\n") + "\n"
		if len(result) == 0 {
			postContent = ""
		}
		// No new lines for a removal — empty line set
		registerAsAgent(root, relFile, "remove", hunk, lineset.LineSet{}, "", preContent, postContent)
	}
}

func registerAsAgent(root, relFile, op string, hunk record.HunkInfo, newLines lineset.LineSet, newContent string, preEditContent string, postEditContent string) {
	if !project.IsInitialized(root) {
		fmt.Fprintln(os.Stderr, "Warning: --as-agent ignored, blamebot not initialized (run 'git-blamebot enable')")
		return
	}

	paths := project.NewPaths(root)
	change := fmt.Sprintf("debug %s %d line(s) at L%d", op, hunk.NewLines, hunk.OldStart)
	if op == "remove" {
		change = fmt.Sprintf("debug remove %d line(s) at L%d", hunk.OldLines, hunk.OldStart)
	}

	pe := provenance.PendingEdit{
		ID:          uuid.New().String(),
		Ts:          time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		File:        relFile,
		Lines:       newLines,
		Hunk:        &hunk,
		ContentHash: record.ContentHash(newContent),
		Prompt:      "debug edit",
		Change:      change,
		Tool:        "Edit",
		Author:      "debug",
		Session:     "debug-session",
	}

	if err := provenance.WritePending(paths.GitDir, pe); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing pending edit: %v\n", err)
		os.Exit(1)
	}

	// Write pre-edit and post-edit checkpoints for attribution tracking
	toolUseID := "debug-" + pe.ID[:8]

	preSHA, err := checkpoint.WriteBlob(paths.CheckpointDir, preEditContent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing pre-edit blob: %v\n", err)
		os.Exit(1)
	}
	if _, err := checkpoint.WriteCheckpoint(paths.CheckpointDir, checkpoint.Checkpoint{
		Kind:       "pre-edit",
		File:       relFile,
		ContentSHA: preSHA,
		ToolUseID:  toolUseID,
		Ts:         pe.Ts,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing pre-edit checkpoint: %v\n", err)
		os.Exit(1)
	}

	postSHA, err := checkpoint.WriteBlob(paths.CheckpointDir, postEditContent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing post-edit blob: %v\n", err)
		os.Exit(1)
	}
	if _, err := checkpoint.WriteCheckpoint(paths.CheckpointDir, checkpoint.Checkpoint{
		Kind:       "post-edit",
		File:       relFile,
		ContentSHA: postSHA,
		EditID:     pe.ID,
		ToolUseID:  toolUseID,
		Ts:         pe.Ts,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing post-edit checkpoint: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  Registered as AI edit (pending: %s)\n", pe.ID[:8])
}

// reorderDebugArgs moves flags before positional args for flag.Parse.
func reorderDebugArgs(args []string) []string {
	var flags, positional []string
	i := 0
	for i < len(args) {
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			// --as-agent is boolean, doesn't take a value
			if a != "--as-agent" && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, a)
		}
		i++
	}
	return append(flags, positional...)
}

func readAllLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)
	if content == "" {
		return nil, nil
	}
	// Split preserving the final newline behavior
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	return lines, nil
}

func writeFileLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}
