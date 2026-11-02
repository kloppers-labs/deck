// Package app is deck's root Bubble Tea model. It owns the tab bar, global
// keymap, and help footer, and dispatches to the Files, Git, Run, and HTTP panes.
// It also brokers cross-pane actions: opening files in micro and generating a
// commit message from the staged diff.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/aicommit"
	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/files"
	"deck/internal/git"
	"deck/internal/httpc"
	"deck/internal/runner"
	"deck/internal/ui"
)

type tab int

const (
	tabFiles tab = iota
	tabGit
	tabRun
	tabHTTP
	numTabs
)

var tabNames = []string{"Files", "Git", "Run", "HTTP"}

const commitMsgTimeout = 90 * time.Second

// editorClosedMsg fires when the launched editor exits and deck regains the TTY.
type editorClosedMsg struct{ err error }

// Model is the top-level application state.
type Model struct {
	cfg      config.Config
	project  string
	active   tab
	files    files.Model
	git      git.Pane
	run      runner.Pane
	httpc    httpc.Pane
	help     help.Model
	showHelp bool
	width    int
	height   int
}

// New builds the root model from loaded config.
func New(cfg config.Config) Model {
	return Model{
		cfg:     cfg,
		project: projectName(),
		files:   files.New("", cfg.Theme),
		git:     git.NewPane(cfg.Theme, aicommit.Available(cfg.AIBackend)),
		run:     runner.NewPane(cfg.Tasks, ""),
		httpc:   httpc.NewPane(cfg.Theme),
		help:    newHelp(),
	}
}

func projectName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "deck"
	}
	return filepath.Base(wd)
}

// newHelp builds the help view styled with deck's Dracula palette.
func newHelp() help.Model {
	h := help.New()
	keyStyle := lipgloss.NewStyle().Foreground(ui.ColorAccent)
	descStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	h.Styles.ShortKey = keyStyle
	h.Styles.FullKey = keyStyle
	h.Styles.ShortDesc = descStyle
	h.Styles.FullDesc = descStyle
	return h
}

func (m Model) Init() tea.Cmd { return nil }

// Rows reserved around the pane body: the bordered tab bar, the status bar,
// the help footer, and slack for pane borders.
const (
	tabBarHeight    = 3
	statusBarHeight = 1
	footerHeight    = 1
	paneSlack       = 2
	chromeHeight    = tabBarHeight + statusBarHeight + footerHeight + paneSlack
)

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.help.Width = w
	paneH := h - chromeHeight
	m.files.SetSize(w, paneH)
	m.git.SetSize(w, paneH)
	m.run.SetSize(w, paneH)
	m.httpc.SetSize(w, paneH)
}

func (m Model) capturing() bool {
	switch m.active {
	case tabFiles:
		return m.files.Capturing()
	case tabGit:
		return m.git.Capturing()
	case tabHTTP:
		return m.httpc.Capturing()
	}
	return false
}

func (m Model) focusCmd() tea.Cmd {
	if m.active == tabGit {
		return git.Refresh
	}
	return nil
}

// Update routes global keys and cross-pane messages, delegating the rest to the
// active pane.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		if cmd, handled := m.globalKey(msg); handled {
			return m, cmd
		}

	case files.OpenMsg:
		c := editor.Command(m.cfg.Editor, msg.Path)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return editorClosedMsg{err: err}
		})

	case git.GenCommitMsgRequest:
		return m, generateCommitMsg(msg.Ctx, m.cfg.AIBackend, m.cfg.AIModel)

	case editorClosedMsg:
		return m, git.Refresh

	// Routed by type, not by the active tab, so these land whichever tab is showing.
	case git.RefreshMsg, git.CommitMsgReady:
		var cmd tea.Cmd
		m.git, cmd = m.git.Update(msg)
		return m, cmd

	case runner.OutputMsg, runner.ExitMsg:
		var cmd tea.Cmd
		m.run, cmd = m.run.Update(msg)
		return m, cmd

	case httpc.RespMsg:
		var cmd tea.Cmd
		m.httpc, cmd = m.httpc.Update(msg)
		return m, cmd

	// Spinner ticks animate regardless of the active tab. Each pane owns its own
	// spinner; the tick's ID means only the owning spinner advances, so fanning
	// the message out to every pane is safe and never amplifies the tick rate.
	case spinner.TickMsg:
		var cmds []tea.Cmd
		var cmd tea.Cmd
		m.git, cmd = m.git.Update(msg)
		cmds = append(cmds, cmd)
		m.httpc, cmd = m.httpc.Update(msg)
		cmds = append(cmds, cmd)
		m.run, cmd = m.run.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	return m.routeToPane(msg)
}

func (m *Model) globalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	// ctrl+c and tab navigation win before the capturing gate, so they work even
	// while a pane owns the keyboard.
	switch {
	case key.Matches(msg, ui.Keys.ForceQuit):
		return tea.Quit, true
	case key.Matches(msg, ui.Keys.Tab):
		m.active = (m.active + 1) % numTabs
		return m.focusCmd(), true
	case key.Matches(msg, ui.Keys.PrevTab):
		m.active = (m.active + numTabs - 1) % numTabs
		return m.focusCmd(), true
	}
	if m.capturing() {
		return nil, false
	}
	switch {
	case key.Matches(msg, ui.Keys.Quit):
		return tea.Quit, true
	case key.Matches(msg, ui.Keys.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		return nil, true
	case key.Matches(msg, ui.Keys.Files):
		m.active = tabFiles
		return m.focusCmd(), true
	case key.Matches(msg, ui.Keys.Git):
		m.active = tabGit
		return m.focusCmd(), true
	case key.Matches(msg, ui.Keys.Run):
		m.active = tabRun
		return m.focusCmd(), true
	case key.Matches(msg, ui.Keys.HTTP):
		m.active = tabHTTP
		return m.focusCmd(), true
	}
	return nil, false
}

