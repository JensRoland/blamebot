package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// FillResult represents a single reason fill result from Haiku.
type FillResult struct {
	ID     int    `json:"id"`
	Reason string `json:"reason"`
}

// Call invokes the claude CLI with a prompt and returns the response text.
func Call(prompt, model string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", "--model", model,
		"--output-format", "text",
		"--allowed-tools", "")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("LLM call timed out")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("claude CLI failed: %s", string(exitErr.Stderr[:min(len(exitErr.Stderr), 200)]))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var codeFenceRe = regexp.MustCompile(`(?s)^` + "```" + `json\s*`)
var codeFenceEndRe = regexp.MustCompile(`\s*` + "```" + `$`)

// CallHaiku calls Claude Haiku and parses a JSON array response.
func CallHaiku(prompt string) ([]FillResult, error) {
	text, err := Call(prompt, "claude-haiku-4-5-20251001", 60*time.Second)
	if err != nil {
		return nil, err
	}
	if text == "" {
		return nil, fmt.Errorf("empty response from Haiku")
	}

	// Strip markdown code fences
	text = codeFenceRe.ReplaceAllString(text, "")
	text = codeFenceEndRe.ReplaceAllString(text, "")

	var results []FillResult
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		return nil, fmt.Errorf("could not parse Haiku response as JSON: %s", text[:min(len(text), 300)])
	}
	return results, nil
}

func filterEnv(env []string, exclude string) []string {
	var filtered []string
	prefix := exclude + "="
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
