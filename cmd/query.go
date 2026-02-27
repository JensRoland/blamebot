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
	"github.com/jensroland/git-blamebot/internal/linemap"
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

	if line != "" {
		// Two-pass adjusted line query
		matches, adjustments := queryAdjustedLine(db, rel, line)
		if len(matches) == 0 {
			fmt.Printf("No reasons found for %s at line %s\n", rel, line)
			return
		}

		if jsonOutput {
			printAdjustedJSON(matches, adjustments, projectRoot)
			return
		}

		// Show only the most recent match
		adj := adjustments[matches[0]]
		fmt.Println(format.FormatReason(matches[0], projectRoot, verbose, adj))
		fmt.Println()
		return
	}

	// No line filter: show all records
	rows, err := queryRows(db,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts DESC",
		rel, "%/"+rel)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if jsonOutput {
		printJSON(rows, projectRoot)
		return
	}

	if len(rows) == 0 {
		fmt.Printf("No reasons found for %s\n", rel)
		return
	}

	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose, nil))
		fmt.Println()
	}
}

// queryAdjustedLine fetches all records for a file, computes adjusted line
// positions, and returns matches for the queried line(s), sorted newest first.
// Also returns a map from row pointer to its LineAdjustment.
func queryAdjustedLine(db *sql.DB, rel, line string) ([]*index.ReasonRow, map[*index.ReasonRow]*format.LineAdjustment) {
	// Fetch all records for this file, ordered oldest first for the simulation
	allRows, err := queryRows(db,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts ASC",
		rel, "%/"+rel)
	if err != nil || len(allRows) == 0 {
		return nil, nil
	}

	adjusted := linemap.AdjustLinePositions(allRows)

	// Parse the query line
	var queryStart, queryEnd int
	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)
		queryStart, _ = strconv.Atoi(parts[0])
		queryEnd, _ = strconv.Atoi(parts[1])
	} else {
		queryStart, _ = strconv.Atoi(line)
		queryEnd = queryStart
	}

	var matches []*index.ReasonRow
	adjMap := make(map[*index.ReasonRow]*format.LineAdjustment)

	for _, adj := range adjusted {
		la := &format.LineAdjustment{
			CurrentLines: adj.CurrentLines,
			Superseded:   adj.Superseded,
		}
		adjMap[adj.ReasonRow] = la

		if adj.Superseded {
			continue
		}

		// Match against adjusted positions using precise LineSet
		if !adj.CurrentLines.IsEmpty() {
			if adj.CurrentLines.Overlaps(queryStart, queryEnd) {
				matches = append(matches, adj.ReasonRow)
			}
		} else if adj.LineStart != nil {
			// Fallback to stored lines for records without any line data
			ls := *adj.LineStart
			le := ls
			if adj.LineEnd != nil {
				le = *adj.LineEnd
			}
			if ls <= queryEnd && le >= queryStart {
				matches = append(matches, adj.ReasonRow)
			}
		}
	}

	// Sort matches newest first
	for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
		matches[i], matches[j] = matches[j], matches[i]
	}

	return matches, adjMap
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
		fmt.Println(format.FormatReason(row, projectRoot, verbose, nil))
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
		fmt.Println(format.FormatReason(row, projectRoot, verbose, nil))
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
		fmt.Println(format.FormatReason(row, projectRoot, verbose, nil))
		fmt.Println()
	}
}

func printJSON(rows []*index.ReasonRow, projectRoot string) {
	var items []map[string]interface{}
	for _, row := range rows {
		items = append(items, format.RowToJSON(row, projectRoot, nil))
	}
	b, _ := json.MarshalIndent(items, "", "  ")
	fmt.Println(string(b))
}

func printAdjustedJSON(rows []*index.ReasonRow, adjMap map[*index.ReasonRow]*format.LineAdjustment, projectRoot string) {
	var items []map[string]interface{}
	for _, row := range rows {
		items = append(items, format.RowToJSON(row, projectRoot, adjMap[row]))
	}
	b, _ := json.MarshalIndent(items, "", "  ")
	fmt.Println(string(b))
}
