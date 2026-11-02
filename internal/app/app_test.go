package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/files"
	"deck/internal/git"
	"deck/internal/httpc"
	"deck/internal/ui"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "f1":
		return tea.KeyMsg{Type: tea.KeyF1}
	}
	if rest, ok := strings.CutPrefix(s, "alt+"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(rest), Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func update(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func sized(t *testing.T, w, h int) Model {
	t.Helper()
	return update(t, newModel(), tea.WindowSizeMsg{Width: w, Height: h})
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func newModel() Model { return New(config.Default(), nil, nil) }

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
	inserting := update(t, newModel(), keyMsg("4"), keyMsg("i"))
	if _, cmd := inserting.Update(keyMsg("q")); isQuit(cmd) {
		t.Error("'q' should be typed into the HTTP input, not quit, in insert mode")
	}
}

func TestHTTPNormalModeKeepsGlobalKeys(t *testing.T) {
	atHTTP := update(t, newModel(), keyMsg("4"))
	if _, cmd := atHTTP.Update(keyMsg("q")); !isQuit(cmd) {
		t.Error("'q' should quit from HTTP normal mode")
	}
	if got := update(t, atHTTP, keyMsg("2")).active; got != tabGit {
		t.Errorf("'2' from HTTP normal mode: active = %d, want tabGit", got)
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

func TestStatusLeft(t *testing.T) {
	got := strings.Join(statusLeft("deck", "main", 3, 2), " · ")
	want := "deck · ⎇ main · 3 dirty · ▶ 2 running"
	if got != want {
		t.Errorf("statusLeft = %q, want %q", got, want)
	}
	if got := statusLeft("deck", "", 0, 0); len(got) != 1 || got[0] != "deck" {
		t.Errorf("minimal statusLeft = %q, want just the project", got)
	}
}

func TestEditorCloseRefreshesGitWhileOnFiles(t *testing.T) {
	m := newModel()
	before := m.git.Reloads()
	next, cmd := m.Update(editorClosedMsg{})
	if cmd == nil {
		t.Fatal("closing the editor should return a refresh command")
	}
	got := update(t, next.(Model), collect(cmd)...)
	if got.active != tabFiles {
		t.Fatalf("active = %d, want to stay on Files", got.active)
	}
	if got.git.Reloads() != before+1 {
		t.Errorf("git reloads = %d, want %d: the refresh never reached the Git pane", got.git.Reloads(), before+1)
	}
}

func TestCommitMsgReadyReachesGitFromAnotherTab(t *testing.T) {
	m := sized(t, 120, 30)
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
	m := newModel()
	opened := update(t, m, keyMsg("?"))
	if !opened.help.open {
		t.Error("'?' on a non-capturing tab should open help")
	}
	if closed := update(t, opened, keyMsg("?")); closed.help.open {
		t.Error("'?' again should close help")
	}
	if typed := update(t, m, keyMsg("4"), keyMsg("i"), keyMsg("?")); typed.help.open {
		t.Error("'?' while the HTTP input is focused should not toggle help")
	}
}

func TestAsyncRoutedWhileOtherTabActive(t *testing.T) {
	cases := []struct {
		name  string
		onTab string
		msg   tea.Msg
		dest  ui.Dest
		check func(Model) bool
	}{
		{"git refresh", "1", git.RefreshMsg{}, ui.DestGit,
			func(m Model) bool { return m.git.Reloads() == newModel().git.Reloads()+1 }},
		{"commit message", "3", git.CommitMsgReady{Err: errors.New("routed-boom")}, ui.DestGit,
			func(m Model) bool { return strings.Contains(ansi.Strip(m.git.View()), "routed-boom") }},
		{"http response", "1", httpc.RespMsg{}, ui.DestHTTP,
			func(m Model) bool { return strings.Contains(ansi.Strip(m.httpc.View()), "0 bytes") }},
		{"files reveal", "2", files.RevealMsg{Target: editor.Target{Path: appGo(), Line: 3}}, ui.DestFiles,
			func(m Model) bool { return m.files.SelectedPath() == appGo() && m.files.ViewerLoc().Path == appGo() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.msg.(ui.Routed).RouteTo(); got != c.dest {
				t.Fatalf("RouteTo = %v, want %v", got, c.dest)
			}
			m := update(t, sized(t, 120, 30), keyMsg(c.onTab), c.msg)
			if !c.check(m) {
				t.Errorf("%T did not reach its pane while another tab was active", c.msg)
			}
		})
	}
}

func TestRunnerOutputRoutedWhileOtherTabActive(t *testing.T) {
	cfg := config.Default()
	cfg.Tasks = []config.Task{{Name: "echo", Cmd: "echo routed-output"}}
	m := update(t, New(cfg, nil, nil), tea.WindowSizeMsg{Width: 120, Height: 30}, keyMsg("3"))
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(Model)
	var out tea.Msg
	for _, msg := range collect(cmd) {
		if r, ok := msg.(ui.Routed); ok && r.RouteTo() == ui.DestRun {
			out = msg
		}
	}
	if out == nil {
		t.Fatal("running a task should stream a routed output message")
	}
	m = update(t, m, keyMsg("1"))
	next, cmd = m.Update(out)
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.run.View()), "routed-output") {
		t.Fatal("runner output did not reach the Run pane while Files was active")
	}
	var exit tea.Msg
	for exit == nil && cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		next, cmd = m.Update(msg)
		m = next.(Model)
		if r, ok := msg.(ui.Routed); ok && r.RouteTo() == ui.DestRun && m.run.RunningCount() == 0 {
			exit = msg
		}
	}
	if exit == nil || m.run.RunningCount() != 0 {
		t.Error("the task's exit did not reach the Run pane while Files was active")
	}
}

func appGo() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "app.go")
}

