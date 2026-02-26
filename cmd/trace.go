package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/transcript"
)

func cmdTrace(db *sql.DB, recordID, projectRoot string, jsonOutput bool) {
	rows, err := queryRows(db, "SELECT * FROM reasons WHERE id = ?", recordID)
	if err != nil || len(rows) == 0 {
		// Try matching by tool_use_id fragment
		rows, err = queryRows(db,
			"SELECT * FROM reasons WHERE trace LIKE ?",
			"%"+recordID+"%")
	}

	if len(rows) == 0 {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Printf("No record found for '%s'\n", recordID)
		}
		return
	}

	row := rows[0]

	if jsonOutput {
		d := format.RowToJSON(row, projectRoot)
		if row.Trace != "" {
			ctx := transcript.ReadTraceContext(row.Trace, projectRoot)
			if ctx != "" {
				d["trace_context"] = ctx
			}
		}
		b, _ := json.MarshalIndent([]map[string]interface{}{d}, "", "  ")
		fmt.Println(string(b))
		return
	}

	fmt.Println(format.FormatReason(row, projectRoot, true))
	fmt.Println()

	if row.Trace == "" {
		fmt.Printf("%sNo trace reference stored for this record.%s\n", format.Yellow, format.Reset)
		return
	}

	fmt.Printf("%sReasoning trace:%s\n\n", format.Bold, format.Reset)
	ctx := transcript.ReadTraceContext(row.Trace, projectRoot)
	if ctx != "" {
		fmt.Println(ctx)
	} else {
		fmt.Printf("%sTranscript not accessible (may be on another machine).%s\n", format.Dim, format.Reset)
		fmt.Printf("%sTrace ref: %s%s\n", format.Dim, row.Trace, format.Reset)
	}
}
