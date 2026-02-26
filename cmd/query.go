package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/lineset"
)

func relativePath(filePath, projectRoot string) string {
	if filepath.IsAbs(filePath) {
		rel, err := filepath.Rel(projectRoot, filePath)
		if err != nil {
			return filePath
		}
		return filepath.ToSlash(rel)
	}
	return filePath
}

func cmdFile(db *sql.DB, filePath, projectRoot, line string, verbose, jsonOutput bool) {
	rel := relativePath(filePath, projectRoot)

	conditions := []string{"(file = ? OR file LIKE ?)"}
	params := []interface{}{rel, "%/" + rel}

	if line != "" {
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			start, _ := strconv.Atoi(parts[0])
			end, _ := strconv.Atoi(parts[1])
			conditions = append(conditions, "(line_start <= ? AND (line_end >= ? OR line_end IS NULL))")
			params = append(params, end, start)
		} else {
			lineNum, _ := strconv.Atoi(line)
			conditions = append(conditions, "(line_start <= ? AND (line_end >= ? OR line_start = ?))")
			params = append(params, lineNum, lineNum, lineNum)
		}
	}

	where := strings.Join(conditions, " AND ")

	rows, err := queryRows(db,
		fmt.Sprintf("SELECT * FROM reasons WHERE %s ORDER BY ts DESC", where),
		params...)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Post-filter by precise changed lines when available
	if line != "" {
		rows = filterByPreciseLines(rows, line)
	}

	// When querying a specific line, only show the most recent match
	if line != "" && len(rows) > 1 {
		rows = rows[:1]
	}

	if jsonOutput {
		printJSON(rows, projectRoot)
		return
	}

	if len(rows) == 0 {
		msg := fmt.Sprintf("No reasons found for %s", rel)
		if line != "" {
			msg += " at line " + line
		}
		fmt.Println(msg)
		return
	}

	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose))
		fmt.Println()
	}
}

// filterByPreciseLines refines SQL bounding-range results using the precise
// changed_lines field when available.
func filterByPreciseLines(rows []*index.ReasonRow, line string) []*index.ReasonRow {
	var filtered []*index.ReasonRow
	for _, row := range rows {
		if row.ChangedLines == "" {
			// Legacy record without precise lines — keep it
			filtered = append(filtered, row)
			continue
		}
		ls, err := lineset.FromString(row.ChangedLines)
		if err != nil {
			filtered = append(filtered, row)
			continue
		}
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			start, _ := strconv.Atoi(parts[0])
			end, _ := strconv.Atoi(parts[1])
			if ls.Overlaps(start, end) {
				filtered = append(filtered, row)
			}
		} else {
			lineNum, _ := strconv.Atoi(line)
			if ls.Contains(lineNum) {
				filtered = append(filtered, row)
			}
		}
	}
	return filtered
}

func cmdGrep(db *sql.DB, pattern, projectRoot string, verbose, jsonOutput bool) {
	p := "%" + pattern + "%"
	rows, err := queryRows(db,
		"SELECT * FROM reasons WHERE prompt LIKE ? OR reason LIKE ? OR change LIKE ? ORDER BY ts DESC",
		p, p, p)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if jsonOutput {
		printJSON(rows, projectRoot)
		return
	}

	if len(rows) == 0 {
		fmt.Printf("No reasons matching '%s'\n", pattern)
		return
	}

	fmt.Printf("Found %d reason(s) matching '%s':\n\n", len(rows), pattern)
	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose))
		fmt.Println()
	}
}

func cmdSince(db *sql.DB, dateStr, filePath, projectRoot string, verbose, jsonOutput bool) {
	conditions := []string{"ts >= ?"}
	params := []interface{}{dateStr}

	if filePath != "" {
		rel := relativePath(filePath, projectRoot)
		conditions = append(conditions, "(file = ? OR file LIKE ?)")
		params = append(params, rel, "%/"+rel)
	}

	where := strings.Join(conditions, " AND ")
	rows, err := queryRows(db,
		fmt.Sprintf("SELECT * FROM reasons WHERE %s ORDER BY ts DESC", where),
		params...)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if jsonOutput {
		printJSON(rows, projectRoot)
		return
	}

	if len(rows) == 0 {
		fmt.Printf("No reasons found since %s\n", dateStr)
		return
	}

	fmt.Printf("Found %d reason(s) since %s:\n\n", len(rows), dateStr)
	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose))
		fmt.Println()
	}
}

func cmdAuthor(db *sql.DB, author, projectRoot string, verbose, jsonOutput bool) {
	rows, err := queryRows(db,
		"SELECT * FROM reasons WHERE author LIKE ? ORDER BY ts DESC",
		"%"+author+"%")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if jsonOutput {
		printJSON(rows, projectRoot)
		return
	}

	if len(rows) == 0 {
		fmt.Printf("No reasons found for author '%s'\n", author)
		return
	}

	fmt.Printf("Found %d reason(s) by '%s':\n\n", len(rows), author)
	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose))
		fmt.Println()
	}
}

func printJSON(rows []*index.ReasonRow, projectRoot string) {
	var items []map[string]interface{}
	for _, row := range rows {
		items = append(items, format.RowToJSON(row, projectRoot))
	}
	b, _ := json.MarshalIndent(items, "", "  ")
	fmt.Println(string(b))
}
