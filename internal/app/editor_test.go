package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/files"
	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/ui"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "deck-app-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func closedCmds(t *testing.T, m Model, msg editorClosedMsg) []tea.Msg {
	t.Helper()
	_, cmd := m.Update(msg)
	return collect(cmd)
}

func revealOf(msgs []tea.Msg) *editor.Target {
	for _, msg := range msgs {
		if r, ok := msg.(files.RevealMsg); ok {
			return &r.Target
		}
	}
	return nil
}

func TestEditorCloseFollowsWhereMicroEnded(t *testing.T) {
	m := sized(t, 120, 30)
	root := m.ed.cwd
	origin := editor.Target{Path: filepath.Join(root, "app.go"), Line: 3, Col: 1, Top: 1}
	ended := filepath.Join(root, "help.go")
	msgs := closedCmds(t, m, editorClosedMsg{
		origin: origin,
		state:  editor.State{Path: ended, Line: 40, Col: 6, Top: 30, Saved: []string{ended}},
		ok:     true,
	})
	if got := revealOf(msgs); got == nil || *got != (editor.Target{Path: ended, Line: 40, Col: 6, Top: 30}) {
		t.Errorf("reveal = %+v, want help.go at 40:6 top 30", got)
	}
	var invalidated, refreshed bool
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case files.InvalidateMsg:
			invalidated = slices.Equal(msg.Saved, []string{ended})
		case git.RefreshMsg:
			refreshed = true
		}
	}
	if !invalidated || !refreshed {
		t.Errorf("closing should invalidate saved files (%v) and refresh git (%v)", invalidated, refreshed)
	}
}

func TestEditorCloseOriginMode(t *testing.T) {
	cfg := config.Default()
	cfg.EditorReturn = "origin"
	m := update(t, New(cfg, nil, nil), tea.WindowSizeMsg{Width: 120, Height: 30})
	origin := editor.Target{Path: filepath.Join(m.ed.cwd, "app.go"), Line: 3}
	msgs := closedCmds(t, m, editorClosedMsg{origin: origin, state: editor.State{Path: filepath.Join(m.ed.cwd, "help.go"), Line: 9, Col: 1, Top: 1}, ok: true})
	if got := revealOf(msgs); got == nil || *got != origin {
		t.Errorf("origin mode should reveal the original target, got %+v", got)
	}
}

func TestEditorCloseOutsideRootReturnsToOrigin(t *testing.T) {
	m := sized(t, 120, 30)
	origin := editor.Target{Path: filepath.Join(m.ed.cwd, "app.go"), Line: 3}
	msgs := closedCmds(t, m, editorClosedMsg{origin: origin, state: editor.State{Path: "/etc/hosts", Line: 1, Col: 1, Top: 1}, ok: true})
	if got := revealOf(msgs); got == nil || *got != origin {
		t.Errorf("a path outside the root should reveal the origin, got %+v", got)
	}
}

func TestEditorExitReadsAndRemovesState(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	origin := editor.Target{Path: "/work/a.go", Line: 2}
	done := editorExited(origin, state, time.Now().Add(-time.Minute))
	if err := os.WriteFile(state, []byte("/work/b.go\n5 2 1\n/work/b.go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg, ok := done(nil).(editorClosedMsg)
	if !ok || !msg.ok || msg.state.Path != "/work/b.go" || msg.origin != origin {
		t.Fatalf("editorExited = %+v", msg)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Error("the state file should be removed after reading")
	}
}

func TestDetachRaisesToast(t *testing.T) {
	msg := detach(exec.Command("true"), "VS Code")()
	toast, ok := msg.(ui.ToastMsg)
	if !ok || toast.Level != ui.LevelInfo || toast.Text != "opened in VS Code" {
		t.Errorf("detach = %+v, want an info toast", msg)
	}
	msg = detach(exec.Command(filepath.Join(t.TempDir(), "missing-editor")), "X")()
	if toast, ok := msg.(ui.ToastMsg); !ok || toast.Level != ui.LevelError {
		t.Errorf("a failed start should raise an error toast, got %+v", msg)
	}
}

func TestOpenMissingEditorToasts(t *testing.T) {
	cfg := config.Default()
	cfg.Editor = "deck-no-such-editor-xyz"
	m := update(t, New(cfg, nil, nil), tea.WindowSizeMsg{Width: 120, Height: 30})
	_, cmd := m.Update(editor.OpenMsg{Target: editor.Target{Path: appGo()}})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %v, want one toast", msgs)
	}
	if toast, ok := msgs[0].(ui.ToastMsg); !ok || toast.Level != ui.LevelError || !strings.Contains(toast.Text, "editor") {
		t.Errorf("a missing editor should toast, got %+v", msgs[0])
	}
}

func TestSyncedIssuesToastOnce(t *testing.T) {
	m := sized(t, 120, 30)
	_, cmd := m.Update(editor.SyncedMsg{Issues: []string{"fzf not found", "bat not found"}, Notify: true})
	msgs := collect(cmd)
	toast, ok := firstToast(msgs)
	if !ok || toast.Level != ui.LevelWarn || !strings.Contains(toast.Text, "fzf not found; bat not found") {
		t.Errorf("issues should raise one warning toast, got %v", msgs)
	}
	if _, cmd := m.Update(editor.SyncedMsg{Issues: []string{"fzf not found"}}); cmd != nil {
		t.Error("issues already shown should stay quiet")
	}
	_, cmd = m.Update(editor.SyncedMsg{Err: fmt.Errorf("disk full")})
	if toast, ok := firstToast(collect(cmd)); !ok || !strings.Contains(toast.Text, "disk full") {
		t.Error("a sync error should raise a toast")
	}
}

func firstToast(msgs []tea.Msg) (ui.ToastMsg, bool) {
	for _, msg := range msgs {
		if t, ok := msg.(ui.ToastMsg); ok {
			return t, true
		}
	}
	return ui.ToastMsg{}, false
}

func TestInitSyncsMicroBundle(t *testing.T) {
	var synced bool
	for _, msg := range collect(newModel().Init()) {
		if _, ok := msg.(editor.SyncedMsg); ok {
			synced = true
		}
	}
	if !synced {
		t.Error("Init should sync the micro bundle")
	}
	cfg := config.Default()
	cfg.MicroBundle = false
	for _, msg := range collect(New(cfg, nil, nil).Init()) {
		if _, ok := msg.(editor.SyncedMsg); ok {
			t.Error("micro_bundle = false should skip the sync")
		}
	}
}
