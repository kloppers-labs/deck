package viewer

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

type stubNav struct {
	mu       sync.Mutex
	status   ServerStatus
	hover    Hover
	hoverErr error
	defs     []Location
	refs     []Location
	syms     []Symbol
	doc      VirtualDoc
	calls    []string
	at       []Loc
}

func (s *stubNav) record(call string, at Loc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
	s.at = append(s.at, at)
}

func (s *stubNav) Hover(_ context.Context, _ string, at Loc) (Hover, error) {
	s.record("hover", at)
	return s.hover, s.hoverErr
}

func (s *stubNav) Definition(_ context.Context, _ string, at Loc) ([]Location, error) {
	s.record("definition", at)
	return s.defs, nil
}

func (s *stubNav) TypeDefinition(_ context.Context, _ string, at Loc) ([]Location, error) {
	s.record("typeDefinition", at)
	return s.defs, nil
}

func (s *stubNav) Implementation(_ context.Context, _ string, at Loc) ([]Location, error) {
	s.record("implementation", at)
	return s.defs, nil
}

func (s *stubNav) References(_ context.Context, _ string, at Loc) ([]Location, error) {
	s.record("references", at)
	return s.refs, nil
}

func (s *stubNav) DocumentSymbols(_ context.Context, path string) ([]Symbol, error) {
	s.record("symbols", Loc{Path: path})
	return s.syms, nil
}

func (s *stubNav) WorkspaceSymbols(_ context.Context, path, _ string) ([]Symbol, error) {
	s.record("workspace", Loc{Path: path})
	return s.syms, nil
}

func (s *stubNav) Document(_ context.Context, uri string) (VirtualDoc, error) {
	s.record("document", Loc{Path: uri})
	return s.doc, nil
}

func (s *stubNav) Status(string) ServerStatus { return s.status }

func (s *stubNav) count(call string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if c == call {
			n++
		}
	}
	return n
}

func (s *stubNav) lastAt() Loc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at[len(s.at)-1]
}

func readyNav() *stubNav {
	return &stubNav{status: ServerStatus{Server: "csharp", Label: "C#", Ext: ".cs", State: ServerReady, Percent: -1}}
}

var codeLines = []string{
	"namespace Deck.Api;",
	"public class Orders {",
	"    public int Get() { return Helper.Count(); }",
	"}",
}

func navViewer(t *testing.T, nav Navigator, lines []string, opts ...func(*Options)) Model {
	t.Helper()
	o := Options{Root: "/repo", LineNumbers: true, Navigator: nav}
	for _, f := range opts {
		f(&o)
	}
	m := sized(New(o), 80, 16)
	return deliver(t, m, docOf("/repo/src/a.cs", lines))
}

// settle runs cmd, hands viewer messages back to the viewer and returns the rest.
func settle(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	queue := collect(cmd)
	for i := 0; i < len(queue); i++ {
		if i > 50 {
			t.Fatal("commands never settled")
		}
		if _, ok := queue[i].(Msg); ok {
			var next tea.Cmd
			m, next = m.Update(queue[i])
			queue = append(queue, collect(next)...)
			continue
		}
		out = append(out, queue[i])
	}
	return m, out
}

func pressFeed(t *testing.T, m Model, keys ...string) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(keyMsg(k))
		var msgs []tea.Msg
		m, msgs = settle(t, m, cmd)
		out = append(out, msgs...)
	}
	return m, out
}

func toasts(msgs []tea.Msg) []string {
	var out []string
	for _, msg := range msgs {
		if t, ok := msg.(ui.ToastMsg); ok {
			out = append(out, t.Text)
		}
	}
	return out
}

func footerOf(m Model) string {
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	return lines[len(lines)-1]
}

