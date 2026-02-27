package hook

import (
	"reflect"
	"testing"

	"github.com/jensroland/git-blamebot/internal/record"
)

func TestExtractEdits_Edit(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "Edit",
		"tool_input": map[string]interface{}{
			"file_path":  "/project/src/main.go",
			"old_string": "a\nb\nc",
			"new_string": "a\nX\nc",
		},
		"tool_response": map[string]interface{}{
			"structuredPatch": []interface{}{
				map[string]interface{}{
					"oldStart": float64(5),
					"oldLines": float64(3),
					"newStart": float64(5),
					"newLines": float64(3),
				},
			},
		},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	e := edits[0]
	if e.File != "src/main.go" {
		t.Errorf("file = %q, want %q", e.File, "src/main.go")
	}
	// LCS should identify only line 6 as changed (line 2 of the new string, at offset 5)
	if !reflect.DeepEqual(e.Lines.Lines(), []int{6}) {
		t.Errorf("lines = %v, want [6]", e.Lines.Lines())
	}
	if e.Hunk == nil {
		t.Fatal("hunk should not be nil")
	}
	if e.Hunk.OldStart != 5 || e.Hunk.OldLines != 3 || e.Hunk.NewStart != 5 || e.Hunk.NewLines != 3 {
		t.Errorf("hunk = %+v, want {5,3,5,3}", *e.Hunk)
	}
	if e.ContentHash == "" {
		t.Error("content_hash should not be empty")
	}
}

func TestExtractEdits_Edit_NoPatch(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "Edit",
		"tool_input": map[string]interface{}{
			"file_path":  "/project/src/main.go",
			"old_string": "a",
			"new_string": "b",
		},
		"tool_response": map[string]interface{}{},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if !edits[0].Lines.IsEmpty() {
		t.Errorf("lines should be empty without patch, got %v", edits[0].Lines.Lines())
	}
	if edits[0].Hunk != nil {
		t.Errorf("hunk should be nil without patch, got %+v", edits[0].Hunk)
	}
}

func TestExtractEdits_Write(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "Write",
		"tool_input": map[string]interface{}{
			"file_path": "/project/src/new.go",
			"content":   "line1\nline2\nline3",
		},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	e := edits[0]
	if e.File != "src/new.go" {
		t.Errorf("file = %q, want %q", e.File, "src/new.go")
	}
	// 3 lines -> FromRange(1, 3)
	if !reflect.DeepEqual(e.Lines.Lines(), []int{1, 2, 3}) {
		t.Errorf("lines = %v, want [1 2 3]", e.Lines.Lines())
	}
	if e.Change != "created file (3 lines)" {
		t.Errorf("change = %q, want %q", e.Change, "created file (3 lines)")
	}
	if e.Hunk == nil {
		t.Fatal("hunk should not be nil for Write")
	}
	if e.Hunk.OldLines != 0 || e.Hunk.NewLines != 3 {
		t.Errorf("hunk = %+v, want OldLines=0 NewLines=3", *e.Hunk)
	}
}

func TestExtractEdits_Write_Empty(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "Write",
		"tool_input": map[string]interface{}{
			"file_path": "/project/empty.txt",
		},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if !edits[0].Lines.IsEmpty() {
		t.Errorf("lines should be empty for Write with no content, got %v", edits[0].Lines.Lines())
	}
	if edits[0].Change != "created file" {
		t.Errorf("change = %q, want %q", edits[0].Change, "created file")
	}
	if edits[0].Hunk != nil {
		t.Errorf("hunk should be nil for empty Write, got %+v", edits[0].Hunk)
	}
}

