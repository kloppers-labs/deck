package files

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/ui"
)

func TestComputeLayout(t *testing.T) {
	cases := []struct {
		name               string
		w, sidebar         int
		show, info         bool
		treeW, viewW, infW int
	}{
		{"default", 120, 32, true, false, 32, 88, 0},
		{"capped at 40%", 60, 32, true, false, 24, 36, 0},
		{"collapsed", 120, 32, false, false, 0, 120, 0},
		{"info", 120, 32, true, true, 32, 64, 24},
		{"info dropped when viewer too narrow", 70, 32, true, true, 28, 42, 0},
		{"minimum sidebar", 200, 4, true, false, minSidebar, 200 - minSidebar, 0},
	}
	for _, c := range cases {
		l := ComputeLayout(c.w, 30, c.sidebar, c.show, c.info)
		if l.Tree.W != c.treeW || l.Viewer.W != c.viewW || l.Info.W != c.infW {
			t.Errorf("%s: tree %d viewer %d info %d, want %d %d %d", c.name, l.Tree.W, l.Viewer.W, l.Info.W, c.treeW, c.viewW, c.infW)
		}
		if l.Tree.W+l.Viewer.W+l.Info.W != c.w || l.Viewer.X != l.Tree.W || l.Info.X != l.Tree.W+l.Viewer.W {
			t.Errorf("%s: regions must tile the width: %+v", c.name, l)
		}
	}
}

func TestSidebarCollapseCtrlB(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := started(t, dir, 120, 30)
	if !m.layout().ShowTree {
		t.Fatal("the tree shows at 120 columns")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if m.layout().ShowTree || m.focus != focusViewer {
		t.Errorf("ctrl+b hides the tree and focuses the viewer: show=%v focus=%d", m.layout().ShowTree, m.focus)
	}
	if r := m.layout().Viewer; r.X != 0 || r.W != 120 {
		t.Errorf("the viewer takes the whole width, got %+v", r)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.layout().ShowTree {
		t.Error("a second ctrl+b brings the tree back")
	}
}

func TestAutoCollapseBelow90Cols(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := started(t, dir, 89, 30)
	if m.layout().ShowTree {
		t.Error("the tree collapses below 90 columns")
	}
	m.SetSize(90, 30)
	if !m.layout().ShowTree {
		t.Error("the tree returns at 90 columns")
	}
	m.SetSize(80, 30)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.layout().ShowTree {
		t.Error("ctrl+b shows the tree even when it auto-collapsed")
	}
}

func TestResizeAngleBrackets(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := started(t, dir, 160, 30)
	w := m.layout().Tree.W
	m, _ = press(t, m, ">")
	if got := m.layout().Tree.W; got != w+sidebarStep {
		t.Errorf("> widens the tree: %d → %d", w, got)
	}
	m, _ = press(t, m, "<", "<")
	if got := m.layout().Tree.W; got != w-sidebarStep {
		t.Errorf("< narrows the tree: %d → %d", w, got)
	}
	for range 40 {
		m, _ = press(t, m, ">")
	}
	if got := m.layout().Tree.W; got != 160*40/100 {
		t.Errorf("the tree is capped at 40%%, got %d", got)
	}
}

func TestDragDivider(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	m := started(t, dir, 160, 30)
	div := m.layout().Tree.W - 1
	m, _ = m.Update(tea.MouseMsg{X: div, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.drag {
		t.Fatal("pressing on the divider starts a drag")
	}
	m, _ = m.Update(tea.MouseMsg{X: 45, Y: 7, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if got := m.layout().Tree.W; got != 46 {
		t.Errorf("dragging to x=45 makes the tree 46 wide, got %d", got)
	}
	m, _ = m.Update(tea.MouseMsg{X: 45, Y: 7, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m.drag {
		t.Error("release ends the drag")
	}
	m, _ = m.Update(tea.MouseMsg{X: 60, Y: 7, Action: tea.MouseActionMotion})
	if got := m.layout().Tree.W; got != 46 {
		t.Errorf("motion after release must not resize, got %d", got)
	}
}

func TestFocusBorders(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30})
	m := started(t, dir, 120, 30)
	active := ui.PaneActive.GetBorderTopForeground()
	idle := ui.Pane.GetBorderTopForeground()
	if m.regionStyle(focusTree).GetBorderTopForeground() != active || m.regionStyle(focusViewer).GetBorderTopForeground() != idle {
		t.Error("with the tree focused only the tree uses the PaneActive border")
	}
	m, _ = press(t, m, "enter")
	if m.regionStyle(focusViewer).GetBorderTopForeground() != active || m.regionStyle(focusTree).GetBorderTopForeground() != idle {
		t.Error("with the viewer focused only the viewer uses the PaneActive border")
	}
}

func TestViewFitsAllSizes(t *testing.T) {
	long := strings.Repeat("x", 90)
	dir := fixture(t, map[string]int{long + ".txt": 300, long + "/" + long + ".go": 3})
	for _, info := range []bool{false, true} {
		for _, size := range [][2]int{{60, 11}, {80, 19}, {89, 20}, {120, 35}, {200, 45}} {
			cfg := config.Default()
			cfg.Files.InfoPanel = info
			m := New(dir, cfg)
			m.SetSize(size[0], size[1])
			m, _ = settle(t, m, m.Init())
			v := m.View()
			if got := lipgloss.Height(v); got != size[1] {
				t.Errorf("info=%v %v: height = %d, want %d", info, size, got, size[1])
			}
			for i, line := range strings.Split(v, "\n") {
				if w := ansi.StringWidth(line); w > size[0] {
					t.Errorf("info=%v %v: row %d is %d wide", info, size, i, w)
				}
			}
		}
	}
}
