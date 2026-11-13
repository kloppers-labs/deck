package git

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/viewer"
)

const diffScrollOff = 3

var (
	diffCursorSGR = "\x1b[48;5;238m"
	diffSelSGR    = "\x1b[48;5;237m"
)

// diffCursor is a line cursor over a single-file diff; raw holds its lines and
// shown the rendered ones, row for row.
type diffCursor struct {
	raw       []string
	rows      []rowMap
	shown     []string
	row       int
	anchor    int
	selecting bool
}

// Spot is where the user is in the Git tab, for presence: Line and EndLine are
// 1-based new-side lines and Text the selected new-side text.
type Spot struct {
	Mode, Base, Path, Side, Text string
	Line, EndLine                int
}

var modeNames = map[mode]string{modeChanges: "changes", modeLog: "log", modeReview: "review"}

// rowMap is a diff row's new-side line; a removed row maps to the new-side
// line it would sit before, and headers have ok false.
type rowMap struct {
	line int
	side byte
	ok   bool
}

func mapDiffRows(lines []string) []rowMap {
	rows := make([]rowMap, len(lines))
	line, inHunk := 0, false
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			line, inHunk = hunkStart(l), true
			continue
		}
		if strings.HasPrefix(l, "diff ") {
			inHunk = false
		}
		if !inHunk || l == "" || (l[0] != '+' && l[0] != '-' && l[0] != ' ') {
			continue
		}
		rows[i] = rowMap{line: max(line, 1), side: l[0], ok: true}
		if l[0] != '-' {
			line++
		}
	}
	return rows
}

func diffLineToFile(diff string, row int) (int, byte, bool) {
	rows := mapDiffRows(strings.Split(diff, "\n"))
	if row < 0 || row >= len(rows) {
		return 0, 0, false
	}
	r := rows[row]
	return r.line, r.side, r.ok
}

func fileLineToRow(lines []string, target int) (int, bool) {
	return rowForLine(mapDiffRows(lines), target)
}

func rowForLine(rows []rowMap, target int) (int, bool) {
	last, found := 0, false
	for i, r := range rows {
		if !r.ok || r.side == '-' {
			continue
		}
		last, found = i, true
		if r.line >= target {
			return i, true
		}
	}
	return last, found
}

func firstChangeRow(rows []rowMap) int {
	for i, r := range rows {
		if r.ok && r.side != ' ' {
			return i
		}
	}
	return 0
}

func (p Pane) rowAt(i int) rowMap {
	if i < 0 || i >= len(p.dc.rows) {
		return rowMap{}
	}
	return p.dc.rows[i]
}

func (p *Pane) setDiffContent(s string) {
	p.diff.SetContent(s)
	p.dc = diffCursor{shown: strings.Split(s, "\n")}
}

func (p *Pane) armDiffCursor() {
	p.dc.raw = strings.Split(p.rawDiff, "\n")
	p.dc.rows = mapDiffRows(p.dc.raw)
	p.moveDiffCursor(firstChangeRow(p.dc.rows))
}

func (p Pane) cursorActive() bool { return p.rawDiff != "" && len(p.dc.raw) > 0 }

func (p *Pane) moveDiffCursor(row int) {
	row = max(min(row, len(p.dc.shown)-1), 0)
	p.dc.row = row
	h := max(p.diff.Height, 1)
	off := min(diffScrollOff, (h-1)/2)
	switch {
	case row < p.diff.YOffset+off:
		p.diff.SetYOffset(row - off)
	case row > p.diff.YOffset+h-1-off:
		p.diff.SetYOffset(row - h + 1 + off)
	}
}

func (p *Pane) keepCursorInView() {
	if !p.cursorActive() {
		return
	}
	top, h := p.diff.YOffset, max(p.diff.Height, 1)
	p.dc.row = max(min(p.dc.row, top+h-1), top)
}

func (p Pane) selRows() (int, int) {
	if !p.dc.selecting {
		return p.dc.row, p.dc.row
	}
	return min(p.dc.anchor, p.dc.row), max(p.dc.anchor, p.dc.row)
}

func (p Pane) selectedPath() string {
	if p.repo == nil {
		return ""
	}
	switch p.mode {
	case modeChanges:
		if it, ok := p.selected(); ok {
			return p.repo.Abs(it.Entry.Path)
		}
	case modeReview:
		if f, ok := p.reviewFile(); ok {
			return p.repo.Abs(f.Path)
		}
	}
	return ""
}

func (p Pane) cursorLines() (int, int, bool) {
	lo, hi := p.selRows()
	first := p.rowAt(lo)
	if !first.ok {
		return 0, 0, false
	}
	last := first.line
	for r := hi; r > lo; r-- {
		if m := p.rowAt(r); m.ok {
			last = max(m.line, first.line)
			break
		}
	}
	return first.line, last, true
}

