package viewer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
)

const defaultRequestTimeout = 8 * time.Second

// NavKind names a location request.
type NavKind int

const (
	NavDefinition NavKind = iota
	NavTypeDefinition
	NavImplementation
	NavReferences
	navHover
	navSymbols
	numNavKinds
)

func (k NavKind) String() string {
	switch k {
	case NavTypeDefinition:
		return "type definition"
	case NavImplementation:
		return "implementation"
	case NavReferences:
		return "references"
	case navHover:
		return "hover"
	case navSymbols:
		return "symbols"
	}
	return "definition"
}

var navSeq atomic.Int64

// LocationsMsg carries the result of a definition-style request.
type LocationsMsg struct {
	Kind NavKind
	Seq  int64
	Path string
	At   Pos
	Locs []Location
	Err  error
}

// HoverMsg carries hover text, already rendered to Lines.
type HoverMsg struct {
	Seq   int64
	Path  string
	At    Pos
	Mouse bool
	Hover Hover
	Lines []string
	Err   error
}

// SymbolsMsg carries the document symbols for the load generation Gen.
type SymbolsMsg struct {
	Seq     int64
	Path    string
	Gen     int
	Symbols []Symbol
	Err     error
}

// GotoMsg asks the container to record From and show To.
type GotoMsg struct {
	From Loc
	To   Location
}

// ChooseMsg asks the container to offer Locs in a picker.
type ChooseMsg struct {
	Kind NavKind
	From Loc
	Locs []Location
}

// OutlineMsg asks the container to offer the document's symbols in a picker.
// Markdown outlines refer to raw source lines.
type OutlineMsg struct {
	From     Loc
	Symbols  []Symbol
	Markdown bool
}

// JumpedMsg reports a long in-file jump, so the container can record From.
type JumpedMsg struct{ From Loc }

func (LocationsMsg) RouteTo() ui.Dest { return ui.DestFiles }
func (HoverMsg) RouteTo() ui.Dest     { return ui.DestFiles }
func (SymbolsMsg) RouteTo() ui.Dest   { return ui.DestFiles }
func (GotoMsg) RouteTo() ui.Dest      { return ui.DestFiles }
func (ChooseMsg) RouteTo() ui.Dest    { return ui.DestFiles }
func (OutlineMsg) RouteTo() ui.Dest   { return ui.DestFiles }
func (JumpedMsg) RouteTo() ui.Dest    { return ui.DestFiles }

func (LocationsMsg) viewerMsg() {}
func (HoverMsg) viewerMsg()     {}
func (SymbolsMsg) viewerMsg()   {}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// SetNavigator sets the language-server view; nil turns LSP features off.
func (m *Model) SetNavigator(n Navigator) { m.nav = n }

func (m Model) timeout() time.Duration {
	if m.opts.RequestTimeout > 0 {
		return m.opts.RequestTimeout
	}
	return defaultRequestTimeout
}

func (m *Model) nextSeq(k NavKind) int64 {
	s := navSeq.Add(1)
	m.reqs[k] = s
	return s
}

// Status is the language server state for the open file.
func (m Model) Status() ServerStatus {
	if m.nav == nil || m.path == "" {
		return ServerStatus{Ext: extOf(m.path)}
	}
	return m.nav.Status(m.path)
}

func (m Model) lspDoc() bool {
	d := m.doc
	return d != nil && !d.Binary && !d.Truncated && !d.virtual && !m.showingMD()
}

// navReady reports whether a request may be sent; otherwise it returns the
// hint to show, which is nil when quiet.
func (m *Model) navReady(quiet bool) (bool, tea.Cmd) {
	if !m.lspDoc() {
		return false, nil
	}
	st := m.Status()
	if m.nav != nil && st.Usable() {
		return true, nil
	}
	if quiet {
		return false, nil
	}
	return false, m.hint(st.NoServerText())
}

// hint toasts text the first time and shows it in the footer afterwards.
func (m *Model) hint(text string) tea.Cmd {
	if m.hints[text] {
		m.flash = text
		return nil
	}
	m.hints[text] = true
	return ui.Toast(ui.LevelWarn, text)
}

func (m *Model) navigate(kind NavKind, p Pos, quiet bool) tea.Cmd {
	ok, cmd := m.navReady(quiet)
	if !ok {
		return cmd
	}
	m.closeHover()
	seq := m.nextSeq(kind)
	nav, path, timeout := m.nav, m.path, m.timeout()
	at := Loc{Path: path, Line: p.Line, Col: p.Col}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var locs []Location
		var err error
		switch kind {
		case NavDefinition:
			locs, err = nav.Definition(ctx, path, at)
		case NavTypeDefinition:
			locs, err = nav.TypeDefinition(ctx, path, at)
		case NavImplementation:
			locs, err = nav.Implementation(ctx, path, at)
		case NavReferences:
			locs, err = nav.References(ctx, path, at)
		}
		return LocationsMsg{Kind: kind, Seq: seq, Path: path, At: p, Locs: locs, Err: err}
	}
}

