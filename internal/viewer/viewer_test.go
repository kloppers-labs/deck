package viewer

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
)

func keyMsg(s string) tea.KeyMsg {
	special := map[string]tea.KeyType{
		"down": tea.KeyDown, "up": tea.KeyUp, "left": tea.KeyLeft, "right": tea.KeyRight,
		"esc": tea.KeyEsc, "enter": tea.KeyEnter, "backspace": tea.KeyBackspace,
		"ctrl+d": tea.KeyCtrlD, "ctrl+u": tea.KeyCtrlU, "ctrl+r": tea.KeyCtrlR,
		"pgdown": tea.KeyPgDown, "pgup": tea.KeyPgUp, "space": tea.KeySpace,
	}
	if k, ok := special[s]; ok {
		return tea.KeyMsg{Type: k}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m Model, ks ...string) Model {
	t.Helper()
	for _, k := range ks {
		m, _ = m.Update(keyMsg(k))
	}
	return m
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func sized(m Model, w, h int) Model {
	m.SetSize(w, h)
	return m
}

func deliver(t *testing.T, m Model, d *Document) Model {
	t.Helper()
	m, _ = m.Open(d.Path, nil)
	m, _ = m.Update(DocLoadedMsg{Path: d.Path, Gen: m.gen, Doc: d})
	return m
}

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d", i+1)
	}
	return out
}

func docOf(path string, lines []string) *Document {
	return newDocument(path, []byte(strings.Join(lines, "\n")+"\n"), 4)
}

func viewerWith(t *testing.T, lines []string, w, h int) Model {
	t.Helper()
	m := sized(New(Options{Root: "/repo", LineNumbers: true}), w, h)
	return deliver(t, m, docOf("/repo/src/a.cs", lines))
}

func TestJScrollsWithCursor(t *testing.T) {
	m := viewerWith(t, numbered(100), 60, 12)
	m = press(t, m, "j", "j", "j")
	if m.cur.Line != 3 || m.top != 0 {
		t.Fatalf("after 3×j: cur=%d top=%d, want 3 and 0", m.cur.Line, m.top)
	}
	for range 5 {
		m = press(t, m, "j")
	}
	if m.cur.Line != 8 || m.top != 2 {
		t.Errorf("after 8×j in a 10-row body: cur=%d top=%d, want 8 and 2 (3-row scroll-off)", m.cur.Line, m.top)
	}
	m = press(t, m, "down", "k", "up")
	if m.cur.Line != 7 {
		t.Errorf("arrows and k: cur = %d, want 7", m.cur.Line)
	}
}

func TestCtrlDHalfPage(t *testing.T) {
	m := viewerWith(t, numbered(100), 60, 22)
	m = press(t, m, "ctrl+d")
	if m.top != 10 || m.cur.Line != 13 {
		t.Errorf("ctrl+d: top=%d cur=%d, want 10 and 13 (the scroll-off moves the cursor, not the view)", m.top, m.cur.Line)
	}
	m = press(t, m, "ctrl+u")
	if m.top != 0 || m.cur.Line != 3 {
		t.Errorf("ctrl+u: top=%d cur=%d, want 0 and 3", m.top, m.cur.Line)
	}
	m = press(t, m, "pgdown")
	if m.top != 20 {
		t.Errorf("pgdown: top = %d, want 20", m.top)
	}
	m = press(t, m, "J")
	if m.top != 30 {
		t.Errorf("J: top = %d, want 30", m.top)
	}
}

