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

	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/project"
)

// ReasonRow mirrors a row from the reasons table.
type ReasonRow struct {
	ID           int
	File         string
	LineStart    *int
	LineEnd      *int
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
	OldStart     *int
	OldLines     *int
	NewStart     *int
	NewLines     *int
	ChangedLines *string
	CommitSHA    string
}

// sourceRef tracks the JSONL source file and line number for a record.
type sourceRef struct {
	sourceFile string
	lineNum    int
}

// ScanRow scans a *sql.Rows into a ReasonRow.
func ScanRow(rows *sql.Rows) (*ReasonRow, error) {
	r := &ReasonRow{}
	err := rows.Scan(
		&r.ID, &r.File, &r.LineStart, &r.LineEnd, &r.ContentHash,
		&r.Ts, &r.Prompt, &r.Reason, &r.Change, &r.Tool,
		&r.Author, &r.Session, &r.Trace, &r.SourceFile,
		&r.OldStart, &r.OldLines, &r.NewStart, &r.NewLines,
		&r.ChangedLines, &r.CommitSHA,
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

	// Check if HEAD has changed (rebase, amend, etc.)
	if headSHAChanged(paths) {
		return true
	}

	return false
}

// Rebuild drops and recreates the SQLite index from JSONL files.
func Rebuild(paths project.Paths, quiet bool) (*sql.DB, error) {
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create cache dir %s: %w", paths.CacheDir, err)
	}
	_ = os.Remove(paths.IndexDB)

	db, err := sql.Open("sqlite", paths.IndexDB)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", paths.IndexDB, err)
	}

	_, err = db.Exec(`
		CREATE TABLE reasons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file TEXT NOT NULL,
			line_start INTEGER,
			line_end INTEGER,
			content_hash TEXT,
			ts TEXT NOT NULL,
			prompt TEXT,
			reason TEXT,
			change TEXT,
			tool TEXT,
			author TEXT,
			session TEXT,
			trace TEXT,
			source_file TEXT,
			old_start INTEGER,
			old_lines INTEGER,
			new_start INTEGER,
			new_lines INTEGER,
			changed_lines TEXT,
			commit_sha TEXT
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
		"CREATE INDEX idx_commit_sha ON reasons(commit_sha)",
	} {
		if _, err := db.Exec(idx); err != nil {
			db.Close()
			return nil, fmt.Errorf("create index: %w", err)
		}
	}

	recordCount := 0
	fileCount := 0

	var insertedRefs []sourceRef

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
			(file, line_start, line_end, content_hash, ts,
			 prompt, reason, change, tool, author, session, trace, source_file,
			 old_start, old_lines, new_start, new_lines, changed_lines, commit_sha)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			jsonlLineNum := 0
			for scanner.Scan() {
				jsonlLineNum++
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}

				var rec map[string]interface{}
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					continue
				}

				// Parse lines: new string format ("5,7-8,12") or legacy array ([5,12])
				var lineStart, lineEnd *int
				var changedLines *string
				if linesVal, ok := rec["lines"]; ok && linesVal != nil {
					switch lv := linesVal.(type) {
					case string:
						// New format: compact LineSet notation
						if lv != "" {
							changedLines = &lv
							ls, err := lineset.FromString(lv)
							if err == nil && !ls.IsEmpty() {
								mn := ls.Min()
								mx := ls.Max()
								lineStart = &mn
								lineEnd = &mx
							}
						}
					case []interface{}:
						// Legacy format: [start, end]
						if len(lv) > 0 {
							if v, ok := lv[0].(float64); ok {
								n := int(v)
								lineStart = &n
							}
						}
						if len(lv) > 1 {
							if v, ok := lv[1].(float64); ok {
								n := int(v)
								lineEnd = &n
							}
						}
					}
				}

				change := getStr(rec, "change")
				if change == "" {
					change = getStr(rec, "reason")
				}

				// Extract hunk info if present
				var oldStart, oldLines, newStart, newLines *int
				if hunk, ok := rec["hunk"].(map[string]interface{}); ok {
					if v, ok := hunk["old_start"].(float64); ok {
						n := int(v)
						oldStart = &n
					}
					if v, ok := hunk["old_lines"].(float64); ok {
						n := int(v)
						oldLines = &n
					}
					if v, ok := hunk["new_start"].(float64); ok {
						n := int(v)
						newStart = &n
					}
					if v, ok := hunk["new_lines"].(float64); ok {
						n := int(v)
						newLines = &n
					}
				}

				stmt.Exec(
					getStr(rec, "file"),
					lineStart,
					lineEnd,
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
					oldStart,
					oldLines,
					newStart,
					newLines,
					changedLines,
					"", // commit_sha populated below via git blame
				)
				insertedRefs = append(insertedRefs, sourceRef{
					sourceFile: e.Name(),
					lineNum:    jsonlLineNum,
				})
				recordCount++
			}
			f.Close()
		}

		if err := tx.Commit(); err != nil {
			db.Close()
			return nil, err
		}
	}

	// Populate commit_sha via git blame on JSONL files
	populateCommitSHAs(db, paths, insertedRefs)

	// Store HEAD SHA for staleness detection
	storeHeadSHA(db, paths.Root)

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

// populateCommitSHAs uses git blame on JSONL files to fill commit_sha for each record.
func populateCommitSHAs(db *sql.DB, paths project.Paths, refs []sourceRef) {
	if len(refs) == 0 {
		return
	}

	// Group refs by source file
	fileRefs := make(map[string][]int) // source_file → []lineNum
	for _, ref := range refs {
		fileRefs[ref.sourceFile] = append(fileRefs[ref.sourceFile], ref.lineNum)
	}

	// For each JSONL file, run git blame to get commit SHAs
	for sourceFile, lineNums := range fileRefs {
		jsonlRel := filepath.Join(".blamebot", "log", sourceFile)
		blameMap, err := git.BlameJSONLLines(paths.Root, jsonlRel)
		if err != nil {
			// Not in git or other error — skip (commit_sha stays empty)
			continue
		}

		// Update each record's commit_sha
		for _, lineNum := range lineNums {
			if sha, ok := blameMap[lineNum]; ok && sha != "" {
				db.Exec(
					"UPDATE reasons SET commit_sha = ? WHERE source_file = ? AND id = (SELECT id FROM reasons WHERE source_file = ? ORDER BY id LIMIT 1 OFFSET ?)",
					sha, sourceFile, sourceFile, lineNum-1,
				)
			}
		}
	}
}

// storeHeadSHA saves the current HEAD SHA for staleness detection.
func storeHeadSHA(db *sql.DB, root string) {
	sha := git.HeadSHA(root)
	if sha == "" {
		return
	}
	db.Exec("CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)")
	db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('head_sha', ?)", sha)
}

// headSHAChanged returns true if HEAD has changed since the last rebuild.
func headSHAChanged(paths project.Paths) bool {
	db, err := sql.Open("sqlite", paths.IndexDB)
	if err != nil {
		return false
	}
	defer db.Close()

	var storedSHA string
	err = db.QueryRow("SELECT value FROM meta WHERE key = 'head_sha'").Scan(&storedSHA)
	if err != nil {
		return false
	}

	currentSHA := git.HeadSHA(paths.Root)
	return currentSHA != "" && currentSHA != storedSHA
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
