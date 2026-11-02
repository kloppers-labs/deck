package app

import (
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/files"
	"deck/internal/git"
	"deck/internal/ui"
)

const issuesToastTTL = 12 * time.Second

// editorState is the root model's view of the external editor: where deck was
// launched and deck's micro runtime directory ("" when the bundle is off).
type editorState struct {
	cwd      string
	microDir string
	bundle   editor.BundleConfig
}

// editorClosedMsg fires when a suspending editor exits and deck regains the TTY.
type editorClosedMsg struct {
	err    error
	origin editor.Target
	state  editor.State
	ok     bool
}

func newEditorState(cfg config.Config) editorState {
	s := editorState{}
	s.cwd, _ = os.Getwd()
	if cfg.MicroBundle && editor.IsMicro(cfg.Editor) {
		s.microDir = editor.ConfigHome()
		s.bundle = editor.BundleConfig{
			Editor: cfg.Editor,
			Dir:    s.microDir,
			LSP:    cfg.MicroLSP != "off",
			Specs:  editor.SpecsFromConfig(cfg.LSP),
		}
	}
	return s
}

func (s editorState) syncCmd() tea.Cmd {
	if s.microDir == "" {
		return nil
	}
	return editor.SyncCmd(s.bundle)
}

func (m Model) openEditor(t editor.Target) (tea.Model, tea.Cmd) {
	var state string
	if m.ed.microDir != "" {
		state = editor.StatePath()
	}
	c, mode, err := editor.Launch(m.cfg.Editor, t, editor.Opts{ConfigDir: m.ed.microDir, StateFile: state, Cwd: m.ed.cwd})
	if err != nil {
		return m, ui.Toast(ui.LevelError, "editor: "+err.Error())
	}
	if mode == editor.Detach {
		return m, detach(c, editor.DisplayName(m.cfg.Editor))
	}
	return m, tea.ExecProcess(c, editorExited(t, state, time.Now()))
}

func editorExited(origin editor.Target, state string, launchedAt time.Time) tea.ExecCallback {
	return func(err error) tea.Msg {
		st, ok := editor.ReadState(state, launchedAt)
		if state != "" {
			_ = os.Remove(state)
		}
		return editorClosedMsg{err: err, origin: origin, state: st, ok: ok}
	}
}

func detach(c *exec.Cmd, name string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Start(); err != nil {
			return ui.ToastMsg{Level: ui.LevelError, Text: "editor: " + err.Error()}
		}
		go func() { _ = c.Wait() }()
		return ui.ToastMsg{Level: ui.LevelInfo, Text: "opened in " + name}
	}
}

func (m Model) editorClosed(msg editorClosedMsg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{git.Refresh}
	target := editor.ReturnTarget(msg.origin, msg.state, msg.ok, m.cfg.EditorReturn, m.ed.cwd)
	if target.Path != "" {
		cmds = append(cmds, func() tea.Msg { return files.RevealMsg{Target: target} })
	} else {
		cmds = append(cmds, files.ReloadCurrent)
	}
	if msg.ok && len(msg.state.Saved) > 0 {
		saved := msg.state.Saved
		cmds = append(cmds, func() tea.Msg { return files.InvalidateMsg{Saved: saved} })
	}
	if msg.err != nil {
		cmds = append(cmds, ui.Toast(ui.LevelError, "editor: "+msg.err.Error()))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) bundleSynced(msg editor.SyncedMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Err != nil:
		return m, ui.Toast(ui.LevelWarn, "micro bundle: "+msg.Err.Error())
	case msg.Notify && len(msg.Issues) > 0:
		text := strings.Join(msg.Issues, "; ")
		return m, func() tea.Msg { return ui.ToastMsg{Level: ui.LevelWarn, Text: text, TTL: issuesToastTTL} }
	}
	return m, nil
}