func wheelAt(x, y int, b tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

func TestWheelScrollsThreeLines(t *testing.T) {
	m := viewerWith(t, numbered(100), 60, 12)
	m.now = func() time.Time { return time.Unix(0, 0) }
	m, _ = m.Update(wheelAt(10, 5, tea.MouseButtonWheelDown))
	if m.top != 3 {
		t.Errorf("one wheel notch: top = %d, want 3", m.top)
	}
	if m.cur.Line < m.top {
		t.Errorf("the cursor should be dragged into view, cur=%d top=%d", m.cur.Line, m.top)
	}
	m.now = func() time.Time { return time.Unix(10, 0) }
	m, _ = m.Update(wheelAt(10, 5, tea.MouseButtonWheelUp))
	if m.top != 0 {
		t.Errorf("wheel up: top = %d, want 0", m.top)
	}
}

func TestWheelAcceleration(t *testing.T) {
	m := viewerWith(t, numbered(1000), 60, 12)
	clock := time.Unix(100, 0)
	m.now = func() time.Time { return clock }
	var tops []int
	for _, gap := range []time.Duration{0, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, 200 * time.Millisecond} {
		clock = clock.Add(gap)
		m, _ = m.Update(wheelAt(10, 5, tea.MouseButtonWheelDown))
		tops = append(tops, m.top)
	}
	want := []int{3, 9, 18, 27, 30}
	if fmt.Sprint(tops) != fmt.Sprint(want) {
		t.Errorf("tops = %v, want %v (ramping to 3× under 40 ms, reset after a pause)", tops, want)
	}
}

func TestShiftWheelScrollsHorizontally(t *testing.T) {
	m := viewerWith(t, []string{strings.Repeat("x", 300)}, 60, 12)
	m, _ = m.Update(tea.MouseMsg{X: 10, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, Shift: true})
	if m.left != 6 || m.top != 0 {
		t.Errorf("shift+wheel: left=%d top=%d, want 6 and 0", m.left, m.top)
	}
	m, _ = m.Update(wheelAt(10, 3, tea.MouseButtonWheelLeft))
	if m.left != 0 {
		t.Errorf("wheel left: left = %d, want 0", m.left)
	}
}

func TestGGandG(t *testing.T) {
	m := viewerWith(t, numbered(100), 60, 12)
	m = press(t, m, "G")
	if m.cur.Line != 99 || m.top != 90 {
		t.Errorf("G: cur=%d top=%d, want 99 and 90", m.cur.Line, m.top)
	}
	m = press(t, m, "g")
	if !m.Capturing() {
		t.Error("a pending g should capture")
	}
	m = press(t, m, "g")
	if m.cur.Line != 0 || m.top != 0 || m.Capturing() {
		t.Errorf("gg: cur=%d top=%d capturing=%v", m.cur.Line, m.top, m.Capturing())
	}
}

func TestColonGotoLineCol(t *testing.T) {
	m := viewerWith(t, numbered(200), 60, 12)
	m = press(t, m, ":")
	if !m.Capturing() {
		t.Fatal(": should open a capturing prompt")
	}
	m = typeText(t, m, "120:4")
	if !strings.Contains(ansi.Strip(m.View()), ":120:4") {
		t.Error("the prompt should show what is typed")
	}
	m = press(t, m, "enter")
	if m.cur != (Pos{119, 3}) || m.Capturing() {
		t.Errorf("goto: cur=%v capturing=%v, want {119 3}", m.cur, m.Capturing())
	}
	if m.top > 119 || m.top+m.bodyHeight() <= 119 {
		t.Errorf("line 120 should be on screen, top = %d", m.top)
	}
	m = press(t, m, ":")
	m = typeText(t, m, "nope")
	m = press(t, m, "enter")
	if m.cur != (Pos{119, 3}) {
		t.Errorf("an invalid goto should not move, cur = %v", m.cur)
	}
}

func searchFixture() []string {
	lines := numbered(40)
	for i := range 17 {
		lines[i*2] += " needle"
	}
	return lines
}

func TestSearchHighlightsAndCounts(t *testing.T) {
	m := viewerWith(t, searchFixture(), 60, 12)
	m = press(t, m, "/")
	m = typeText(t, m, "needle")
	m = press(t, m, "enter")
	if m.Capturing() {
		t.Fatal("enter should close the search prompt")
	}
	if got := ansi.Strip(m.footer()); !strings.Contains(got, "1/17") {
		t.Errorf("footer = %q, want 1/17", got)
	}
	m = press(t, m, "n", "n")
	if got := ansi.Strip(m.footer()); !strings.Contains(got, "3/17") {
		t.Errorf("footer after 2×n = %q, want 3/17", got)
	}
	if m.cur.Line != 4 {
		t.Errorf("third match is on line 5, cur = %d", m.cur.Line)
	}
	if !strings.Contains(m.View(), matchSGR) {
		t.Error("other matches should be highlighted in the view")
	}
	m = press(t, m, "N")
	if got := ansi.Strip(m.footer()); !strings.Contains(got, "2/17") {
		t.Errorf("footer after N = %q, want 2/17", got)
	}
}

func TestSearchRegexToggle(t *testing.T) {
	m := viewerWith(t, []string{"a.c abc", "axc"}, 60, 12)
	m = press(t, m, "/")
	m = typeText(t, m, "a.c")
	if n := len(m.search.matches); n != 1 {
		t.Errorf("literal search: %d matches, want 1", n)
	}
	m = press(t, m, "ctrl+r")
	if n := len(m.search.matches); n != 3 {
		t.Errorf("regex search: %d matches, want 3", n)
	}
}

func TestEscClearsSearch(t *testing.T) {
	m := viewerWith(t, searchFixture(), 60, 12)
	m = press(t, m, "/")
	m = typeText(t, m, "needle")
	m = press(t, m, "enter")
	if !m.WantsEsc() {
		t.Fatal("an active search should claim esc")
	}
	m = press(t, m, "esc")
	if m.search.query != "" || len(m.search.matches) != 0 || m.WantsEsc() {
		t.Errorf("esc should clear the search, got %+v", m.search)
	}
	m = press(t, m, "j", "/")
	m = typeText(t, m, "needle")
	m = press(t, m, "esc")
	if m.cur.Line != 1 || m.search.query != "" || m.Capturing() {
		t.Errorf("esc in the prompt restores the cursor and cancels: cur=%d query=%q", m.cur.Line, m.search.query)
	}
}

func TestStarSearchesWordUnderCursor(t *testing.T) {
	m := viewerWith(t, []string{"var foo = 1;", "foo2 = foo;", "x = foo"}, 60, 12)
	m = press(t, m, "w", "*")
	if n := len(m.search.matches); n != 3 {
		t.Errorf("* on foo: %d matches, want 3 (whole word)", n)
	}
	if m.cur != (Pos{1, 7}) {
		t.Errorf("* jumps to the next occurrence, cur = %v", m.cur)
	}
}

func TestToggleLineNumbersAndWrap(t *testing.T) {
	m := viewerWith(t, []string{strings.Repeat("abcdefghij", 10), "short"}, 40, 10)
	if !strings.HasPrefix(ansi.Strip(strings.Split(m.View(), "\n")[1]), "1   abc") {
		t.Errorf("line numbers should be on: %q", ansi.Strip(strings.Split(m.View(), "\n")[1]))
	}
	m = press(t, m, "#")
	if row := ansi.Strip(strings.Split(m.View(), "\n")[1]); !strings.HasPrefix(row, "abcdefghij") {
		t.Errorf("# should hide the gutter: %q", row)
	}
	if strings.Contains(ansi.Strip(m.View()), "short") == false {
		t.Fatal("line 2 should be visible")
	}
	m = press(t, m, "W")
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if strings.TrimRight(rows[3], " ") != strings.Repeat("abcdefghij", 2) || !strings.HasPrefix(rows[4], "short") {
		t.Errorf("wrap should split the long line over rows:\n%s", strings.Join(rows, "\n"))
	}
}

func TestViewHeightExact(t *testing.T) {
	for _, size := range [][2]int{{30, 3}, {60, 12}, {120, 40}, {200, 60}} {
		for _, lines := range [][]string{numbered(5), numbered(500), {strings.Repeat("日本", 200)}} {
			m := viewerWith(t, lines, size[0], size[1])
			for _, wrap := range []bool{false, true} {
				m.wrap = wrap
				m.relayout()
				v := m.View()
				if got := lipgloss.Height(v); got != size[1] {
					t.Errorf("%v wrap=%v: height = %d, want %d", size, wrap, got, size[1])
				}
				if got := lipgloss.Width(v); got > size[0] {
					t.Errorf("%v wrap=%v: width = %d, want <= %d", size, wrap, got, size[0])
				}
			}
		}
	}
	empty := sized(New(Options{}), 40, 8)
	if got := lipgloss.Height(empty.View()); got != 8 {
		t.Errorf("empty viewer height = %d, want 8", got)
	}
	note := sized(New(Options{}), 40, 8).ShowNote("📁 src/")
	if got := lipgloss.Height(note.View()); got != 8 || !strings.Contains(note.View(), "src/") {
		t.Errorf("note view: height %d\n%s", got, note.View())
	}
}

func TestEOpensEditorAtCursor(t *testing.T) {
	m := viewerWith(t, numbered(100), 60, 12)
	m = press(t, m, "J", "j", "j", "l", "l", "l")
	_, cmd := m.Update(keyMsg("e"))
	if cmd == nil {
		t.Fatal("e should return a command")
	}
	var open *editor.OpenMsg
	for _, msg := range collect(cmd) {
		if o, ok := msg.(editor.OpenMsg); ok {
			open = &o
		}
	}
	if open == nil {
		t.Fatal("e should emit editor.OpenMsg")
	}
	want := editor.Target{Path: "/repo/src/a.cs", Line: m.cur.Line + 1, Col: m.cur.Col + 1, Top: m.top + 1}
	if open.Target != want || want.Line != 16 || want.Col != 4 {
		t.Errorf("target = %+v, want %+v", open.Target, want)
	}
}

func TestCapturingOnlyInPromptOrChord(t *testing.T) {
	m := viewerWith(t, numbered(100), 60, 12)
	for _, k := range []string{"j", "G", "ctrl+d", "n", "W", "#"} {
		if m = press(t, m, k); m.Capturing() {
			t.Errorf("%q should not capture", k)
		}
	}
	for _, k := range []string{"g", "z", "]", "["} {
		if !press(t, m, k).Capturing() {
			t.Errorf("a pending %q chord should capture", k)
		}
	}
	for _, k := range []string{"/", ":"} {
		if !press(t, m, k).Capturing() {
			t.Errorf("the %q prompt should capture", k)
		}
	}
	if press(t, m, "z", "z").Capturing() {
		t.Error("a completed chord should release capture")
	}
}

func TestYCopiesRelPathLine(t *testing.T) {
	var buf bytes.Buffer
	prev := clipboardOut
	clipboardOut = &buf
	t.Cleanup(func() { clipboardOut = prev })

	m := viewerWith(t, numbered(100), 60, 12)
	m = press(t, m, "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j")
	_, cmd := m.Update(keyMsg("y"))
	var toast *ui.ToastMsg
	for _, msg := range collect(cmd) {
		if tm, ok := msg.(ui.ToastMsg); ok {
			toast = &tm
		}
	}
	want := filepath.Join("src", "a.cs") + ":12"
	if buf.String() != ansi.SetSystemClipboard(want) {
		t.Errorf("clipboard payload = %q, want OSC52 of %q", buf.String(), want)
	}
	if toast == nil || !strings.Contains(toast.Text, want) {
		t.Errorf("copy should confirm with a toast, got %+v", toast)
	}
	buf.Reset()
	_, cmd = m.Update(keyMsg("Y"))
	collect(cmd)
	if buf.String() != ansi.SetSystemClipboard("line 12") {
		t.Errorf("Y payload = %q, want the current line", buf.String())
	}
}

func TestStaleGenDropped(t *testing.T) {
	m := sized(New(Options{}), 60, 12)
	m, _ = m.Open("/a.go", nil)
	staleGen := m.gen
	m, _ = m.Open("/b.go", nil)
	m, _ = m.Update(DocLoadedMsg{Path: "/a.go", Gen: staleGen, Doc: docOf("/a.go", []string{"stale"})})
	if m.Loaded() {
		t.Fatal("a load from a superseded Open must be dropped")
	}
	m, _ = m.Update(DocLoadedMsg{Path: "/b.go", Gen: m.gen, Doc: docOf("/b.go", []string{"fresh"})})
	if !strings.Contains(ansi.Strip(m.View()), "fresh") {
		t.Error("the current load should show")
	}
	stale := docOf("/a.go", []string{"x"})
	m, _ = m.Update(DocHighlightedMsg{Doc: stale, Gen: staleGen, From: 0, Lines: []string{"STYLED"}})
	if strings.Contains(m.View(), "STYLED") {
		t.Error("a highlight for another document must not reach the view")
	}
}

func TestHighlightRunsAsync(t *testing.T) {
	m := sized(New(Options{}), 60, 12)
	d := docOf("/a.go", []string{"package main", "func main() {}"})
	m, _ = m.Open(d.Path, nil)
	m, cmd := m.Update(DocLoadedMsg{Path: d.Path, Gen: m.gen, Doc: d})
	if m.Highlighted() {
		t.Fatal("loading must not highlight synchronously")
	}
	if !strings.Contains(ansi.Strip(m.View()), "package main") {
		t.Error("plain text should show before highlighting")
	}
	var hl tea.Msg
	for _, msg := range collect(cmd) {
		if h, ok := msg.(DocHighlightedMsg); ok {
			hl = h
		}
	}
	if hl == nil {
		t.Fatal("the load should request highlighting as a command")
	}
	m, _ = m.Update(hl)
	if !m.Highlighted() || !strings.Contains(m.View(), "\x1b[38;") {
		t.Error("the highlight result should style the view")
	}
	if _, cmd := m.Update(keyMsg("j")); len(collect(cmd)) != 0 {
		t.Error("a highlighted document should not be highlighted again")
	}
}

func TestSupersededHighlightIsSkipped(t *testing.T) {
	m := sized(New(Options{}), 60, 12)
	d := docOf("/a.go", []string{"package main"})
	m, _ = m.Open(d.Path, nil)
	m, cmd := m.Update(DocLoadedMsg{Path: d.Path, Gen: m.gen, Doc: d})
	m, _ = m.Open("/b.go", nil)
	for _, msg := range collect(cmd) {
		if h, ok := msg.(DocHighlightedMsg); ok && !h.Skipped {
			t.Error("a highlight whose load was superseded should skip the work")
		}
	}
}

func TestBigDocHighlightsChunksAroundViewport(t *testing.T) {
	m := sized(New(Options{MaxHighlightBytes: 100}), 60, 12)
	d := docOf("/big.go", numbered(5000))
	m, _ = m.Open(d.Path, nil)
	m, cmd := m.Update(DocLoadedMsg{Path: d.Path, Gen: m.gen, Doc: d})
	var froms []int
	for _, msg := range collect(cmd) {
		if h, ok := msg.(DocHighlightedMsg); ok {
			froms = append(froms, h.From)
			m, _ = m.Update(h)
		}
	}
	if fmt.Sprint(froms) != "[0]" {
		t.Fatalf("first chunk requests = %v, want [0]", froms)
	}
	if d.Styled[0] == "" || d.Styled[chunkLines] != "" {
		t.Error("only the first chunk should be styled")
	}
	m, cmd = m.Update(keyMsg("G"))
	froms = nil
	for _, msg := range collect(cmd) {
		if h, ok := msg.(DocHighlightedMsg); ok {
			froms = append(froms, h.From)
		}
	}
	if fmt.Sprint(froms) != "[4000]" {
		t.Errorf("after G, requests = %v, want [4000]", froms)
	}
}

func TestClickPlacesCursor(t *testing.T) {
	m := viewerWith(t, numbered(100), 60, 12)
	g := m.gutterWidth()
	m, _ = m.Update(tea.MouseMsg{X: g + 3, Y: 1 + 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.cur != (Pos{4, 3}) {
		t.Errorf("click: cur = %v, want {4 3}", m.cur)
	}
	m, _ = m.Update(tea.MouseMsg{X: g + 3, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.cur != (Pos{4, 3}) {
		t.Errorf("a click on the header should not move the cursor, cur = %v", m.cur)
	}
}

func TestPendingSpotApplied(t *testing.T) {
	m := sized(New(Options{}), 60, 12)
	d := docOf("/a.go", numbered(300))
	m, _ = m.Open(d.Path, &Spot{Line: 150, Col: 3, Top: 145})
	m, _ = m.Update(DocLoadedMsg{Path: d.Path, Gen: m.gen, Doc: d})
	if m.cur != (Pos{150, 3}) || m.top != 145 {
		t.Errorf("pending spot: cur=%v top=%d, want {150 3} and 145", m.cur, m.top)
	}
	r, _ := m.Reload()
	if r.pending == nil || *r.pending != (Spot{150, 3, 145}) {
		t.Errorf("Reload should restore the same spot, got %+v", r.pending)
	}
}

func TestMarkdownRenderedByDefaultAndMToggles(t *testing.T) {
	m := sized(New(Options{}), 60, 12)
	d := docOf("/README.md", []string{"# Title", "", "Some **bold** text."})
	m, _ = m.Open(d.Path, nil)
	m, cmd := m.Update(DocLoadedMsg{Path: d.Path, Gen: m.gen, Doc: d})
	var md tea.Msg
	for _, msg := range collect(cmd) {
		switch msg.(type) {
		case MarkdownRenderedMsg:
			md = msg
		case DocHighlightedMsg:
			t.Error("rendered markdown should not request source highlighting")
		}
	}
	if md == nil {
		t.Fatal("a markdown document should be rendered through a command")
	}
	m, _ = m.Update(md)
	if v := ansi.Strip(m.View()); strings.Contains(v, "**bold**") || !strings.Contains(v, "bold") {
		t.Errorf("rendered view should not show raw markdown:\n%s", v)
	}
	m, cmd = m.Update(keyMsg("m"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "**bold**") {
		t.Errorf("m should show the raw source:\n%s", v)
	}
	if cmd == nil {
		t.Error("raw mode should request highlighting")
	}
	_, cmd = m.Update(keyMsg("e"))
	for _, msg := range collect(cmd) {
		if o, ok := msg.(editor.OpenMsg); ok && o.Target.Line != 1 {
			t.Errorf("raw markdown opens at the cursor, got %+v", o.Target)
		}
	}
}

func TestReopenedMarkdownRendersAgain(t *testing.T) {
	m := sized(New(Options{}), 60, 12)
	d := docOf("/README.md", []string{"# Title"})
	for i := range 2 {
		m, _ = m.Open(d.Path, nil)
		_, cmd := m.Update(DocLoadedMsg{Path: d.Path, Gen: m.gen, Doc: d})
		var rendered bool
		for _, msg := range collect(cmd) {
			_, ok := msg.(MarkdownRenderedMsg)
			rendered = rendered || ok
		}
		if !rendered {
			t.Errorf("open #%d of the same cached document did not render it", i+1)
		}
	}
}

func TestBinaryAndErrorViews(t *testing.T) {
	m := sized(New(Options{}), 60, 8)
	bin := &Document{Path: "/x.bin", Binary: true}
	m = deliver(t, m, bin)
	if !strings.Contains(m.View(), "binary file") {
		t.Error("binary documents show a placeholder")
	}
	m, _ = m.Open("/gone", nil)
	m, _ = m.Update(DocLoadedMsg{Path: "/gone", Gen: m.gen, Err: fmt.Errorf("no such file")})
	if !strings.Contains(m.View(), "no such file") {
		t.Error("load errors should show")
	}
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
