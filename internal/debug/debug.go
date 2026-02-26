package debug

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Log appends a debug entry to the specified log file in cacheDir/logs/.
func Log(cacheDir, logName, message string, data interface{}) {
	logDir := filepath.Join(cacheDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)

	logFile := filepath.Join(logDir, logName)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02T15:04:05")
	fmt.Fprintf(f, "\n%s\n", strings.Repeat("=", 60))
	fmt.Fprintf(f, "[%s] %s\n", ts, message)

	if data != nil {
		b, err := json.MarshalIndent(data, "", "  ")
		if err == nil {
			fmt.Fprintf(f, "%s\n", b)
		}
	}
}
