package viewer

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

const (
	hoverMaxWidth     = 80
	hoverMaxRows      = 14
	hoverLeaveDelay   = 300 * time.Millisecond
	defaultHoverDelay = 350 * time.Millisecond
	hoverCacheMax     = 64
	underlineSGR      = "\x1b[4m"
)

type hoverState struct {
	open  bool
	span  Range
	lines []string
	top   int
	leave int64
}

type dwellState struct {
	span Range
	on   bool
	seq  int64
}

type hoverDwellMsg struct{ seq int64 }
type hoverLeaveMsg struct{ seq int64 }

func (hoverDwellMsg) RouteTo() ui.Dest { return ui.DestFiles }
func (hoverLeaveMsg) RouteTo() ui.Dest { return ui.DestFiles }
func (hoverDwellMsg) viewerMsg()       {}
func (hoverLeaveMsg) viewerMsg()       {}

var hoverBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.ColorAccent)

var hoverRender = renderMarkdown

type hoverKey struct {
	markdown, theme string
	width           int
}

var (
	hoverMu    sync.Mutex
	hoverCache = map[hoverKey][]string{}
)

// renderHover renders hover markdown at width columns, trimmed of glamour's
// margins, caching the result.
func renderHover(markdown string, width int, theme string) []string {
	k := hoverKey{markdown, theme, width}
	hoverMu.Lock()
	cached, ok := hoverCache[k]
	hoverMu.Unlock()
	if ok {
		return cached
	}
	lines := tidyHover(hoverRender(markdown, width, theme))
	hoverMu.Lock()
	if len(hoverCache) >= hoverCacheMax {
		hoverCache = map[hoverKey][]string{}
	}
	hoverCache[k] = lines
	hoverMu.Unlock()
	return lines
}

func blankLine(l string) bool { return strings.TrimSpace(ansi.Strip(l)) == "" }

func tidyHover(out string) []string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, l := range lines {
		lines[i] = sanitizeStyled(l)
	}
	for len(lines) > 0 && blankLine(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blankLine(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	margin := -1
	for _, l := range lines {
		if blankLine(l) {
			continue
		}
		p := ansi.Strip(l)
		if lead := len(p) - len(strings.TrimLeft(p, " ")); margin < 0 || lead < margin {
			margin = lead
		}
	}
	margin = max(margin, 0)
	out2 := make([]string, len(lines))
	for i, l := range lines {
		w := ansi.StringWidth(strings.TrimRight(ansi.Strip(l), " ")) - margin
		if margin > 0 {
			l = ansi.TruncateLeft(l, margin, "")
		}
		out2[i] = ansi.Truncate(l, max(w, 0), "")
	}
	return out2
}

func (m Model) hoverDelay() time.Duration {
	if m.opts.HoverDelay > 0 {
		return m.opts.HoverDelay
	}
	return defaultHoverDelay
}

func (m Model) hoverInnerWidth() int {
	return max(min(hoverMaxWidth, m.layout().TextW-4)-2, 10)
}

func (m *Model) requestHover(p Pos, mouse bool) tea.Cmd {
	ok, cmd := m.navReady(mouse)
	if !ok {
		return cmd
	}
	if m.hoverStop != nil {
		m.hoverStop()
	}
	seq := m.nextSeq(navHover)
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout())
	m.hoverStop = cancel
	nav, path, width, theme := m.nav, m.path, m.hoverInnerWidth(), m.opts.Theme
	at := Loc{Path: path, Line: p.Line, Col: p.Col}
	return func() tea.Msg {
		defer cancel()
		h, err := nav.Hover(ctx, path, at)
		var lines []string
		if err == nil && strings.TrimSpace(h.Markdown) != "" {
			lines = renderHover(h.Markdown, width, theme)
		}
		return HoverMsg{Seq: seq, Path: path, At: p, Mouse: mouse, Hover: h, Lines: lines, Err: err}
	}
}

func (m *Model) gotHover(msg HoverMsg) tea.Cmd {
	if msg.Seq != m.reqs[navHover] || msg.Path != m.path {
		return nil
	}
	m.reqs[navHover], m.hoverStop = 0, nil
	switch {
	case msg.Mouse && (msg.Err != nil || len(msg.Lines) == 0):
		return nil
	case msg.Err != nil:
		return m.navFailed("hover", msg.Err)
	case len(msg.Lines) == 0:
		return m.nothingFound("hover info")
	}
	m.hover = hoverState{open: true, span: m.hoverSpan(msg), lines: msg.Lines}
	return nil
}

func (m Model) hoverSpan(msg HoverMsg) Range {
	if r := msg.Hover.Range; r != nil && r.Start.Line == r.End.Line && r.Start.Col < r.End.Col {
		return *r
	}
	raw, _ := m.lines()
	if msg.At.Line < len(raw) {
		if s, e, ok := WordSpan(raw[msg.At.Line], msg.At.Col); ok {
			return Range{Pos{msg.At.Line, s}, Pos{msg.At.Line, e}}
		}
	}
	return Range{msg.At, Pos{msg.At.Line, msg.At.Col + 1}}
}

