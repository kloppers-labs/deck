package viewer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

var (
	gutterSGR     = "\x1b[38;5;" + strconv.Itoa(nearest256(0x62, 0x72, 0xa4)) + "m"
	gutterCurSGR  = "\x1b[38;5;" + strconv.Itoa(nearest256(0xbd, 0x93, 0xf9)) + "m"
	cursorSGR     = "\x1b[7m"
	cursorLineSGR = "\x1b[48;5;236m"
	matchSGR      = "\x1b[30;48;5;" + strconv.Itoa(nearest256(0xf1, 0xfa, 0x8c)) + "m"
	curMatchSGR   = "\x1b[30;48;5;" + strconv.Itoa(nearest256(0xff, 0xb8, 0x6c)) + "m"
	warnStyle     = lipgloss.NewStyle().Foreground(ui.ColorWarn)
)

// View renders exactly the size given to SetSize: a header row, the text rows
// and a footer row. Only the visible rows are rendered.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	out := make([]string, 0, m.height)
	out = append(out, ui.Fit(m.header(), m.width, 1))
	body := m.bodyHeight()
	switch {
	case m.err != nil:
		out = append(out, ui.Fit(ui.Err.Render("cannot preview: "+m.err.Error()), m.width, 1))
	case m.doc == nil && m.note != "":
		out = append(out, ui.Fit(ui.Muted.Render(m.note), m.width, 1))
	case m.doc == nil && m.path != "":
		out = append(out, ui.Fit(ui.Muted.Render("loading…"), m.width, 1))
	case m.doc != nil && m.doc.Binary:
		out = append(out, ui.Fit(ui.Muted.Render("⬡ binary file — preview not available"), m.width, 1))
	case m.doc != nil:
		out = m.renderRows(out)
	}
	blank := strings.Repeat(" ", m.width)
	for len(out) < 1+body {
		out = append(out, blank)
	}
	out = out[:1+body]
	if m.height > 1 {
		out = append(out, ui.Fit(m.footer(), m.width, 1))
	}
	view := strings.Join(out[:m.height], "\n")
	if r, ok := m.hoverRect(); ok {
		view = ui.Overlay(view, m.hoverView(r), r.X, r.Y)
	}
	return view
}

func (m Model) renderRows(out []string) []string {
	raw, plain := m.lines()
	l := m.layout()
	numW := max(l.Gutter-3, 0)
	diags := m.diagnostics()
	var b strings.Builder
	for _, r := range m.rows {
		b.Reset()
		if l.Gutter > 0 {
			sgr := gutterSGR
			if r.line == m.cur.Line && m.focused {
				sgr = gutterCurSGR
			}
			b.WriteString(sgr)
			marker := " "
			if r.sub == 0 {
				num := strconv.Itoa(r.line + 1)
				b.WriteString(strings.Repeat(" ", max(numW-len(num), 0)))
				b.WriteString(num)
				if d, ok := lineDiagnostic(diags, r.line); ok {
					marker = diagSGR[Severity(d.Severity.rank())] + d.Severity.glyph() + sgrReset
				}
			} else {
				b.WriteString(strings.Repeat(" ", numW))
			}
			b.WriteString(" " + sgrReset + marker + " ")
		}
		start := m.left
		if l.Wrap {
			start = r.sub * l.TextW
		}
		text, w := cutRow(m.styledLine(r.line), plain[r.line], start, l.TextW)
		text += strings.Repeat(" ", max(l.TextW-w, 0))
		if spans := m.rowSpans(raw, r.line, start, l); len(spans) > 0 {
			text = Paint(text, spans)
		}
		b.WriteString(text)
		out = append(out, b.String())
	}
	return out
}

func cutRow(styled, plain string, start, textW int) (string, int) {
	pw := strWidth(plain)
	if start == 0 && pw <= textW {
		return styled, pw
	}
	if start >= pw {
		return "", 0
	}
	cut := ansi.Cut(styled, start, start+textW)
	return cut, ansi.StringWidth(cut)
}

