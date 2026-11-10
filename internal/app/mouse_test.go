package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/files"
)

func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func TestTabHitTest(t *testing.T) {
	widths := []int{9, 7, 7, 8}
	cases := map[int]int{-1: -1, 0: 0, 8: 0, 9: 1, 15: 1, 16: 2, 22: 2, 23: 3, 30: 3, 31: -1, 90: -1}
	for x, want := range cases {
		if got := tabHitTest(x, widths); got != want {
			t.Errorf("tabHitTest(%d) = %d, want %d", x, got, want)
		}
	}
}

func TestClickOnTabSwitches(t *testing.T) {
	m := sized(t, 120, 30)
	widths := m.tabWidths()
	gitX := widths[0] + 1
	if got := update(t, m, press(gitX, 1)).active; got != tabGit {
		t.Errorf("clicking the Git tab: active = %d, want tabGit", got)
	}
	httpX := widths[0] + widths[1] + widths[2] + 1
	if got := update(t, m, press(httpX, 0)).active; got != tabHTTP {
		t.Errorf("clicking the HTTP tab: active = %d, want tabHTTP", got)
	}
	motion := tea.MouseMsg{X: gitX, Y: 1, Action: tea.MouseActionMotion}
	if got := update(t, m, motion).active; got != tabFiles {
		t.Errorf("hovering a tab should not switch: active = %d", got)
	}
	if got := update(t, m, press(widths[0]+widths[1]+widths[2]+widths[3]+5, 1)).active; got != tabFiles {
		t.Errorf("clicking the rule beside the tabs should not switch: active = %d", got)
	}
}

func TestMouseYTranslated(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	m := update(t, New(config.Default(), nil, nil), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = update(t, m, press(2, tabBarHeight+files.ListTop+1))
	if got := filepath.Base(m.files.SelectedPath()); got != "b.go" {
		t.Errorf("clicking the second row selected %q, want b.go: the click's Y was not translated", got)
	}
}

func TestStatusAndFooterClicksIgnored(t *testing.T) {
	m := sized(t, 120, 30)
	for _, y := range []int{28, 29} {
		if got := update(t, m, press(2, y)); got.files.SelectedPath() != m.files.SelectedPath() {
			t.Errorf("a click on row %d (status/footer) reached the pane", y)
		}
	}
}

func TestDuplicateMotionDropped(t *testing.T) {
	var m Model
	motion := func(x, y int) tea.MouseMsg { return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion} }
	if m.duplicateMotion(motion(5, 10)) {
		t.Error("the first motion event is not a duplicate")
	}
	if !m.duplicateMotion(motion(5, 10)) {
		t.Error("a repeated motion on the same cell should be dropped")
	}
	if m.duplicateMotion(motion(6, 10)) {
		t.Error("motion to a new cell should pass")
	}
	if m.duplicateMotion(press(6, 10)) {
		t.Error("presses are never dropped")
	}
	if !m.duplicateMotion(motion(6, 10)) {
		t.Error("motion still on the last motion cell should be dropped")
	}
}

func TestWheelOverHelpScrolls(t *testing.T) {
	m := update(t, sized(t, 80, 16), keyMsg("?"))
	m = update(t, m, tea.MouseMsg{X: 40, Y: 8, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.help.scroll == 0 {
		t.Error("the wheel should scroll the help modal")
	}
}
