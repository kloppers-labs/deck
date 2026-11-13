package viewer

import (
	"bytes"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

var selLines = []string{"ab\tc", "xyz", "😀é"}

func selViewer(t *testing.T) Model {
	t.Helper()
	return viewerWith(t, selLines, 60, 12)
}

func wantSel(t *testing.T, m Model, want Range) {
	t.Helper()
	got, ok := m.Selection()
	if !ok || got != want {
		t.Fatalf("Selection() = %+v, %v; want %+v, true", got, ok, want)
	}
}

func captureClipboard(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := clipboardOut
	clipboardOut = &buf
	t.Cleanup(func() { clipboardOut = prev })
	return &buf
}

func mouseAt(m Model, line, col int, action tea.MouseAction, b tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: m.gutterWidth() + col, Y: 1 + line, Action: action, Button: b}
}

func TestVisualCharSelectionExtends(t *testing.T) {
	m := press(t, selViewer(t), "v", "l", "l", "j")
	wantSel(t, m, Range{Pos{0, 0}, Pos{1, 3}})
}

func TestVisualLineSelection(t *testing.T) {
	m := press(t, selViewer(t), "V", "j")
	wantSel(t, m, Range{Pos{0, 0}, Pos{1, 3}})
	if got := m.SelectionText(); got != "ab\tc\nxyz" {
		t.Errorf("SelectionText() = %q", got)
	}
}

func TestSelectionWithTabsAndWide(t *testing.T) {
	m := press(t, selViewer(t), "j", "j", "v", "l")
	wantSel(t, m, Range{Pos{2, 0}, Pos{2, 2}})
	if got := m.SelectionText(); got != "😀é" {
		t.Errorf("SelectionText() = %q, want 😀é", got)
	}
	raw, _ := m.lines()
	var sel *Span
	for _, s := range m.rowSpans(raw, 2, 0, m.layout()) {
		if s.SGR == selectionSGR {
			sel = &s
		}
	}
	if sel == nil || sel.Start != 0 || sel.End != 3 {
		t.Errorf("selection span = %+v, want cells 0..3 (emoji is two cells)", sel)
	}
}

func TestEscClearsSelectionFirst(t *testing.T) {
	m := press(t, selViewer(t), "v", "l")
	if !m.WantsEsc() {
		t.Fatal("a selection should claim esc")
	}
	m = press(t, m, "esc")
	if _, ok := m.Selection(); ok || m.WantsEsc() {
		t.Error("esc should clear the selection and release esc")
	}
}

func TestYankSelection(t *testing.T) {
	buf := captureClipboard(t)
	m := press(t, selViewer(t), "v", "l")
	_, cmd := m.Update(keyMsg("y"))
	collect(cmd)
	if buf.String() != ansi.SetSystemClipboard("ab") {
		t.Errorf("clipboard = %q, want OSC52 of ab", buf.String())
	}
}

func TestYankRef(t *testing.T) {
	buf := captureClipboard(t)
	m := press(t, selViewer(t), "V", "j")
	_, cmd := m.Update(keyMsg("Y"))
	collect(cmd)
	if buf.String() != ansi.SetSystemClipboard("src/a.cs:1-2") {
		t.Errorf("clipboard = %q, want OSC52 of src/a.cs:1-2", buf.String())
	}
}

func TestMouseDragSelects(t *testing.T) {
	m := selViewer(t)
	m, _ = m.Update(mouseAt(m, 0, 0, tea.MouseActionPress, tea.MouseButtonLeft))
	if _, ok := m.Selection(); ok {
		t.Fatal("a press alone should not select")
	}
	m, _ = m.Update(mouseAt(m, 1, 2, tea.MouseActionMotion, tea.MouseButtonLeft))
	m, _ = m.Update(mouseAt(m, 1, 2, tea.MouseActionRelease, tea.MouseButtonNone))
	wantSel(t, m, Range{Pos{0, 0}, Pos{1, 3}})
}

func TestShiftClickExtends(t *testing.T) {
	m := selViewer(t)
	click := mouseAt(m, 1, 1, tea.MouseActionPress, tea.MouseButtonLeft)
	click.Shift = true
	m, _ = m.Update(click)
	wantSel(t, m, Range{Pos{0, 0}, Pos{1, 2}})
}

func TestSelectionClearsOnOtherFile(t *testing.T) {
	m := press(t, selViewer(t), "V")
	m = deliver(t, m, docOf("/repo/src/b.cs", selLines))
	if _, ok := m.Selection(); ok {
		t.Error("opening another file should clear the selection")
	}
}

func TestSelectionSurvivesReloadClamped(t *testing.T) {
	m := press(t, selViewer(t), "V", "j", "j")
	m, _ = m.Reload()
	m, _ = m.Update(DocLoadedMsg{Path: m.path, Gen: m.gen, Doc: docOf(m.path, []string{"ab"})})
	got, ok := m.Selection()
	if !ok || got.End.Line != 0 {
		t.Errorf("after shrinking reload: %+v, %v; want a selection ending on line 0", got, ok)
	}
}

func TestSelectionNotCapturing(t *testing.T) {
	if press(t, selViewer(t), "v").Capturing() {
		t.Error("selecting must not capture global keys")
	}
}

func mentions(msgs []tea.Msg) []ui.MentionMsg {
	var out []ui.MentionMsg
	for _, msg := range msgs {
		if mm, ok := msg.(ui.MentionMsg); ok {
			out = append(out, mm)
		}
	}
	return out
}