func (m Model) rowSpans(raw []string, line, start int, l Layout) []Span {
	var spans []Span
	end := start + l.TextW
	if m.focused && line == m.cur.Line {
		spans = append(spans, Span{Start: 0, End: l.TextW, SGR: cursorLineSGR})
	}
	ms := m.search.matches
	i := sort.Search(len(ms), func(i int) bool { return ms[i].Line >= line })
	for ; i < len(ms) && ms[i].Line == line; i++ {
		mt := ms[i]
		if mt.End <= start || mt.Start >= end {
			continue
		}
		sgr := matchSGR
		if i == m.search.idx {
			sgr = curMatchSGR
		}
		spans = append(spans, Span{Start: max(mt.Start-start, 0), End: min(mt.End, end) - start, SGR: sgr})
	}
	if h := m.hover.span; m.hover.open && line == h.Start.Line {
		from := DispCol(raw[line], h.Start.Col, l.TabWidth) - start
		to := DispCol(raw[line], h.End.Col, l.TabWidth) - start
		if to > 0 && from < l.TextW {
			sgr := underlineSGR
			if m.focused && line == m.cur.Line {
				sgr = cursorLineSGR + underlineSGR
			}
			spans = append(spans, Span{Start: max(from, 0), End: min(to, l.TextW), SGR: sgr})
		}
	}
	if m.focused && line == m.cur.Line {
		dc := DispCol(raw[line], m.cur.Col, l.TabWidth)
		if dc >= start && dc < end {
			w := 1
			if rs := []rune(raw[line]); m.cur.Col < len(rs) {
				w = max(cellWidth(rs[m.cur.Col], dc, l.TabWidth), 1)
			}
			spans = append(spans, Span{Start: dc - start, End: min(dc-start+w, l.TextW), SGR: cursorSGR})
		}
	}
	return spans
}

func (m Model) header() string {
	if m.path == "" {
		return ""
	}
	var banner string
	if d := m.doc; d != nil && d.Truncated {
		banner = warnStyle.Render(fmt.Sprintf("showing %s of %s · e to open in editor", humanSize(d.Loaded), humanSize(d.Size)))
	}
	room := m.width
	if banner != "" {
		room -= ansi.StringWidth(banner) + 1
	}
	path := m.relPath()
	if d := m.doc; d != nil && d.Title != "" {
		path = d.Title
	}
	if crumbs := m.breadcrumb(); len(crumbs) > 0 {
		path = filepath.Base(path) + " › " + strings.Join(crumbs, " › ")
	}
	if over := ansi.StringWidth(path) - room; over > 0 {
		path = ansi.TruncateLeft(path, over+1, "…")
	}
	if room <= 0 {
		path = ""
	}
	h := ui.Muted.Render(path)
	if banner != "" {
		h += strings.Repeat(" ", max(room-ansi.StringWidth(path), 0)+1) + banner
	}
	return h
}

func (m Model) footer() string {
	var left string
	switch {
	case m.prompt.kind != 0:
		left = string(m.prompt.kind) + m.prompt.text + "█"
		if m.prompt.kind == '/' && m.prompt.regex {
			left += ui.Muted.Render("  [regex · ctrl+r]")
		}
		if m.prompt.kind == '/' {
			left += "  " + m.searchCount()
		}
	case m.chord.Pending():
		left = m.chord.Prefix() + "…"
	case m.flash != "":
		left = warnStyle.Render(m.flash)
	case m.search.query != "":
		left = "/" + m.search.query + "  " + m.searchCount()
	default:
		if d, ok := lineDiagnostic(m.diagnostics(), m.cur.Line); ok && m.doc != nil {
			left = diagSGR[Severity(d.Severity.rank())] + d.summary() + sgrReset
		}
	}
	var right string
	if raw, _ := m.lines(); m.doc != nil && len(raw) > 0 {
		pct := (m.cur.Line + 1) * 100 / len(raw)
		right = fmt.Sprintf("Ln %d, Col %d · %d%%", m.cur.Line+1, m.cur.Col+1, pct)
		if m.showingMD() {
			right = "rendered · m raw · " + right
		} else if m.doc.Lang != "" && m.doc.Lang != "plaintext" {
			right = m.doc.Lang + " · " + right
		}
		if badge := m.lspBadge(); badge != "" {
			right = badge + " · " + right
		}
	}
	right = ui.Muted.Render(right)
	rw := ansi.StringWidth(right)
	left = ui.FitWidth(left, m.width-rw-1, "…")
	gap := max(m.width-ansi.StringWidth(left)-rw, 1)
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) searchCount() string {
	switch {
	case m.search.err != nil:
		return ui.Err.Render("bad pattern")
	case len(m.search.matches) == 0:
		if m.search.query == "" {
			return ""
		}
		return ui.Err.Render("no match")
	case m.search.idx < 0:
		return fmt.Sprintf("%d matches", len(m.search.matches))
	}
	return fmt.Sprintf("%d/%d", m.search.idx+1, len(m.search.matches))
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d %cB", int64(v), "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGTPE"[exp])
}

func (m Model) lspBadge() string {
	if m.nav == nil || m.path == "" || m.doc == nil || m.doc.virtual {
		return ""
	}
	st := m.nav.Status(m.path)
	switch st.State {
	case ServerNone:
		return ""
	case ServerReady:
		return st.Label + " ●"
	case ServerExited, ServerFailed:
		return st.Label + " ✗"
	case ServerIdle:
		return st.Label + " ○"
	}
	return st.Label + " …"
}
