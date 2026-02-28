package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jensroland/git-blamebot/internal/format"
	"github.com/jensroland/git-blamebot/internal/index"
	"github.com/jensroland/git-blamebot/internal/lineset"
	"github.com/jensroland/git-blamebot/internal/provenance"
	"github.com/jensroland/git-blamebot/internal/record"
)

// ── invariant assertion ─────────────────────────────────────────────────────

// assertHashInvariant checks the core invariant: every non-superseded Edit
// record that reports line numbers MUST have content at those lines whose
// hash matches the stored ContentHash. Any violation is a test failure.
func assertHashInvariant(t *testing.T, root string, rows []*index.ReasonRow, adjMap map[*index.ReasonRow]*format.LineAdjustment) {
	t.Helper()
	for _, row := range rows {
		adj := adjMap[row]
		if adj == nil || adj.CurrentLines.IsEmpty() || adj.Superseded {
			continue
		}
		if row.ContentHash == "" || row.Tool == "Write" {
			continue
		}
		if row.NewLines == nil || *row.NewLines <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, row.File))
		if err != nil {
			t.Errorf("HASH INVARIANT VIOLATION: edit %q at L%s, file unreadable: %v",
				row.Change, adj.CurrentLines.String(), err)
			continue
		}
		fileLines := strings.Split(string(data), "\n")
		start, end := adj.CurrentLines.Min(), adj.CurrentLines.Max()
		if start < 1 || end > len(fileLines) {
			t.Errorf("HASH INVARIANT VIOLATION: edit %q L%d-%d out of range (%d lines in file)",
				row.Change, start, end, len(fileLines))
			continue
		}
		region := strings.Join(fileLines[start-1:end], "\n")
		actual := record.ContentHash(region)
		if actual != row.ContentHash {
			t.Errorf("HASH INVARIANT VIOLATION: edit %q at L%d-%d hash=%s stored=%s\n  content: %q",
				row.Change, start, end, actual, row.ContentHash, region)
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func fileInvariantCheck(t *testing.T, db *sql.DB, root, file string) ([]*index.ReasonRow, map[*index.ReasonRow]*format.LineAdjustment) {
	t.Helper()
	rows := queryFileRows(t, db, file)
	adjMap := blameAdjustFile(root, file, rows)
	assertHashInvariant(t, root, rows, adjMap)
	return rows, adjMap
}

func lineInvariantCheck(t *testing.T, db *sql.DB, file, root, line string) ([]*index.ReasonRow, map[*index.ReasonRow]*format.LineAdjustment) {
	t.Helper()
	matches, adjMap := queryLineBlame(db, file, root, line)
	assertHashInvariant(t, root, matches, adjMap)
	return matches, adjMap
}

// fileWithAI builds a file where lines [start, start+len(ai)-1] contain AI text.
func fileWithAI(total, start int, ai []string) string {
	var b strings.Builder
	end := start + len(ai) - 1
	idx := 0
	for i := 1; i <= total; i++ {
		if i >= start && i <= end {
			b.WriteString(ai[idx])
			idx++
		} else {
			fmt.Fprintf(&b, "line %d", i)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func insertAbove(existing string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "inserted %d\n", i)
	}
	b.WriteString(existing)
	return b.String()
}

func deleteLines(content string, from, to int) string {
	lines := strings.Split(content, "\n")
	var kept []string
	for i, line := range lines {
		lineNum := i + 1
		if lineNum >= from && lineNum <= to {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func replaceLines(content string, from, to int, newTexts []string) string {
	lines := strings.Split(content, "\n")
	var result []string
	for i, line := range lines {
		lineNum := i + 1
		if lineNum == from {
			result = append(result, newTexts...)
		}
		if lineNum >= from && lineNum <= to {
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func insertAfterLine(content string, afterLine, count int) string {
	lines := strings.Split(content, "\n")
	var result []string
	for i, line := range lines {
		result = append(result, line)
		if i+1 == afterLine {
			for j := 1; j <= count; j++ {
				result = append(result, fmt.Sprintf("inserted %d", j))
			}
		}
	}
	return strings.Join(result, "\n")
}

func pendingReplace(t *testing.T, gitDir, id, file string, start int, ai []string, ts string) {
	t.Helper()
	n := len(ai)
	ls := lineset.FromRange(start, start+n-1)
	_ = provenance.WritePending(gitDir, provenance.PendingEdit{
		ID: id, Ts: ts, File: file, Lines: ls,
		Hunk:        &record.HunkInfo{OldStart: start, OldLines: n, NewStart: start, NewLines: n},
		ContentHash: record.ContentHash(strings.Join(ai, "\n")),
		Change:      fmt.Sprintf("AI replaced L%d-%d", start, start+n-1),
		Tool:        "Edit", Author: "claude",
	})
}

func pendingInsert(t *testing.T, gitDir, id, file string, start int, ai []string, ts string) {
	t.Helper()
	n := len(ai)
	ls := lineset.FromRange(start, start+n-1)
	_ = provenance.WritePending(gitDir, provenance.PendingEdit{
		ID: id, Ts: ts, File: file, Lines: ls,
		Hunk:        &record.HunkInfo{OldStart: start, OldLines: 0, NewStart: start, NewLines: n},
		ContentHash: record.ContentHash(strings.Join(ai, "\n")),
		Change:      fmt.Sprintf("AI inserted at L%d", start),
		Tool:        "Edit", Author: "claude",
	})
}

func committedReplace(t *testing.T, root, gitDir, id, sha, file string, start int, ai []string, ts string) {
	t.Helper()
	n := len(ai)
	ls := lineset.FromRange(start, start+n-1)
	_ = provenance.WriteManifest(root, gitDir, provenance.Manifest{
		ID: id, CommitSHA: sha, Author: "claude", Timestamp: ts,
		Edits: []provenance.ManifestEdit{{
			File: file, Lines: ls,
			Hunk:        &record.HunkInfo{OldStart: start, OldLines: n, NewStart: start, NewLines: n},
			ContentHash: record.ContentHash(strings.Join(ai, "\n")),
			Change:      fmt.Sprintf("AI replaced L%d-%d", start, start+n-1),
			Tool:        "Edit",
		}},
	})
}

func findByChange(rows []*index.ReasonRow, adjMap map[*index.ReasonRow]*format.LineAdjustment, change string) (*index.ReasonRow, *format.LineAdjustment) {
	for _, row := range rows {
		if row.Change == change {
			return row, adjMap[row]
		}
	}
	return nil, nil
}

func assertAtLines(t *testing.T, adj *format.LineAdjustment, lines ...int) {
	t.Helper()
	if adj == nil {
		t.Fatal("nil adjustment")
	}
	if adj.Superseded {
		t.Fatal("expected active, got superseded")
	}
	if adj.CurrentLines.IsEmpty() {
		t.Fatal("expected lines, got empty")
	}
	for _, l := range lines {
		if !adj.CurrentLines.Contains(l) {
			t.Errorf("expected line %d in %s", l, adj.CurrentLines.String())
		}
	}
}

func assertIsSuperseded(t *testing.T, adj *format.LineAdjustment) {
	t.Helper()
	if adj == nil {
		t.Fatal("nil adjustment")
	}
	if !adj.Superseded && !adj.CurrentLines.IsEmpty() {
		t.Errorf("expected superseded, got lines=%s", adj.CurrentLines.String())
	}
}

// ── Pending edits: human modifications ──────────────────────────────────────

func TestContentHash_PendingEdits(t *testing.T) {
	ai := []string{"ai 1", "ai 2", "ai 3"}

	t.Run("human inserts above → AI shifts down", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		// Commit ORIGINAL content (no AI yet)
		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI replaces 3-5 (pending, uncommitted)
		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai, "2025-01-01T00:01:00Z")

		// Human inserts 3 lines at top → AI shifts from 3-5 to 6-8
		writeTestFileAt(t, root, "f.go", insertAbove(fileWithAI(10, 3, ai), 3))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertAtLines(t, adj, 6, 7, 8)

		matches, _ := lineInvariantCheck(t, db, "f.go", root, "6")
		if len(matches) == 0 {
			t.Error("query L6 should find shifted AI edit")
		}
		matches3, _ := lineInvariantCheck(t, db, "f.go", root, "3")
		for _, m := range matches3 {
			if m.Change == "AI replaced L3-5" {
				t.Error("query L3 should not find shifted AI edit")
			}
		}
	})

	t.Run("human inserts below → AI stays", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai, "2025-01-01T00:01:00Z")

		// AI at 3-5, human inserts 3 lines after line 7 → AI stays at 3-5
		writeTestFileAt(t, root, "f.go", insertAfterLine(fileWithAI(10, 3, ai), 7, 3))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertAtLines(t, adj, 3, 4, 5)
	})

	t.Run("human replaces all AI lines → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai, "2025-01-01T00:01:00Z")

		// Human overwrites where AI wrote → AI content gone
		writeTestFileAt(t, root, "f.go",
			replaceLines(generateLines(1, 10), 3, 5, []string{"human 1", "human 2", "human 3"}))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)

		matches, _ := lineInvariantCheck(t, db, "f.go", root, "3")
		for _, m := range matches {
			if m.Change == "AI replaced L3-5" {
				t.Error("superseded edit should not match line query")
			}
		}
	})

	t.Run("human replaces part of AI lines → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai, "2025-01-01T00:01:00Z")

		// AI wrote at 3-5, human modifies only line 4 → block hash broken
		writeTestFileAt(t, root, "f.go",
			replaceLines(fileWithAI(10, 3, ai), 4, 4, []string{"human override"}))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)
	})

	t.Run("human deletes AI lines → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai, "2025-01-01T00:01:00Z")

		// AI wrote at 3-5, human deletes those lines entirely
		writeTestFileAt(t, root, "f.go", deleteLines(fileWithAI(10, 3, ai), 3, 5))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)
	})

	t.Run("human deletes above → AI shifts up", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI replaces 5-7 (pending)
		pendingReplace(t, paths.GitDir, "p1", "f.go", 5, ai, "2025-01-01T00:01:00Z")

		// Human deletes lines 1-2 → AI shifts from 5-7 to 3-5
		writeTestFileAt(t, root, "f.go", deleteLines(fileWithAI(10, 5, ai), 1, 2))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L5-7")
		assertAtLines(t, adj, 3, 4, 5)
	})

	t.Run("human splits AI block by inserting in the middle → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI replaces 3-5 (pending)
		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai, "2025-01-01T00:01:00Z")

		// AI content at 3-5, then human inserts a line in the middle (after line 3)
		// → AI content split: line 3="ai 1", line 4="inserted", line 5="ai 2", line 6="ai 3"
		writeTestFileAt(t, root, "f.go", insertAfterLine(fileWithAI(10, 3, ai), 3, 1))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)

		// Line queries should NOT find the split edit
		matches3, _ := lineInvariantCheck(t, db, "f.go", root, "3")
		for _, m := range matches3 {
			if m.Change == "AI replaced L3-5" {
				t.Error("query L3 should not find split AI edit")
			}
		}
		matches5, _ := lineInvariantCheck(t, db, "f.go", root, "5")
		for _, m := range matches5 {
			if m.Change == "AI replaced L3-5" {
				t.Error("query L5 should not find split AI edit")
			}
		}
	})

	t.Run("AI inserts new lines then human shifts them", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI inserts 3 new lines at position 5 (OldLines=0) → file grows to 13
		aiFile := fileWithAI(13, 5, ai)
		writeTestFileAt(t, root, "f.go", aiFile)
		pendingInsert(t, paths.GitDir, "p1", "f.go", 5, ai, "2025-01-01T00:01:00Z")

		// Human inserts 2 lines at top → AI shifts from 5-7 to 7-9
		writeTestFileAt(t, root, "f.go", insertAbove(aiFile, 2))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI inserted at L5")
		assertAtLines(t, adj, 7, 8, 9)
	})
}

