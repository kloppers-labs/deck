package files

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/ui/picker"
	"github.com/kloppers-labs/deck/internal/viewer"
)

const (
	pickNav     = "nav"
	pickOutline = "outline"
	pickSymbols = "symbols"
	pickLog     = "log"

	wsTimeout = 8 * time.Second
)

var wsDebounce = 150 * time.Millisecond

type wsState struct {
	seq   int
	query string
}

type wsDueMsg struct{ seq int }

type wsResultMsg struct {
	seq     int
	symbols []viewer.Symbol
	err     error
}

func (wsDueMsg) RouteTo() ui.Dest    { return ui.DestFiles }
func (wsResultMsg) RouteTo() ui.Dest { return ui.DestFiles }

// WithLSP connects the viewer to the language servers; nil leaves LSP off.
func (m Model) WithLSP(l LSP) Model {
	m.nav = l
	m.viewer.SetNavigator(l)
	return m
}

func spotOf(r viewer.Range) *viewer.Spot {
	return &viewer.Spot{Line: r.Start.Line, Col: r.Start.Col, Top: -1}
}

// openAt shows path at spot: in place when it is already open, as a virtual
// document for a URI, and otherwise loaded and revealed in the tree.
func (m Model) openAt(path string, spot *viewer.Spot) (Model, tea.Cmd) {
	inPlace := path == m.viewer.Path() && m.viewer.Loaded() && spot != nil
	outside := viewer.IsVirtual(path) || !virtualOrUnder(path, m.tree.Launch())
	switch {
	case inPlace && (outside || path == m.SelectedPath()):
		m.viewer = m.viewer.JumpTo(*spot)
		m.setFocus(focusViewer)
		return m, nil
	case viewer.IsVirtual(path):
		m.cardPath = ""
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.OpenURI(path, spot)
		m.setFocus(focusViewer)
		return m, cmd
	case outside:
		m.cardPath = ""
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Open(path, spot)
		m.setFocus(focusViewer)
		return m, cmd
	}
	return m.show(path, spot)
}

func (m Model) gotoTarget(msg viewer.GotoMsg) (Model, tea.Cmd) {
	if msg.From.Path != "" {
		m.history.Push(msg.From)
	}
	return m.openAt(msg.To.Path, spotOf(msg.To.Range))
}

func (m Model) back() (Model, tea.Cmd) {
	loc, ok := m.history.Back(m.viewer.Loc())
	if !ok {
		return m, nil
	}
	return m.openAt(loc.Path, &viewer.Spot{Line: loc.Line, Col: loc.Col, Top: -1})
}

func (m Model) forward() (Model, tea.Cmd) {
	loc, ok := m.history.Forward()
	if !ok {
		return m, nil
	}
	return m.openAt(loc.Path, &viewer.Spot{Line: loc.Line, Col: loc.Col, Top: -1})
}

