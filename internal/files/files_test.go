package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/viewer"
)

func keyMsg(s string) tea.KeyMsg {
	special := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "right": tea.KeyRight, "left": tea.KeyLeft,
		"down": tea.KeyDown, "up": tea.KeyUp, "backspace": tea.KeyBackspace, "ctrl+b": tea.KeyCtrlB,
		"ctrl+p": tea.KeyCtrlP, "ctrl+f": tea.KeyCtrlF,
	}
	if k, ok := special[s]; ok {
		return tea.KeyMsg{Type: k}
	}
	if r, ok := strings.CutPrefix(s, "alt+"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r), Alt: true}
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

// settle feeds every Files-bound message the commands produce back into the
// pane, the way the root model would, and returns the messages that leave it.
func settle(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	queue := collect(cmd)
	for i := 0; i < len(queue); i++ {
		if i > 200 {
			t.Fatal("commands never settled")
		}
		if r, ok := queue[i].(ui.Routed); ok && r.RouteTo() == ui.DestFiles {
			var next tea.Cmd
			m, next = m.Update(queue[i])
			queue = append(queue, collect(next)...)
			continue
		}
		out = append(out, queue[i])
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

func TestInitLoadsFirstFile(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := New(dir, config.Default())
	m.SetSize(100, 24)
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
	m := started(t, dir, 100, 24)
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
		m := started(t, dir, 100, 24)
		m, out := press(t, m, k)
		if m.focus != focusViewer {
			t.Errorf("%q on a file should focus the viewer", k)
		}
		if len(openMsgs(out)) != 0 {
			t.Errorf("%q must not launch the editor", k)
		}
		m, _ = press(t, m, "j", "j")
		if m.tree.sel != 0 || m.viewer.Loc().Line != 2 {
			t.Errorf("j in the viewer should move its cursor (line %d), not the tree (%d)", m.viewer.Loc().Line, m.tree.sel)
		}
		if m.Capturing() {
			t.Error("reading in the viewer must not capture")
		}
	}
}

func TestEnterOnDirTogglesAndLExpands(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 1, "sub/b.go": 1, "z.go": 1})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "enter")
	if got := rowNames(m.tree.Rows()); strings.Join(got, ",") != "sub,  a.go,  b.go,z.go" {
		t.Fatalf("enter on a directory expands it in place: %v", got)
	}
	if m.focus != focusTree || m.SelectedPath() != filepath.Join(dir, "sub") {
		t.Error("expanding keeps the tree focused on the directory")
	}
	m, _ = press(t, m, "enter")
	if len(m.tree.Rows()) != 2 {
		t.Errorf("a second enter collapses it, rows = %v", rowNames(m.tree.Rows()))
	}
	m, _ = press(t, m, "l", "l")
	if m.SelectedPath() != filepath.Join(dir, "sub", "a.go") {
		t.Errorf("l expands, then l again steps into the first child; selected %q", m.SelectedPath())
	}
	m, _ = press(t, m, "h")
	if m.SelectedPath() != filepath.Join(dir, "sub") {
		t.Errorf("h on a file selects its directory, got %q", m.SelectedPath())
	}
	m, _ = press(t, m, "h")
	if len(m.tree.Rows()) != 2 {
		t.Errorf("h on an expanded directory collapses it")
	}
}

func TestSwitchingFilesKeepsExpansion(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 30, "sub/b.go": 3, "z.go": 1})
	m := started(t, dir, 100, 30)
	m, _ = press(t, m, "l", "j", "enter", "j", "j", "esc", "j")
	if m.SelectedPath() != filepath.Join(dir, "sub", "b.go") || len(m.tree.Rows()) != 4 {
		t.Fatalf("selected %q rows %v", m.SelectedPath(), rowNames(m.tree.Rows()))
	}
	m, _ = press(t, m, "k")
	if line := m.viewer.Loc().Line; line != 2 {
		t.Errorf("returning to a.go restores its position, line = %d", line)
	}
}

