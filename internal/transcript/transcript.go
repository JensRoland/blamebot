package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type transcriptEntry struct {
	Message struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	} `json:"message"`
	Type string `json:"type"`
}

type contentBlock struct {
	Type     string          `json:"type"`
	ID       string          `json:"id,omitempty"`
	Text     string          `json:"text,omitempty"`
	Thinking string          `json:"thinking,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

// ReadTraceContext reads reasoning context for a specific tool call.
// traceRef format: /path/to/transcript.jsonl#tool_use_id
func ReadTraceContext(traceRef string, projectRoot string) string {
	if traceRef == "" {
		return ""
	}

	parts := strings.SplitN(traceRef, "#", 2)
	transcriptPath := parts[0]
	var toolUseID string
	if len(parts) > 1 {
		toolUseID = parts[1]
	}

	// 1. Try committed traces (portable)
	if projectRoot != "" && toolUseID != "" && transcriptPath != "" {
		sessionID := strings.TrimSuffix(filepath.Base(transcriptPath), filepath.Ext(transcriptPath))
		tracesFile := filepath.Join(projectRoot, ".blamebot", "traces", sessionID+".json")
		if data, err := os.ReadFile(tracesFile); err == nil {
			var traces map[string]string
			if json.Unmarshal(data, &traces) == nil {
				if ctx, ok := traces[toolUseID]; ok && ctx != "" {
					return ctx
				}
			}
		}
	}

	// 2. Fall back to full local transcript
	if transcriptPath == "" {
		return ""
	}
	if _, err := os.Stat(transcriptPath); err != nil {
		return ""
	}

	entries, err := readEntries(transcriptPath)
	if err != nil {
		return fmt.Sprintf("Error reading transcript: %v", err)
	}

	if toolUseID == "" {
		return fmt.Sprintf("Transcript has %d entries (no tool_use_id to locate specific call)", len(entries))
	}

	// Find the entry containing this tool_use_id
	targetIdx := -1
	for i, entry := range entries {
		for _, raw := range entry.Message.Content {
			var block contentBlock
			if json.Unmarshal(raw, &block) == nil && block.ID == toolUseID {
				targetIdx = i
				break
			}
		}
		if targetIdx >= 0 {
			break
		}
	}

	if targetIdx < 0 {
		return fmt.Sprintf("tool_use_id %s not found in transcript (%d entries)", toolUseID, len(entries))
	}

	return walkBackwards(entries, targetIdx)
}

// ExtractTraceContexts extracts trace context for multiple tool_use_ids from a transcript.
func ExtractTraceContexts(transcriptPath string, toolUseIDs []string) map[string]string {
	if len(toolUseIDs) == 0 || transcriptPath == "" {
		return nil
	}
	if _, err := os.Stat(transcriptPath); err != nil {
		return nil
	}

	entries, err := readEntries(transcriptPath)
	if err != nil {
		return nil
	}

	// Build index: tool_use_id -> entry index
	idSet := make(map[string]bool)
	for _, id := range toolUseIDs {
		idSet[id] = true
	}

	idToIdx := make(map[string]int)
	for i, entry := range entries {
		for _, raw := range entry.Message.Content {
			var block contentBlock
			if json.Unmarshal(raw, &block) == nil && idSet[block.ID] {
				idToIdx[block.ID] = i
			}
		}
	}

	results := make(map[string]string)
	for _, id := range toolUseIDs {
		targetIdx, ok := idToIdx[id]
		if !ok {
			continue
		}
		ctx := walkBackwards(entries, targetIdx)
		if ctx != "" {
			results[id] = ctx
		}
	}
	return results
}

// ExtractDiffFromTrace extracts old_string/new_string from a transcript for a tool call.
func ExtractDiffFromTrace(traceRef string) (string, string, bool) {
	if traceRef == "" {
		return "", "", false
	}

	parts := strings.SplitN(traceRef, "#", 2)
	transcriptPath := parts[0]
	if len(parts) < 2 || transcriptPath == "" {
		return "", "", false
	}
	toolUseID := parts[1]

	if _, err := os.Stat(transcriptPath); err != nil {
		return "", "", false
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry transcriptEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}

		for _, raw := range entry.Message.Content {
			var block contentBlock
			if json.Unmarshal(raw, &block) != nil {
				continue
			}
			if block.ID != toolUseID {
				continue
			}

			var input map[string]interface{}
			if json.Unmarshal(block.Input, &input) != nil {
				return "", "", false
			}

			oldStr, _ := input["old_string"].(string)
			newStr, _ := input["new_string"].(string)
			if oldStr != "" || newStr != "" {
				return oldStr, newStr, true
			}

			// Write tool: content is the new file
			if content, ok := input["content"].(string); ok && content != "" {
				return "", content, true
			}
			return "", "", false
		}
	}

	return "", "", false
}

var ideTagRe = regexp.MustCompile(`(?s)<ide_\w+>.*?</ide_\w+>\s*`)
var sysReminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>\s*`)