func TestExtractEdits_Write_FileText(t *testing.T) {
	// Some payloads use "file_text" instead of "content"
	data := map[string]interface{}{
		"tool_name": "Write",
		"tool_input": map[string]interface{}{
			"file_path": "/project/alt.txt",
			"file_text": "line1\nline2",
		},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if !reflect.DeepEqual(edits[0].Lines.Lines(), []int{1, 2}) {
		t.Errorf("lines = %v, want [1 2]", edits[0].Lines.Lines())
	}
}

func TestExtractEdits_MultiEdit(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "MultiEdit",
		"tool_input": map[string]interface{}{
			"file_path": "/project/src/main.go",
			"edits": []interface{}{
				map[string]interface{}{
					"old_string": "a\nb",
					"new_string": "a\nX",
				},
				map[string]interface{}{
					"old_string": "c\nd",
					"new_string": "Y\nd",
				},
			},
		},
		"tool_response": map[string]interface{}{
			"structuredPatch": []interface{}{
				map[string]interface{}{
					"oldStart": float64(10),
					"oldLines": float64(2),
					"newStart": float64(10),
					"newLines": float64(2),
				},
				map[string]interface{}{
					"oldStart": float64(30),
					"oldLines": float64(2),
					"newStart": float64(30),
					"newLines": float64(2),
				},
			},
		},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}

	// First edit: line 11 changed (b -> X)
	if !reflect.DeepEqual(edits[0].Lines.Lines(), []int{11}) {
		t.Errorf("edit 0 lines = %v, want [11]", edits[0].Lines.Lines())
	}
	if edits[0].Hunk == nil || edits[0].Hunk.NewStart != 10 {
		t.Errorf("edit 0 hunk unexpected: %+v", edits[0].Hunk)
	}

	// Second edit: line 30 changed (c -> Y)
	if !reflect.DeepEqual(edits[1].Lines.Lines(), []int{30}) {
		t.Errorf("edit 1 lines = %v, want [30]", edits[1].Lines.Lines())
	}
}

func TestExtractEdits_MultiEdit_NoEditsKey(t *testing.T) {
	data := map[string]interface{}{
		"tool_name":  "MultiEdit",
		"tool_input": map[string]interface{}{},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 0 {
		t.Errorf("expected 0 edits for missing edits key, got %d", len(edits))
	}
}

func TestExtractEdits_MultiEdit_ChangesKey(t *testing.T) {
	// Some payloads use "changes" instead of "edits"
	data := map[string]interface{}{
		"tool_name": "MultiEdit",
		"tool_input": map[string]interface{}{
			"file_path": "/project/src/main.go",
			"changes": []interface{}{
				map[string]interface{}{
					"old_string": "a",
					"new_string": "b",
				},
			},
		},
		"tool_response": map[string]interface{}{
			"structuredPatch": []interface{}{
				map[string]interface{}{
					"oldStart": float64(1),
					"oldLines": float64(1),
					"newStart": float64(1),
					"newLines": float64(1),
				},
			},
		},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit via 'changes' key, got %d", len(edits))
	}
}

func TestExtractEdits_MultiEdit_SubEditFilePath(t *testing.T) {
	// Sub-edits can override the parent file_path
	data := map[string]interface{}{
		"tool_name": "MultiEdit",
		"tool_input": map[string]interface{}{
			"file_path": "/project/default.go",
			"edits": []interface{}{
				map[string]interface{}{
					"file_path":  "/project/override.go",
					"old_string": "x",
					"new_string": "y",
				},
				map[string]interface{}{
					"old_string": "a",
					"new_string": "b",
				},
			},
		},
		"tool_response": map[string]interface{}{},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}
	if edits[0].File != "override.go" {
		t.Errorf("edit 0 file = %q, want %q", edits[0].File, "override.go")
	}
	if edits[1].File != "default.go" {
		t.Errorf("edit 1 file = %q, want %q", edits[1].File, "default.go")
	}
}

func TestExtractEdits_UnknownTool(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "Bash",
		"tool_input": map[string]interface{}{
			"command": "echo hello",
		},
	}
	edits := extractEdits(data, "/project")

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Change != "unknown tool: Bash" {
		t.Errorf("change = %q, want %q", edits[0].Change, "unknown tool: Bash")
	}
	// No file_path → falls back to "unknown:Bash"
	if edits[0].File != "unknown:Bash" {
		t.Errorf("file = %q, want %q", edits[0].File, "unknown:Bash")
	}
}

func TestExtractEdits_UnknownTool_WithPath(t *testing.T) {
	data := map[string]interface{}{
		"tool_name": "Bash",
		"tool_input": map[string]interface{}{
			"file_path": "/project/script.sh",
		},
	}
	edits := extractEdits(data, "/project")

	if edits[0].File != "script.sh" {
		t.Errorf("file = %q, want %q", edits[0].File, "script.sh")
	}
}