func (p Pane) Spot() (Spot, bool) {
	s := Spot{Mode: modeNames[p.mode]}
	if p.mode == modeReview {
		s.Base = p.review.rv.Base
	}
	s.Path = p.selectedPath()
	if s.Path == "" {
		return s, true
	}
	if p.rawDiff != "" {
		s.Line = firstHunkLine(p.rawDiff)
	}
	if !p.cursorActive() || p.focus != focusDiff {
		return s, true
	}
	first, last, ok := p.cursorLines()
	if !ok {
		return s, true
	}
	s.Line, s.Side = first, map[byte]string{'+': "new", ' ': "new", '-': "old"}[p.rowAt(p.dc.row).side]
	if p.dc.selecting && last > first {
		s.EndLine = last
	}
	lo, hi := p.selRows()
	var text []string
	if p.dc.selecting {
		for r := lo; r <= hi && r < len(p.dc.raw); r++ {
			if m := p.rowAt(r); m.ok && m.side != '-' {
				text = append(text, p.dc.raw[r][1:])
			}
		}
		s.Text = strings.Join(text, "\n")
	}
	return s, true
}

// EndCol is one past the last rune of the spot's selected text.
func (s Spot) EndCol() int {
	last := s.Text
	if i := strings.LastIndexByte(last, '\n'); i >= 0 {
		last = last[i+1:]
	}
	return utf8.RuneCountInString(last) + 1
}

func (p Pane) cursorDiffKeys(msg tea.KeyMsg) (Pane, tea.Cmd, bool) {
	h := max(p.diff.Height, 1)
	switch msg.String() {
	case "j", "down":
		p.moveDiffCursor(p.dc.row + 1)
	case "k", "up":
		p.moveDiffCursor(p.dc.row - 1)
	case "ctrl+d":
		p.moveDiffCursor(p.dc.row + h/2)
	case "ctrl+u":
		p.moveDiffCursor(p.dc.row - h/2)
	case "pgdown", " ", "f":
		p.moveDiffCursor(p.dc.row + h)
	case "pgup", "b":
		p.moveDiffCursor(p.dc.row - h)
	case "g", "home":
		p.moveDiffCursor(0)
	case "G", "end":
		p.moveDiffCursor(len(p.dc.shown) - 1)
	case "v", "V":
		p.dc.selecting, p.dc.anchor = !p.dc.selecting, p.dc.row
	case "esc":
		if !p.dc.selecting {
			return p, nil, false
		}
		p.dc.selecting = false
	case "enter":
		return p, p.showCursorFile(), true
	case "@":
		return p, p.mentionCursor(), true
	case "e":
		return p, p.editAtCursor(), true
	default:
		return p, nil, false
	}
	return p, nil, true
}

func (p Pane) showCursorFile() tea.Cmd {
	path := p.selectedPath()
	first, _, ok := p.cursorLines()
	if path == "" || !ok {
		return nil
	}
	msg := ui.ShowFileMsg{Path: path, Line: first}
	return func() tea.Msg { return msg }
}

func (p Pane) mentionCursor() tea.Cmd {
	path := p.selectedPath()
	first, last, ok := p.cursorLines()
	if path == "" || !ok {
		return nil
	}
	msg := ui.MentionMsg{Path: path, LineStart: first - 1, LineEnd: last - 1}
	return func() tea.Msg { return msg }
}

func (p Pane) editAtCursor() tea.Cmd {
	path := p.selectedPath()
	if path == "" {
		return nil
	}
	line, _, ok := p.cursorLines()
	if !ok {
		line = firstHunkLine(p.rawDiff)
	}
	msg := editor.OpenMsg{Target: editor.Target{Path: path, Line: line}}
	return func() tea.Msg { return msg }
}

func (p Pane) diffView() string {
	if !p.cursorActive() || p.focus != focusDiff {
		return p.diff.View()
	}
	w, h, top := p.diff.Width, p.diff.Height, p.diff.YOffset
	lo, hi := p.selRows()
	out := make([]string, 0, h)
	for i := top; i < top+h && i < len(p.dc.shown); i++ {
		line := p.dc.shown[i]
		if i >= lo && i <= hi {
			cut := ansi.Truncate(line, w, "")
			cut += strings.Repeat(" ", max(w-ansi.StringWidth(cut), 0))
			sgr := diffSelSGR
			if i == p.dc.row {
				sgr = diffCursorSGR
			}
			line = viewer.Paint(cut, []viewer.Span{{Start: 0, End: w, SGR: sgr}})
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func (p *Pane) placePendingLine() {
	if p.review.pendingLine <= 0 || !p.cursorActive() {
		return
	}
	if row, ok := rowForLine(p.dc.rows, p.review.pendingLine); ok {
		p.moveDiffCursor(row)
	}
	p.review.pendingLine = 0
}