// ── Committed edits: human modifications in new commits ─────────────────────

func TestContentHash_CommittedEdits(t *testing.T) {
	ai := []string{"ai 1", "ai 2", "ai 3"}

	t.Run("human overwrites AI in new commit → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI replaces 3-5, commit
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit")
		aiSHA := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", aiSHA, "f.go", 3, ai, "2025-01-01T00:00:00Z")

		// Human replaces same lines, new commit
		writeTestFileAt(t, root, "f.go",
			replaceLines(fileWithAI(10, 3, ai), 3, 5, []string{"human 1", "human 2", "human 3"}))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "human edit")

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)
	})

	t.Run("human inserts above in new commit → AI shifts", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI replaces 3-5, commit
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit")
		aiSHA := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", aiSHA, "f.go", 3, ai, "2025-01-01T00:00:00Z")

		// Human inserts 3 lines at top, new commit → AI at 6-8
		writeTestFileAt(t, root, "f.go", insertAbove(fileWithAI(10, 3, ai), 3))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "human insert")

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertAtLines(t, adj, 6, 7, 8)

		matches, _ := lineInvariantCheck(t, db, "f.go", root, "6")
		if len(matches) == 0 {
			t.Error("query L6 should find committed AI edit shifted to 6-8")
		}
	})

	t.Run("human partially modifies AI in new commit → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit")
		aiSHA := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", aiSHA, "f.go", 3, ai, "2025-01-01T00:00:00Z")

		// Human changes only line 4, new commit → block hash broken
		writeTestFileAt(t, root, "f.go",
			replaceLines(fileWithAI(10, 3, ai), 4, 4, []string{"human mid"}))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "human partial edit")

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)
	})

	t.Run("human splits committed AI block → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI replaces 3-5, commit
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit")
		aiSHA := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", aiSHA, "f.go", 3, ai, "2025-01-01T00:00:00Z")

		// Human splits AI block by inserting in the middle, new commit
		writeTestFileAt(t, root, "f.go", insertAfterLine(fileWithAI(10, 3, ai), 3, 1))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "human split")

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)
	})

	t.Run("uncommitted change over committed AI → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit")
		aiSHA := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", aiSHA, "f.go", 3, ai, "2025-01-01T00:00:00Z")

		// User modifies line 4 but does NOT commit
		writeTestFileAt(t, root, "f.go",
			replaceLines(fileWithAI(10, 3, ai), 4, 4, []string{"wip change"}))

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)
	})
}