func TestEscReturnsToTree(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30, "b.go": 3})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "enter", "/")
	if !m.Capturing() {
		t.Fatal("the viewer's search prompt should capture")
	}
	m, _ = press(t, m, "esc")
	if m.focus != focusViewer {
		t.Error("the first esc closes the prompt and keeps viewer focus")
	}
	m, _ = press(t, m, "esc")
	if m.focus != focusTree {
		t.Error("esc with nothing to close in the viewer returns to the tree")
	}
	m, _ = press(t, m, "j")
	if m.tree.sel != 1 {
		t.Error("j after returning should move the tree")
	}
}

func TestEOpensEditorAtCursor(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30})
	m := started(t, dir, 100, 24)
	_, out := press(t, m, "e")
	opens := openMsgs(out)
	want := editor.Target{Path: filepath.Join(dir, "a.go"), Line: 1, Col: 1}
	if len(opens) != 1 || opens[0].Target != want {
		t.Errorf("e from the tree = %+v, want %+v", opens, want)
	}
	m, _ = press(t, m, "enter", "j", "j", "l")
	_, out = press(t, m, "e")
	opens = openMsgs(out)
	if len(opens) != 1 || opens[0].Target.Line != 3 || opens[0].Target.Col != 2 {
		t.Errorf("e in the viewer = %+v, want line 3 col 2", opens)
	}
	m, _ = press(t, m, "esc")
	_, out = press(t, m, "e")
	if opens = openMsgs(out); len(opens) != 1 || opens[0].Target.Line != 3 {
		t.Errorf("e from the tree on the open file uses the viewer cursor, got %+v", opens)
	}
}

func TestRerootKeys(t *testing.T) {
	parent := t.TempDir()
	touch(t, parent, "proj/src/a.go", "proj/z.go", "other.txt")
	proj := filepath.Join(parent, "proj")
	m := started(t, proj, 100, 24)
	m, _ = press(t, m, "-")
	if m.tree.Root().Path != parent || m.SelectedPath() != proj {
		t.Errorf("- moves the root up and selects the old root: root %q selected %q", m.tree.Root().Path, m.SelectedPath())
	}
	m, _ = press(t, m, "~")
	if m.tree.Root().Path != proj {
		t.Errorf("~ returns to the launch root, got %q", m.tree.Root().Path)
	}
	m, _ = press(t, m, "=")
	if m.tree.Root().Path != filepath.Join(proj, "src") {
		t.Errorf("= on src makes it the root, got %q", m.tree.Root().Path)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, filepath.Join("proj", "src")) {
		t.Errorf("the tree header shows the root relative to the launch dir:\n%s", v)
	}
}

func TestSortAndHideKeys(t *testing.T) {
	dir := fixture(t, map[string]int{"b.go": 1, "a.txt": 1, ".env": 1})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "s")
	if m.tree.Sort() != SortModified || !strings.Contains(ansi.Strip(m.View()), "↓modified") {
		t.Error("s cycles the sort mode and the header shows it")
	}
	m, _ = press(t, m, ".")
	if got := rowNames(m.tree.Rows()); len(got) != 3 {
		t.Errorf(". shows dotfiles: %v", got)
	}
}

func TestRevealMsgSelectsAndGoes(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 1, "sub/c.go": 100})
	m := started(t, dir, 100, 30)
	target := filepath.Join(dir, "sub", "c.go")
	m, cmd := m.Update(RevealMsg{Target: editor.Target{Path: target, Line: 50, Col: 3}})
	m, _ = settle(t, m, cmd)
	if m.SelectedPath() != target {
		t.Fatalf("reveal selected %q", m.SelectedPath())
	}
	if loc := m.viewer.Loc(); loc.Line != 49 || loc.Col != 2 || m.focus != focusViewer {
		t.Errorf("reveal: viewer at %+v focus %d, want line 49 col 2 with viewer focus", loc, m.focus)
	}
	if got := rowNames(m.tree.Rows()); strings.Join(got, ",") != "sub,  c.go,a.go" {
		t.Errorf("reveal expands the ancestors: %v", got)
	}
}

