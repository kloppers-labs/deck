package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/config"
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
	m := newModel()
	jumped, _ := m.Update(keyMsg("3"))
	if got := jumped.(Model).active; got != tabAI {
		t.Errorf("after '3', active = %d, want tabAI (%d)", got, tabAI)
	}
}

func TestQDoesNotQuitWhileTypingInAI(t *testing.T) {
	m := newModel()
	atAI, _ := m.Update(keyMsg("3")) // jump to AI tab (prompt always captures)
	_, cmd := atAI.(Model).Update(keyMsg("q"))
	if isQuit(cmd) {
		t.Error("'q' should be typed into the AI prompt, not quit, while on the AI tab")
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	m := newModel()
	atAI, _ := m.Update(keyMsg("3"))
	_, cmd := atAI.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) {
		t.Error("ctrl+c should quit even while the AI prompt is focused")
	}
}

func TestStatusLine(t *testing.T) {
	got := statusLine("main", 3, 2, "claude/sonnet")
	want := "⎇ main · 3 dirty · ▶ 2 running · claude/sonnet"
	if got != want {
		t.Errorf("statusLine = %q, want %q", got, want)
	}
	// Outside a repo with nothing running, only the backend shows.
	if got := statusLine("", 0, 0, "claude/sonnet"); got != "claude/sonnet" {
		t.Errorf("minimal statusLine = %q, want just the backend", got)
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

	// On the AI tab the prompt captures, so '?' is typed, not a help toggle.
	atAI, _ := m.Update(keyMsg("3"))
	typed, _ := atAI.(Model).Update(keyMsg("?"))
	if typed.(Model).showHelp {
		t.Error("'?' while the AI prompt is focused should not toggle help")
	}
}
