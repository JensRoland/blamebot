package cmd

import (
	"strings"
	"testing"
)

func TestBuildFillPrompt(t *testing.T) {
	intPtr := func(v int) *int { return &v }

	t.Run("basic prompt and edits", func(t *testing.T) {
		prompts := []string{"fix the login bug", "also update the tests"}
		edits := []fillEdit{
			{ID: 1, File: "auth.go", LineStart: intPtr(10), LineEnd: intPtr(20), Change: "fixed null check"},
			{ID: 2, File: "auth_test.go", LineStart: intPtr(5), LineEnd: intPtr(5), Change: "added test case"},
		}

		result := buildFillPrompt(prompts, edits)

		if !strings.Contains(result, "fix the login bug") {
			t.Error("expected prompt text in output")
		}
		if !strings.Contains(result, "also update the tests") {
			t.Error("expected second prompt text in output")
		}
		if !strings.Contains(result, "auth.go") {
			t.Error("expected file name in output")
		}
		if !strings.Contains(result, "fixed null check") {
			t.Error("expected change description in output")
		}
	})

	t.Run("long prompt truncated", func(t *testing.T) {
		longPrompt := strings.Repeat("a", 250)
		prompts := []string{longPrompt}
		edits := []fillEdit{
			{ID: 1, File: "main.go", Change: "refactored"},
		}

		result := buildFillPrompt(prompts, edits)

		// The truncated prompt should end with "..."
		if !strings.Contains(result, "...") {
			t.Error("expected truncated prompt to contain '...'")
		}
		// The full 250-char string should not appear
		if strings.Contains(result, longPrompt) {
			t.Error("expected long prompt to be truncated")
		}
		// The truncated version should be 197 chars + "..." = 200 chars total
		truncated := longPrompt[:197] + "..."
		if !strings.Contains(result, truncated) {
			t.Error("expected truncated prompt (197 chars + '...')")
		}
	})

	t.Run("line range format", func(t *testing.T) {
		edits := []fillEdit{
			{ID: 1, File: "handler.go", LineStart: intPtr(5), LineEnd: intPtr(10), Change: "added validation"},
		}

		result := buildFillPrompt(nil, edits)

		if !strings.Contains(result, "L5-10") {
			t.Errorf("expected 'L5-10' in output, got:\n%s", result)
		}
	})

	t.Run("single line format", func(t *testing.T) {
		edits := []fillEdit{
			{ID: 1, File: "config.go", LineStart: intPtr(5), LineEnd: intPtr(5), Change: "updated default"},
		}

		result := buildFillPrompt(nil, edits)

		if !strings.Contains(result, "L5") {
			t.Errorf("expected 'L5' in output, got:\n%s", result)
		}
		// Should NOT contain "L5-5" for single line
		if strings.Contains(result, "L5-5") {
			t.Errorf("expected single line format 'L5', not 'L5-5'")
		}
	})

	t.Run("nil lines no line info", func(t *testing.T) {
		edits := []fillEdit{
			{ID: 1, File: "utils.go", Change: "added helper"},
		}

		result := buildFillPrompt(nil, edits)

		// The line containing the file should not have any L-notation
		for _, line := range strings.Split(result, "\n") {
			if strings.Contains(line, "utils.go") {
				if strings.Contains(line, " L") {
					t.Errorf("expected no line info for nil lines, got: %s", line)
				}
			}
		}
	})
}
