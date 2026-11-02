package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/ui"
	"deck/internal/ui/picker"
	"deck/internal/viewer"
)

type stubLSP struct {
	mu       sync.Mutex
	status   viewer.ServerStatus
	defs     []viewer.Location
	refs     []viewer.Location
	syms     []viewer.Symbol
	ws       []viewer.Symbol
	doc      viewer.VirtualDoc
	calls    []string
	reloaded []string
	events   []any
	logs     []string
}

func (s *stubLSP) record(c string) {
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
}

func (s *stubLSP) count(c string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, x := range s.calls {
		if x == c {
			n++
		}
	}
	return n
}

func (s *stubLSP) Hover(context.Context, string, viewer.Loc) (viewer.Hover, error) {
	s.record("hover")
	return viewer.Hover{}, nil
}

func (s *stubLSP) Definition(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	s.record("definition")
	return s.defs, nil
}

func (s *stubLSP) TypeDefinition(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	return s.defs, nil
}

func (s *stubLSP) Implementation(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	return s.defs, nil
}

func (s *stubLSP) References(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	s.record("references")
	return s.refs, nil
}

func (s *stubLSP) DocumentSymbols(context.Context, string) ([]viewer.Symbol, error) {
	s.record("symbols")
	return s.syms, nil
}

func (s *stubLSP) WorkspaceSymbols(_ context.Context, _, query string) ([]viewer.Symbol, error) {
	s.record("workspace:" + query)
	return s.ws, nil
}

func (s *stubLSP) Document(_ context.Context, uri string) (viewer.VirtualDoc, error) {
	s.record("document")
	return s.doc, nil
}

func (s *stubLSP) Status(path string) viewer.ServerStatus {
	st := s.status
	st.Ext = filepath.Ext(path)
	return st
}

func (s *stubLSP) Reload(_ context.Context, path string) error {
	s.mu.Lock()
	s.reloaded = append(s.reloaded, path)
	s.mu.Unlock()
	return nil
}

func (s *stubLSP) NextEvent() (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return nil, false
	}
	ev := s.events[0]
	s.events = s.events[1:]
	return ev, true
}

func (s *stubLSP) Logs() []string { return s.logs }

func newStubLSP() *stubLSP {
	return &stubLSP{status: viewer.ServerStatus{Server: "csharp", Label: "C#", State: viewer.ServerReady, Percent: -1}}
}

func navFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, n int) {
		var b strings.Builder
		for i := range n {
			b.WriteString("var item" + string(rune('a'+i%26)) + " = Helper.Count();\n")
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.cs", 60)
	write("lib/b.cs", 80)
	write("lib/c.cs", 20)
	return dir
}

func startedLSP(t *testing.T, dir string, nav LSP) Model {
	t.Helper()
	m := New(dir, config.Default()).WithLSP(nav)
	m.SetSize(120, 30)
	m, _ = settle(t, m, m.Init())
	return m
}

func at(path string, line, col int) viewer.Location {
	return viewer.Location{Path: path, Range: viewer.Range{Start: viewer.Pos{Line: line, Col: col}, End: viewer.Pos{Line: line, Col: col + 3}}, Text: "line text"}
}

func ctrlKey(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func send(t *testing.T, m Model, msgs ...tea.Msg) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	for _, msg := range msgs {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		var o []tea.Msg
		m, o = settle(t, m, cmd)
		out = append(out, o...)
	}
	return m, out
}

func TestGdSingleJumpPushesHistory(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	b := filepath.Join(dir, "lib", "b.cs")
	nav.defs = []viewer.Location{at(b, 40, 4)}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j")
	a := filepath.Join(dir, "a.cs")
	if m.SelectedPath() != a {
		t.Fatalf("fixture: selected %q", m.SelectedPath())
	}
	m, _ = press(t, m, "enter", "j", "j", "g", "d")
	if loc := m.viewer.Loc(); loc.Path != b || loc.Line != 40 || loc.Col != 4 {
		t.Fatalf("gd should jump to b.cs:41:5, at %+v", loc)
	}
	back, ok := m.history.Back(m.viewer.Loc())
	if !ok || back.Path != a || back.Line != 2 {
		t.Errorf("the jump origin should be in the history, got %+v %v", back, ok)
	}
}

func TestCrossFileJumpRevealsInTree(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	b := filepath.Join(dir, "lib", "b.cs")
	nav.defs = []viewer.Location{at(b, 3, 0)}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j", "enter", "g", "d")
	if m.SelectedPath() != b || m.focus != focusViewer {
		t.Errorf("the target should be revealed in the tree: selected %q", m.SelectedPath())
	}
	nav.defs = []viewer.Location{at(b, 70, 2)}
	m, _ = press(t, m, "g", "d")
	if loc := m.viewer.Loc(); loc.Path != b || loc.Line != 70 {
		t.Errorf("an in-file jump should move the cursor, at %+v", loc)
	}
}

func TestGdMultipleOpensPicker(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	nav.defs = []viewer.Location{at(filepath.Join(dir, "lib", "b.cs"), 3, 0), at(filepath.Join(dir, "lib", "c.cs"), 5, 0)}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j", "enter", "g", "d")
	if !m.pick.Active() || m.pick.ID() != pickNav || m.pick.Total() != 2 {
		t.Fatalf("several definitions should open the picker: active %v total %d", m.pick.Active(), m.pick.Total())
	}
	if !m.Capturing() {
		t.Error("the picker captures")
	}
	m, _ = press(t, m, "enter")
	if loc := m.viewer.Loc(); loc.Path != filepath.Join(dir, "lib", "b.cs") || loc.Line != 3 {
		t.Errorf("enter should jump to the selection, at %+v", loc)
	}
	if m.SelectedPath() != filepath.Join(dir, "lib", "b.cs") {
		t.Errorf("accepting a peeked file should reveal it in the tree, selected %q", m.SelectedPath())
	}
	if m.history.Len() == 0 {
		t.Error("the jump should be recorded")
	}
}

func TestMetadataURIOpensVirtualDoc(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	uri := "csharp:/metadata/projects/Deck/assemblies/System.Runtime/symbols/System.String.cs"
	nav.defs = []viewer.Location{{Path: uri, Range: viewer.Range{Start: viewer.Pos{Line: 1, Col: 20}}}}
	nav.doc = viewer.VirtualDoc{Title: "[metadata] System.String", Text: "namespace System;\npublic sealed class String {}\n"}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j", "enter")
	selected := m.SelectedPath()
	m, _ = press(t, m, "g", "d")
	if m.viewer.Path() != uri || m.viewer.Loc().Line != 1 || m.viewer.Loc().Col != 20 {
		t.Fatalf("the metadata document should open at its spot, viewer %q %+v", m.viewer.Path(), m.viewer.Loc())
	}
	if !strings.Contains(ansi.Strip(m.View()), "[metadata] System.String") {
		t.Error("the viewer header should show the metadata title")
	}
	if m.SelectedPath() != selected {
		t.Errorf("a virtual document leaves the tree alone, selected %q", m.SelectedPath())
	}
	m, _ = send(t, m, ctrlKey(tea.KeyCtrlO))
	if m.viewer.Path() != filepath.Join(dir, "a.cs") {
		t.Errorf("back should return from the metadata document, at %q", m.viewer.Path())
	}
	m, _ = send(t, m, ctrlKey(tea.KeyCtrlCloseBracket))
	if m.viewer.Path() != uri || nav.count("document") != 2 {
		t.Errorf("forward should reopen the metadata document, at %q", m.viewer.Path())
	}
}

func TestBackForwardAcrossFiles(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	a, b := filepath.Join(dir, "a.cs"), filepath.Join(dir, "lib", "b.cs")
	nav.defs = []viewer.Location{at(b, 30, 1)}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j", "enter", "j", "j", "j", "g", "d")
	if m.viewer.Path() != b {
		t.Fatalf("setup: gd should reach b.cs")
	}
	for _, c := range []struct {
		name          string
		back, forward tea.Msg
	}{
		{"keys", ctrlKey(tea.KeyCtrlO), ctrlKey(tea.KeyCtrlCloseBracket)},
		{"alt+arrows", tea.KeyMsg{Type: tea.KeyLeft, Alt: true}, tea.KeyMsg{Type: tea.KeyRight, Alt: true}},
		{"mouse", tea.MouseMsg{X: 60, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonBackward},
			tea.MouseMsg{X: 60, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonForward}},
	} {
		m, _ = send(t, m, c.back)
		if loc := m.viewer.Loc(); loc.Path != a || loc.Line != 3 {
			t.Errorf("%s: back = %+v, want a.cs line 3", c.name, loc)
		}
		m, _ = send(t, m, c.forward)
		if loc := m.viewer.Loc(); loc.Path != b || loc.Line != 30 {
			t.Errorf("%s: forward = %+v, want b.cs line 30", c.name, loc)
		}
	}
}

func TestReferencesGroupedCurrentFileFirst(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	a, b, c := filepath.Join(dir, "a.cs"), filepath.Join(dir, "lib", "b.cs"), filepath.Join(dir, "lib", "c.cs")
	nav.refs = []viewer.Location{at(c, 2, 0), at(b, 9, 4), at(a, 20, 1), at(b, 1, 0), at(a, 5, 8)}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j", "enter", "g", "r")
	if !m.pick.Active() || m.pick.Total() != 5 {
		t.Fatalf("gr should list references, active %v", m.pick.Active())
	}
	var got []string
	for i := 0; i < m.pick.Len(); i++ {
		it, _ := m.pick.At(i)
		got = append(got, strings.SplitN(it.Title, "  ", 2)[0]+"|"+it.Group)
	}
	want := []string{
		"a.cs:6:9|a.cs", "a.cs:21:2|a.cs",
		filepath.Join("lib", "b.cs") + ":2:1|" + filepath.Join("lib", "b.cs"),
		filepath.Join("lib", "b.cs") + ":10:5|" + filepath.Join("lib", "b.cs"),
		filepath.Join("lib", "c.cs") + ":3:1|" + filepath.Join("lib", "c.cs"),
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("reference rows = %v, want %v", got, want)
	}
	if it, _ := m.pick.At(0); !strings.HasSuffix(it.Title, "line text") {
		t.Errorf("rows should show the trimmed line, got %q", it.Title)
	}
}

func TestPeekAndEscRestores(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	a, c := filepath.Join(dir, "a.cs"), filepath.Join(dir, "lib", "c.cs")
	nav.refs = []viewer.Location{at(a, 50, 1), at(c, 12, 0)}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j", "enter", "j", "j", "g", "r")
	m, _ = press(t, m, "down")
	if loc := m.viewer.Loc(); loc.Path != c || loc.Line != 12 {
		t.Fatalf("moving should peek at the reference, at %+v", loc)
	}
	m, _ = press(t, m, "up")
	if loc := m.viewer.Loc(); loc.Path != a || loc.Line != 50 {
		t.Fatalf("peeking within the current file, at %+v", loc)
	}
	m, _ = press(t, m, "esc")
	if loc := m.viewer.Loc(); m.pick.Active() || loc.Path != a || loc.Line != 2 {
		t.Errorf("esc should restore the original position, at %+v", loc)
	}
	if m.history.Len() != 0 {
		t.Error("a cancelled picker records nothing")
	}
}

func TestOutlineIndentedWithGlyphs(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	nav.syms = []viewer.Symbol{
		{Name: "Deck", Kind: viewer.SymbolNamespace, Parent: -1, Range: viewer.Range{End: viewer.Pos{Line: 59}}},
		{Name: "Orders", Kind: viewer.SymbolClass, Depth: 1, Parent: 0, Selection: viewer.Range{Start: viewer.Pos{Line: 3, Col: 13}}},
		{Name: "Get", Kind: viewer.SymbolMethod, Depth: 2, Parent: 1, Detail: "int Get()", Selection: viewer.Range{Start: viewer.Pos{Line: 30, Col: 8}}},
	}
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j", "enter", "o")
	if !m.pick.Active() || m.pick.ID() != pickOutline {
		t.Fatal("o should open the outline")
	}
	var titles []string
	for i := 0; i < m.pick.Len(); i++ {
		it, _ := m.pick.At(i)
		titles = append(titles, it.Title)
	}
	if want := []string{"▣ Deck", "  ◆ Orders", "    ƒ Get"}; strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("outline rows = %q, want %q", titles, want)
	}
	m, _ = press(t, m, "down", "down", "enter")
	if loc := m.viewer.Loc(); loc.Line != 30 || loc.Col != 8 {
		t.Errorf("enter should jump to the symbol, at %+v", loc)
	}
}

func TestMarkdownOutlineShowsSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Title\n\ntext\n\n## Usage\n\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := startedLSP(t, dir, nil)
	m, _ = press(t, m, "enter", "o")
	if !m.pick.Active() || m.pick.Total() != 2 || !m.viewer.RawMarkdown() {
		t.Fatalf("o on markdown should list headings over the source, active %v raw %v", m.pick.Active(), m.viewer.RawMarkdown())
	}
	m, _ = press(t, m, "esc")
	if m.viewer.RawMarkdown() {
		t.Error("esc should return to the rendered markdown")
	}
	m, _ = press(t, m, "o", "down", "enter")
	if !m.viewer.RawMarkdown() || m.viewer.Loc().Line != 4 {
		t.Errorf("accepting should show the heading in the source, at %+v", m.viewer.Loc())
	}
}

func TestWorkspaceSymbolsDebounced(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	b := filepath.Join(dir, "lib", "b.cs")
	nav.ws = []viewer.Symbol{{Name: "OrdersController", Kind: viewer.SymbolClass, Path: b, Container: "Deck.Api", Selection: viewer.Range{Start: viewer.Pos{Line: 7, Col: 13}}}}
	m := startedLSP(t, dir, nav)
	m, _ = send(t, m, ctrlKey(tea.KeyCtrlT))
	if !m.pick.Active() || m.pick.ID() != pickSymbols {
		t.Fatal("ctrl+t should open workspace symbols")
	}
	for _, q := range []string{"O", "Or", "Ord"} {
		var cmd tea.Cmd
		m, cmd = m.Update(picker.QueryMsg{Dest: ui.DestFiles, ID: pickSymbols, Query: q})
		if cmd == nil {
			t.Fatalf("query %q should schedule a search", q)
		}
	}
	for seq := m.ws.seq - 2; seq < m.ws.seq; seq++ {
		if _, cmd := m.Update(wsDueMsg{seq: seq}); cmd != nil {
			t.Errorf("a superseded query (seq %d) must not search", seq)
		}
	}
	m, _ = send(t, m, wsDueMsg{seq: m.ws.seq})
	if nav.count("workspace:Ord") != 1 || nav.count("workspace:O") != 0 || nav.count("workspace:Or") != 0 {
		t.Errorf("only the settled query should search, calls %v", nav.calls)
	}
	if m.pick.Total() != 1 {
		t.Fatalf("results should fill the picker, total %d", m.pick.Total())
	}
	m, _ = press(t, m, "enter")
	if loc := m.viewer.Loc(); loc.Path != b || loc.Line != 7 || loc.Col != 13 {
		t.Errorf("enter should jump to the symbol, at %+v", loc)
	}
}

