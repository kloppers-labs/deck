package files

import (
	"errors"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/ui"
)

type promptKind int

const (
	promptNone promptKind = iota
	promptNew
	promptRename
	promptTrash
)

// prompt is the inline text prompt at the foot of the tree. target is the
// directory to create in, or the entry to rename or trash.
type prompt struct {
	kind   promptKind
	target string
	label  string
	input  textinput.Model
}

// opDoneMsg reports a finished file operation; path is the entry to select
// afterwards ("" keeps the selection's place).
type opDoneMsg struct {
	path string
	note string
	err  error
}

type shellClosedMsg struct{ err error }

func (opDoneMsg) RouteTo() ui.Dest      { return ui.DestFiles }
func (shellClosedMsg) RouteTo() ui.Dest { return ui.DestFiles }

func newPrompt(kind promptKind, target, label, value string, pos int) prompt {
	in := textinput.New()
	in.Prompt = ""
	in.Cursor.SetMode(cursor.CursorStatic)
	in.SetValue(value)
	in.SetCursor(pos)
	in.Focus()
	return prompt{kind: kind, target: target, label: label, input: in}
}

func (p prompt) view(w int) string {
	label := ui.Active.Render(p.label)
	if p.kind == promptTrash {
		return ansi.Truncate(label+ui.Muted.Render(" y/N"), w, "…")
	}
	p.input.Width = max(w-ansi.StringWidth(p.label)-1, 1)
	return ansi.Truncate(label+p.input.View(), w, "")
}

func (m Model) openPrompt(kind promptKind) (Model, tea.Cmd) {
	n := m.tree.Selected()
	switch kind {
	case promptNew:
		dir := m.tree.Root().Path
		if n != nil {
			dir = filepath.Dir(n.Path)
			if n.IsDir {
				dir = n.Path
			}
		}
		m.prompt = newPrompt(kind, dir, "new in "+m.relLabel(dir)+"/: ", "", 0)
	case promptRename:
		if n == nil {
			return m, nil
		}
		m.prompt = newPrompt(kind, n.Path, "rename: ", n.Name, renameCursor(n.Name, n.IsDir))
	case promptTrash:
		if n == nil {
			return m, nil
		}
		m.prompt = newPrompt(kind, n.Path, "trash "+n.Name+"?", "", 0)
	}
	m.relayout()
	return m, nil
}

func (m Model) relLabel(dir string) string {
	rel, err := filepath.Rel(m.tree.Launch(), dir)
	if err != nil || rel == "." {
		return filepath.Base(dir)
	}
	return rel
}

func (m *Model) closePrompt() {
	m.prompt = prompt{}
	m.relayout()
}

func (m Model) promptKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	p := m.prompt
	if p.kind == promptTrash {
		m.closePrompt()
		if msg.String() != "y" && msg.String() != "Y" {
			return m, nil
		}
		return m, trashCmd(p.target)
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.closePrompt()
		return m, nil
	case tea.KeyEnter:
		m.closePrompt()
		return m, opCmd(p)
	}
	var cmd tea.Cmd
	m.prompt.input, cmd = m.prompt.input.Update(msg)
	return m, cmd
}

func opCmd(p prompt) tea.Cmd {
	value := p.input.Value()
	return func() tea.Msg {
		switch p.kind {
		case promptNew:
			path, err := CreatePath(p.target, value)
			return opDoneMsg{path: path, note: "created " + filepath.Base(path), err: err}
		case promptRename:
			path, err := RenamePath(p.target, value)
			return opDoneMsg{path: path, note: "renamed to " + filepath.Base(path), err: err}
		}
		return nil
	}
}

func trashCmd(path string) tea.Cmd {
	return func() tea.Msg {
		err := trashFor().Move(path)
		return opDoneMsg{note: "moved " + filepath.Base(path) + " to the trash", err: err}
	}
}

func revealCmd(path string) tea.Cmd {
	return func() tea.Msg {
		if err := revealInOS(path); err != nil {
			return ui.ToastMsg{Level: ui.LevelError, Text: "reveal: " + err.Error()}
		}
		return nil
	}
}

func shellCmd(path string) tea.Cmd {
	return tea.ExecProcess(shellCommand(path), func(err error) tea.Msg { return shellClosedMsg{err: err} })
}

func (m Model) opDone(msg opDoneMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m, ui.Toast(ui.LevelError, msg.err.Error())
	}
	m.tree.Reload()
	if msg.path != "" {
		m.tree.Reveal(msg.path)
	}
	m.finder.stale = true
	m.shown, m.infoPath = "", ""
	return m, tea.Batch(m.syncSelection(), m.refreshBadges(), git.Refresh, ui.Toast(ui.LevelInfo, msg.note))
}

func (m Model) shellClosed(msg shellClosedMsg) (Model, tea.Cmd) {
	m, cmd := m.reload()
	m.finder.stale = true
	var toast tea.Cmd
	var exit *exec.ExitError
	if msg.err != nil && !errors.As(msg.err, &exit) {
		toast = ui.Toast(ui.LevelError, "shell: "+msg.err.Error())
	}
	return m, tea.Batch(cmd, git.Refresh, toast)
}