// ── Multiple AI edits ───────────────────────────────────────────────────────

func TestContentHash_MultipleEdits(t *testing.T) {
	ai1 := []string{"ai 1", "ai 2", "ai 3"}
	ai2 := []string{"ai A", "ai B", "ai C"}

	t.Run("two pending edits, human insert between → both survive", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 15))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai1, "2025-01-01T00:01:00Z")
		pendingReplace(t, paths.GitDir, "p2", "f.go", 10, ai2, "2025-01-01T00:02:00Z")

		// Both AI edits applied, then human inserts 2 lines after line 7
		content := fileWithAI(15, 3, ai1)
		content = replaceLines(content, 10, 12, ai2)
		content = insertAfterLine(content, 7, 2)
		writeTestFileAt(t, root, "f.go", content)

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")

		// AI1 stays at 3-5 (above insert point)
		_, adj1 := findByChange(rows, adjMap, "AI replaced L3-5")
		assertAtLines(t, adj1, 3, 4, 5)

		// AI2 shifts from 10-12 to 12-14 (below insert point, +2 offset)
		_, adj2 := findByChange(rows, adjMap, "AI replaced L10-12")
		assertAtLines(t, adj2, 12, 13, 14)
	})

	t.Run("second committed AI edit supersedes first on same lines", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// First AI edit at 3-5
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai1))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit 1")
		sha1 := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", sha1, "f.go", 3, ai1, "2025-01-01T00:00:00Z")

		// Second AI edit overwrites 3-5 with new content
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai2))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit 2")
		sha2 := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m2", sha2, "f.go", 3, ai2, "2025-01-01T00:01:00Z")

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")

		for _, row := range rows {
			adj := adjMap[row]
			if row.CommitSHA == sha1 {
				assertIsSuperseded(t, adj)
			}
			if row.CommitSHA == sha2 {
				assertAtLines(t, adj, 3, 4, 5)
			}
		}
	})

	t.Run("human deletes one of two adjacent AI edits → deleted superseded, other survives", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai1, "2025-01-01T00:01:00Z")
		pendingReplace(t, paths.GitDir, "p2", "f.go", 6, ai2, "2025-01-01T00:02:00Z")

		// Both AI edits applied, then human deletes AI2 (lines 6-8)
		content := fileWithAI(10, 3, ai1)
		content = replaceLines(content, 6, 8, ai2)
		content = deleteLines(content, 6, 8)
		writeTestFileAt(t, root, "f.go", content)

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")

		_, adj1 := findByChange(rows, adjMap, "AI replaced L3-5")
		assertAtLines(t, adj1, 3, 4, 5)

		_, adj2 := findByChange(rows, adjMap, "AI replaced L6-8")
		assertIsSuperseded(t, adj2)
	})

	t.Run("human edit partially overlaps AI edit → superseded", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai1, "2025-01-01T00:01:00Z")

		// AI at 3-5, human replaces 4-6 (overlapping: corrupts AI block)
		content := fileWithAI(10, 3, ai1)
		content = replaceLines(content, 4, 6, []string{"overlap 1", "overlap 2", "overlap 3"})
		writeTestFileAt(t, root, "f.go", content)

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertIsSuperseded(t, adj)
	})
}

