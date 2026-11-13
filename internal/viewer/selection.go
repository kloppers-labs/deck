package viewer

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/ui"
)

var selectionSGR = "\x1b[48;5;" + strconv.Itoa(nearest256(0x44, 0x47, 0x5a)) + "m"

type selection struct {
	active bool
	lines  bool
	anchor Pos
}

// Selection is the selected text in the source, Start before End and End.Col
// exclusive; line-wise selections cover whole lines.
func (m Model) Selection() (Range, bool) {
	if !m.sel.active || m.doc == nil || m.showingMD() {
		return Range{}, false
	}
	raw, _ := m.lines()
	if len(raw) == 0 {
		return Range{}, false
	}
	a, b := m.clampPos(m.sel.anchor), m.cur
	if b.before(a) {
		a, b = b, a
	}
	if m.sel.lines {
		return Range{Pos{a.Line, 0}, Pos{b.Line, utf8.RuneCountInString(raw[b.Line])}}, true
	}
	return Range{a, Pos{b.Line, min(b.Col+1, utf8.RuneCountInString(raw[b.Line]))}}, true
}

func (m Model) SelectionText() string {
	r, ok := m.Selection()
	if !ok {
		return ""
	}
	raw, _ := m.lines()
	var b strings.Builder
	for line := r.Start.Line; line <= r.End.Line; line++ {
		rs := []rune(raw[line])
		from, to := 0, len(rs)
		if line == r.Start.Line {
			from = min(r.Start.Col, len(rs))
		}
		if line == r.End.Line {
			to = min(r.End.Col, len(rs))
		}
		if line > r.Start.Line {
			b.WriteByte('\n')
		}
		b.WriteString(string(rs[from:max(from, to)]))
	}
	return b.String()
}

// SelectRange selects r character-wise, End.Col exclusive, with the cursor on its last character.
func (m Model) SelectRange(r Range) Model {
	if m.doc == nil {
		return m
	}
	m.sel = selection{active: true, anchor: m.clampPos(r.Start)}
	m.moveTo(Pos{r.End.Line, max(r.End.Col-1, 0)})
	m.center()
	m.relayout()
	return m
}

// SelectLines selects whole lines first..last (0-based) with the cursor on first.
func (m Model) SelectLines(first, last int) Model {
	if m.doc == nil {
		return m
	}
	m.sel = selection{active: true, lines: true, anchor: m.clampPos(Pos{last, 0})}
	m.moveTo(Pos{first, 0})
	m.center()
	m.relayout()
	return m
}

func (m *Model) clearSelection() { m.sel = selection{} }

func (m *Model) toggleSelection(lines bool) {
	if m.showingMD() {
		_, start, _, _ := m.Section()
		*m = m.SetRawMarkdown(true)
		m.moveLine(start)
		m.center()
		m.sel = selection{active: true, lines: lines, anchor: m.cur}
		return
	}
	switch {
	case m.sel.active && m.sel.lines == lines:
		m.clearSelection()
	case m.sel.active:
		m.sel.lines = lines
	default:
		m.sel = selection{active: true, lines: lines, anchor: m.cur}
	}
}

func (m Model) mentionLines() (int, int, bool) {
	if _, start, end, ok := m.Section(); ok {
		return start, end, true
	}
	if r, ok := m.Selection(); ok {
		end := r.End.Line
		if r.End.Col == 0 && end > r.Start.Line {
			end--
		}
		return r.Start.Line, end, true
	}
	return m.cur.Line, m.cur.Line, m.doc != nil
}

func (m Model) mention() tea.Cmd {
	if m.path == "" || IsVirtual(m.path) {
		return nil
	}
	start, end, ok := m.mentionLines()
	if !ok {
		return nil
	}
	msg := ui.MentionMsg{Path: m.path, LineStart: start, LineEnd: end}
	return func() tea.Msg { return msg }
}

func (m Model) copySelection(ref bool) tea.Cmd {
	r, _ := m.Selection()
	if ref {
		label := m.relPath() + ":" + strconv.Itoa(r.Start.Line+1) + "-" + strconv.Itoa(r.End.Line+1)
		return copyCmd(label, label)
	}
	n := r.End.Line - r.Start.Line + 1
	label := "selection (" + strconv.Itoa(n) + " line"
	if n != 1 {
		label += "s"
	}
	return copyCmd(m.SelectionText(), label+")")
}

