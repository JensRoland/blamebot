package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/git"
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/linemap"
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

	if line != "" {
		matches, adjustments := queryLineBlame(db, rel, projectRoot, line)
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

	// No line filter: show all records with blame-derived line positions
	rows, err := queryRows(db,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts DESC",
		rel, "%/"+rel)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if len(rows) == 0 {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Printf("No reasons found for %s\n", rel)
		}
		return
	}

	adjMap := blameAdjustFile(projectRoot, rel, rows)

	if jsonOutput {
		printAdjustedJSON(rows, adjMap, projectRoot)
		return
	}

	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose, adjMap[row]))
		fmt.Println()
	}
}

// queryLineBlame uses git blame to find which records own the queried lines.
// Returns matches sorted newest first, plus a map of adjustments for all records.
func queryLineBlame(db *sql.DB, rel, projectRoot, line string) ([]*index.ReasonRow, map[*index.ReasonRow]*format.LineAdjustment) {
	// Parse the query line (supports "42", "10:20", "10,20")
	queryStart, queryEnd := parseLineRange(line)

	// Run git blame on the queried range
	blameEntries, blameErr := git.BlameRange(projectRoot, rel, queryStart, queryEnd)

	// Collect unique commit SHAs from blame
	commitSHAs := make(map[string]bool)
	hasUncommitted := false
	if blameErr == nil {
		for _, entry := range blameEntries {
			if entry.IsUncommitted() {
				hasUncommitted = true
			} else {
				commitSHAs[entry.SHA] = true
			}
		}
	}

	adjMap := make(map[*index.ReasonRow]*format.LineAdjustment)
	var matches []*index.ReasonRow

	// For committed SHAs: look up records by commit_sha
	for sha := range commitSHAs {
		rows, err := queryRows(db,
			"SELECT * FROM reasons WHERE commit_sha = ? AND (file = ? OR file LIKE ?) ORDER BY ts DESC",
			sha, rel, "%/"+rel)
		if err != nil {
			continue
		}
		shaLines := blameLinesForSHA(blameEntries, sha)
		if len(rows) == 1 {
			adjMap[rows[0]] = &format.LineAdjustment{CurrentLines: shaLines}
			matches = append(matches, rows[0])
		} else {
			// Multiple records share this SHA — disambiguate by proximity
			regions := groupContiguous(shaLines.Lines())
			for _, row := range rows {
				mid := recordMidpoint(row)
				best := nearestRegion(regions, mid)
				adjMap[row] = &format.LineAdjustment{CurrentLines: lineset.New(best...)}
				matches = append(matches, row)
			}
		}
	}

	// For uncommitted lines or when blame failed: fall back to forward simulation
	if hasUncommitted || blameErr != nil {
		simMatches, simAdj := queryAdjustedLineFallback(db, rel, queryStart, queryEnd)
		for _, row := range simMatches {
			if _, already := adjMap[row]; !already {
				adjMap[row] = simAdj[row]
				matches = append(matches, row)
			}
		}
	}

	// Sort matches newest first by timestamp
	sortNewestFirst(matches)

	return matches, adjMap
}