func (m Model) routeToPane(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.active {
	case tabFiles:
		m.files, cmd = m.files.Update(msg)
	case tabGit:
		m.git, cmd = m.git.Update(msg)
	case tabRun:
		m.run, cmd = m.run.Update(msg)
	case tabHTTP:
		m.httpc, cmd = m.httpc.Update(msg)
	}
	return m, cmd
}

func generateCommitMsg(parent context.Context, backend, model string) tea.Cmd {
	if parent == nil {
		parent = context.Background()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, commitMsgTimeout)
		defer cancel()
		diff, err := git.StagedDiff()
		if err != nil {
			return git.CommitMsgReady{Err: fmt.Errorf("staged diff: %w", err)}
		}
		if strings.TrimSpace(diff) == "" {
			return git.CommitMsgReady{Err: errors.New("nothing staged")}
		}
		text, err := aicommit.Generate(ctx, backend, model, diff)
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("timed out after %s", commitMsgTimeout)
		}
		return git.CommitMsgReady{Text: text, Err: err}
	}
}

// View renders the tab bar, the active pane (or full help), the status bar,
// and the help footer.
func (m Model) View() string {
	body := m.paneView()
	if m.showHelp {
		body = m.helpView()
	}
	frame := strings.Join([]string{
		m.tabBar(),
		body,
		m.statusBar(),
		m.footer(),
	}, "\n")
	// Last line of defence: a pane that surfaces externally-sourced bytes (a
	// binary file preview, a command's raw output, an HTTP body) can otherwise
	// leak cursor-movement escapes or control bytes that scribble over the tab
	// bar. Strip them here so the chrome always survives.
	return ui.SanitizeTerminal(frame)
}

// tabBar renders bordered tabs; the Run tab carries a live running-count badge
// so background processes stay visible from every tab.
func (m Model) tabBar() string {
	var parts []string
	for i, name := range tabNames {
		if tab(i) == tabRun {
			if n := m.run.RunningCount(); n > 0 {
				name += lipgloss.NewStyle().Foreground(ui.ColorString).
					Render(fmt.Sprintf("(%d)", n))
			}
		}
		if tab(i) == m.active {
			parts = append(parts, ui.TabActive.Render(name))
		} else {
			parts = append(parts, ui.TabInactive.Render(name))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Bottom, parts...)
	if gap := m.width - lipgloss.Width(row); gap > 0 {
		rule := ui.Muted.Render(strings.Repeat("─", gap))
		row = lipgloss.JoinHorizontal(lipgloss.Bottom, row, rule)
	}
	return row
}

// statusBar renders the persistent project-state line: project, branch, dirty
// count, and running processes.
func (m Model) statusBar() string {
	line := statusLine(m.project, m.git.Branch(), m.git.DirtyCount(), m.run.RunningCount())
	return ui.StatusBar.Width(m.width).Render(line)
}

// statusLine assembles the status-bar text; pure so it can be unit-tested.
func statusLine(project, branch string, dirty, running int) string {
	parts := []string{project}
	if branch != "" {
		parts = append(parts, "⎇ "+branch)
	}
	if dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", dirty))
	}
	if running > 0 {
		parts = append(parts, fmt.Sprintf("▶ %d running", running))
	}
	return strings.Join(parts, " · ")
}

func (m Model) paneView() string {
	switch m.active {
	case tabFiles:
		return m.files.View()
	case tabGit:
		return m.git.View()
	case tabRun:
		return m.run.View()
	case tabHTTP:
		return m.httpc.View()
	}
	return ""
}

// helpKeys adapts the active pane's bindings plus deck's global navigation to
// the help.KeyMap interface: ShortHelp drives the one-line footer, FullHelp the
// expanded view toggled with "?".
type helpKeys struct {
	pane []key.Binding
}

func (h helpKeys) ShortHelp() []key.Binding {
	return append(append([]key.Binding{}, h.pane...), ui.Keys.Tab, ui.Keys.Help, ui.Keys.Quit)
}

func (h helpKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{h.pane, ui.Keys.NavBindings(), ui.Keys.GlobalBindings()}
}

// paneBindings returns the active pane's help bindings.
func (m Model) paneBindings() []key.Binding {
	switch m.active {
	case tabFiles:
		return m.files.HelpBindings()
	case tabGit:
		return m.git.HelpBindings()
	case tabRun:
		return m.run.HelpBindings()
	case tabHTTP:
		return m.httpc.HelpBindings()
	}
	return nil
}

// footer renders the one-line short help for the active pane plus global keys.
func (m Model) footer() string {
	return m.help.ShortHelpView(helpKeys{pane: m.paneBindings()}.ShortHelp())
}

// helpView renders the expanded help shown in the body when "?" is toggled.
func (m Model) helpView() string {
	return m.help.FullHelpView(helpKeys{pane: m.paneBindings()}.FullHelp())
}