func (m Model) displayPath(path string) string {
	if viewer.IsVirtual(path) {
		return "[metadata] " + strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if rel, err := filepath.Rel(m.tree.Launch(), path); err == nil && filepath.IsLocal(rel) {
		return rel
	}
	return path
}

// locationItems groups locations by file, the current file first.
func (m Model) locationItems(locs []viewer.Location, current string) []picker.Item {
	locs = append([]viewer.Location(nil), locs...)
	sort.SliceStable(locs, func(i, j int) bool {
		a, b := locs[i], locs[j]
		if (a.Path == current) != (b.Path == current) {
			return a.Path == current
		}
		if a.Path != b.Path {
			return m.displayPath(a.Path) < m.displayPath(b.Path)
		}
		if a.Range.Start.Line != b.Range.Start.Line {
			return a.Range.Start.Line < b.Range.Start.Line
		}
		return a.Range.Start.Col < b.Range.Start.Col
	})
	items := make([]picker.Item, len(locs))
	for i, l := range locs {
		rel := m.displayPath(l.Path)
		line, col := l.Range.Start.Line+1, l.Range.Start.Col
		items[i] = picker.Item{
			Title: rel + ":" + strconv.Itoa(line) + ":" + strconv.Itoa(col+1) + "  " + l.Text,
			Path:  l.Path, Line: line, Col: col, Group: rel,
		}
	}
	return items
}

func (m Model) choose(msg viewer.ChooseMsg) (Model, tea.Cmd) {
	items := m.locationItems(msg.Locs, msg.From.Path)
	title := msg.Kind.String()
	if msg.Kind == viewer.NavReferences {
		title = "references (" + strconv.Itoa(len(items)) + ")"
	}
	m.openPicker(picker.Options{ID: pickNav, Dest: ui.DestFiles, Title: title, Placeholder: "filter"})
	m.origin.loc = msg.From
	return m, m.pick.SetItems(items)
}

func (m Model) outline(msg viewer.OutlineMsg) (Model, tea.Cmd) {
	path := m.viewer.Path()
	items := make([]picker.Item, len(msg.Symbols))
	for i, s := range msg.Symbols {
		items[i] = picker.Item{
			Title: viewer.SymbolLabel(s), Detail: s.Detail, Path: path,
			Line: s.Selection.Start.Line + 1, Col: s.Selection.Start.Col,
		}
	}
	m.openPicker(picker.Options{ID: pickOutline, Dest: ui.DestFiles, Title: "outline", Placeholder: "filter symbols"})
	if msg.Markdown && !m.viewer.RawMarkdown() {
		m.origin.markdown = true
		m.viewer = m.viewer.SetRawMarkdown(true)
		m.origin.peeked = true
	}
	return m, m.pick.SetItems(items)
}

func (m Model) openSymbols() (Model, tea.Cmd) {
	if m.nav == nil {
		return m, ui.Toast(ui.LevelWarn, "no language server")
	}
	m.openPicker(picker.Options{ID: pickSymbols, Dest: ui.DestFiles, Title: "workspace symbols", Placeholder: "type to search symbols", Live: true})
	m.ws = wsState{seq: m.ws.seq + 1}
	return m, nil
}

func (m Model) wsQuery(query string) (Model, tea.Cmd) {
	m.ws.seq++
	m.ws.query = query
	if strings.TrimSpace(query) == "" {
		m.pick.SetStatus("")
		return m, m.pick.SetItems(nil)
	}
	seq := m.ws.seq
	return m, tea.Tick(wsDebounce, func(time.Time) tea.Msg { return wsDueMsg{seq: seq} })
}

func (m Model) wsDue(msg wsDueMsg) (Model, tea.Cmd) {
	if msg.seq != m.ws.seq || !m.pick.Active() || m.pick.ID() != pickSymbols || m.nav == nil {
		return m, nil
	}
	m.pick.SetStatus("searching…")
	nav, path, query, seq := m.nav, m.viewer.Path(), m.ws.query, msg.seq
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), wsTimeout)
		defer cancel()
		syms, err := nav.WorkspaceSymbols(ctx, path, query)
		return wsResultMsg{seq: seq, symbols: syms, err: err}
	}
}

func (m Model) wsResult(msg wsResultMsg) (Model, tea.Cmd) {
	if msg.seq != m.ws.seq || !m.pick.Active() || m.pick.ID() != pickSymbols {
		return m, nil
	}
	if msg.err != nil {
		m.pick.SetStatus(ui.Err.Render(msg.err.Error()))
		return m, m.pick.SetItems(nil)
	}
	m.pick.SetStatus("")
	items := make([]picker.Item, len(msg.symbols))
	for i, s := range msg.symbols {
		detail := m.displayPath(s.Path)
		if s.Container != "" {
			detail = s.Container + " · " + detail
		}
		items[i] = picker.Item{
			Title: viewer.KindGlyph(s.Kind) + " " + s.Name, Detail: detail, Path: s.Path,
			Line: s.Selection.Start.Line + 1, Col: s.Selection.Start.Col,
		}
	}
	return m, m.pick.SetItems(items)
}

func (m Model) openLog() (Model, tea.Cmd) {
	lines := append([]string(nil), m.lspLog...)
	if m.nav != nil {
		lines = append(lines, m.nav.Logs()...)
	}
	items := make([]picker.Item, len(lines))
	for i, l := range lines {
		items[i] = picker.Item{Title: l}
	}
	m.openPicker(picker.Options{ID: pickLog, Dest: ui.DestFiles, Title: "LSP log", Placeholder: "filter"})
	if len(items) == 0 {
		m.pick.SetStatus("no language server output yet")
	}
	return m, m.pick.SetItems(items)
}

func (m Model) LSP() LSP { return m.nav }
