package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/viewer"
)

func keyMsg(s string) tea.KeyMsg {
	special := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "right": tea.KeyRight, "left": tea.KeyLeft,
		"down": tea.KeyDown, "up": tea.KeyUp, "backspace": tea.KeyBackspace,
	}
	if k, ok := special[s]; ok {
		return tea.KeyMsg{Type: k}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collect(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// settle feeds every message the commands produce back into the pane, the way
// the Bubble Tea runtime would, and returns the messages that leave the pane.
func settle(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	queue := collect(cmd)
	for i := 0; i < len(queue); i++ {
		if i > 200 {
			t.Fatal("commands never settled")
		}
		switch msg := queue[i].(type) {
		case viewer.Msg, list.FilterMatchesMsg, ReloadMsg, RevealMsg:
			var next tea.Cmd
			m, next = m.Update(msg)
			queue = append(queue, collect(next)...)
		default:
			out = append(out, msg)
		}
	}
	return m, out
}

func press(t *testing.T, m Model, keys ...string) (Model, []tea.Msg) {
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

func started(t *testing.T, dir string, w, h int) Model {
	t.Helper()
	m := New(dir, config.Default())
	m.SetSize(w, h)
	m, _ = settle(t, m, m.Init())
	return m
}

func fixture(t *testing.T, files map[string]int) string {
	t.Helper()
	dir := t.TempDir()
	for name, n := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeLines(t, p, n)
	}
	return dir
}

func openMsgs(msgs []tea.Msg) []editor.OpenMsg {
	var out []editor.OpenMsg
	for _, msg := range msgs {
		if o, ok := msg.(editor.OpenMsg); ok {
			out = append(out, o)
		}
	}
	return out
}

func TestSortEntriesDirsFirst(t *testing.T) {
	in := []entry{
		{name: "zebra.go", isDir: false},
		{name: "src", isDir: true},
		{name: "Apple.txt", isDir: false},
		{name: "build", isDir: true},
	}
	sortEntries(in)
	want := []string{"build", "src", "Apple.txt", "zebra.go"}
	for i, w := range want {
		if in[i].name != w {
			t.Errorf("position %d = %q, want %q (order: %+v)", i, in[i].name, w, in)
		}
	}
}

func TestInitLoadsFirstFile(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := New(dir, config.Default())
	m.SetSize(80, 24)
	if m.viewer.Loaded() {
		t.Fatal("New must not read the file; that belongs to Init")
	}
	m, _ = settle(t, m, m.Init())
	if !m.viewer.Loaded() || !strings.Contains(ansi.Strip(m.View()), "line 2") {
		t.Errorf("after Init the first file should show:\n%s", ansi.Strip(m.View()))
	}
}

func TestSelectionLoadsAsync(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3, "b.go": 5})
	m := started(t, dir, 80, 24)
	m, cmd := m.Update(keyMsg("j"))
	if m.viewer.Path() != filepath.Join(dir, "b.go") || m.viewer.Loaded() {
		t.Fatalf("moving should start a load, not read in Update: path=%q loaded=%v", m.viewer.Path(), m.viewer.Loaded())
	}
	var loaded tea.Msg
	for _, msg := range collect(cmd) {
		if _, ok := msg.(viewer.DocLoadedMsg); ok {
			loaded = msg
		}
	}
	if loaded == nil {
		t.Fatal("moving the selection should return a load command")
	}
	m, cmd = m.Update(loaded)
	if !m.viewer.Loaded() || m.viewer.Highlighted() {
		t.Fatalf("after the load: loaded=%v highlighted=%v; highlighting must stay async", m.viewer.Loaded(), m.viewer.Highlighted())
	}
	m, _ = settle(t, m, cmd)
	if !m.viewer.Highlighted() {
		t.Error("the highlight command should finish the job")
	}
}

