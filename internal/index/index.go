package index

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/project"
)

// ReasonRow mirrors a row from the reasons table.
type ReasonRow struct {
	ID           int
	File         string
	LineStart    *int
	LineEnd      *int
	ChangedLines string
	ContentHash  string
	Ts           string
	Prompt       string
	Reason       string
	Change       string
	Tool         string
	Author       string
	Session      string
	Trace        string
	SourceFile   string
}

// ScanRow scans a *sql.Rows into a ReasonRow.
func ScanRow(rows *sql.Rows) (*ReasonRow, error) {
	r := &ReasonRow{}
	err := rows.Scan(
		&r.ID, &r.File, &r.LineStart, &r.LineEnd, &r.ChangedLines, &r.ContentHash,
		&r.Ts, &r.Prompt, &r.Reason, &r.Change, &r.Tool,
		&r.Author, &r.Session, &r.Trace, &r.SourceFile,
	)
	return r, err
}

// IsStale returns true if the index needs rebuilding.
func IsStale(paths project.Paths) bool {
	info, err := os.Stat(paths.IndexDB)
	if err != nil {
		return true
	}
	indexMtime := info.ModTime()

	entries, err := os.ReadDir(paths.LogDir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fInfo, err := e.Info()
		if err != nil {
			continue
		}
		if fInfo.ModTime().After(indexMtime) {
			return true
		}
	}
	return false
}

// Rebuild drops and recreates the SQLite index from JSONL files.
func Rebuild(paths project.Paths, quiet bool) (*sql.DB, error) {
	_ = os.MkdirAll(paths.CacheDir, 0o755)
	_ = os.Remove(paths.IndexDB)

	db, err := sql.Open("sqlite", paths.IndexDB)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE reasons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file TEXT NOT NULL,
			line_start INTEGER,
			line_end INTEGER,
			changed_lines TEXT,
			content_hash TEXT,
			ts TEXT NOT NULL,
			prompt TEXT,
			reason TEXT,
			change TEXT,
			tool TEXT,
			author TEXT,
			session TEXT,
			trace TEXT,
			source_file TEXT
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	for _, idx := range []string{
		"CREATE INDEX idx_file ON reasons(file)",
		"CREATE INDEX idx_content_hash ON reasons(content_hash)",
		"CREATE INDEX idx_ts ON reasons(ts)",
		"CREATE INDEX idx_author ON reasons(author)",
	} {
		if _, err := db.Exec(idx); err != nil {
			db.Close()
			return nil, fmt.Errorf("create index: %w", err)
		}
	}

	recordCount := 0
	fileCount := 0

	if _, err := os.Stat(paths.LogDir); err == nil {
		entries, _ := os.ReadDir(paths.LogDir)

		// Sort by name for deterministic ordering
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, err
		}

		stmt, err := tx.Prepare(`
			INSERT INTO reasons
			(file, line_start, line_end, changed_lines, content_hash, ts,
			 prompt, reason, change, tool, author, session, trace, source_file)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			tx.Rollback()
			db.Close()
			return nil, err
		}
		defer stmt.Close()

		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			fileCount++

			jsonlPath := filepath.Join(paths.LogDir, e.Name())
			f, err := os.Open(jsonlPath)
			if err != nil {
				continue
			}

			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}

				var rec map[string]interface{}
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					continue
				}

				var lineStart, lineEnd *int
				var changedLines string

				switch v := rec["lines"].(type) {
				case string:
					// New format: compact notation like "5,7-8,12"
					changedLines = v
					ls, err := lineset.FromString(v)
					if err == nil && !ls.IsEmpty() {
						min := ls.Min()
						max := ls.Max()
						lineStart = &min
						lineEnd = &max
					}
				case []interface{}:
					// Legacy format: [start, end] or [null, null]
					if len(v) > 0 {
						if n, ok := v[0].(float64); ok {
							i := int(n)
							lineStart = &i
						}
					}
					if len(v) > 1 {
						if n, ok := v[1].(float64); ok {
							i := int(n)
							lineEnd = &i
						}
					}
				}

				change := getStr(rec, "change")
				if change == "" {
					change = getStr(rec, "reason")
				}

				stmt.Exec(
					getStr(rec, "file"),
					lineStart,
					lineEnd,
					changedLines,
					getStr(rec, "content_hash"),
					getStr(rec, "ts"),
					getStr(rec, "prompt"),
					getStr(rec, "reason"),
					change,
					getStr(rec, "tool"),
					getStr(rec, "author"),
					getStr(rec, "session"),
					getStr(rec, "trace"),
					e.Name(),
				)
				recordCount++
			}
			f.Close()
		}

		if err := tx.Commit(); err != nil {
			db.Close()
			return nil, err
		}
	}

	if !quiet {
		fmt.Fprintf(os.Stderr, "\033[2mIndex rebuilt: %d records from %d log files\033[0m\n\n", recordCount, fileCount)
	}

	return db, nil
}

// Open returns a database connection, rebuilding the index if stale.
func Open(paths project.Paths, forceRebuild bool) (*sql.DB, error) {
	if forceRebuild || IsStale(paths) {
		return Rebuild(paths, false)
	}
	db, err := sql.Open("sqlite", paths.IndexDB)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func getStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
