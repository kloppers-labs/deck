package app

import (
	"fmt"
	"math/rand/v2"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/runner"
	"deck/internal/ui"
)

func sleepMarker() string {
	return fmt.Sprintf("sleep 30.%06d", rand.IntN(1_000_000))
}

func alive(marker string) bool {
	out, err := exec.Command("pgrep", "-f", marker).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func withRunningTask(t *testing.T, marker string) Model {
	t.Helper()
	cfg := config.Default()
	cfg.Tasks = []config.Task{{Name: "api", Cmd: marker}}
	m := update(t, New(cfg, nil), tea.WindowSizeMsg{Width: 120, Height: 30}, keyMsg("3"), keyMsg("enter"), keyMsg("1"))
	t.Cleanup(func() { m.StopAll(5 * time.Second) })
	if m.run.RunningCount() != 1 {
		t.Fatal("the task should be running")
	}
	return m
}

func TestQuitIdleImmediate(t *testing.T) {
	m := sized(t, 120, 30)
	if _, cmd := m.Update(keyMsg("q")); !isQuit(cmd) {
		t.Error("q with nothing running should quit at once")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); !isQuit(cmd) {
		t.Error("ctrl+c with nothing running should quit at once")
	}
}

func TestQuitWithRunningAsks(t *testing.T) {
	m := withRunningTask(t, sleepMarker())
	next, cmd := m.Update(keyMsg("q"))
	m = next.(Model)
	if cmd != nil && isQuit(cmd) {
		t.Fatal("q with a running task should ask first")
	}
	if !m.quit.asking {
		t.Fatal("the quit confirmation should be open")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "1 task running (api): quit and stop them? [y/N]") {
		t.Errorf("modal text missing:\n%s", v)
	}
	if got := update(t, m, keyMsg("2")); got.active != tabFiles || got.quit.asking {
		t.Error("any other key should only dismiss the modal")
	}
	m = update(t, m, keyMsg("n"))
	if m.quit.asking || m.run.RunningCount() != 1 {
		t.Error("n should cancel and leave the task running")
	}
}

func TestYStopsThenQuits(t *testing.T) {
	marker := sleepMarker()
	m := withRunningTask(t, marker)
	m = update(t, m, keyMsg("q"))
	_, cmd := m.Update(keyMsg("y"))
	if cmd == nil {
		t.Fatal("y should stop the tasks and quit")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Errorf("y finished with %T, want tea.QuitMsg", msg)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("stopping the tasks never finished")
	}
	if alive(marker) {
		t.Error("the task survived quitting")
	}
}

func TestDoubleCtrlCForces(t *testing.T) {
	m := withRunningTask(t, sleepMarker())
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)
	if isQuit(cmd) || !m.quit.asking {
		t.Fatal("the first ctrl+c should ask")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); !isQuit(cmd) {
		t.Error("a second ctrl+c should force quit")
	}
}

func pump(t *testing.T, m Model, cmd tea.Cmd, until func(Model) bool) Model {
	t.Helper()
	pending := []tea.Cmd{cmd}
	deadline := time.Now().Add(5 * time.Second)
	for !until(m) && len(pending) > 0 && time.Now().Before(deadline) {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		got := make(chan tea.Msg, 1)
		go func() { got <- c() }()
		var msg tea.Msg
		select {
		case msg = <-got:
		case <-time.After(500 * time.Millisecond):
			continue
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case nil, spinner.TickMsg, toastExpireMsg:
		default:
			next, more := m.Update(msg)
			m = next.(Model)
			pending = append(pending, more)
		}
	}
	return m
}

func idle(m Model) bool { return m.run.RunningCount() == 0 }

func startTask(t *testing.T, task config.Task) Model {
	t.Helper()
	cfg := config.Default()
	cfg.Tasks = []config.Task{task}
	m := update(t, New(cfg, nil), tea.WindowSizeMsg{Width: 120, Height: 30}, keyMsg("3"))
	next, cmd := m.Update(keyMsg("enter"))
	m = update(t, next.(Model), keyMsg("1"))
	return pump(t, m, cmd, func(m Model) bool { return idle(m) && len(m.toasts) > 0 })
}

func TestCtrlRRerunsLastFromOtherTab(t *testing.T) {
	m := startTask(t, config.Task{Name: "echo", Cmd: "echo rerun-me"})
	if !idle(m) {
		t.Fatal("the first run should have finished")
	}
	m = update(t, m, keyMsg("2"))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if m.active != tabGit {
		t.Error("ctrl+r should not switch tabs")
	}
	if m.run.RunningCount() != 1 {
		t.Error("ctrl+r should re-run the last task from another tab")
	}
	pump(t, m, cmd, idle)
}

func TestFailedExitBadgeRedAndToast(t *testing.T) {
	m := startTask(t, config.Task{Name: "boom", Cmd: "exit 4"})
	if m.run.FailedCount() != 1 {
		t.Fatalf("FailedCount = %d, want 1", m.run.FailedCount())
	}
	if label := ansi.Strip(m.tabLabels()[tabRun]); !strings.Contains(label, "✗1") {
		t.Errorf("Run tab label %q should carry a failure badge", label)
	}
	if len(m.toasts) == 0 || m.toasts[len(m.toasts)-1].msg.Level != ui.LevelError ||
		!strings.Contains(m.toasts[len(m.toasts)-1].msg.Text, "boom failed (exit 4)") {
		t.Errorf("toasts = %+v, want a failure toast", m.toasts)
	}
}

func TestFocusTabMsgSwitches(t *testing.T) {
	m := update(t, sized(t, 120, 30), ui.FocusTabMsg{Dest: ui.DestHTTP})
	if m.active != tabHTTP {
		t.Errorf("active = %d, want tabHTTP", m.active)
	}
}

func TestEditorOpenMsgLaunchesEditor(t *testing.T) {
	_, cmd := sized(t, 120, 30).Update(editor.OpenMsg{Target: editor.Target{Path: "/tmp/x.go", Line: 3}})
	if cmd == nil {
		t.Error("an OpenMsg should launch the editor")
	}
}

func TestInitDetectsTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	var found bool
	for _, msg := range collect(New(config.Default(), nil).Init()) {
		if _, ok := msg.(runner.DetectedMsg); ok {
			found = true
		}
	}
	if !found {
		t.Error("Init should start task detection")
	}
}
