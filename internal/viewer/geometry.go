package viewer

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	scrollOffRows = 3
	scrollOffCols = 8
)

// Pos is a document position: a 0-based line and a 0-based rune column in Raw.
type Pos struct{ Line, Col int }

func (p Pos) before(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

// Layout maps the text area onto the screen: a Gutter, TextW text columns and
// Height rows, starting at line Top and display column Left (unused when wrapping).
type Layout struct {
	Gutter, TextW, Height int
	Top, Left             int
	TabWidth              int
	Wrap                  bool
}

func runeWidth(r rune) int {
	if r < utf8.RuneSelf {
		return 1
	}
	return ansi.StringWidth(string(r))
}

func tabStop(col, tw int) int {
	tw = max(tw, 1)
	return tw - col%tw
}

// ExpandTabs replaces each tab with spaces up to the next multiple of tabWidth.
func ExpandTabs(s string, tabWidth int) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	out, _ := expandFrom(s, 0, tabWidth)
	return out
}

func expandFrom(s string, col, tabWidth int) (string, int) {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' {
			n := tabStop(col, tabWidth)
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col += runeWidth(r)
	}
	return b.String(), col
}

func cellWidth(r rune, col, tabWidth int) int {
	if r == '\t' {
		return tabStop(col, tabWidth)
	}
	return runeWidth(r)
}

// DispCol is the display column where rune runeCol of a raw line starts.
// Columns past the end count one cell each.
func DispCol(line string, runeCol, tabWidth int) int {
	col, i := 0, 0
	for _, r := range line {
		if i >= runeCol {
			return col
		}
		col += cellWidth(r, col, tabWidth)
		i++
	}
	return col + max(runeCol-i, 0)
}

// RuneCol is the rune whose cells cover display column disp, clamped to the
// line end.
func RuneCol(line string, disp, tabWidth int) int {
	if disp <= 0 {
		return 0
	}
	col, i := 0, 0
	for _, r := range line {
		col += cellWidth(r, col, tabWidth)
		if disp < col {
			return i
		}
		i++
	}
	return i
}

func lineWidth(line string, tabWidth int) int {
	return DispCol(line, utf8.RuneCountInString(line), tabWidth)
}

// rowsOf counts the screen rows a line takes. A wrapped line always keeps a
// cell for its end-of-line position.
func (l Layout) rowsOf(line string) int {
	if !l.Wrap || l.TextW <= 0 {
		return 1
	}
	return lineWidth(line, l.TabWidth)/l.TextW + 1
}

// DocToScreen is the screen cell of p, local to the text area's top-left.
func DocToScreen(lines []string, l Layout, p Pos) (x, y int, ok bool) {
	if p.Line < l.Top || p.Line >= len(lines) || l.TextW <= 0 {
		return 0, 0, false
	}
	for i := l.Top; i < p.Line; i++ {
		y += l.rowsOf(lines[i])
		if y >= l.Height {
			return 0, 0, false
		}
	}
	dc := DispCol(lines[p.Line], p.Col, l.TabWidth)
	if l.Wrap {
		y += dc / l.TextW
		x = l.Gutter + dc%l.TextW
	} else {
		if dc < l.Left || dc >= l.Left+l.TextW {
			return 0, 0, false
		}
		x = l.Gutter + dc - l.Left
	}
	if y >= l.Height {
		return 0, 0, false
	}
	return x, y, true
}

// ScreenToDoc maps a cell local to the text area to the position under it, clamping
// gutter and past-the-end clicks; rows below the document report !ok.
func ScreenToDoc(lines []string, l Layout, x, y int) (Pos, bool) {
	if y < 0 || l.TextW <= 0 {
		return Pos{}, false
	}
	row := 0
	for i := l.Top; i < len(lines); i++ {
		n := l.rowsOf(lines[i])
		if y < row+n {
			sub := y - row
			tx := max(x-l.Gutter, 0)
			dc := l.Left + tx
			if l.Wrap {
				dc = sub*l.TextW + min(tx, l.TextW-1)
			}
			return Pos{Line: i, Col: RuneCol(lines[i], dc, l.TabWidth)}, true
		}
		row += n
	}
	return Pos{}, false
}

// EnsureVisible scrolls l so p sits inside the view with a 3-row and 8-column
// scroll-off, never past the end of the document.
func EnsureVisible(lines []string, l Layout, p Pos) Layout {
	if len(lines) == 0 || l.Height <= 0 {
		l.Top, l.Left = 0, 0
		return l
	}
	off := min(scrollOffRows, (l.Height-1)/2)
	if p.Line < l.Top+off {
		l.Top = p.Line - off
	}
	if l.Wrap {
		l.Left = 0
		l.Top = max(l.Top, 0)
		for l.Top < p.Line && l.rowsBetween(lines, l.Top, min(p.Line+off, len(lines)-1)) > l.Height {
			l.Top++
		}
	} else {
		if p.Line > l.Top+l.Height-1-off {
			l.Top = p.Line - l.Height + 1 + off
		}
		l.Left = scrollLeft(l, DispCol(lines[p.Line], p.Col, l.TabWidth))
	}
	l.Top = min(max(l.Top, 0), MaxTop(lines, l))
	return l
}

func scrollLeft(l Layout, dc int) int {
	off := min(scrollOffCols, max((l.TextW-1)/2, 0))
	left := l.Left
	if dc < left+off {
		left = dc - off
	}
	if dc >= left+l.TextW-off {
		left = dc - l.TextW + off + 1
	}
	return max(left, 0)
}

func (l Layout) rowsBetween(lines []string, from, to int) int {
	n := 0
	for i := from; i <= to && i < len(lines); i++ {
		n += l.rowsOf(lines[i])
	}
	return n
}

// MaxTop is the largest Top that still fills the view to the last line.
func MaxTop(lines []string, l Layout) int {
	if !l.Wrap {
		return max(len(lines)-l.Height, 0)
	}
	rows := 0
	for i := len(lines) - 1; i >= 0; i-- {
		rows += l.rowsOf(lines[i])
		if rows > l.Height {
			return i + 1
		}
	}
	return 0
}