func (m *Model) closeHover() {
	if m.hoverStop != nil {
		m.hoverStop()
		m.hoverStop = nil
	}
	m.reqs[navHover] = 0
	m.hover = hoverState{}
}

func (m Model) HoverOpen() bool { return m.hover.open }

func (m Model) hoverRows() int { return min(len(m.hover.lines), hoverMaxRows) }

func (m *Model) scrollHover(d int) {
	m.hover.top = min(max(m.hover.top+d, 0), max(len(m.hover.lines)-m.hoverRows(), 0))
}

// hoverRect is the popup's box in view cells, below or above the target.
func (m Model) hoverRect() (ui.Rect, bool) {
	if !m.hover.open || m.doc == nil {
		return ui.Rect{}, false
	}
	raw, _ := m.lines()
	x, y, ok := DocToScreen(raw, m.layout(), m.hover.span.Start)
	if !ok {
		return ui.Rect{}, false
	}
	w := 1
	for _, l := range m.hover.lines {
		w = max(w, ansi.StringWidth(l))
	}
	w = min(w, m.hoverInnerWidth()) + 2
	h := m.hoverRows() + 2
	return PlacePopup(ui.Rect{X: x, Y: y + 1, W: 1, H: 1}, w, h, ui.Rect{Y: 1, W: m.width, H: m.bodyHeight()}), true
}

func (m Model) hoverView(r ui.Rect) string {
	rows := max(r.H-2, 0)
	end := min(m.hover.top+rows, len(m.hover.lines))
	body := ui.Fit(strings.Join(m.hover.lines[m.hover.top:end], "\n"), max(r.W-2, 0), rows)
	return hoverBox.Render(body)
}

func (m Model) wordAtCell(x, y int) (Range, bool) {
	y--
	if m.doc == nil || y < 0 || y >= m.bodyHeight() || m.showingMD() {
		return Range{}, false
	}
	l := m.layout()
	if x < l.Gutter {
		return Range{}, false
	}
	raw, _ := m.lines()
	p, ok := ScreenToDoc(raw, l, x, y)
	if !ok {
		return Range{}, false
	}
	s, e, ok := WordSpan(raw[p.Line], p.Col)
	if !ok {
		return Range{}, false
	}
	return Range{Pos{p.Line, s}, Pos{p.Line, e}}, true
}

func overlaps(a, b Range) bool {
	return a.Start.Line == b.Start.Line && a.Start.Col < b.End.Col && b.Start.Col < a.End.Col
}

// dwell tracks the word under the mouse: a new word starts the hover delay,
// and leaving the hover target starts the close delay.
func (m *Model) dwell(x, y int) tea.Cmd {
	if r, ok := m.hoverRect(); ok && r.Contains(x, y) {
		m.hover.leave = 0
		return nil
	}
	span, on := m.wordAtCell(x, y)
	if on == m.dwellAt.on && span == m.dwellAt.span {
		return nil
	}
	m.dwellAt = dwellState{span: span, on: on, seq: navSeq.Add(1)}
	var cmds []tea.Cmd
	if m.hover.open {
		if on && overlaps(span, m.hover.span) {
			m.hover.leave = 0
		} else {
			cmds = append(cmds, m.startLeave())
		}
	}
	if ready, _ := m.navReady(true); on && ready {
		seq := m.dwellAt.seq
		cmds = append(cmds, tea.Tick(m.hoverDelay(), func(time.Time) tea.Msg { return hoverDwellMsg{seq} }))
	}
	return tea.Batch(cmds...)
}

func (m *Model) startLeave() tea.Cmd {
	if m.hover.leave != 0 {
		return nil
	}
	seq := navSeq.Add(1)
	m.hover.leave = seq
	return tea.Tick(hoverLeaveDelay, func(time.Time) tea.Msg { return hoverLeaveMsg{seq} })
}

// MouseLeave forgets the word under the mouse and starts closing the hover,
// for when the mouse moves out of the viewer.
func (m *Model) MouseLeave() tea.Cmd {
	m.dwellAt = dwellState{seq: navSeq.Add(1)}
	if !m.hover.open {
		return nil
	}
	return m.startLeave()
}

func (m *Model) dwellDue(msg hoverDwellMsg) tea.Cmd {
	if msg.seq != m.dwellAt.seq || !m.dwellAt.on {
		return nil
	}
	if m.hover.open && overlaps(m.dwellAt.span, m.hover.span) {
		return nil
	}
	return m.requestHover(m.dwellAt.span.Start, true)
}

func (m *Model) leaveDue(msg hoverLeaveMsg) {
	if msg.seq == m.hover.leave && m.hover.open {
		m.closeHover()
	}
}