// blameAdjustFile uses git blame to compute current line positions for all records of a file.
func blameAdjustFile(projectRoot, rel string, rows []*index.ReasonRow) map[*index.ReasonRow]*format.LineAdjustment {
	adjMap := make(map[*index.ReasonRow]*format.LineAdjustment)

	// Run git blame on the whole file
	blameEntries, err := git.BlameFile(projectRoot, rel)
	if err != nil {
		// Blame failed (untracked file, not a git repo, etc.) — fall back
		return fallbackAdjustments(rows)
	}

	// Build reverse map: commit_sha → set of current line numbers
	shaToLines := make(map[string][]int)
	for lineNum, entry := range blameEntries {
		if !entry.IsUncommitted() {
			shaToLines[entry.SHA] = append(shaToLines[entry.SHA], lineNum)
		}
	}

	// Group records by commit_sha
	shaRecords := make(map[string][]*index.ReasonRow)
	var uncommittedRows []*index.ReasonRow
	for _, row := range rows {
		if row.CommitSHA != "" {
			shaRecords[row.CommitSHA] = append(shaRecords[row.CommitSHA], row)
		} else {
			uncommittedRows = append(uncommittedRows, row)
		}
	}

	// Process committed records
	for sha, records := range shaRecords {
		lines, ok := shaToLines[sha]
		if !ok || len(lines) == 0 {
			// Commit SHA not in current file — content was superseded
			for _, row := range records {
				adjMap[row] = &format.LineAdjustment{Superseded: true}
			}
			continue
		}

		if len(records) == 1 {
			// Single record for this SHA — assign all lines
			adjMap[records[0]] = &format.LineAdjustment{
				CurrentLines: lineset.New(lines...),
			}
			continue
		}

		// Multiple records share this SHA — disambiguate by proximity
		// to each record's original line range
		regions := groupContiguous(lines)
		for _, row := range records {
			mid := recordMidpoint(row)
			best := nearestRegion(regions, mid)
			adjMap[row] = &format.LineAdjustment{
				CurrentLines: lineset.New(best...),
			}
		}
	}

	// Process uncommitted records with forward simulation fallback
	if len(uncommittedRows) > 0 {
		allSorted := make([]*index.ReasonRow, len(rows))
		copy(allSorted, rows)
		sortOldestFirst(allSorted)
		adjusted := linemap.AdjustLinePositions(allSorted)

		uncommittedSet := make(map[*index.ReasonRow]bool)
		for _, row := range uncommittedRows {
			uncommittedSet[row] = true
		}
		for _, adj := range adjusted {
			if uncommittedSet[adj.ReasonRow] {
				adjMap[adj.ReasonRow] = &format.LineAdjustment{
					CurrentLines: adj.CurrentLines,
					Superseded:   adj.Superseded,
				}
			}
		}
	}

	return adjMap
}

// groupContiguous groups sorted line numbers into contiguous regions.
func groupContiguous(lines []int) [][]int {
	if len(lines) == 0 {
		return nil
	}
	sort.Ints(lines)
	var regions [][]int
	region := []int{lines[0]}
	for i := 1; i < len(lines); i++ {
		if lines[i] == lines[i-1]+1 {
			region = append(region, lines[i])
		} else {
			regions = append(regions, region)
			region = []int{lines[i]}
		}
	}
	regions = append(regions, region)
	return regions
}

// recordMidpoint returns the midpoint of a record's original line range.
func recordMidpoint(row *index.ReasonRow) float64 {
	if row.LineStart == nil {
		return 0
	}
	start := float64(*row.LineStart)
	if row.LineEnd != nil {
		return (start + float64(*row.LineEnd)) / 2
	}
	return start
}

// nearestRegion returns the region closest to the given midpoint.
func nearestRegion(regions [][]int, mid float64) []int {
	if len(regions) == 0 {
		return nil
	}
	best := regions[0]
	bestDist := math.Abs(mid - regionCenter(best))
	for _, r := range regions[1:] {
		d := math.Abs(mid - regionCenter(r))
		if d < bestDist {
			best = r
			bestDist = d
		}
	}
	return best
}

func regionCenter(region []int) float64 {
	if len(region) == 0 {
		return 0
	}
	return float64(region[0]+region[len(region)-1]) / 2
}

// fallbackAdjustments uses forward simulation for line adjustment when blame is unavailable.
func fallbackAdjustments(rows []*index.ReasonRow) map[*index.ReasonRow]*format.LineAdjustment {
	// Need oldest-first for forward simulation
	sorted := make([]*index.ReasonRow, len(rows))
	copy(sorted, rows)
	sortOldestFirst(sorted)

	adjusted := linemap.AdjustLinePositions(sorted)
	adjMap := make(map[*index.ReasonRow]*format.LineAdjustment)
	for _, adj := range adjusted {
		adjMap[adj.ReasonRow] = &format.LineAdjustment{
			CurrentLines: adj.CurrentLines,
			Superseded:   adj.Superseded,
		}
	}
	return adjMap
}

