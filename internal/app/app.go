// Package app is deck's root Bubble Tea model: the chrome around the Files, Git,
// Run and HTTP panes, message routing, and cross-pane actions.
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
	"github.com/charmbracelet/x/ansi"

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

var tabDests = [numTabs]ui.Dest{ui.DestFiles, ui.DestGit, ui.DestRun, ui.DestHTTP}

const commitMsgTimeout = 90 * time.Second

const (
	tabBarHeight    = 3
	statusBarHeight = 1
	footerHeight    = 1
	chromeHeight    = tabBarHeight + statusBarHeight + footerHeight
	minWidth        = 60
	minHeight       = 16
	maxToasts       = 8
)

// editorClosedMsg fires when the launched editor exits and deck regains the TTY.
type editorClosedMsg struct{ err error }

type toastExpireMsg struct{ id int }

type toast struct {
	id  int
	msg ui.ToastMsg
}

type cell struct{ x, y int }

// Model is the top-level application state.
type Model struct {
	cfg        config.Config
	warnings   []config.Warning
	project    string
	active     tab
	files      files.Model
	git        git.Pane
	run        runner.Pane
	httpc      httpc.Pane
	keyHelp    help.Model
	help       helpState
	toasts     []toast
	toastSeq   int
	mouseOn    bool
	lastMotion *cell
	width      int
	height     int
}

// New builds the root model from loaded config. warnings are shown as a toast
// once the program starts.
func New(cfg config.Config, warnings []config.Warning) Model {
	return Model{
		cfg:      cfg,
		warnings: warnings,
		project:  projectName(),
		files:    files.New("", cfg.Theme),
		git:      git.NewPane(cfg.Theme, aicommit.Available(cfg.AIBackend)),
		run:      runner.NewPane(cfg.Tasks, ""),
		httpc:    httpc.NewPane(cfg.Theme),
		keyHelp:  newKeyHelp(),
		help:     newHelpState(),
		mouseOn:  cfg.Mouse != "off",
	}
}

func projectName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "deck"
	}
	return filepath.Base(wd)
}

func newKeyHelp() help.Model {
	h := help.New()
	keyStyle := lipgloss.NewStyle().Foreground(ui.ColorAccent)
	descStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	h.Styles.ShortKey = keyStyle
	h.Styles.FullKey = keyStyle
	h.Styles.ShortDesc = descStyle
	h.Styles.FullDesc = descStyle
	h.Styles.ShortSeparator = descStyle
	return h
}

func (m Model) Init() tea.Cmd {
	if len(m.warnings) == 0 {
		return nil
	}
	text := "config: " + m.warnings[0].String()
	if n := len(m.warnings) - 1; n > 0 {
		text += fmt.Sprintf(" (+%d more)", n)
	}
	return func() tea.Msg {
		return ui.ToastMsg{Level: ui.LevelWarn, Text: text, TTL: 12 * time.Second}
	}
}

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.keyHelp.Width = w
	paneH := max(h-chromeHeight, 1)
	m.files.SetSize(w, paneH)
	m.git.SetSize(w, paneH)
	m.run.SetSize(w, paneH)
	m.httpc.SetSize(w, paneH)
}

func (m Model) bodyHeight() int { return m.height - chromeHeight }

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

// Update handles global keys, mouse translation and app-level messages, and
// delivers every ui.Routed message to its destination whichever tab is active.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		if cmd, handled := m.globalKey(msg); handled {
			return m, cmd
		}

	case tea.MouseMsg:
		return m.mouse(msg)

	case files.OpenMsg:
		c := editor.Command(m.cfg.Editor, msg.Path)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return editorClosedMsg{err: err}
		})

	case git.GenCommitMsgRequest:
		return m, generateCommitMsg(msg.Ctx, m.cfg.AIBackend, m.cfg.AIModel)

	case editorClosedMsg:
		if msg.err != nil {
			return m, tea.Batch(git.Refresh, ui.Toast(ui.LevelError, "editor: "+msg.err.Error()))
		}
		return m, git.Refresh

	case toastExpireMsg:
		m.toasts = withoutToast(m.toasts, msg.id)
		return m, nil

	// Each pane's spinner only advances on its own tick ID, so fanning out is safe.
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

	case ui.Routed:
		return m.routeTo(msg.RouteTo(), msg)
	}

	return m.routeTo(tabDests[m.active], msg)
}

func (m Model) routeTo(dest ui.Dest, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch dest {
	case ui.DestApp:
		if t, ok := msg.(ui.ToastMsg); ok {
			return m.addToast(t)
		}
	case ui.DestFiles:
		m.files, cmd = m.files.Update(msg)
	case ui.DestGit:
		m.git, cmd = m.git.Update(msg)
	case ui.DestRun:
		m.run, cmd = m.run.Update(msg)
	case ui.DestHTTP:
		m.httpc, cmd = m.httpc.Update(msg)
	}
	return m, cmd
}

func (m Model) addToast(t ui.ToastMsg) (Model, tea.Cmd) {
	m.toastSeq++
	id := m.toastSeq
	next := append([]toast(nil), m.toasts...)
	next = append(next, toast{id: id, msg: t})
	if len(next) > maxToasts {
		next = next[len(next)-maxToasts:]
	}
	m.toasts = next
	return m, tea.Tick(t.Lifetime(), func(time.Time) tea.Msg { return toastExpireMsg{id: id} })
}

func withoutToast(ts []toast, id int) []toast {
	var out []toast
	for _, t := range ts {
		if t.id != id {
			out = append(out, t)
		}
	}
	return out
}

func (m *Model) jump(t tab) tea.Cmd {
	m.active = t
	return m.focusCmd()
}

