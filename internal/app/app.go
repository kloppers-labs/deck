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
	quit       quitState
	toasts     []toast
	toastSeq   int
	mouseOn    bool
	ed         editorState
	lastMotion *cell
	width      int
	height     int
	lsp        LSP
	now        func() time.Time
}

// New builds the root model from loaded config. warnings are shown as a toast
// once the program starts; a nil nav runs without language servers.
func New(cfg config.Config, warnings []config.Warning, nav LSP) Model {
	var fl files.LSP
	if nav != nil {
		fl = nav
	}
	return Model{
		cfg:      cfg,
		warnings: warnings,
		project:  projectName(),
		files:    files.New("", cfg).WithBadges(gitBadges).WithLSP(fl),
		git: git.NewPane(git.Options{
			Theme:         cfg.Theme,
			CommitAI:      aicommit.Available(cfg.AIBackend),
			ConfirmCommit: cfg.Git.ConfirmCommit,
		}),
		run:     runner.NewPane(cfg.Tasks, ""),
		httpc:   httpc.NewPane(cfg.Theme),
		keyHelp: newKeyHelp(),
		help:    newHelpState(),
		mouseOn: cfg.Mouse != "off",
		ed:      newEditorState(cfg),
		lsp:     nav,
		now:     time.Now,
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
	return tea.Batch(m.files.Init(), git.Refresh, m.warningToast(), m.run.Init(), m.ed.syncCmd(),
		files.ListenLSP(m.files.LSP()), prestartCmd(m.lsp, m.ed.cwd))
}

func (m Model) warningToast() tea.Cmd {
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
	case tabRun:
		return m.run.Capturing()
	case tabHTTP:
		return m.httpc.Capturing()
	}
	return false
}

func (m Model) focusCmd() tea.Cmd {
	switch m.active {
	case tabGit:
		return git.Refresh
	case tabFiles:
		return files.RefreshBadges
	}
	return nil
}

// Update handles global keys, mouse translation and app-level messages, and
// delivers every ui.Routed message to its destination whichever tab is active.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, m.files.ResizeCmd()

	case tea.KeyMsg:
		if cmd, handled := m.globalKey(msg); handled {
			return m, cmd
		}

	case tea.MouseMsg:
		return m.mouse(msg)

	case editor.OpenMsg:
		return m.openEditor(msg.Target)

	case editor.SyncedMsg:
		return m.bundleSynced(msg)

	case git.GenCommitMsgRequest:
		return m, generateCommitMsg(msg.Ctx, msg.Repo, m.cfg.AIBackend, m.cfg.AIModel)

	case editorClosedMsg:
		return m.editorClosed(msg)

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
		return m.deliver(msg)
	}

	return m.routeTo(tabDests[m.active], msg)
}

func (m Model) deliver(msg ui.Routed) (tea.Model, tea.Cmd) {
	next, cmd := m.routeTo(msg.RouteTo(), msg)
	b, ok := msg.(ui.Broadcast)
	if !ok {
		return next, cmd
	}
	cmds := []tea.Cmd{cmd}
	for _, dest := range b.AlsoTo() {
		next, cmd = next.(Model).routeTo(dest, msg)
		cmds = append(cmds, cmd)
	}
	return next, tea.Batch(cmds...)
}

func (m Model) routeTo(dest ui.Dest, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch dest {
	case ui.DestApp:
		switch msg := msg.(type) {
		case ui.ToastMsg:
			return m.addToast(msg)
		case ui.FocusTabMsg:
			for i, d := range tabDests {
				if d == msg.Dest {
					return m, m.jump(tab(i))
				}
			}
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
		return m.requestQuit(true), true
	}
	if m.quit.asking || m.quit.stopping {
		return m.quitKey(msg), true
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
		return m.requestQuit(false), true
	case key.Matches(msg, ui.Keys.Rerun):
		var cmd tea.Cmd
		m.run, cmd = m.run.Rerun()
		return cmd, true
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

func generateCommitMsg(parent context.Context, repo *git.Repo, backend, model string) tea.Cmd {
	if parent == nil {
		parent = context.Background()
	}
	return func() tea.Msg {
		if repo == nil {
			return git.CommitMsgReady{Err: git.ErrNotRepo}
		}
		ctx, cancel := context.WithTimeout(parent, commitMsgTimeout)
		defer cancel()
		diff, err := repo.StagedDiff(ctx)
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
	switch {
	case m.quit.asking || m.quit.stopping:
		body = m.quitOverlay(body)
	case m.help.open:
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
			name += m.runBadge()
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

func (m Model) runBadge() string {
	var parts []string
	if n := m.run.RunningCount(); n > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(ui.ColorString).Render(fmt.Sprintf("%d", n)))
	}
	if n := m.run.FailedCount(); n > 0 {
		parts = append(parts, ui.Err.Render(fmt.Sprintf("✗%d", n)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " ") + ")"
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
	project := m.project
	if top := m.git.Top(); top != "" {
		project = filepath.Base(top)
	}
	left := statusLeft(project, m.git.Branch(), m.git.DirtyCount(), m.run.RunningCount())
	right := lspBadges(m.files.LSPStatus(), m.now())
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

var mouseOffHint = key.NewBinding(key.WithKeys("alt+m"), key.WithHelp(ui.Alt("m"), "mouse off"))

var quitFooter = []key.Binding{
	key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "stop tasks and quit")),
	key.NewBinding(key.WithKeys("n"), key.WithHelp("n/esc", "cancel")),
	key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "force quit")),
}

// footerBindings splits the footer into the active pane's hints and the global
// tail, which always stays visible.
func (m Model) footerBindings() (pane, global []key.Binding) {
	if m.quit.asking || m.quit.stopping {
		return nil, quitFooter
	}
	if m.help.open {
		return nil, helpFooter
	}
	if !m.mouseOn {
		global = append(global, mouseOffHint)
	}
	global = append(global, ui.Keys.Tab)
	if m.capturing() {
		return m.paneBindings(), append(global, ui.Keys.Normal)
	}
	return m.paneBindings(), append(global, ui.Keys.Help, ui.Keys.Quit)
}

func (m Model) footer() string {
	pane, global := m.footerBindings()
	tail := m.keyHelp.ShortHelpView(global)
	if len(pane) == 0 {
		return tail
	}
	sep := m.keyHelp.Styles.ShortSeparator.Render(m.keyHelp.ShortSeparator)
	head := m.keyHelp
	head.Width = max(m.width-lipgloss.Width(tail)-lipgloss.Width(sep), 1)
	return head.ShortHelpView(pane) + sep + tail
}
