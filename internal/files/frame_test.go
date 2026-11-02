package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func writeLines(t *testing.T, path string, n int) {
	t.Helper()
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wheel(x, y int, down bool) tea.MouseMsg {
	b := tea.MouseButtonWheelUp
	if down {
		b = tea.MouseButtonWheelDown
	}
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

func TestViewIsExactlyTheOuterBox(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 90)
	writeLines(t, filepath.Join(dir, long+".txt"), 300)
	if err := os.Mkdir(filepath.Join(dir, long), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{60, 11}, {80, 19}, {200, 45}} {
		m := New(dir, "dracula")
		m.SetSize(size[0], size[1])
		v := m.View()
		if got := lipgloss.Height(v); got != size[1] {
			t.Errorf("%v: height = %d, want %d", size, got, size[1])
		}
		if got := lipgloss.Width(v); got > size[0] {
			t.Errorf("%v: width = %d, want <= %d", size, got, size[0])
		}
	}
}

func TestHeaderPath(t *testing.T) {
	root := "/home/u/src/deck"
	cases := []struct {
		dir   string
		w     int
		want  string
		start string
	}{
		{root, 40, "deck", ""},
		{root + "/internal/app", 40, "deck/internal/app", ""},
		{root + "/internal/some-very-long-directory-name", 20, "", "…"},
		{"/home/u/src", 40, "/home/u/src", ""},
	}
	for _, c := range cases {
		got := headerPath(root, c.dir, c.w)
		if w := ansi.StringWidth(got); w > c.w {
			t.Errorf("headerPath(%q, %d) = %q is %d wide", c.dir, c.w, got, w)
		}
		if c.want != "" && got != c.want {
			t.Errorf("headerPath(%q) = %q, want %q", c.dir, got, c.want)
		}
		if c.start != "" && (!strings.HasPrefix(got, c.start) || !strings.HasSuffix(got, "name")) {
			t.Errorf("headerPath(%q) = %q, want it left-truncated keeping the tail", c.dir, got)
		}
	}
}

func TestWheelOverPreviewScrollsPreview(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, filepath.Join(dir, "a-long.txt"), 300)
	writeLines(t, filepath.Join(dir, "b.txt"), 3)
	m := New(dir, "dracula")
	m.SetSize(90, 20)
	listW := m.listWidth()

	m, _ = m.Update(wheel(listW+5, 4, true))
	if m.preview.YOffset != 3 {
		t.Errorf("preview offset = %d, want 3 after one wheel notch over the preview", m.preview.YOffset)
	}
	if m.list.Index() != 0 {
		t.Errorf("wheel over the preview moved the file cursor to %d", m.list.Index())
	}

	m, _ = m.Update(wheel(1, 4, true))
	if m.list.Index() != 1 {
		t.Errorf("wheel over the list: cursor = %d, want 1", m.list.Index())
	}
}

func TestClickSelectsRow(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeLines(t, filepath.Join(dir, name), 1)
	}
	m := New(dir, "dracula")
	m.SetSize(90, 20)
	m, _ = m.Update(tea.MouseMsg{X: 1, Y: ListTop + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if got := filepath.Base(m.SelectedPath()); got != "c.go" {
		t.Errorf("clicking the third row selected %q, want c.go", got)
	}
	m, _ = m.Update(tea.MouseMsg{X: 1, Y: ListTop + 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if got := filepath.Base(m.SelectedPath()); got != "c.go" {
		t.Errorf("clicking below the last row changed the selection to %q", got)
	}
	m, _ = m.Update(tea.MouseMsg{X: m.listWidth() + 3, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.focus != focusPreview {
		t.Error("clicking the preview should focus it")
	}
}

func TestLongNamesTruncatedInRows(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("n", 80) + ".go"
	writeLines(t, filepath.Join(dir, long), 1)
	m := New(dir, "dracula")
	m.SetSize(60, 12)
	for i, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(line), strings.Repeat("n", 30)) && !strings.Contains(ansi.Strip(line), "…") {
			t.Errorf("row %d shows a long name without truncation: %q", i, ansi.Strip(line))
		}
	}
}

func TestLoadErrorShownInPane(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, "dracula")
	m.SetSize(80, 12)
	m.dir = filepath.Join(dir, "gone")
	m.load()
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "no such file") {
		t.Errorf("load error not shown:\n%s", v)
	}
	if got := lipgloss.Height(m.View()); got != 12 {
		t.Errorf("height with an error = %d, want 12", got)
	}
}