func TestEnterOnFileFocusesViewer(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30, "b.go": 3})
	for _, k := range []string{"enter", "l", "right"} {
		m := started(t, dir, 80, 24)
		m, out := press(t, m, k)
		if m.focus != focusViewer {
			t.Errorf("%q on a file should focus the viewer", k)
		}
		if len(openMsgs(out)) != 0 {
			t.Errorf("%q must not launch the editor", k)
		}
		m, _ = press(t, m, "j", "j")
		if m.list.Index() != 0 || m.viewer.Loc().Line != 2 {
			t.Errorf("j in the viewer should move its cursor (line %d), not the list (%d)", m.viewer.Loc().Line, m.list.Index())
		}
		if m.Capturing() {
			t.Error("reading in the viewer must not capture")
		}
	}
}

func TestEscReturnsToList(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30, "b.go": 3})
	m := started(t, dir, 80, 24)
	m, _ = press(t, m, "enter", "/")
	if !m.Capturing() {
		t.Fatal("the viewer's search prompt should capture")
	}
	m, _ = press(t, m, "esc")
	if m.focus != focusViewer {
		t.Error("the first esc closes the prompt and keeps viewer focus")
	}
	m, _ = press(t, m, "esc")
	if m.focus != focusList {
		t.Error("esc with nothing to close in the viewer returns to the list")
	}
	m, _ = press(t, m, "j")
	if m.list.Index() != 1 {
		t.Error("j after returning should move the list")
	}
}

func TestLDoesNotLaunchEditor(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := started(t, dir, 80, 24)
	if _, out := press(t, m, "l"); len(openMsgs(out)) != 0 {
		t.Error("l on a file must not launch the editor")
	}
}

func TestEOpensEditorAtLineOne(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := started(t, dir, 80, 24)
	_, out := press(t, m, "e")
	opens := openMsgs(out)
	want := editor.Target{Path: filepath.Join(dir, "a.go"), Line: 1, Col: 1}
	if len(opens) != 1 || opens[0].Target != want {
		t.Errorf("e from the list = %+v, want %+v", opens, want)
	}
}

func TestEInViewerOpensAtCursor(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30})
	m := started(t, dir, 80, 24)
	m, _ = press(t, m, "enter", "j", "j", "l")
	_, out := press(t, m, "e")
	opens := openMsgs(out)
	if len(opens) != 1 || opens[0].Target.Line != 3 || opens[0].Target.Col != 2 {
		t.Errorf("e in the viewer = %+v, want line 3 col 2", opens)
	}
}

func TestUpDirSelectsChild(t *testing.T) {
	dir := fixture(t, map[string]int{"aaa/x.go": 1, "sub/y.go": 1, "z.go": 1})
	m := started(t, dir, 80, 24)
	m, _ = press(t, m, "j", "enter")
	if m.dir != filepath.Join(dir, "sub") {
		t.Fatalf("enter on a directory should open it, dir = %q", m.dir)
	}
	m, _ = press(t, m, "h")
	if m.dir != dir || m.selectedName() != "sub" {
		t.Errorf("up: dir=%q selected=%q, want the root with sub selected", m.dir, m.selectedName())
	}
}

func TestEscClearsAppliedFilter(t *testing.T) {
	dir := fixture(t, map[string]int{"alpha.go": 1, "beta.go": 1, "gamma.go": 1})
	m := started(t, dir, 80, 24)
	m, _ = press(t, m, "/", "b", "e", "t", "enter")
	if m.list.FilterState() != list.FilterApplied || len(m.list.VisibleItems()) != 1 {
		t.Fatalf("filter state = %v with %d items", m.list.FilterState(), len(m.list.VisibleItems()))
	}
	if m.Capturing() {
		t.Error("an applied filter no longer captures")
	}
	m, _ = press(t, m, "esc")
	if m.list.FilterState() != list.Unfiltered || len(m.list.VisibleItems()) != 3 {
		t.Errorf("esc should clear the applied filter: state %v, %d items", m.list.FilterState(), len(m.list.VisibleItems()))
	}
}

