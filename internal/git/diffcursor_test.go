package git

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
)

const twoHunks = `diff --git a/x.go b/x.go
index 1111111..2222222 100644
--- a/x.go
+++ b/x.go
@@ -10,3 +10,4 @@ func f() {
 ctx10
-old11
+new11
+new12
 ctx13
@@ -30,2 +31,2 @@
 ctx31
-gone
\ No newline at end of file
`

func TestDiffLineToFile(t *testing.T) {
	cases := []struct {
		row  int
		line int
		side byte
		ok   bool
	}{
		{0, 0, 0, false},
		{3, 0, 0, false},
		{4, 0, 0, false},
		{5, 10, ' ', true},
		{6, 11, '-', true},
		{7, 11, '+', true},
		{8, 12, '+', true},
		{9, 13, ' ', true},
		{10, 0, 0, false},
		{11, 31, ' ', true},
		{12, 32, '-', true},
		{13, 0, 0, false},
		{99, 0, 0, false},
	}
	for _, tc := range cases {
		line, side, ok := diffLineToFile(twoHunks, tc.row)
		if line != tc.line || side != tc.side || ok != tc.ok {
			t.Errorf("row %d = %d %q %v, want %d %q %v", tc.row, line, side, ok, tc.line, tc.side, tc.ok)
		}
	}
	rename := "diff --git a/a b/b\nsimilarity index 90%\nrename from a\nrename to b\n"
	if _, _, ok := diffLineToFile(rename, 2); ok {
		t.Error("a rename header row maps to no line")
	}
}

func TestFileLineToRow(t *testing.T) {
	lines := strings.Split(twoHunks, "\n")
	for _, tc := range []struct{ line, row int }{{10, 5}, {12, 8}, {13, 9}, {20, 11}, {31, 11}, {99, 11}} {
		if got, ok := fileLineToRow(lines, tc.line); !ok || got != tc.row {
			t.Errorf("line %d → row %d (%v), want %d", tc.line, got, ok, tc.row)
		}
	}
}

func diffPane(t *testing.T, raw string, h int) Pane {
	t.Helper()
	p := newTestPane(Options{})
	p.SetSize(120, h)
	p.repo = &Repo{Top: "/repo"}
	p.items = []Item{{Section: SectionUnstaged, Entry: Entry{Kind: '1', X: '.', Y: 'M', Path: "x.go"}}}
	p.rows = layoutRows(p.items)
	k := p.keyFor(p.items[0])
	p.setDiff(k, cachedDiff{raw: raw, rendered: raw})
	p.focus = focusDiff
	return p
}

func TestDiffCursorStartsOnFirstChange(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	if p.dc.row != 6 {
		t.Errorf("cursor row = %d, want the first change row 6", p.dc.row)
	}
}

func TestDiffCursorMovesAndScrolls(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,100 +1,100 @@\n")
	for range 100 {
		b.WriteString(" ctx\n")
	}
	p := diffPane(t, b.String(), 12)
	p, _ = press(p, "g")
	for range 20 {
		p, _ = press(p, "j")
	}
	if p.dc.row != 20 {
		t.Fatalf("cursor row = %d, want 20", p.dc.row)
	}
	h := p.diff.Height
	if p.dc.row < p.diff.YOffset || p.dc.row > p.diff.YOffset+h-1-3 {
		t.Errorf("cursor row %d not kept 3 rows inside the view (offset %d, height %d)", p.dc.row, p.diff.YOffset, h)
	}
	p, _ = press(p, "G")
	if p.dc.row < 100 {
		t.Errorf("G should go to the end, row %d", p.dc.row)
	}
}

func TestEnterOnDiffLineShowsFile(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	p, _ = press(p, "j")
	_, cmd := press(p, "enter")
	var got []ui.ShowFileMsg
	for _, msg := range collect(cmd) {
		if s, ok := msg.(ui.ShowFileMsg); ok {
			got = append(got, s)
		}
	}
	if len(got) != 1 || got[0] != (ui.ShowFileMsg{Path: "/repo/x.go", Line: 11}) {
		t.Errorf("enter emitted %+v, want x.go:11", got)
	}
}

func mentionsOf(cmd tea.Cmd) []ui.MentionMsg {
	var out []ui.MentionMsg
	for _, msg := range collect(cmd) {
		if m, ok := msg.(ui.MentionMsg); ok {
			out = append(out, m)
		}
	}
	return out
}