// ExtractSessionPrompts extracts the ordered list of user prompts from a transcript.
func ExtractSessionPrompts(transcriptPath string) []string {
	if _, err := os.Stat(transcriptPath); err != nil {
		return nil
	}

	entries, err := readEntries(transcriptPath)
	if err != nil {
		return nil
	}

	var prompts []string
	for _, entry := range entries {
		if entry.Message.Role != "user" {
			continue
		}

		// Skip tool_result entries
		allToolResult := true
		for _, raw := range entry.Message.Content {
			var block contentBlock
			if json.Unmarshal(raw, &block) == nil {
				if block.Type != "tool_result" {
					allToolResult = false
					break
				}
			}
		}
		if allToolResult && len(entry.Message.Content) > 0 {
			continue
		}

		for _, raw := range entry.Message.Content {
			var block contentBlock
			if json.Unmarshal(raw, &block) != nil {
				continue
			}
			if block.Type == "text" {
				text := cleanPromptText(block.Text)
				if text != "" {
					prompts = append(prompts, text)
				}
			}
		}
	}
	return prompts
}

func cleanPromptText(raw string) string {
	cleaned := ideTagRe.ReplaceAllString(raw, "")
	cleaned = sysReminderRe.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

func readEntries(path string) ([]transcriptEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []transcriptEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry transcriptEntry
		if json.Unmarshal([]byte(line), &entry) == nil {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// walkBackwards walks backwards from targetIdx to find thinking/text blocks.
func walkBackwards(entries []transcriptEntry, targetIdx int) string {
	var contextParts []string
	foundReasoning := false

	for j := targetIdx - 1; j >= 0; j-- {
		entry := entries[j]
		role := entry.Message.Role
		if role == "" {
			role = entry.Type
		}

		if role == "user" {
			// Check if all blocks are tool_result
			allToolResult := true
			for _, raw := range entry.Message.Content {
				var block contentBlock
				if json.Unmarshal(raw, &block) == nil {
					if block.Type != "tool_result" {
						allToolResult = false
						break
					}
				}
			}
			if allToolResult {
				continue
			}
			break // real user message — stop
		}

		if role != "assistant" {
			continue
		}

		for _, raw := range entry.Message.Content {
			var block contentBlock
			if json.Unmarshal(raw, &block) != nil {
				continue
			}
			switch block.Type {
			case "thinking":
				if block.Thinking != "" {
					thinking := block.Thinking
					if len(thinking) > 1500 {
						thinking = thinking[len(thinking)-1500:]
					}
					contextParts = append(contextParts, "[Thinking]\n"+thinking)
					foundReasoning = true
				}
			case "text":
				if block.Text != "" {
					text := block.Text
					if len(text) > 500 {
						text = text[len(text)-500:]
					}
					contextParts = append(contextParts, "[Response]\n"+text)
					foundReasoning = true
				}
			case "tool_use":
				// Skip other tool calls
				continue
			}
		}

		if foundReasoning {
			break
		}
	}

	if len(contextParts) > 0 {
		// Reverse for chronological order
		for i, j := 0, len(contextParts)-1; i < j; i, j = i+1, j-1 {
			contextParts[i], contextParts[j] = contextParts[j], contextParts[i]
		}
		return strings.Join(contextParts, "\n\n")
	}

	return fmt.Sprintf("Tool call found at entry %d, but no thinking/text blocks found before it", targetIdx)
}