func (m Model) selectionSpan(raw []string, line, start int, l Layout) (Span, bool) {
	r, ok := m.Selection()
	if !ok || line < r.Start.Line || line > r.End.Line {
		return Span{}, false
	}
	if m.sel.lines {
		return Span{Start: 0, End: l.TextW, SGR: selectionSGR}, true
	}
	n := utf8.RuneCountInString(raw[line])
	from, to := 0, DispCol(raw[line], n, l.TabWidth)+1
	if line == r.Start.Line {
		from = DispCol(raw[line], r.Start.Col, l.TabWidth)
	}
	if line == r.End.Line {
		to = DispCol(raw[line], r.End.Col, l.TabWidth)
	}
	from, to = max(from-start, 0), min(to-start, l.TextW)
	if to <= from {
		return Span{}, false
	}
	return Span{Start: from, End: to, SGR: selectionSGR}, true
}

// Section is the markdown section under the cursor while markdown is rendered,
// in 0-based source lines.
func (m Model) Section() (title string, startLine, endLine int, ok bool) {
	if !m.showingMD() || m.doc == nil {
		return "", 0, 0, false
	}
	headings := MarkdownOutline(m.doc.Raw)
	last := max(len(m.doc.Raw)-1, 0)
	i, found := renderedSection(m.md.plain, headings, m.cur.Line)
	if !found {
		end := last
		if len(headings) > 0 {
			end = max(headings[0].Range.Start.Line-1, 0)
		}
		return "", 0, end, true
	}
	h := headings[i]
	return h.Name, h.Range.Start.Line, h.Range.End.Line, true
}

var mdLink = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)

var mdMarks = strings.NewReplacer("`", "", "*", "", "_", "", "~", "")

// plainHeading reduces heading text to what both the source and glamour's
// output share; glamour prints a link as its text followed by its URL.
func plainHeading(s string, withURL bool) string {
	link := "$1"
	if withURL {
		link = "$1 $2"
	}
	return strings.Join(strings.Fields(mdMarks.Replace(mdLink.ReplaceAllString(s, link))), " ")
}

func renderedSection(rendered []string, headings []Symbol, row int) (int, bool) {
	names := make([]string, len(headings))
	byName := map[string][]int{}
	for i, h := range headings {
		names[i] = plainHeading(h.Name, true)
		byName[names[i]] = append(byName[names[i]], i)
		if bare := plainHeading(h.Name, false); bare != names[i] {
			byName[bare] = append(byName[bare], i)
		}
	}
	found, next := -1, 0
	for r := 0; r <= row && r < len(rendered) && next < len(headings); r++ {
		text := plainHeading(strings.TrimLeft(strings.TrimSpace(rendered[r]), "#"), false)
		if text == "" {
			continue
		}
		j := -1
		for _, i := range byName[text] {
			if i >= next {
				j = i
				break
			}
		}
		if j < 0 && len(text) >= 8 && strings.HasPrefix(names[next], text) {
			j = next
		}
		if j >= 0 {
			found, next = j, j+1
		}
	}
	return found, found >= 0
}

func (m *Model) dragTo(x, y int) {
	if m.dragFrom == nil || m.showingMD() {
		return
	}
	y--
	if y < 0 || y >= m.bodyHeight() {
		return
	}
	raw, _ := m.lines()
	p, _ := ScreenToDoc(raw, m.layout(), x, y)
	p = m.clampPos(p)
	if p == *m.dragFrom && !m.sel.active {
		return
	}
	m.sel = selection{active: true, anchor: *m.dragFrom}
	m.moveTo(p)
}

// VisibleLines is the first and last document line on screen, 0-based.
func (m Model) VisibleLines() (int, int) {
	if len(m.rows) == 0 {
		return 0, 0
	}
	return m.rows[0].line, m.rows[len(m.rows)-1].line
}

// ShowingDocument reports whether a real file is loaded, not a note, a binary or a virtual document.
func (m Model) ShowingDocument() bool {
	return m.doc != nil && !m.doc.Binary && m.path != "" && !IsVirtual(m.path)
}

// SourceLine is line i of the file's source text, or "" when out of range.
func (m Model) SourceLine(i int) string {
	if m.doc == nil || i < 0 || i >= len(m.doc.Raw) {
		return ""
	}
	return m.doc.Raw[i]
}

// SelectLinesWhenLoaded selects lines first..last now, or once the document being opened arrives.
func (m Model) SelectLinesWhenLoaded(first, last int) Model {
	if m.doc != nil && m.pending == nil {
		return m.SelectLines(first, last)
	}
	m.selOnLoad = &[2]int{first, last}
	return m
}

func IsMarkdown(path string) bool { return isMarkdownPath(path) }