func TestLSPEventsUpdateStatusAndDiagnostics(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	a := filepath.Join(dir, "a.cs")
	m := startedLSP(t, dir, nav)
	m, _ = press(t, m, "j", "j", "j", "j")
	nav.events = []any{LogEvent{Server: "csharp", Text: "next"}}
	m, cmd := m.Update(LSPEventMsg{Event: viewer.ServerStatus{Server: "csharp", Label: "C#", Project: "Deck", State: viewer.ServerIndexing, Percent: 42}})
	if st := m.LSPStatus(); len(st) != 1 || st[0].Percent != 42 {
		t.Fatalf("status = %+v", st)
	}
	if cmd == nil {
		t.Fatal("the listener should re-arm and the elapsed tick start")
	}
	m, _ = m.Update(LSPEventMsg{Event: viewer.ServerStatus{Server: "csharp", Label: "C#", Project: "Deck", State: viewer.ServerReady}})
	if st := m.LSPStatus(); len(st) != 1 || st[0].State != viewer.ServerReady {
		t.Errorf("a newer status replaces the old one: %+v", st)
	}
	m, _ = m.Update(LSPEventMsg{Event: DiagnosticsEvent{Path: a, Diagnostics: []viewer.Diagnostic{{Range: viewer.Range{Start: viewer.Pos{Line: 1}}, Severity: viewer.SeverityError, Message: "boom"}}}})
	if !strings.Contains(ansi.Strip(m.View()), "2 ●") {
		t.Error("diagnostics should reach the viewer gutter")
	}
	_, out := send(t, m, LSPEventMsg{Event: MessageEvent{Server: "C#", Level: ui.LevelError, Text: "solution failed"}})
	found := false
	for _, msg := range out {
		if tm, ok := msg.(ui.ToastMsg); ok && strings.Contains(tm.Text, "solution failed") {
			found = true
		}
	}
	if !found {
		t.Error("showMessage should raise a toast")
	}
}