func (m *Model) located(msg LocationsMsg) tea.Cmd {
	if msg.Kind < 0 || msg.Kind >= numNavKinds || msg.Seq != m.reqs[msg.Kind] || msg.Path != m.path {
		return nil
	}
	m.reqs[msg.Kind] = 0
	if msg.Err != nil {
		return m.navFailed(msg.Kind.String(), msg.Err)
	}
	if len(msg.Locs) == 0 {
		return m.nothingFound(msg.Kind.String())
	}
	from := Loc{Path: msg.Path, Line: msg.At.Line, Col: msg.At.Col}
	if msg.Kind != NavReferences && len(msg.Locs) == 1 {
		return emit(GotoMsg{From: from, To: msg.Locs[0]})
	}
	return emit(ChooseMsg{Kind: msg.Kind, From: from, Locs: slices.Clone(msg.Locs)})
}

func (m *Model) navFailed(what string, err error) tea.Cmd {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if st := m.Status(); !st.Usable() {
		return m.hint(st.NoServerText())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		m.flash = what + " timed out"
		return nil
	}
	m.flash = what + ": " + firstLine(err.Error())
	return nil
}

func (m *Model) nothingFound(what string) tea.Cmd {
	if st := m.Status(); st.Loading() {
		return ui.Toast(ui.LevelInfo, st.LoadingText(m.now()))
	}
	m.flash = "no " + what + " found"
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func (m *Model) requestSymbols() tea.Cmd {
	if m.nav == nil || !m.lspDoc() || !m.Status().Usable() {
		return nil
	}
	seq := m.nextSeq(navSymbols)
	nav, path, gen, timeout := m.nav, m.path, m.gen, m.timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		syms, err := nav.DocumentSymbols(ctx, path)
		return SymbolsMsg{Seq: seq, Path: path, Gen: gen, Symbols: syms, Err: err}
	}
}

func (m *Model) gotSymbols(msg SymbolsMsg) tea.Cmd {
	if msg.Seq != m.reqs[navSymbols] || msg.Path != m.path || msg.Gen != m.gen {
		return nil
	}
	m.reqs[navSymbols] = 0
	if msg.Err == nil {
		m.symbols, m.symGen = msg.Symbols, msg.Gen
	}
	if !m.outline {
		return nil
	}
	m.outline = false
	switch {
	case msg.Err != nil:
		return m.navFailed("outline", msg.Err)
	case len(msg.Symbols) == 0:
		return m.nothingFound("symbols")
	}
	return emit(OutlineMsg{From: m.Loc(), Symbols: msg.Symbols})
}

func (m *Model) openOutline() tea.Cmd {
	if m.doc == nil || m.doc.Binary || m.doc.virtual {
		return nil
	}
	if m.doc.Markdown {
		syms := MarkdownOutline(m.doc.Raw)
		if len(syms) == 0 {
			m.flash = "no headings"
			return nil
		}
		return emit(OutlineMsg{From: m.Loc(), Symbols: syms, Markdown: true})
	}
	if ok, cmd := m.navReady(false); !ok {
		return cmd
	}
	if m.gen != 0 && m.symGen == m.gen {
		return emit(OutlineMsg{From: m.Loc(), Symbols: m.symbols})
	}
	m.outline = true
	return m.requestSymbols()
}

func (m Model) breadcrumb() []string {
	if m.gen == 0 || m.symGen != m.gen || m.showingMD() {
		return nil
	}
	return Breadcrumb(m.symbols, m.cur)
}

// OpenURI shows the read-only document the language server serves for uri.
func (m Model) OpenURI(uri string, at *Spot) (Model, tea.Cmd) {
	nav, tw, timeout := m.nav, m.opts.TabWidth, m.timeout()
	return m.OpenVirtual(uri, at, func() (*Document, error) {
		if nav == nil {
			return nil, errors.New("no language server")
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		vd, err := nav.Document(ctx, uri)
		if err != nil {
			return nil, err
		}
		d := newDocument(uri, []byte(vd.Text), tw)
		d.Title = vd.Title
		return d, nil
	})
}

// JumpTo moves the cursor within the open document; a negative Top centres it.
func (m Model) JumpTo(s Spot) Model {
	if m.doc == nil {
		m.pending = &s
		return m
	}
	m.closeHover()
	m.moveTo(Pos{s.Line, s.Col})
	if s.Top < 0 {
		m.center()
	} else {
		m.setTop(s.Top)
	}
	m.ensureVisible()
	m.relayout()
	return m
}

func (m Model) jumpedFrom(from Loc) tea.Cmd {
	if from.Path == "" || abs(m.cur.Line-from.Line) <= m.bodyHeight() {
		return nil
	}
	return emit(JumpedMsg{From: from})
}
