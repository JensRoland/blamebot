package index

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"

	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/project"
	"github.com/jensroland/git-blamebot/internal/provenance"
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
	if _, err := os.Stat(paths.IndexDB); err != nil {
		return true
	}

	db, err := sql.Open("sqlite", paths.IndexDB)
	if err != nil {
		return true
	}
	defer db.Close()

	// Check provenance branch tip SHA
	var storedProvSHA string
	db.QueryRow("SELECT value FROM meta WHERE key = 'prov_tip_sha'").Scan(&storedProvSHA)
	currentProvSHA := provenance.BranchTipSHA(paths.Root)
	if currentProvSHA != storedProvSHA {
		return true
	}

	// Check HEAD
	if headSHAChanged(db, paths.Root) {
		return true
	}

	// Check pending edits
	currentPending := countPendingFiles(paths.PendingDir)
	var storedPending int
	db.QueryRow("SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'pending_count'").Scan(&storedPending)
	return currentPending != storedPending
}

// Rebuild drops and recreates the SQLite index from provenance branch manifests.
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
	manifestCount := 0

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

	// Process committed manifests from provenance branch
	manifests, _ := provenance.ReadAllManifests(paths.Root)
	for _, m := range manifests {
		manifestCount++
		for _, edit := range m.Edits {
			// Parse lines
			var lineStart, lineEnd *int
			var changedLines *string
			if !edit.Lines.IsEmpty() {
				s := edit.Lines.String()
				changedLines = &s
				mn := edit.Lines.Min()
				mx := edit.Lines.Max()
				lineStart = &mn
				lineEnd = &mx
			}

			// Extract hunk data
			var oldStart, oldLines, newStart, newLines *int
			if edit.Hunk != nil {
				oldStart = &edit.Hunk.OldStart
				oldLines = &edit.Hunk.OldLines
				newStart = &edit.Hunk.NewStart
				newLines = &edit.Hunk.NewLines
			}

			change := edit.Change
			if change == "" {
				change = edit.Reason
			}

			stmt.Exec(
				edit.File,
				lineStart,
				lineEnd,
				edit.ContentHash,
				m.Timestamp,
				edit.Prompt,
				edit.Reason,
				change,
				edit.Tool,
				m.Author,
				edit.Session,
				edit.Trace,
				m.ID,
				oldStart,
				oldLines,
				newStart,
				newLines,
				changedLines,
				m.CommitSHA,
			)
			recordCount++
		}
	}

	// Process pending edits (uncommitted, no commit_sha yet)
	pendingEdits, _ := provenance.ReadAllPending(paths.GitDir)
	for _, pe := range pendingEdits {
		var lineStart, lineEnd *int
		var changedLines *string
		if !pe.Lines.IsEmpty() {
			s := pe.Lines.String()
			changedLines = &s
			mn := pe.Lines.Min()
			mx := pe.Lines.Max()
			lineStart = &mn
			lineEnd = &mx
		}

		var oldStart, oldLines, newStart, newLines *int
		if pe.Hunk != nil {
			oldStart = &pe.Hunk.OldStart
			oldLines = &pe.Hunk.OldLines
			newStart = &pe.Hunk.NewStart
			newLines = &pe.Hunk.NewLines
		}

		stmt.Exec(
			pe.File,
			lineStart,
			lineEnd,
			pe.ContentHash,
			pe.Ts,
			pe.Prompt,
			"",
			pe.Change,
			pe.Tool,
			pe.Author,
			pe.Session,
			pe.Trace,
			"pending",
			oldStart,
			oldLines,
			newStart,
			newLines,
			changedLines,
			"", // no commit_sha for pending edits
		)
		recordCount++
	}

	if err := tx.Commit(); err != nil {
		db.Close()
		return nil, err
	}

	storeHeadSHA(db, paths.Root)
	storeProvTipSHA(db, paths.Root)
	storePendingCount(db, countPendingFiles(paths.PendingDir))

	if !quiet {
		msg := fmt.Sprintf("\033[2mIndex rebuilt: %d records from %d manifests", recordCount, manifestCount)
		if len(pendingEdits) > 0 {
			msg += fmt.Sprintf(" + %d pending", len(pendingEdits))
		}
		msg += "\033[0m\n\n"
		fmt.Fprint(os.Stderr, msg)
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

// storeHeadSHA saves the current HEAD SHA for staleness detection.
func storeHeadSHA(db *sql.DB, root string) {
	sha := git.HeadSHA(root)
	if sha == "" {
		return
	}
	_, _ = db.Exec("CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)")
	_, _ = db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('head_sha', ?)", sha)
}

// storeProvTipSHA saves the provenance branch tip SHA for staleness detection.
func storeProvTipSHA(db *sql.DB, root string) {
	sha := provenance.BranchTipSHA(root)
	_, _ = db.Exec("CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)")
	_, _ = db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('prov_tip_sha', ?)", sha)
}

// headSHAChanged returns true if HEAD has changed since the last rebuild.
func headSHAChanged(db *sql.DB, root string) bool {
	var storedSHA string
	err := db.QueryRow("SELECT value FROM meta WHERE key = 'head_sha'").Scan(&storedSHA)
	if err != nil {
		return false
	}
	currentSHA := git.HeadSHA(root)
	return currentSHA != "" && currentSHA != storedSHA
}

// storePendingCount saves the number of pending edit files for staleness detection.
func storePendingCount(db *sql.DB, count int) {
	_, _ = db.Exec("CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)")
	_, _ = db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('pending_count', ?)", count)
}

// countPendingFiles counts files in the pending edits directory.
func countPendingFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}