func TestNilNavigatorDegrades(t *testing.T) {
	m := navViewer(t, nil, codeLines)
	m, msgs := pressFeed(t, m, "g", "d")
	if got := toasts(msgs); len(got) != 1 || got[0] != "no language server for .cs" {
		t.Fatalf("first gd toasts = %q, want one hint", got)
	}
	m, msgs = pressFeed(t, m, "g", "d")
	if got := toasts(msgs); len(got) != 0 {
		t.Errorf("the hint should toast once, got %q", got)
	}
	if !strings.Contains(footerOf(m), "no language server for .cs") {
		t.Errorf("repeat requests should show the hint in the footer, got %q", footerOf(m))
	}
	m, _ = pressFeed(t, m, "j", "j", "K")
	if m.cur.Line != 0 || m.hover.open {
		t.Errorf("K without a server should still scroll up, cur = %v", m.cur)
	}
	m, _ = pressFeed(t, m, "g", "r", "g", "i", "g", "y", "o", "f12")
	for _, b := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonRight} {
		m, _ = m.Update(tea.MouseMsg{X: 10, Y: 2, Action: tea.MouseActionPress, Button: b, Ctrl: true})
		m, _ = m.Update(tea.MouseMsg{X: 10, Y: 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	}
	_ = m.View()
}

func TestStaleSeqDropped(t *testing.T) {
	nav := readyNav()
	nav.hover = Hover{Markdown: "**int** Get()"}
	nav.defs = []Location{{Path: "/repo/src/b.cs"}}
	nav.refs = []Location{{Path: "/repo/src/b.cs"}, {Path: "/repo/src/c.cs"}}
	nav.syms = []Symbol{{Name: "Orders", Kind: SymbolClass, Range: Range{Pos{1, 0}, Pos{3, 1}}, Parent: -1}}

	m := navViewer(t, nav, codeLines)
	m = press(t, m, "j", "j")

	_, first := m.Update(keyMsg("K"))
	m, second := m.Update(keyMsg("K"))
	stale, fresh := collect(first)[0].(HoverMsg), collect(second)[0].(HoverMsg)
	if m, _ = m.Update(stale); m.hover.open {
		t.Fatal("a superseded hover must be dropped")
	}
	if m, _ = m.Update(fresh); !m.hover.open {
		t.Fatal("the latest hover should open")
	}

	for _, keys := range [][]string{{"g", "d"}, {"g", "r"}} {
		m, _ = m.Update(keyMsg(keys[0]))
		m, first = m.Update(keyMsg(keys[1]))
		m, _ = m.Update(keyMsg(keys[0]))
		m, second = m.Update(keyMsg(keys[1]))
		var cmd tea.Cmd
		if m, cmd = m.Update(collect(first)[0]); cmd != nil {
			t.Errorf("%s: a superseded result must be dropped", strings.Join(keys, ""))
		}
		if m, cmd = m.Update(collect(second)[0]); len(collect(cmd)) != 1 {
			t.Errorf("%s: the latest result should be acted on", strings.Join(keys, ""))
		}
	}

	m, _ = m.Reload()
	m, _ = m.Update(DocLoadedMsg{Path: m.path, Gen: m.gen, Doc: m.doc})
	staleSyms := SymbolsMsg{Seq: m.reqs[navSymbols] - 1, Path: m.path, Gen: m.gen, Symbols: nav.syms}
	if m, _ = m.Update(staleSyms); m.symGen == m.gen {
		t.Error("superseded symbols must be dropped")
	}
	m, _ = m.Update(SymbolsMsg{Seq: m.reqs[navSymbols], Path: m.path, Gen: m.gen, Symbols: nav.syms})
	if m.symGen != m.gen || len(m.symbols) != 1 {
		t.Error("current symbols should be kept")
	}
}

func TestGdSingleResultEmitsGoto(t *testing.T) {
	nav := readyNav()
	nav.defs = []Location{{Path: "/repo/src/helper.cs", Range: Range{Pos{4, 2}, Pos{4, 7}}}}
	m := navViewer(t, nav, codeLines)
	m = press(t, m, "j", "j")
	_, msgs := pressFeed(t, m, "g", "d")
	if len(msgs) != 1 {
		t.Fatalf("gd should emit one message, got %#v", msgs)
	}
	g, ok := msgs[0].(GotoMsg)
	if !ok || g.To.Path != "/repo/src/helper.cs" || g.From != (Loc{Path: "/repo/src/a.cs", Line: 2}) {
		t.Errorf("gd = %#v, want GotoMsg from a.cs:2 to helper.cs", msgs[0])
	}
	nav.defs = append(nav.defs, Location{Path: "/repo/src/other.cs"})
	_, msgs = pressFeed(t, m, "g", "d")
	if c, ok := msgs[0].(ChooseMsg); !ok || len(c.Locs) != 2 || c.Kind != NavDefinition {
		t.Errorf("several definitions should ask for a choice, got %#v", msgs[0])
	}
	_, msgs = pressFeed(t, m, "g", "i")
	if nav.count("implementation") != 1 || len(msgs) != 1 {
		t.Errorf("gi should request the implementation")
	}
	_, _ = pressFeed(t, m, "g", "y")
	if nav.count("typeDefinition") != 1 {
		t.Errorf("gy should request the type definition")
	}
	_, msgs = pressFeed(t, m, "f12")
	if nav.count("definition") != 3 || len(msgs) != 1 {
		t.Errorf("F12 should request the definition")
	}
}

func TestCtrlClickRequestsAtClickedCell(t *testing.T) {
	nav := readyNav()
	nav.defs = []Location{{Path: "/repo/src/helper.cs"}}
	m := navViewer(t, nav, codeLines)
	g := m.gutterWidth()
	col := strings.Index(codeLines[2], "Count")
	m, cmd := m.Update(tea.MouseMsg{X: g + col + 1, Y: 1 + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Ctrl: true})
	_, msgs := settle(t, m, cmd)
	if nav.count("definition") != 1 {
		t.Fatal("ctrl+click should request the definition")
	}
	if at := nav.lastAt(); at != (Loc{Path: "/repo/src/a.cs", Line: 2, Col: col + 1}) {
		t.Errorf("requested at %+v, want line 2 col %d", at, col+1)
	}
	if len(msgs) != 1 {
		t.Errorf("ctrl+click should end in a jump, got %#v", msgs)
	}
	_, cmd = m.Update(tea.MouseMsg{X: g + col, Y: 1 + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Alt: true})
	settle(t, m, cmd)
	if nav.count("references") != 1 {
		t.Error("alt+click should request references")
	}
}

func TestDoubleClickAndRightClickPerConfig(t *testing.T) {
	click := func(m Model, b tea.MouseButton) Model {
		m, cmd := m.Update(tea.MouseMsg{X: m.gutterWidth() + 2, Y: 2, Action: tea.MouseActionPress, Button: b})
		m, _ = settle(t, m, cmd)
		return m
	}
	for _, c := range []struct {
		gestures []string
		want     int
	}{
		{nil, 2},
		{[]string{"ctrl", "double", "right"}, 2},
		{[]string{"ctrl"}, 0},
		{[]string{"double"}, 1},
	} {
		nav := readyNav()
		nav.defs = []Location{{Path: "/repo/src/b.cs"}}
		m := navViewer(t, nav, codeLines, func(o *Options) { o.GotoClick = c.gestures })
		m = click(m, tea.MouseButtonLeft)
		m = click(m, tea.MouseButtonLeft)
		m.clicks = ui.ClickTracker{}
		click(m, tea.MouseButtonRight)
		if got := nav.count("definition"); got != c.want {
			t.Errorf("goto_click %v: %d definition requests, want %d", c.gestures, got, c.want)
		}
	}
}

func TestIndexingMessageOnEmpty(t *testing.T) {
	nav := readyNav()
	nav.status.State = ServerIndexing
	nav.status.Project = "Foo.sln"
	nav.status.Since = time.Now().Add(-23 * time.Second)
	m := navViewer(t, nav, codeLines)
	_, msgs := pressFeed(t, m, "g", "d")
	got := toasts(msgs)
	if len(got) != 1 || !strings.Contains(got[0], "C#: still loading Foo.sln (23s)") || !strings.Contains(got[0], "results may be incomplete") {
		t.Errorf("empty result while indexing = %q", got)
	}
	nav.status.State = ServerReady
	m, msgs = pressFeed(t, m, "g", "r")
	if len(toasts(msgs)) != 0 || !strings.Contains(footerOf(m), "no references found") {
		t.Errorf("empty result when ready should say so in the footer, got %q", footerOf(m))
	}
}

func TestKShowsHover(t *testing.T) {
	nav := readyNav()
	nav.hover = Hover{Markdown: "```csharp\nint Helper.Count()\n```\nCounts things."}
	m := navViewer(t, nav, codeLines)
	m = press(t, m, "j", "j", "w", "w", "w")
	m, _ = pressFeed(t, m, "K")
	if !m.hover.open {
		t.Fatal("K should open the hover")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Counts things.") || !strings.Contains(view, "int Helper.Count()") {
		t.Errorf("hover popup missing from view:\n%s", view)
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) != 16 {
		t.Errorf("view height = %d, want 16", len(lines))
	}
	if !strings.Contains(m.View(), underlineSGR) {
		t.Error("the hover target should be underlined")
	}
	if m.Capturing() {
		t.Error("an open hover should not capture typing")
	}
}

func TestDwellOnlyOnWordChange(t *testing.T) {
	nav := readyNav()
	m := navViewer(t, nav, codeLines)
	g := m.gutterWidth()
	col := strings.Index(codeLines[2], "Helper")
	motion := func(x int) tea.MouseMsg {
		return tea.MouseMsg{X: g + x, Y: 3, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}
	}
	m, cmd := m.Update(motion(col))
	if cmd == nil {
		t.Fatal("moving onto a word should start the dwell timer")
	}
	seq := m.dwellAt.seq
	if m, cmd = m.Update(motion(col + 2)); cmd != nil || m.dwellAt.seq != seq {
		t.Error("moving within the same word must not restart the timer")
	}
	m, cmd = m.Update(motion(col + len("Helper.")))
	if cmd == nil || m.dwellAt.seq == seq {
		t.Fatal("moving onto another word should restart the timer")
	}
	if _, cmd = m.Update(hoverDwellMsg{seq: seq}); cmd != nil {
		t.Error("a stale dwell tick must not request a hover")
	}
	_, cmd = m.Update(hoverDwellMsg{seq: m.dwellAt.seq})
	collect(cmd)
	if nav.count("hover") != 1 || nav.lastAt().Col != col+len("Helper.") {
		t.Errorf("a due dwell should request hover at the word, calls %v", nav.calls)
	}
}

func openHover(t *testing.T, nav *stubNav) Model {
	t.Helper()
	m := navViewer(t, nav, codeLines)
	m = press(t, m, "j", "j", "w", "w", "w")
	m, _ = pressFeed(t, m, "K")
	if !m.hover.open {
		t.Fatal("hover did not open")
	}
	return m
}

func TestHoverClosesOnMoveEscAndLeave(t *testing.T) {
	nav := readyNav()
	nav.hover = Hover{Markdown: "doc"}
	if m := press(t, openHover(t, nav), "j"); m.hover.open || m.cur.Line != 3 {
		t.Error("a move should close the hover and still move")
	}
	m := press(t, openHover(t, nav), "esc")
	if m.hover.open || m.cur.Line != 2 {
		t.Error("esc should close the hover only")
	}
	m = openHover(t, nav)
	if !m.WantsEsc() {
		t.Error("esc belongs to the viewer while the hover is open")
	}
	m, cmd := m.Update(tea.MouseMsg{X: m.gutterWidth() + 1, Y: 1, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if cmd == nil || m.hover.leave == 0 {
		t.Fatal("leaving the target should start the close timer")
	}
	m, _ = m.Update(hoverLeaveMsg{seq: m.hover.leave})
	if m.hover.open {
		t.Error("the hover should close once the mouse has been away")
	}
	m = openHover(t, nav)
	m, _ = m.Update(tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.hover.open {
		t.Error("scrolling should close the hover")
	}
}

func TestHoverPopupScrolls(t *testing.T) {
	nav := readyNav()
	var md []string
	for i := range 40 {
		md = append(md, "para "+string(rune('A'+i%26)), "")
	}
	nav.hover = Hover{Markdown: strings.Join(md, "\n")}
	m := openHover(t, nav)
	if m.hoverRows() != hoverMaxRows {
		t.Fatalf("hover rows = %d, want the %d-row cap", m.hoverRows(), hoverMaxRows)
	}
	if r, _ := m.hoverRect(); r.H > 16 || r.W > hoverMaxWidth {
		t.Errorf("hover box %+v exceeds 80x16", r)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if m.hover.top != 1 || !m.hover.open {
		t.Fatalf("ctrl+j should scroll the hover, top = %d", m.hover.top)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.hover.top != 0 {
		t.Errorf("ctrl+k should scroll back, top = %d", m.hover.top)
	}
	r, _ := m.hoverRect()
	m, _ = m.Update(tea.MouseMsg{X: r.X + 1, Y: r.Y + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.hover.top != wheelHoverRows || !m.hover.open {
		t.Errorf("the wheel over the popup should scroll it, top = %d", m.hover.top)
	}
}

func TestHoverMarkdownRenderedCached(t *testing.T) {
	calls := 0
	prev := hoverRender
	hoverRender = func(source string, width int, theme string) string {
		calls++
		return "  \n  rendered " + source + "   \n  \n"
	}
	t.Cleanup(func() { hoverRender = prev })
	nav := readyNav()
	nav.hover = Hover{Markdown: "cached-hover-text"}
	m := openHover(t, nav)
	m, _ = pressFeed(t, m, "esc", "K")
	if calls != 1 {
		t.Errorf("rendered %d times, want 1", calls)
	}
	if got := m.hover.lines; len(got) != 1 || got[0] != "rendered cached-hover-text" {
		t.Errorf("hover lines = %q, want margins trimmed", got)
	}
}

func TestBreadcrumbNesting(t *testing.T) {
	syms := []Symbol{
		{Name: "Deck.Api", Kind: SymbolNamespace, Range: Range{Pos{0, 0}, Pos{20, 1}}, Parent: -1},
		{Name: "Orders", Kind: SymbolClass, Range: Range{Pos{2, 0}, Pos{10, 1}}, Depth: 1, Parent: 0},
		{Name: "Get", Kind: SymbolMethod, Range: Range{Pos{3, 4}, Pos{5, 5}}, Depth: 2, Parent: 1},
		{Name: "Put", Kind: SymbolMethod, Range: Range{Pos{6, 4}, Pos{8, 5}}, Depth: 2, Parent: 1},
		{Name: "Other", Kind: SymbolClass, Range: Range{Pos{12, 0}, Pos{18, 1}}, Depth: 1, Parent: 0},
	}
	for _, c := range []struct {
		at   Pos
		want string
	}{
		{Pos{4, 8}, "Deck.Api › Orders › Get"},
		{Pos{6, 0}, "Deck.Api › Orders"},
		{Pos{7, 6}, "Deck.Api › Orders › Put"},
		{Pos{13, 0}, "Deck.Api › Other"},
		{Pos{1, 0}, "Deck.Api"},
		{Pos{30, 0}, ""},
	} {
		if got := strings.Join(Breadcrumb(syms, c.at), " › "); got != c.want {
			t.Errorf("Breadcrumb(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

func TestBreadcrumbInHeader(t *testing.T) {
	nav := readyNav()
	nav.syms = []Symbol{
		{Name: "Orders", Kind: SymbolClass, Range: Range{Pos{1, 0}, Pos{3, 1}}, Parent: -1},
		{Name: "Get", Kind: SymbolMethod, Range: Range{Pos{2, 4}, Pos{2, 40}}, Depth: 1, Parent: 0},
	}
	m := sized(New(Options{Root: "/repo", LineNumbers: true, Navigator: nav}), 80, 16)
	m, _ = m.Open("/repo/src/a.cs", nil)
	m, cmd := m.Update(DocLoadedMsg{Path: "/repo/src/a.cs", Gen: m.gen, Doc: docOf("/repo/src/a.cs", codeLines)})
	m, _ = settle(t, m, cmd)
	m = press(t, m, "j", "j", "w")
	if h := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(h, "a.cs › Orders › Get") {
		t.Errorf("header = %q, want the breadcrumb", h)
	}
}

func TestOutlineMarkdownFallback(t *testing.T) {
	md := []string{"# Title", "intro", "```", "# not a heading", "```", "## Install", "### From source", "## Usage"}
	syms := MarkdownOutline(md)
	var got []string
	for _, s := range syms {
		got = append(got, SymbolLabel(s))
	}
	want := []string{"# Title", "  # Install", "    # From source", "  # Usage"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("outline = %q, want %q", got, want)
	}
	if syms[1].Range.End.Line != 6 || syms[2].Parent != 1 || syms[3].Selection.Start.Line != 7 {
		t.Errorf("outline ranges/parents wrong: %+v", syms)
	}
	m := sized(New(Options{Root: "/repo"}), 80, 16)
	m = deliver(t, m, docOf("/repo/README.md", md))
	_, msgs := pressFeed(t, m, "o")
	if len(msgs) != 1 {
		t.Fatalf("o should emit an outline, got %#v", msgs)
	}
	if o, ok := msgs[0].(OutlineMsg); !ok || !o.Markdown || len(o.Symbols) != 4 {
		t.Errorf("markdown outline = %#v", msgs[0])
	}
}

func TestOutlineUsesDocumentSymbols(t *testing.T) {
	nav := readyNav()
	nav.syms = []Symbol{{Name: "Orders", Kind: SymbolClass, Parent: -1}}
	m := navViewer(t, nav, codeLines)
	_, msgs := pressFeed(t, m, "o")
	if len(msgs) != 1 || nav.count("symbols") != 1 {
		t.Fatalf("o should request symbols once and emit the outline, got %#v", msgs)
	}
	if o, ok := msgs[0].(OutlineMsg); !ok || o.Markdown || o.Symbols[0].Name != "Orders" {
		t.Errorf("outline = %#v", msgs[0])
	}
}

func TestDiagnosticsGutterAndJump(t *testing.T) {
	m := viewerWith(t, numbered(40), 60, 20)
	m.SetDiagnostics(m.path, []Diagnostic{
		{Range: Range{Pos{25, 2}, Pos{25, 4}}, Severity: SeverityWarning, Message: "unused"},
		{Range: Range{Pos{5, 1}, Pos{5, 3}}, Severity: SeverityError, Message: "boom"},
	})
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[1+5], "6 ●") {
		t.Errorf("error marker missing from the gutter: %q", rows[1+5])
	}
	m = press(t, m, "]", "d")
	if m.cur != (Pos{5, 1}) {
		t.Errorf("]d = %v, want {5 1}", m.cur)
	}
	m = press(t, m, "]", "d")
	if m.cur != (Pos{25, 2}) {
		t.Errorf("second ]d = %v, want {25 2}", m.cur)
	}
	if !strings.Contains(ansi.Strip(m.View()), "26 ▲") {
		t.Error("warning marker missing from the gutter")
	}
	m = press(t, m, "]", "d")
	if m.cur != (Pos{5, 1}) {
		t.Errorf("]d should wrap, got %v", m.cur)
	}
	m = press(t, m, "[", "d")
	if m.cur != (Pos{25, 2}) {
		t.Errorf("[d should wrap backwards, got %v", m.cur)
	}
	m.SetDiagnostics(m.path, nil)
	if m = press(t, m, "]", "d"); !strings.Contains(footerOf(m), "no diagnostics") {
		t.Error("]d without diagnostics should say so")
	}
}

func TestFooterShowsDiagnosticOnLine(t *testing.T) {
	m := viewerWith(t, numbered(40), 80, 20)
	m.SetDiagnostics(m.path, []Diagnostic{{Range: Range{Pos{3, 0}, Pos{3, 2}}, Severity: SeverityError, Source: "CS0103", Message: "The name 'x' does not exist\nmore"}})
	if strings.Contains(footerOf(m), "does not exist") {
		t.Error("the diagnostic should only show on its line")
	}
	m = press(t, m, "j", "j", "j")
	if f := footerOf(m); !strings.Contains(f, "● CS0103: The name 'x' does not exist") || strings.Contains(f, "more") {
		t.Errorf("footer = %q, want the first line of the diagnostic", f)
	}
}

func TestLongJumpsReported(t *testing.T) {
	m := viewerWith(t, numbered(200), 60, 12)
	_, msgs := pressFeed(t, m, "G")
	if len(msgs) != 1 || msgs[0].(JumpedMsg).From != (Loc{Path: "/repo/src/a.cs"}) {
		t.Errorf("G should report the jump origin, got %#v", msgs)
	}
	m, msgs = pressFeed(t, m, "j")
	if len(msgs) != 0 {
		t.Error("short moves are not jumps")
	}
	m, _ = pressFeed(t, m, ":")
	_, msgs = pressFeed(t, typeText(t, m, "150"), "enter")
	if len(msgs) != 1 || msgs[0].(JumpedMsg).From.Line != 1 {
		t.Errorf(":150 should report the jump origin, got %#v", msgs)
	}
}

func TestOpenURIShowsVirtualDocument(t *testing.T) {
	nav := readyNav()
	nav.doc = VirtualDoc{Title: "[metadata] System.String", Text: "namespace System;\npublic sealed class String {}\n"}
	m := sized(New(Options{Root: "/repo", LineNumbers: true, Navigator: nav}), 80, 16)
	uri := "csharp:/metadata/projects/Deck/assemblies/System.Runtime/symbols/System.String.cs"
	m, cmd := m.OpenURI(uri, &Spot{Line: 1, Col: 20, Top: -1})
	m, _ = settle(t, m, cmd)
	if m.cur != (Pos{1, 20}) || !m.Loaded() {
		t.Fatalf("virtual doc not shown at its spot: cur %v", m.cur)
	}
	if h := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(h, "[metadata] System.String") {
		t.Errorf("header = %q, want the metadata title", h)
	}
	if m.doc.Lang != "C#" {
		t.Errorf("Lang = %q, want C#", m.doc.Lang)
	}
	m, msgs := pressFeed(t, m, "e")
	if len(msgs) != 0 || !strings.Contains(footerOf(m), "read-only") {
		t.Error("e on a virtual document should not launch the editor")
	}
}

func TestBreadcrumbSkipsFileSymbol(t *testing.T) {
	syms := []Symbol{
		{Name: "$metadata$/assemblies/Microsoft.AspNetCore/symbols/WebApplication.cs", Kind: SymbolFile, Range: Range{Pos{0, 0}, Pos{40, 0}}, Parent: -1},
		{Name: "Builder", Kind: SymbolNamespace, Range: Range{Pos{1, 0}, Pos{39, 1}}, Depth: 1, Parent: 0},
		{Name: "WebApplication", Kind: SymbolClass, Range: Range{Pos{3, 0}, Pos{38, 1}}, Depth: 2, Parent: 1},
	}
	if got := strings.Join(Breadcrumb(syms, Pos{5, 2}), " › "); got != "Builder › WebApplication" {
		t.Errorf("Breadcrumb = %q, want the file-level symbol left out", got)
	}
	if got := Breadcrumb(syms[:1], Pos{5, 2}); len(got) != 0 {
		t.Errorf("Breadcrumb inside only the file symbol = %q, want empty", got)
	}
}