func TestEditorCloseResyncsSavedDocs(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	a, b := filepath.Join(dir, "a.cs"), filepath.Join(dir, "lib", "b.cs")
	m := startedLSP(t, dir, nav)
	_, _ = send(t, m, InvalidateMsg{Saved: []string{a, b}})
	if strings.Join(nav.reloaded, ",") != a+","+b {
		t.Errorf("saved documents should be re-sent, got %v", nav.reloaded)
	}
}

func TestLOpensLog(t *testing.T) {
	dir := navFixture(t)
	nav := newStubLSP()
	nav.logs = []string{"── csharp stderr", "warn: something"}
	m := startedLSP(t, dir, nav)
	m, _ = m.Update(LSPEventMsg{Event: LogEvent{Server: "csharp", Text: "loaded 3 projects"}})
	m, _ = press(t, m, "L")
	if !m.pick.Active() || m.pick.ID() != pickLog || m.pick.Total() != 3 {
		t.Fatalf("L should open the log, total %d", m.pick.Total())
	}
	if it, _ := m.pick.At(0); it.Title != "[csharp] loaded 3 projects" {
		t.Errorf("first log row = %q", it.Title)
	}
	m, _ = press(t, m, "enter")
	if m.pick.Active() {
		t.Error("enter closes the log")
	}
}

func TestNoLSPKeepsTabWorking(t *testing.T) {
	dir := navFixture(t)
	m := startedLSP(t, dir, nil)
	m, _ = press(t, m, "j", "j", "j", "j", "enter")
	m, out := press(t, m, "g", "d")
	if len(out) != 1 {
		t.Errorf("gd without servers should only hint, got %#v", out)
	}
	m, _ = send(t, m, ctrlKey(tea.KeyCtrlT), ctrlKey(tea.KeyCtrlO))
	if m.pick.Active() {
		t.Error("ctrl+t without servers should not open a picker")
	}
}