func TestSymlinkedDirIsDir(t *testing.T) {
	dir := fixture(t, map[string]int{"real/x.go": 1})
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	entries, err := readDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range entries {
		kinds[e.name] = e.isDir
	}
	if !kinds["link"] || !kinds["real"] || kinds["broken"] {
		t.Errorf("isDir by name = %v; a symlinked directory is a directory, a broken link is not", kinds)
	}
}

func TestMouseClickFocusesRegion(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30, "b.go": 3})
	m := started(t, dir, 90, 20)
	click := func(x, y int) tea.MouseMsg {
		return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	}
	r := m.viewerRect()
	m, _ = m.Update(click(r.X+r.W/2, r.Y+3))
	if m.focus != focusViewer {
		t.Fatal("a click in the viewer should focus it")
	}
	if m.viewer.Loc().Line != 2 {
		t.Errorf("the click should place the viewer cursor on the clicked row, line = %d", m.viewer.Loc().Line)
	}
	m, _ = m.Update(click(1, ListTop))
	if m.focus != focusList {
		t.Error("a click on a list row should focus the list")
	}
}

func TestRevealMsgSelectsAndGoes(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 1, "sub/c.go": 100})
	m := started(t, dir, 90, 30)
	target := filepath.Join(dir, "sub", "c.go")
	m, cmd := m.Update(RevealMsg{Target: editor.Target{Path: target, Line: 50, Col: 3}})
	m, _ = settle(t, m, cmd)
	if m.dir != filepath.Join(dir, "sub") || m.SelectedPath() != target {
		t.Fatalf("reveal: dir=%q selected=%q", m.dir, m.SelectedPath())
	}
	if loc := m.viewer.Loc(); loc.Line != 49 || loc.Col != 2 || m.focus != focusViewer {
		t.Errorf("reveal: viewer at %+v focus %d, want line 49 col 2 with viewer focus", loc, m.focus)
	}
}

func TestPerDirPositionRemembered(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 3, "sub/b.go": 3, "sub/c.go": 50, "z.go": 1})
	m := started(t, dir, 90, 30)
	m, _ = press(t, m, "enter", "j", "j", "enter", "j", "j", "j", "j", "j", "esc", "h")
	if m.dir != dir {
		t.Fatalf("expected to be back at the root, dir = %q", m.dir)
	}
	m, _ = press(t, m, "enter")
	if m.selectedName() != "c.go" {
		t.Errorf("re-entering sub should select c.go again, got %q", m.selectedName())
	}
	if line := m.viewer.Loc().Line; line != 5 {
		t.Errorf("the viewer should return to line 5 of c.go, got %d", line)
	}
}

func TestReloadKeepsPosition(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 50})
	m := started(t, dir, 90, 30)
	m, _ = press(t, m, "enter", "j", "j", "j", "j")
	m, cmd := m.Update(ReloadCurrent())
	m, _ = settle(t, m, cmd)
	if line := m.viewer.Loc().Line; line != 4 || !m.viewer.Loaded() {
		t.Errorf("after reload the viewer should stay on line 4, got %d", line)
	}
}

func TestDirectorySelectionShowsNote(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 1})
	m := started(t, dir, 90, 20)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "sub/") {
		t.Errorf("selecting a directory should say so in the viewer:\n%s", v)
	}
}

func TestCapturingWhileFiltering(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 1})
	m := started(t, dir, 80, 24)
	if m.Capturing() {
		t.Fatal("list focus without an active filter should not capture")
	}
	m, _ = m.Update(keyMsg("/"))
	if !m.Capturing() {
		t.Error("'/' should start the filter and capture the keyboard")
	}
}

func TestNoAIKeys(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 1})
	for _, k := range []string{"a", "t"} {
		m := started(t, dir, 80, 24)
		if _, cmd := m.Update(keyMsg(k)); cmd != nil {
			t.Errorf("%q on a file returned a command; the AI keys should be gone", k)
		}
	}
}
