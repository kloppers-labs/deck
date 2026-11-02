package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/config"
	"deck/internal/git"
	"deck/internal/ui"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func newModel() Model { return New(config.Default()) }

func TestQuitOnFilesTab(t *testing.T) {
	m := newModel()
	_, cmd := m.Update(keyMsg("q"))
	if !isQuit(cmd) {
		t.Error("expected 'q' on Files tab to quit")
	}
}

func TestTabCyclesPanes(t *testing.T) {
	m := newModel()
	next, _ := m.Update(keyMsg("tab"))
	if got := next.(Model).active; got != tabGit {
		t.Errorf("after tab, active = %d, want tabGit (%d)", got, tabGit)
	}
	prev, _ := m.Update(keyMsg("shift+tab"))
	if got := prev.(Model).active; got != tabHTTP {
		t.Errorf("after shift+tab from Files, active = %d, want tabHTTP (%d, the last tab)", got, tabHTTP)
	}
}

func TestNumberKeysJump(t *testing.T) {
	cases := []struct {
		key  string
		want tab
	}{
		{"1", tabFiles},
		{"2", tabGit},
		{"3", tabRun},
		{"4", tabHTTP},
	}
	for _, c := range cases {
		jumped, _ := newModel().Update(keyMsg(c.key))
		if got := jumped.(Model).active; got != c.want {
			t.Errorf("after %q, active = %d, want %d", c.key, got, c.want)
		}
	}
}

func TestNoAITab(t *testing.T) {
	if len(tabNames) != 4 || int(numTabs) != 4 {
		t.Fatalf("tabNames = %v (numTabs %d), want 4 tabs", tabNames, numTabs)
	}
	for _, n := range tabNames {
		if n == "AI" {
			t.Error("the AI tab should be gone")
		}
	}
}

func TestQDoesNotQuitWhileTypingInHTTP(t *testing.T) {
	m := newModel()
	atHTTP, _ := m.Update(keyMsg("4"))
	_, cmd := atHTTP.(Model).Update(keyMsg("q"))
	if isQuit(cmd) {
		t.Error("'q' should be typed into the HTTP input, not quit, while on the HTTP tab")
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	m := newModel()
	atHTTP, _ := m.Update(keyMsg("4"))
	_, cmd := atHTTP.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) {
		t.Error("ctrl+c should quit even while the HTTP input is focused")
	}
}

func TestStatusLine(t *testing.T) {
	got := statusLine("deck", "main", 3, 2)
	want := "deck · ⎇ main · 3 dirty · ▶ 2 running"
	if got != want {
		t.Errorf("statusLine = %q, want %q", got, want)
	}
	if got := statusLine("deck", "", 0, 0); got != "deck" {
		t.Errorf("minimal statusLine = %q, want just the project", got)
	}
}

func TestEditorCloseRefreshesGitWhileOnFiles(t *testing.T) {
	m := newModel()
	before := m.git.Reloads()
	next, cmd := m.Update(editorClosedMsg{})
	if cmd == nil {
		t.Fatal("closing the editor should return a refresh command")
	}
	next, _ = next.(Model).Update(cmd())
	got := next.(Model)
	if got.active != tabFiles {
		t.Fatalf("active = %d, want to stay on Files", got.active)
	}
	if got.git.Reloads() != before+1 {
		t.Errorf("git reloads = %d, want %d: the refresh never reached the Git pane", got.git.Reloads(), before+1)
	}
}

func TestCommitMsgReadyReachesGitFromAnotherTab(t *testing.T) {
	m := newModel()
	atRun, _ := m.Update(keyMsg("3"))
	next, _ := atRun.(Model).Update(git.CommitMsgReady{Err: errors.New("boom")})
	if v := next.(Model).git.View(); !strings.Contains(v, "boom") {
		t.Error("CommitMsgReady should be delivered to the Git pane while another tab is active")
	}
}

// TestViewIsSanitized confirms the View backstop is wired: the rendered frame
// carries no cursor-control escapes or bare control bytes, so a pane can never
// scribble over the tab bar. Sanitizing is idempotent, so a sanitized frame
// equals itself re-sanitized.
func TestViewIsSanitized(t *testing.T) {
	m := newModel()
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := sized.(Model).View()
	if got := ui.SanitizeTerminal(view); got != view {
		t.Error("View output is not sanitized — the tab-bar backstop is not wired")
	}
	// Spot-check: no NUL and no bare carriage return survived into the frame.
	if strings.ContainsAny(view, "\x00\r") {
		t.Error("rendered frame still contains raw control bytes")
	}
}

func TestHelpTogglesOnlyWhenNotCapturing(t *testing.T) {
	m := newModel() // starts on Files, which does not capture
	opened, _ := m.Update(keyMsg("?"))
	if !opened.(Model).showHelp {
		t.Error("'?' on a non-capturing tab should open full help")
	}
	closed, _ := opened.(Model).Update(keyMsg("?"))
	if closed.(Model).showHelp {
		t.Error("'?' again should close full help")
	}

	atHTTP, _ := m.Update(keyMsg("4"))
	typed, _ := atHTTP.(Model).Update(keyMsg("?"))
	if typed.(Model).showHelp {
		t.Error("'?' while the HTTP input is focused should not toggle help")
	}
}