// queryAdjustedLineFallback uses forward simulation for uncommitted content.
func queryAdjustedLineFallback(db *sql.DB, rel string, queryStart, queryEnd int) ([]*index.ReasonRow, map[*index.ReasonRow]*format.LineAdjustment) {
	allRows, err := queryRows(db,
		"SELECT * FROM reasons WHERE (file = ? OR file LIKE ?) ORDER BY ts ASC",
		rel, "%/"+rel)
	if err != nil || len(allRows) == 0 {
		return nil, nil
	}

	adjusted := linemap.AdjustLinePositions(allRows)

	var matches []*index.ReasonRow
	adjMap := make(map[*index.ReasonRow]*format.LineAdjustment)

	for _, adj := range adjusted {
		// Only include records without commit_sha (uncommitted)
		if adj.CommitSHA != "" {
			continue
		}

		la := &format.LineAdjustment{
			CurrentLines: adj.CurrentLines,
			Superseded:   adj.Superseded,
		}
		adjMap[adj.ReasonRow] = la

		if adj.Superseded {
			continue
		}

		if !adj.CurrentLines.IsEmpty() {
			if adj.CurrentLines.Overlaps(queryStart, queryEnd) {
				matches = append(matches, adj.ReasonRow)
			}
		} else if adj.LineStart != nil {
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

	return matches, adjMap
}

// blameLinesForSHA extracts the set of line numbers attributed to a given SHA.
func blameLinesForSHA(entries map[int]git.BlameEntry, sha string) lineset.LineSet {
	var lines []int
	for lineNum, entry := range entries {
		if entry.SHA == sha {
			lines = append(lines, lineNum)
		}
	}
	return lineset.New(lines...)
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

	if len(rows) == 0 {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Printf("No reasons matching '%s'\n", pattern)
		}
		return
	}

	adjMap := computeAdjustments(db, rows, projectRoot)

	if jsonOutput {
		printAdjustedJSON(rows, adjMap, projectRoot)
		return
	}

	fmt.Printf("Found %d reason(s) matching '%s':\n\n", len(rows), pattern)
	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose, adjMap[row]))
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

	if len(rows) == 0 {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Printf("No reasons found since %s\n", dateStr)
		}
		return
	}

	adjMap := computeAdjustments(db, rows, projectRoot)

	if jsonOutput {
		printAdjustedJSON(rows, adjMap, projectRoot)
		return
	}

	fmt.Printf("Found %d reason(s) since %s:\n\n", len(rows), dateStr)
	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose, adjMap[row]))
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

	if len(rows) == 0 {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Printf("No reasons found for author '%s'\n", author)
		}
		return
	}

	adjMap := computeAdjustments(db, rows, projectRoot)

	if jsonOutput {
		printAdjustedJSON(rows, adjMap, projectRoot)
		return
	}

	fmt.Printf("Found %d reason(s) by '%s':\n\n", len(rows), author)
	for _, row := range rows {
		fmt.Println(format.FormatReason(row, projectRoot, verbose, adjMap[row]))
		fmt.Println()
	}
}

// computeAdjustments uses git blame per file to derive current line positions.
// Falls back to forward simulation if blame is unavailable.
func computeAdjustments(_ *sql.DB, rows []*index.ReasonRow, projectRoot string) map[*index.ReasonRow]*format.LineAdjustment {
	// Group rows by file
	fileRows := make(map[string][]*index.ReasonRow)
	for _, row := range rows {
		fileRows[row.File] = append(fileRows[row.File], row)
	}

	adjMap := make(map[*index.ReasonRow]*format.LineAdjustment)

	for file, fRows := range fileRows {
		fileAdj := blameAdjustFile(projectRoot, file, fRows)
		for row, la := range fileAdj {
			adjMap[row] = la
		}
	}

	return adjMap
}

// sortNewestFirst sorts rows by timestamp descending.
func sortNewestFirst(rows []*index.ReasonRow) {
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
}

// sortOldestFirst sorts rows by timestamp ascending (reverses a desc-sorted slice).
func sortOldestFirst(rows []*index.ReasonRow) {
	// Simple sort by Ts
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[i].Ts > rows[j].Ts {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
}

// parseLineRange parses a line spec like "42", "10:20", or "10,20" into start and end.
func parseLineRange(line string) (int, int) {
	sep := ""
	if strings.Contains(line, ":") {
		sep = ":"
	} else if strings.Contains(line, ",") {
		sep = ","
	}
	if sep != "" {
		parts := strings.SplitN(line, sep, 2)
		start, _ := strconv.Atoi(parts[0])
		end, _ := strconv.Atoi(parts[1])
		return start, end
	}
	n, _ := strconv.Atoi(line)
	return n, n
}

func printAdjustedJSON(rows []*index.ReasonRow, adjMap map[*index.ReasonRow]*format.LineAdjustment, projectRoot string) {
	var items []map[string]interface{}
	for _, row := range rows {
		items = append(items, format.RowToJSON(row, projectRoot, adjMap[row]))
	}
	b, _ := json.MarshalIndent(items, "", "  ")
	fmt.Println(string(b))
}
