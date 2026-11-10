package picker

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

func kp(s string) tea.KeyMsg {
	special := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "down": tea.KeyDown, "up": tea.KeyUp,
		"ctrl+n": tea.KeyCtrlN, "ctrl+p": tea.KeyCtrlP, "pgdown": tea.KeyPgDown, "backspace": tea.KeyBackspace,
	}
	if k, ok := special[s]; ok {
		return tea.KeyMsg{Type: k}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, run(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func typeText(t *testing.T, m Model, s string) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	for _, r := range s {
		var cmd tea.Cmd
		m, cmd = m.Update(kp(string(r)))
		out = append(out, run(cmd)...)
	}
	return m, out
}

func titles(items ...string) []Item {
	out := make([]Item, len(items))
	for i, s := range items {
		out[i] = Item{Title: s, Path: "/" + s}
	}
	return out
}

func opened(items []Item) Model {
	m := New(Options{ID: "t", Dest: ui.DestFiles, Title: "Pick"})
	m.SetSize(60, 14)
	m.SetItems(items)
	return m
}

func shown(m Model) []string {
	var out []string
	for i := 0; i < m.Len(); i++ {
		it, _ := m.At(i)
		out = append(out, it.Title)
	}
	return out
}

func TestFuzzyRanksAndHighlights(t *testing.T) {
	m := opened(titles("src/zebra.go", "Program.cs", "docs/progress.md", "pgm"))
	m, _ = typeText(t, m, "prog")
	got := shown(m)
	if len(got) != 2 || got[0] != "Program.cs" {
		t.Fatalf("prog should keep the two matches with the prefix match first, got %v", got)
	}
	_, pos := m.At(0)
	if fmt.Sprint(pos) != "[0 1 2 3]" {
		t.Errorf("matched rune positions = %v, want [0 1 2 3]", pos)
	}
	if !strings.Contains(m.View(), ui.Active.Render("Prog")) {
		t.Error("matched runes should be highlighted in the view")
	}
	m, _ = m.Update(kp("backspace"))
	m, _ = typeText(t, m, "zz")
	if m.Len() != 0 || !strings.Contains(ansi.Strip(m.View()), "no matches") {
		t.Errorf("a query with no match shows an empty list, got %v", shown(m))
	}
}

func TestAppendStreams(t *testing.T) {
	m := opened(titles("alpha.go"))
	m, _ = typeText(t, m, "a")
	m, _ = m.Update(kp("down"))
	m.Append(titles("beta.go", "gamma.txt", "zzz"))
	if got := shown(m); len(got) != 3 || slices.Contains(got, "zzz") {
		t.Errorf("appended items are matched against the live query: %v", got)
	}
	if it, ok := m.Selected(); !ok || it.Title != "alpha.go" {
		t.Errorf("appending keeps the selection, got %+v", it)
	}
	live := New(Options{ID: "g", Live: true})
	live.SetSize(60, 14)
	live, _ = typeText(t, live, "zz")
	live.Append(titles("one", "two"))
	if live.Len() != 2 {
		t.Errorf("a live picker shows every item the owner sends, got %v", shown(live))
	}
}

func TestPeekEmittedOnMove(t *testing.T) {
	m := opened(titles("a.go", "b.go", "c.go"))
	_, msgs := m.Update(kp("down"))
	var peeks []PeekMsg
	for _, msg := range run(msgs) {
		if p, ok := msg.(PeekMsg); ok {
			peeks = append(peeks, p)
		}
	}
	if len(peeks) != 1 || peeks[0].Item.Title != "b.go" || peeks[0].ID != "t" || peeks[0].RouteTo() != ui.DestFiles {
		t.Fatalf("down should peek at b.go routed to the owner, got %+v", peeks)
	}
	m, _ = m.Update(kp("up"))
	if _, cmd := m.Update(kp("up")); cmd != nil {
		t.Error("a move that stays put does not peek again")
	}
}

func TestEscCancels(t *testing.T) {
	m := opened(titles("a.go"))
	if !m.Active() || !m.Capturing() {
		t.Fatal("an open picker is active and captures the keyboard")
	}
	m, cmd := m.Update(kp("esc"))
	msgs := run(cmd)
	if len(msgs) != 1 {
		t.Fatalf("esc emits one message, got %v", msgs)
	}
	if c, ok := msgs[0].(CancelMsg); !ok || c.ID != "t" {
		t.Errorf("esc should cancel, got %#v", msgs[0])
	}
	if m.Active() || m.Capturing() {
		t.Error("a cancelled picker closes")
	}
}

func TestEnterAccepts(t *testing.T) {
	m := opened(titles("a.go", "b.go"))
	m, _ = typeText(t, m, "b")
	m, cmd := m.Update(kp("enter"))
	msgs := run(cmd)
	a, ok := msgs[0].(AcceptMsg)
	if !ok || a.Item.Path != "/b.go" || a.Query != "b" {
		t.Fatalf("enter should accept b.go with the query, got %#v", msgs)
	}
	if m.Active() {
		t.Error("accepting closes the picker")
	}
	empty := opened(titles("a.go"))
	empty, _ = typeText(t, empty, "zz")
	if _, cmd := empty.Update(kp("enter")); cmd != nil {
		if _, ok := run(cmd)[0].(AcceptMsg); ok {
			t.Error("enter with nothing selected must not accept")
		}
	}
}

func TestGroupsRendered(t *testing.T) {
	m := New(Options{ID: "g", Live: true})
	m.SetSize(60, 14)
	m.SetItems([]Item{
		{Title: "one", Group: "a.go"}, {Title: "two", Group: "a.go"}, {Title: "three", Group: "b.go"},
	})
	v := ansi.Strip(m.View())
	if strings.Count(v, "a.go") != 1 || strings.Count(v, "b.go") != 1 {
		t.Errorf("each group gets one header row:\n%s", v)
	}
	if strings.Index(v, "a.go") > strings.Index(v, "one") || strings.Index(v, "b.go") > strings.Index(v, "three") {
		t.Errorf("headers come before their items:\n%s", v)
	}
}

func TestViewFits(t *testing.T) {
	long := strings.Repeat("deeply/nested/", 20) + "file.go"
	items := titles(long, "short.go")
	items[0].Detail = strings.Repeat("detail ", 30)
	items[1].Group = strings.Repeat("g", 200)
	for _, size := range [][2]int{{30, 6}, {60, 14}, {120, 40}} {
		m := opened(items)
		m.SetStatus(strings.Repeat("status ", 40))
		m.SetSize(size[0], size[1])
		v := m.View()
		if h := lipgloss.Height(v); h != size[1] {
			t.Errorf("%v: height %d", size, h)
		}
		if w := lipgloss.Width(v); w > size[0] {
			t.Errorf("%v: width %d", size, w)
		}
	}
}

func TestScrollKeepsSelectionVisible(t *testing.T) {
	var items []string
	for i := range 100 {
		items = append(items, fmt.Sprintf("file%03d.go", i))
	}
	m := opened(titles(items...))
	for range 30 {
		m, _ = m.Update(kp("down"))
	}
	if !strings.Contains(ansi.Strip(m.View()), "file030.go") {
		t.Errorf("the selected row stays on screen:\n%s", ansi.Strip(m.View()))
	}
}

func TestLargeListMatchesAsync(t *testing.T) {
	var many []Item
	for i := range AsyncThreshold + 1 {
		many = append(many, Item{Title: fmt.Sprintf("dir/file%06d.go", i)})
	}
	many = append(many, Item{Title: "special.cs"})
	m := opened(many)
	m, msgs := typeText(t, m, "es")
	if m.Len() != len(many) {
		t.Fatalf("matching a large list must not run inside Update; shown %d", m.Len())
	}
	var results []tea.Msg
	for _, msg := range msgs {
		if _, ok := msg.(Msg); ok {
			results = append(results, msg)
		}
	}
	if len(results) != 2 {
		t.Fatalf("each keystroke starts one match command, got %d", len(results))
	}
	m, _ = m.Update(results[1])
	if got := shown(m); len(got) != 1 || got[0] != "special.cs" {
		t.Fatalf("the latest match result applies, got %d items", len(got))
	}
	m, _ = m.Update(results[0])
	if m.Len() != 1 {
		t.Errorf("a stale match result is dropped, shown %d", m.Len())
	}
}