func TestRevealMsgRereadsExpandedDirectories(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 5, "z.go": 1})
	m := started(t, dir, 100, 30)
	m, _ = press(t, m, "l")
	touch(t, dir, "made-in-editor.go")
	m, cmd := m.Update(RevealMsg{Target: editor.Target{Path: filepath.Join(dir, "sub", "a.go"), Line: 2}})
	m, _ = settle(t, m, cmd)
	if got := rowNames(m.tree.Rows()); strings.Join(got, ",") != "sub,  a.go,made-in-editor.go,z.go" {
		t.Errorf("returning from the editor re-reads the tree: %v", got)
	}
}

func TestReloadKeepsPositionAndExpansion(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 50, "z.go": 1})
	m := started(t, dir, 100, 30)
	m, _ = press(t, m, "l", "j", "enter", "j", "j", "j", "j")
	touch(t, dir, "sub/new.go")
	m, cmd := m.Update(ReloadCurrent())
	m, _ = settle(t, m, cmd)
	if line := m.viewer.Loc().Line; line != 4 || !m.viewer.Loaded() {
		t.Errorf("after reload the viewer should stay on line 4, got %d", line)
	}
	if got := rowNames(m.tree.Rows()); strings.Join(got, ",") != "sub,  a.go,  new.go,z.go" {
		t.Errorf("reload re-reads expanded directories: %v", got)
	}
}

func TestCapturingOnlyInViewerPrompt(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 1})
	m := started(t, dir, 100, 24)
	if m.Capturing() {
		t.Fatal("the tree never captures")
	}
	m, _ = press(t, m, "/")
	if m.Capturing() {
		t.Error("/ in the tree is not a prompt")
	}
}

func TestNoAIKeys(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 1})
	for _, k := range []string{"a", "t"} {
		m := started(t, dir, 100, 24)
		if _, cmd := m.Update(keyMsg(k)); cmd != nil {
			t.Errorf("%q on a file returned a command; the AI keys should be gone", k)
		}
	}
}

func TestMouseClickFocusesRegion(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30, "b.go": 3})
	m := started(t, dir, 100, 20)
	click := func(x, y int) tea.MouseMsg {
		return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	}
	r := m.layout().ViewerInner()
	m, _ = m.Update(click(r.X+r.W/2, r.Y+3))
	if m.focus != focusViewer {
		t.Fatal("a click in the viewer should focus it")
	}
	if m.viewer.Loc().Line != 2 {
		t.Errorf("the click should place the viewer cursor on the clicked row, line = %d", m.viewer.Loc().Line)
	}
	m, _ = m.Update(click(2, ListTop))
	if m.focus != focusTree {
		t.Error("a click on a tree row should focus the tree")
	}
}

func TestClickOnMarkerTogglesAndDoubleClickOpens(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 1, "z.go": 5})
	m := started(t, dir, 100, 20)
	click := tea.MouseMsg{X: 1, Y: ListTop, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m, _ = m.Update(click)
	if len(m.tree.Rows()) != 3 {
		t.Fatalf("a click on a directory's marker expands it: %v", rowNames(m.tree.Rows()))
	}
	row := tea.MouseMsg{X: 8, Y: ListTop + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m, _ = m.Update(row)
	m, cmd := m.Update(row)
	m, _ = settle(t, m, cmd)
	if m.SelectedPath() != filepath.Join(dir, "z.go") || m.focus != focusViewer {
		t.Errorf("a double-click on a file opens it in the viewer: selected %q focus %d", m.SelectedPath(), m.focus)
	}
}

func TestLargeRootLoadsAsync(t *testing.T) {
	defer func(n int) { bigDirEntries = n }(bigDirEntries)
	bigDirEntries = 2
	dir := fixture(t, map[string]int{"a.go": 1, "b.go": 1, "c.go": 1})
	m := New(dir, config.Default())
	m.SetSize(100, 20)
	if len(m.tree.Rows()) != 0 || !strings.Contains(ansi.Strip(m.View()), "loading…") {
		t.Fatal("a root over the cap is listed by a command, not in New")
	}
	m, _ = settle(t, m, m.Init())
	if len(m.tree.Rows()) != 3 || !m.viewer.Loaded() {
		t.Errorf("after Init: rows %v, viewer loaded %v", rowNames(m.tree.Rows()), m.viewer.Loaded())
	}
}
