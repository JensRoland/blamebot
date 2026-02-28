package debug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLog(t *testing.T) {
	t.Run("writes_timestamp_and_message", func(t *testing.T) {
		cacheDir := t.TempDir()

		Log(cacheDir, "test.log", "hello world", nil)

		logFile := filepath.Join(cacheDir, "logs", "test.log")
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}
		content := string(data)

		if !strings.Contains(content, "hello world") {
			t.Errorf("log should contain message, got: %s", content)
		}
		// Timestamp format: [2006-01-02T15:04:05]
		if !strings.Contains(content, "[") || !strings.Contains(content, "T") {
			t.Errorf("log should contain timestamp, got: %s", content)
		}
		// Should contain separator line
		if !strings.Contains(content, "====") {
			t.Errorf("log should contain separator, got: %s", content)
		}
	})

	t.Run("appends_json_when_data_non_nil", func(t *testing.T) {
		cacheDir := t.TempDir()

		Log(cacheDir, "test.log", "with data", map[string]string{"key": "value"})

		logFile := filepath.Join(cacheDir, "logs", "test.log")
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}
		content := string(data)

		if !strings.Contains(content, `"key"`) || !strings.Contains(content, `"value"`) {
			t.Errorf("log should contain JSON data, got: %s", content)
		}
	})

	t.Run("no_json_block_when_data_nil", func(t *testing.T) {
		cacheDir := t.TempDir()

		Log(cacheDir, "test.log", "nil data", nil)

		logFile := filepath.Join(cacheDir, "logs", "test.log")
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}
		content := string(data)

		// Should have the message but no JSON curly braces
		if !strings.Contains(content, "nil data") {
			t.Errorf("log should contain message, got: %s", content)
		}
		if strings.Contains(content, "{") {
			t.Errorf("log should not contain JSON block for nil data, got: %s", content)
		}
	})

	t.Run("appends_to_existing_file", func(t *testing.T) {
		cacheDir := t.TempDir()

		Log(cacheDir, "test.log", "first entry", nil)
		Log(cacheDir, "test.log", "second entry", nil)

		logFile := filepath.Join(cacheDir, "logs", "test.log")
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}
		content := string(data)

		if !strings.Contains(content, "first entry") {
			t.Errorf("log should contain first entry, got: %s", content)
		}
		if !strings.Contains(content, "second entry") {
			t.Errorf("log should contain second entry, got: %s", content)
		}
	})
}