// ── Sequences: multi-step scenarios ─────────────────────────────────────────

func TestContentHash_Sequences(t *testing.T) {
	ai := []string{"ai 1", "ai 2", "ai 3"}
	ai2 := []string{"ai X", "ai Y", "ai Z"}

	t.Run("pending → commit with manual changes → query both phases", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		// Phase 1: pending AI edit at 3-5
		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		pendingReplace(t, paths.GitDir, "p1", "f.go", 3, ai, "2025-01-01T00:01:00Z")

		db := rebuildTestIndex(t, paths)
		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertAtLines(t, adj, 3, 4, 5)
		db.Close()

		// Phase 2: commit AI content + manual changes at 6-8
		mixed := replaceLines(fileWithAI(10, 3, ai), 6, 8,
			[]string{"manual 6", "manual 7", "manual 8"})
		writeTestFileAt(t, root, "f.go", mixed)
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "mixed commit")
		sha := gitHeadSHA(t, root)

		_ = provenance.ClearPending(paths.GitDir)
		_ = os.Remove(paths.IndexDB)
		committedReplace(t, root, paths.GitDir, "m1", sha, "f.go", 3, ai, "2025-01-01T00:01:00Z")

		db2 := rebuildTestIndex(t, paths)
		defer db2.Close()

		rows2, adjMap2 := fileInvariantCheck(t, db2, root, "f.go")
		_, adj2 := findByChange(rows2, adjMap2, "AI replaced L3-5")
		assertAtLines(t, adj2, 3, 4, 5)
	})

	t.Run("committed AI edit survives multiple human insert commits", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// AI edit at 3-5, commit
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit")
		aiSHA := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", aiSHA, "f.go", 3, ai, "2025-01-01T00:00:00Z")

		// Human commit 1: insert 2 lines at top → AI at 5-7
		current := insertAbove(fileWithAI(10, 3, ai), 2)
		writeTestFileAt(t, root, "f.go", current)
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "human insert 1")

		// Human commit 2: insert 3 more at top → AI at 8-10
		current = insertAbove(current, 3)
		writeTestFileAt(t, root, "f.go", current)
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "human insert 2")

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")
		_, adj := findByChange(rows, adjMap, "AI replaced L3-5")
		assertAtLines(t, adj, 8, 9, 10)

		matches, _ := lineInvariantCheck(t, db, "f.go", root, "8")
		if len(matches) == 0 {
			t.Error("query L8 should find AI edit after two insert commits")
		}
	})

	t.Run("AI edit → human overwrites → new AI edit on same lines", func(t *testing.T) {
		root := initQueryTestRepo(t)
		paths := makeTestPaths(root)

		writeTestFileAt(t, root, "f.go", generateLines(1, 10))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "init")

		// First AI edit at 3-5, commit
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit 1")
		sha1 := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m1", sha1, "f.go", 3, ai, "2025-01-01T00:00:00Z")

		// Human overwrites lines 3-5, commit
		writeTestFileAt(t, root, "f.go",
			replaceLines(fileWithAI(10, 3, ai), 3, 5, []string{"human 1", "human 2", "human 3"}))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "human edit")

		// Second AI edit at 3-5 with new content, commit
		writeTestFileAt(t, root, "f.go", fileWithAI(10, 3, ai2))
		runGitCmd(t, root, "add", "f.go")
		runGitCmd(t, root, "commit", "-m", "AI edit 2")
		sha3 := gitHeadSHA(t, root)
		committedReplace(t, root, paths.GitDir, "m2", sha3, "f.go", 3, ai2, "2025-01-01T00:02:00Z")

		db := rebuildTestIndex(t, paths)
		defer db.Close()

		rows, adjMap := fileInvariantCheck(t, db, root, "f.go")

		for _, row := range rows {
			adj := adjMap[row]
			if row.CommitSHA == sha1 {
				assertIsSuperseded(t, adj)
			}
			if row.CommitSHA == sha3 {
				assertAtLines(t, adj, 3, 4, 5)
			}
		}
	})
}