func TestMentionEmits(t *testing.T) {
	m := press(t, selViewer(t), "V", "j")
	_, cmd := m.Update(keyMsg("@"))
	got := mentions(collect(cmd))
	want := ui.MentionMsg{Path: "/repo/src/a.cs", LineStart: 0, LineEnd: 1}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("mentions = %+v, want %+v", got, want)
	}
	m = press(t, selViewer(t), "j", "j")
	_, cmd = m.Update(keyMsg("@"))
	got = mentions(collect(cmd))
	if len(got) != 1 || got[0].LineStart != 2 || got[0].LineEnd != 2 {
		t.Errorf("without a selection: %+v, want the cursor line 2", got)
	}
}

func TestRenderedSection(t *testing.T) {
	rendered := []string{"  Title", "", "  ## Goals", "x", "  ## Plan"}
	headings := []Symbol{
		{Name: "Title", Range: Range{Pos{0, 0}, Pos{12, 0}}},
		{Name: "Goals", Range: Range{Pos{4, 0}, Pos{8, 0}}},
		{Name: "Plan", Range: Range{Pos{9, 0}, Pos{12, 0}}},
	}
	for _, tc := range []struct{ row, want int }{{0, 0}, {1, 0}, {3, 1}, {4, 2}} {
		if got, ok := renderedSection(rendered, headings, tc.row); !ok || got != tc.want {
			t.Errorf("row %d: section %d, %v; want %d", tc.row, got, ok, tc.want)
		}
	}
	if _, ok := renderedSection([]string{"intro"}, headings, 0); ok {
		t.Error("a row before any rendered heading has no section")
	}
}

var mdLines = []string{"# Title", "", "intro", "", "## Goals", "", "goal one", "goal two", "", "## Plan", "", "step"}

func renderedViewer(t *testing.T) Model {
	t.Helper()
	m := sized(New(Options{Root: "/repo"}), 80, 30)
	m, _ = m.Open("/repo/x.md", nil)
	m, cmd := m.Update(DocLoadedMsg{Path: "/repo/x.md", Gen: m.gen, Doc: docOf("/repo/x.md", mdLines)})
	for _, msg := range collect(cmd) {
		m, _ = m.Update(msg)
	}
	if !m.showingMD() {
		t.Fatal("markdown should render")
	}
	return m
}

func goalsRow(t *testing.T, m Model) int {
	t.Helper()
	_, plain := m.lines()
	for i, l := range plain {
		if bytes.Contains([]byte(l), []byte("goal one")) {
			return i
		}
	}
	t.Fatal("rendered markdown lacks the goals body")
	return 0
}

func TestSectionWhileRendered(t *testing.T) {
	m := renderedViewer(t)
	m.moveLine(goalsRow(t, m))
	title, start, end, ok := m.Section()
	if !ok || title != "Goals" || start != 4 || end != 8 {
		t.Errorf("Section() = %q %d-%d %v, want Goals 4-8", title, start, end, ok)
	}
}

func TestVOnRenderedMarkdownSwitchesToRaw(t *testing.T) {
	m := renderedViewer(t)
	m.moveLine(goalsRow(t, m))
	m = press(t, m, "v")
	if !m.RawMarkdown() {
		t.Fatal("v should switch markdown to source")
	}
	if m.cur.Line != 4 {
		t.Errorf("cursor line = %d, want the section start 4", m.cur.Line)
	}
	if _, ok := m.Selection(); !ok {
		t.Error("v should start a selection in the source")
	}
}

func TestSectionAfterInlineCodeHeading(t *testing.T) {
	lines := []string{"# Title", "", "intro", "", "## The `foo` *bold* [link](http://x) thing", "", "body a", "",
		"## Later", "", "later body", "", "## Last", "", "last body"}
	m := sized(New(Options{Root: "/repo"}), 80, 40)
	m, _ = m.Open("/repo/y.md", nil)
	m, cmd := m.Update(DocLoadedMsg{Path: "/repo/y.md", Gen: m.gen, Doc: docOf("/repo/y.md", lines)})
	for _, msg := range collect(cmd) {
		m, _ = m.Update(msg)
	}
	_, plain := m.lines()
	for i, l := range plain {
		if bytes.Contains([]byte(l), []byte("last body")) {
			m.moveLine(i)
		}
	}
	if title, s, e, ok := m.Section(); !ok || title != "Last" || s != 12 || e != 14 {
		t.Errorf("Section() = %q %d-%d %v, want Last 12-14", title, s, e, ok)
	}
	for i, l := range plain {
		if bytes.Contains([]byte(l), []byte("body a")) {
			m.moveLine(i)
		}
	}
	if title, _, _, _ := m.Section(); title != "The `foo` *bold* [link](http://x) thing" {
		t.Errorf("Section() title = %q, want the heading with inline markdown", title)
	}
}

func TestRenderedSectionSkipsUnrenderedHeading(t *testing.T) {
	rendered := []string{"  Title", "x", "  ## Plan", "y"}
	headings := []Symbol{
		{Name: "Title", Range: Range{Pos{0, 0}, Pos{9, 0}}},
		{Name: "Gone heading", Range: Range{Pos{3, 0}, Pos{5, 0}}},
		{Name: "Plan", Range: Range{Pos{6, 0}, Pos{9, 0}}},
	}
	if got, ok := renderedSection(rendered, headings, 3); !ok || got != 2 {
		t.Errorf("section = %d, %v; want Plan (2) even though heading 1 never rendered", got, ok)
	}
}