func TestExtractEdits_PathFallback(t *testing.T) {
	// "path" field instead of "file_path"
	data := map[string]interface{}{
		"tool_name": "Edit",
		"tool_input": map[string]interface{}{
			"path":       "/project/src/alt.go",
			"old_string": "a",
			"new_string": "b",
		},
		"tool_response": map[string]interface{}{},
	}
	edits := extractEdits(data, "/project")

	if edits[0].File != "src/alt.go" {
		t.Errorf("file = %q, want %q", edits[0].File, "src/alt.go")
	}
}

func TestEditChangedLines_Normal(t *testing.T) {
	resp := map[string]interface{}{
		"structuredPatch": []interface{}{
			map[string]interface{}{
				"oldStart": float64(10),
				"oldLines": float64(3),
				"newStart": float64(10),
				"newLines": float64(3),
			},
		},
	}

	lines, hunk := editChangedLines("a\nb\nc", "a\nX\nc", resp)
	if !reflect.DeepEqual(lines.Lines(), []int{11}) {
		t.Errorf("lines = %v, want [11]", lines.Lines())
	}
	if hunk == nil {
		t.Fatal("hunk should not be nil")
	}
	want := record.HunkInfo{OldStart: 10, OldLines: 3, NewStart: 10, NewLines: 3}
	if *hunk != want {
		t.Errorf("hunk = %+v, want %+v", *hunk, want)
	}
}

func TestEditChangedLines_EmptyPatches(t *testing.T) {
	lines, hunk := editChangedLines("a", "b", map[string]interface{}{})
	if !lines.IsEmpty() {
		t.Errorf("lines should be empty, got %v", lines.Lines())
	}
	if hunk != nil {
		t.Errorf("hunk should be nil, got %+v", hunk)
	}
}

func TestEditChangedLines_InvalidPatch(t *testing.T) {
	resp := map[string]interface{}{
		"structuredPatch": []interface{}{"not a map"},
	}
	lines, hunk := editChangedLines("a", "b", resp)
	if !lines.IsEmpty() {
		t.Errorf("lines should be empty, got %v", lines.Lines())
	}
	if hunk != nil {
		t.Errorf("hunk should be nil, got %+v", hunk)
	}
}

func TestEditChangedLines_MissingNewStart(t *testing.T) {
	resp := map[string]interface{}{
		"structuredPatch": []interface{}{
			map[string]interface{}{
				"oldStart": float64(5),
				"oldLines": float64(3),
				// newStart missing
			},
		},
	}
	lines, hunk := editChangedLines("a", "b", resp)
	if !lines.IsEmpty() {
		t.Errorf("lines should be empty, got %v", lines.Lines())
	}
	if hunk != nil {
		t.Errorf("hunk should be nil, got %+v", hunk)
	}
}

func TestEditChangedLines_Insertion(t *testing.T) {
	resp := map[string]interface{}{
		"structuredPatch": []interface{}{
			map[string]interface{}{
				"oldStart": float64(5),
				"oldLines": float64(2),
				"newStart": float64(5),
				"newLines": float64(3),
			},
		},
	}

	// Insert "b" between "a" and "c"
	lines, hunk := editChangedLines("a\nc", "a\nb\nc", resp)
	if !reflect.DeepEqual(lines.Lines(), []int{6}) {
		t.Errorf("lines = %v, want [6]", lines.Lines())
	}
	if hunk.NewLines != 3 || hunk.OldLines != 2 {
		t.Errorf("hunk = %+v, want NewLines=3 OldLines=2", *hunk)
	}
}

func TestEditChangedLines_DefaultHunkFields(t *testing.T) {
	// Patch with only newStart — oldStart/oldLines/newLines should default
	resp := map[string]interface{}{
		"structuredPatch": []interface{}{
			map[string]interface{}{
				"newStart": float64(7),
			},
		},
	}

	_, hunk := editChangedLines("a", "b", resp)
	if hunk == nil {
		t.Fatal("hunk should not be nil")
	}
	// oldStart defaults to newStart, others to 0
	if hunk.OldStart != 7 {
		t.Errorf("hunk.OldStart = %d, want 7 (default to newStart)", hunk.OldStart)
	}
	if hunk.OldLines != 0 || hunk.NewLines != 0 {
		t.Errorf("hunk missing fields should default to 0, got %+v", *hunk)
	}
}
