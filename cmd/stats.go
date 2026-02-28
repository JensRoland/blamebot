package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jensroland/git-blamebot/internal/format"
)

func cmdStats(db *sql.DB, jsonOutput bool) {
	var total, files, authors, sources int
	var first, last sql.NullString

	db.QueryRow("SELECT COUNT(*) FROM reasons").Scan(&total)
	db.QueryRow("SELECT COUNT(DISTINCT file) FROM reasons").Scan(&files)
	db.QueryRow("SELECT COUNT(DISTINCT author) FROM reasons").Scan(&authors)
	db.QueryRow("SELECT COUNT(DISTINCT source_file) FROM reasons").Scan(&sources)
	db.QueryRow("SELECT MIN(ts) FROM reasons").Scan(&first)
	db.QueryRow("SELECT MAX(ts) FROM reasons").Scan(&last)

	type fileCount struct {
		File  string
		Count int
	}
	type authorCount struct {
		Author string
		Count  int
	}

	var topFiles []fileCount
	rows, _ := db.Query("SELECT file, COUNT(*) as cnt FROM reasons GROUP BY file ORDER BY cnt DESC LIMIT 5")
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var fc fileCount
			rows.Scan(&fc.File, &fc.Count)
			topFiles = append(topFiles, fc)
		}
	}

	var topAuthors []authorCount
	rows2, _ := db.Query("SELECT author, COUNT(*) as cnt FROM reasons WHERE author != '' GROUP BY author ORDER BY cnt DESC LIMIT 5")
	if rows2 != nil {
		defer rows2.Close()
		for rows2.Next() {
			var ac authorCount
			rows2.Scan(&ac.Author, &ac.Count)
			topAuthors = append(topAuthors, ac)
		}
	}

	if jsonOutput {
		topFilesJSON := make([]map[string]interface{}, len(topFiles))
		for i, f := range topFiles {
			topFilesJSON[i] = map[string]interface{}{"file": f.File, "count": f.Count}
		}
		topAuthorsJSON := make([]map[string]interface{}, len(topAuthors))
		for i, a := range topAuthors {
			topAuthorsJSON[i] = map[string]interface{}{"author": a.Author, "count": a.Count}
		}
		b, _ := json.MarshalIndent(map[string]interface{}{
			"total_records": total,
			"files_tracked": files,
			"authors":       authors,
			"manifests":     sources,
			"first_record":  nullStr(first),
			"last_record":   nullStr(last),
			"top_files":     topFilesJSON,
			"top_authors":   topAuthorsJSON,
		}, "", "  ")
		fmt.Println(string(b))
		return
	}

	fmt.Printf("%sblamebot statistics%s\n\n", format.Bold, format.Reset)
	fmt.Printf("  Total records:  %d\n", total)
	fmt.Printf("  Files tracked:  %d\n", files)
	fmt.Printf("  Authors:        %d\n", authors)
	fmt.Printf("  Manifests:      %d\n", sources)
	fmt.Printf("  First record:   %s\n", nullStr(first))
	fmt.Printf("  Last record:    %s\n", nullStr(last))

	if len(topFiles) > 0 {
		fmt.Printf("\n  %sMost edited files:%s\n", format.Bold, format.Reset)
		for _, f := range topFiles {
			fmt.Printf("    %4d  %s\n", f.Count, f.File)
		}
	}

	if len(topAuthors) > 0 {
		fmt.Printf("\n  %sBy author:%s\n", format.Bold, format.Reset)
		for _, a := range topAuthors {
			fmt.Printf("    %4d  %s\n", a.Count, a.Author)
		}
	}
}

func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return "n/a"
}