func TestAtInDiffEmitsMention(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	_, cmd := press(p, "@")
	if got := mentionsOf(cmd); len(got) != 1 || got[0] != (ui.MentionMsg{Path: "/repo/x.go", LineStart: 10, LineEnd: 10}) {
		t.Errorf("@ emitted %+v, want x.go line index 10", got)
	}
}

func TestDiffSelection(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	p, _ = press(p, "j", "v", "j", "j")
	_, cmd := press(p, "@")
	if got := mentionsOf(cmd); len(got) != 1 || got[0].LineStart != 10 || got[0].LineEnd != 12 {
		t.Errorf("@ on a selection emitted %+v, want lines 10-12", got)
	}
	spot, _ := p.Spot()
	if spot.Line != 11 || spot.EndLine != 13 || spot.Text != "new11\nnew12\nctx13" {
		t.Errorf("Spot = %+v", spot)
	}
}

func TestEscClearsDiffSelectionFirst(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	p, _ = press(p, "v", "j", "esc")
	if p.dc.selecting || p.focus != focusDiff {
		t.Fatalf("first esc should only clear the selection (selecting %v, focus %d)", p.dc.selecting, p.focus)
	}
	p, _ = press(p, "esc")
	if p.focus != focusList {
		t.Error("second esc should return to the list")
	}
}

func TestEUsesCursorLine(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	p, _ = press(p, "j", "j", "j")
	_, cmd := press(p, "e")
	var target editor.Target
	for _, msg := range collect(cmd) {
		if o, ok := msg.(editor.OpenMsg); ok {
			target = o.Target
		}
	}
	if target.Path != "/repo/x.go" || target.Line != 13 {
		t.Errorf("e opened %+v, want x.go:13", target)
	}
}

func TestSpotFromListFocus(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	p.focus = focusList
	spot, ok := p.Spot()
	if !ok || spot.Mode != "changes" || spot.Path != "/repo/x.go" || spot.Line != 11 || spot.Side != "" {
		t.Errorf("Spot = %+v, %v; want the first changed line", spot, ok)
	}
}

func TestReviewMsgLinePlacesCursor(t *testing.T) {
	dir := reviewRepo(t)
	writeFile(t, dir, "keep.txt", "keep\nmore\nthird\nfourth\n")
	p := newTestPane(Options{Dir: dir})
	p, cmd := p.Update(ReviewMsg{Path: filepath.Join(dir, "keep.txt"), Line: 3})
	p = settle(t, p, cmd)
	spot, _ := p.Spot()
	if spot.Line != 3 || spot.Mode != "review" || spot.Base != "main" {
		repo, _ := OpenRepo(context.Background(), dir)
		t.Errorf("Spot = %+v (top %s), want review keep.txt:3", spot, repo.Top)
	}
}

func bigDiffPane(t *testing.T, n int) Pane {
	t.Helper()
	var b strings.Builder
	b.WriteString("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n")
	b.WriteString("@@ -1,0 +1," + strconv.Itoa(n) + " @@\n")
	for i := range n {
		b.WriteString("+line " + strconv.Itoa(i) + " with some text\n")
	}
	return diffPane(t, b.String(), 40)
}

func TestSpotLargeSelectionIsFast(t *testing.T) {
	const n = 8000
	p := bigDiffPane(t, n)
	p.dc.selecting, p.dc.anchor = true, 4
	p.moveDiffCursor(n + 3)
	start := time.Now()
	spot, _ := p.Spot()
	if took := time.Since(start); took > 50*time.Millisecond {
		t.Errorf("Spot over an %d-row selection took %v; it runs on every update", n, took)
	}
	if spot.Line != 1 || spot.EndLine != n {
		t.Errorf("Spot = %d-%d, want 1-%d", spot.Line, spot.EndLine, n)
	}
	start = time.Now()
	if row, ok := fileLineToRow(p.dc.raw, n); !ok || row != n+3 {
		t.Errorf("fileLineToRow(%d) = %d, %v", n, row, ok)
	}
	if took := time.Since(start); took > 50*time.Millisecond {
		t.Errorf("fileLineToRow over %d rows took %v", n, took)
	}
}

func TestDiffRowsMappedOnce(t *testing.T) {
	p := diffPane(t, twoHunks, 30)
	if len(p.dc.rows) != len(p.dc.raw) {
		t.Fatalf("row table has %d entries for %d rows", len(p.dc.rows), len(p.dc.raw))
	}
	for i := range p.dc.raw {
		line, side, ok := diffLineToFile(twoHunks, i)
		if r := p.dc.rows[i]; r.line != line || r.side != side || r.ok != ok {
			t.Errorf("row %d = %+v, want %d %q %v", i, r, line, side, ok)
		}
	}
}