func TestEditorOpenMsgLaunchesEditor(t *testing.T) {
	_, cmd := sized(t, 120, 30).Update(editor.OpenMsg{Target: editor.Target{Path: appGo(), Line: 3, Col: 2}})
	if cmd == nil {
		t.Error("editor.OpenMsg should hand off to the editor")
	}
}

func TestEditorCloseReloadsFiles(t *testing.T) {
	_, cmd := sized(t, 120, 30).Update(editorClosedMsg{})
	var reload bool
	for _, msg := range collect(cmd) {
		if _, ok := msg.(files.ReloadMsg); ok {
			reload = true
		}
	}
	if !reload {
		t.Error("closing the editor should reload the Files tab in place")
	}
}

func TestInitLoadsFilesViewer(t *testing.T) {
	m := sized(t, 120, 30)
	m = update(t, m, collect(m.Init())...)
	if m.files.ViewerLoc().Path == "" {
		t.Error("Init should load the selected file into the viewer")
	}
}

func TestInitLoadsGitStatus(t *testing.T) {
	var refreshed bool
	for _, msg := range collect(newModel().Init()) {
		if _, ok := msg.(git.RefreshMsg); ok {
			refreshed = true
		}
	}
	if !refreshed {
		t.Error("Init should start the first async git status load")
	}
}

func TestStatusLoadedReachesGitAndNamesProject(t *testing.T) {
	m := update(t, sized(t, 120, 30), git.RefreshMsg{})
	snap := git.Snapshot{Branch: "feat/x", Upstream: "origin/feat/x", Ahead: 2}
	m = update(t, m, git.StatusLoadedMsg{Seq: 1, Repo: &git.Repo{Top: "/work/project-root"}, Snapshot: snap})
	if m.active != tabFiles {
		t.Fatalf("active = %d, want Files", m.active)
	}
	status := ansi.Strip(m.statusBar())
	if !strings.Contains(status, "project-root") || !strings.Contains(status, "⎇ feat/x ↑2 ↓0") {
		t.Errorf("status bar = %q, want the repo-root name and ahead/behind", status)
	}
}