func (m *Model) globalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if key.Matches(msg, ui.Keys.ForceQuit) {
		return tea.Quit, true
	}
	if m.help.open {
		return m.helpKey(msg), true
	}
	switch {
	case key.Matches(msg, ui.Keys.Tab):
		return m.jump((m.active + 1) % numTabs), true
	case key.Matches(msg, ui.Keys.PrevTab):
		return m.jump((m.active + numTabs - 1) % numTabs), true
	case key.Matches(msg, ui.Keys.AltFiles):
		return m.jump(tabFiles), true
	case key.Matches(msg, ui.Keys.AltGit):
		return m.jump(tabGit), true
	case key.Matches(msg, ui.Keys.AltRun):
		return m.jump(tabRun), true
	case key.Matches(msg, ui.Keys.AltHTTP):
		return m.jump(tabHTTP), true
	case key.Matches(msg, ui.Keys.HelpAlways):
		m.openHelp()
		return nil, true
	case key.Matches(msg, ui.Keys.ToggleMouse):
		return m.toggleMouse(), true
	}
	if m.capturing() {
		return nil, false
	}
	switch {
	case key.Matches(msg, ui.Keys.Quit):
		return tea.Quit, true
	case key.Matches(msg, ui.Keys.Help):
		m.openHelp()
		return nil, true
	case key.Matches(msg, ui.Keys.Files):
		return m.jump(tabFiles), true
	case key.Matches(msg, ui.Keys.Git):
		return m.jump(tabGit), true
	case key.Matches(msg, ui.Keys.Run):
		return m.jump(tabRun), true
	case key.Matches(msg, ui.Keys.HTTP):
		return m.jump(tabHTTP), true
	}
	return nil, false
}

func (m *Model) toggleMouse() tea.Cmd {
	m.mouseOn = !m.mouseOn
	m.lastMotion = nil
	switch {
	case !m.mouseOn:
		return tea.DisableMouse
	case m.cfg.Mouse == "click":
		return tea.EnableMouseCellMotion
	}
	return tea.EnableMouseAllMotion
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

// View renders exactly width×height cells: the tab bar, the active pane (with
// the help modal over it when open), the status bar and the key footer.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		msg := fmt.Sprintf("terminal too small (%dx%d, need %dx%d)", m.width, m.height, minWidth, minHeight)
		return ui.Fit(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg), m.width, m.height)
	}
	body := ui.Fit(m.paneView(), m.width, m.bodyHeight())
	if m.help.open {
		body = m.helpOverlay(body)
	}
	frame := strings.Join([]string{
		ui.Fit(m.tabBar(), m.width, tabBarHeight),
		body,
		ui.Fit(m.statusBar(), m.width, statusBarHeight),
		ui.Fit(m.footer(), m.width, footerHeight),
	}, "\n")
	return ui.SanitizeTerminal(frame)
}

func (m Model) tabLabels() []string {
	labels := make([]string, len(tabNames))
	for i, name := range tabNames {
		if tab(i) == tabRun {
			if n := m.run.RunningCount(); n > 0 {
				name += lipgloss.NewStyle().Foreground(ui.ColorString).Render(fmt.Sprintf("(%d)", n))
			}
		}
		if tab(i) == m.active {
			labels[i] = ui.TabActive.Render(name)
		} else {
			labels[i] = ui.TabInactive.Render(name)
		}
	}
	return labels
}

func (m Model) tabWidths() []int {
	labels := m.tabLabels()
	widths := make([]int, len(labels))
	for i, l := range labels {
		widths[i] = lipgloss.Width(l)
	}
	return widths
}

// tabBar renders bordered tabs; the Run tab carries a live running-count badge
// so background processes stay visible from every tab.
func (m Model) tabBar() string {
	row := lipgloss.JoinHorizontal(lipgloss.Bottom, m.tabLabels()...)
	if gap := m.width - lipgloss.Width(row); gap > 0 {
		rule := ui.Muted.Render(strings.Repeat("─", gap))
		row = lipgloss.JoinHorizontal(lipgloss.Bottom, row, rule)
	}
	return row
}

func (m Model) statusBar() string {
	left := statusLeft(m.project, m.git.Branch(), m.git.DirtyCount(), m.run.RunningCount())
	var right []string
	if n := len(m.toasts); n > 0 {
		right = append(right, m.toasts[n-1].msg.Render())
	}
	inner := max(m.width-ui.StatusBar.GetHorizontalFrameSize(), 0)
	return ui.StatusBar.Render(statusLine(left, right, inner))
}

func statusLeft(project, branch string, dirty, running int) []string {
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
	return parts
}

// statusLine truncates the left side first so toasts and badges stay readable.
func statusLine(left, right []string, w int) string {
	r := ui.FitWidth(strings.Join(right, "  "), w, "…")
	rw := ansi.StringWidth(r)
	gap := 1
	if rw == 0 {
		gap = 0
	}
	l := ui.FitWidth(strings.Join(left, " · "), w-rw-gap, "…")
	pad := max(w-ansi.StringWidth(l)-rw, 0)
	return l + strings.Repeat(" ", pad) + r
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

var mouseOffHint = key.NewBinding(key.WithKeys("alt+m"), key.WithHelp("alt+m", "mouse off"))

func (m Model) footerBindings() []key.Binding {
	if m.help.open {
		return helpFooter
	}
	var b []key.Binding
	if !m.mouseOn {
		b = append(b, mouseOffHint)
	}
	b = append(b, m.paneBindings()...)
	b = append(b, ui.Keys.Tab)
	if m.capturing() {
		return append(b, ui.Keys.Normal)
	}
	return append(b, ui.Keys.Help, ui.Keys.Quit)
}

func (m Model) footer() string {
	return m.keyHelp.ShortHelpView(m.footerBindings())
}
