package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/project"
)

func cmdLog(paths project.Paths, hookLog bool) {
	logName := "capture_prompt.log"
	if hookLog {
		logName = "hook.log"
	}
	logFile := filepath.Join(paths.CacheDir, "logs", logName)

	data, err := os.ReadFile(logFile)
	if err != nil {
		fmt.Printf("No log file at %s\n", logFile)
		return
	}

	lines := strings.Split(string(data), "\n")
	start := 0
	if len(lines) > 100 {
		start = len(lines) - 100
	}
	tail := lines[start:]

	fmt.Printf("%s--- %s (last %d lines) ---%s\n\n", format.Dim, logFile, len(tail), format.Reset)
	fmt.Println(strings.Join(tail, "\n"))
}

func cmdDumpPayload(paths project.Paths) {
	for _, logName := range []string{"hook.log", "capture_prompt.log"} {
		logFile := filepath.Join(paths.CacheDir, "logs", logName)
		data, err := os.ReadFile(logFile)
		if err != nil {
			continue
		}

		fmt.Printf("\n%s=== %s (last entry) ===%s\n\n", format.Bold, logName, format.Reset)
		entries := strings.Split(string(data), strings.Repeat("=", 60))
		start := len(entries) - 3
		if start < 0 {
			start = 0
		}
		for _, entry := range entries[start:] {
			trimmed := strings.TrimSpace(entry)
			if trimmed != "" {
				fmt.Println(trimmed)
				fmt.Println()
			}
		}
	}
}
