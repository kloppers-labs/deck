package runner

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/ui"
)

type jobState int

const (
	stateIdle jobState = iota
	stateRunning
	stateDone
	stateFailed
	stateStopped
)

func (s jobState) glyph() string {
	switch s {
	case stateRunning:
		return "▶"
	case stateDone:
		return "✓"
	case stateFailed:
		return "✗"
	case stateStopped:
		return "■"
	default:
		return "▷"
	}
}

func (s jobState) style() lipgloss.Style {
	switch s {
	case stateRunning:
		return lipgloss.NewStyle().Foreground(ui.ColorAccent)
	case stateDone:
		return lipgloss.NewStyle().Foreground(ui.ColorString)
	case stateFailed:
		return ui.Err
	default:
		return ui.Muted
	}
}

type focus int

const (
	focusList focus = iota
	focusLog
)

const (
	wheelLines = 3
	logChrome  = 4
	listTop    = 1
)

type job struct {
	name           string
	task           config.Task
	auto           bool
	state          jobState
	log            logView
	proc           *proc
	stopped        bool
	gen            int
	exitCode       int
	exitErr        string
	pendingRestart bool
	startedAt      time.Time
	finishedAt     time.Time
}

func (j *job) elapsed() string {
	switch {
	case j.state == stateRunning:
		return "↑ " + fmtDuration(time.Since(j.startedAt))
	case j.finishedAt.IsZero():
		return ""
	default:
		return fmtDuration(j.finishedAt.Sub(j.startedAt))
	}
}

// fmtDuration renders durations compactly: "850ms", "12s", "12m34s", "1h02m".
func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

type jobItem struct{ job *job }

func (i jobItem) FilterValue() string { return i.job.name }

type jobDelegate struct{ spinnerFrame string }

func (d jobDelegate) Height() int                         { return 1 }
func (d jobDelegate) Spacing() int                        { return 0 }
func (d jobDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d jobDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(jobItem)
	if !ok {
		return
	}
	j := it.job
	glyph := j.state.style().Render(j.state.glyph())
	if j.state == stateRunning && d.spinnerFrame != "" {
		glyph = d.spinnerFrame
	}
	line := glyph + " " + j.name
	if j.auto {
		line += ui.Muted.Render(" auto")
	}
	if e := j.elapsed(); e != "" {
		line += ui.Muted.Render(" " + e)
	}
	line = ui.FitWidth(line, m.Width()-2, "…")
	if index == m.Index() {
		fmt.Fprint(w, ui.Active.Render("› ")+line)
		return
	}
	fmt.Fprint(w, "  "+line)
}

// Pane is the Run tab: a task list beside the selected task's log.
type Pane struct {
	jobs      []*job
	root      string
	list      list.Model
	spinner   spinner.Model
	search    textinput.Model
	searching bool
	spinning  bool
	focus     focus
	wrap      bool
	detected  bool
	more      int
	last      *job
	clicks    *ui.ClickTracker
	width     int
	height    int
}

// NewPane builds the Run tab from configured tasks; tasks detected under dir
// are merged in by Init. dir "" means the working directory.
func NewPane(tasks []config.Task, dir string) Pane {
	root, err := filepath.Abs(dir)
	if err != nil {
		root = dir
	}
	l := list.New(nil, jobDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.KeyMap.Quit = key.Binding{}
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)
	s := textinput.New()
	s.Prompt = "/"
	s.Placeholder = "search"
	p := Pane{root: root, list: l, spinner: sp, search: s, clicks: &ui.ClickTracker{}}
	p.addJobs(tasks, false)
	return p
}

func (p *Pane) addJobs(tasks []config.Task, auto bool) {
	for _, t := range tasks {
		p.jobs = append(p.jobs, &job{name: t.Name, task: t, auto: auto, log: newLogView(taskDir(p.root, t.Dir))})
	}
	items := make([]list.Item, len(p.jobs))
	for i, j := range p.jobs {
		items[i] = jobItem{job: j}
	}
	p.list.SetItems(items)
}

// Init detects project tasks in the background.
func (p Pane) Init() tea.Cmd { return detectCmd(p.root) }

func (p Pane) RunningCount() int { return p.count(stateRunning) }

// FailedCount is how many tasks' latest run failed.
func (p Pane) FailedCount() int { return p.count(stateFailed) }

func (p Pane) count(s jobState) int {
	n := 0
	for _, j := range p.jobs {
		if j.state == s {
			n++
		}
	}
	return n
}

func (p Pane) RunningNames() []string {
	var names []string
	for _, j := range p.jobs {
		if j.state == stateRunning {
			names = append(names, j.name)
		}
	}
	return names
}

// StopAll stops every running task's process group and waits up to timeout
// for them to exit, reporting whether they all did.
func (p Pane) StopAll(timeout time.Duration) bool { return waitAll(p.signalAll(), timeout) }

// StopAllCmd signals every running task now and returns a command that waits
// for them, up to timeout, before delivering then.
func (p Pane) StopAllCmd(timeout time.Duration, then tea.Msg) tea.Cmd {
	procs := p.signalAll()
	return func() tea.Msg {
		waitAll(procs, timeout)
		return then
	}
}

func (p Pane) signalAll() []*proc {
	var procs []*proc
	for _, j := range p.jobs {
		if j.state == stateRunning && j.proc != nil {
			j.stopped = true
			j.pendingRestart = false
			j.proc.stop()
			procs = append(procs, j.proc)
		}
	}
	return procs
}

func waitAll(procs []*proc, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	ok := true
	for _, pr := range procs {
		if !pr.wait(max(time.Until(deadline), 0)) {
			ok = false
		}
	}
	return ok
}

func (p *Pane) SetSize(w, h int) {
	p.width, p.height = w, h
	p.list.SetSize(p.listWidth(), p.listHeight())
	p.search.Width = max(p.logWidth()-12, 1)
}

func (p Pane) listWidth() int { return max(p.width/3, 24) }

func (p Pane) listHeight() int {
	if p.more > 0 {
		return max(p.height-1, 0)
	}
	return p.height
}

func (p Pane) logWidth() int  { return max(p.width-p.listWidth()-4, 0) }
func (p Pane) logHeight() int { return max(p.height-logChrome, 1) }

// Capturing is true while typing into the task filter or the log search.
func (p Pane) Capturing() bool {
	return p.searching || p.list.FilterState() == list.Filtering
}

func (p Pane) HelpBindings() []key.Binding {
	b := []key.Binding{
		key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("enter/r", "run")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
	}
	if p.focus == focusList {
		return append(b,
			key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l", "log")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		)
	}
	return append(b,
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/ n N", "search")),
		key.NewBinding(key.WithKeys("e", "E"), key.WithHelp("e/E", "problem")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open problem")),
		key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "wrap")),
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
		key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "follow")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "tasks")),
	)
}

func (p *Pane) selected() *job {
	if it, ok := p.list.SelectedItem().(jobItem); ok {
		return it.job
	}
	return nil
}

func (p Pane) jobIndex(j *job) int {
	for i, jj := range p.jobs {
		if jj == j {
			return i
		}
	}
	return -1
}

func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return p.key(msg)
	case tea.MouseMsg:
		return p.mouse(msg)
	case OutputMsg:
		if msg.idx < 0 || msg.idx >= len(p.jobs) {
			return p, nil
		}
		j := p.jobs[msg.idx]
		if msg.gen != j.gen {
			return p, nil
		}
		j.log.append(msg.lines)
		if j.proc != nil {
			return p, j.proc.next()
		}
		return p, nil
	case ExitMsg:
		return p.exit(msg)
	case DetectedMsg:
		p.detected = true
		p.more = msg.more
		configured := make([]config.Task, 0, len(p.jobs))
		for _, j := range p.jobs {
			configured = append(configured, j.task)
		}
		p.addJobs(mergeDetected(configured, msg.tasks), true)
		p.SetSize(p.width, p.height)
		return p, nil
	case spinner.TickMsg:
		if p.RunningCount() > 0 {
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			p.list.SetDelegate(jobDelegate{spinnerFrame: p.spinner.View()})
			return p, cmd
		}
		p.spinning = false
		return p, nil
	}
	return p, nil
}

func (p Pane) exit(msg ExitMsg) (Pane, tea.Cmd) {
	if msg.idx < 0 || msg.idx >= len(p.jobs) {
		return p, nil
	}
	j := p.jobs[msg.idx]
	if msg.gen != j.gen {
		return p, nil
	}
	j.finishedAt = time.Now()
	j.proc = nil
	j.exitCode = msg.code
	j.exitErr = ""
	var cmd tea.Cmd
	switch {
	case j.stopped:
		j.state = stateStopped
	case msg.err != nil:
		j.state = stateFailed
		j.exitErr = msg.err.Error()
		cmd = ui.Toast(ui.LevelError, fmt.Sprintf("%s failed (%s)", j.name, j.exitSummary()))
	default:
		j.state = stateDone
	}
	if j.pendingRestart {
		return p.runJob(j)
	}
	return p, cmd
}

func (j *job) exitSummary() string {
	if j.exitCode >= 0 {
		return fmt.Sprintf("exit %d", j.exitCode)
	}
	return j.exitErr
}

func (p Pane) key(msg tea.KeyMsg) (Pane, tea.Cmd) {
	if p.searching {
		return p.searchKey(msg)
	}
	if p.list.FilterState() == list.Filtering {
		return p.listUpdate(msg)
	}
	switch msg.String() {
	case "r":
		return p.runJob(p.selected())
	case "R":
		return p.restart(p.selected())
	case "x":
		p.stop(p.selected())
		return p, nil
	case "c":
		if j := p.selected(); j != nil {
			j.log.reset()
		}
		return p, nil
	}
	if p.focus == focusLog {
		return p.logKey(msg)
	}
	switch msg.String() {
	case "enter":
		return p.runJob(p.selected())
	case "l", "right":
		p.focus = focusLog
		return p, nil
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		p.scrollKey(msg.String())
		return p, nil
	}
	return p.listUpdate(msg)
}

func (p Pane) listUpdate(msg tea.Msg) (Pane, tea.Cmd) {
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p Pane) logKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	j := p.selected()
	if j == nil {
		p.focus = focusList
		return p, nil
	}
	w, h := p.logWidth(), p.logHeight()
	v := &j.log
	switch msg.String() {
	case "esc":
		if v.query != "" {
			v.setQuery("")
			return p, nil
		}
		p.focus = focusList
	case "h", "left":
		p.focus = focusList
	case "enter":
		return p, p.openProblem(j)
	case "/":
		p.searching = true
		p.search.SetValue(v.query)
		p.search.CursorEnd()
		return p, p.search.Focus()
	case "n":
		v.nextMatch(1, w, h, p.wrap)
	case "N":
		v.nextMatch(-1, w, h, p.wrap)
	case "e":
		v.nextProblem(1, w, h, p.wrap)
	case "E":
		v.nextProblem(-1, w, h, p.wrap)
	case "w":
		p.wrap = !p.wrap
	case "g", "home":
		v.toTop()
	case "G", "end", "F":
		v.toBottom()
	default:
		p.scrollKey(msg.String())
	}
	return p, nil
}

func (p *Pane) scrollKey(k string) {
	j := p.selected()
	if j == nil {
		return
	}
	w, h := p.logWidth(), p.logHeight()
	delta := 0
	switch k {
	case "j", "down":
		delta = 1
	case "k", "up":
		delta = -1
	case "ctrl+d":
		delta = h / 2
	case "ctrl+u":
		delta = -h / 2
	case "pgdown", " ":
		delta = h
	case "pgup":
		delta = -h
	default:
		return
	}
	j.log.scroll(delta, w, h, p.wrap)
}

func (p Pane) searchKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	j := p.selected()
	switch msg.String() {
	case "esc":
		p.searching = false
		p.search.Blur()
		return p, nil
	case "enter":
		p.searching = false
		p.search.Blur()
		if j != nil {
			j.log.setQuery(p.search.Value())
			j.log.nextMatch(1, p.logWidth(), p.logHeight(), p.wrap)
		}
		return p, nil
	}
	var cmd tea.Cmd
	p.search, cmd = p.search.Update(msg)
	return p, cmd
}

func (p Pane) openProblem(j *job) tea.Cmd {
	pr, ok := j.log.selectedProblem()
	if !ok {
		if len(j.log.problems) == 0 {
			return nil
		}
		j.log.nextProblem(1, p.logWidth(), p.logHeight(), p.wrap)
		pr, _ = j.log.selectedProblem()
	}
	return editor.Open(editor.Target{Path: pr.Path, Line: pr.Line, Col: pr.Col})
}

func (p Pane) mouse(msg tea.MouseMsg) (Pane, tea.Cmd) {
	inLog := msg.X >= p.listWidth()
	if tea.MouseEvent(msg).IsWheel() {
		delta := wheelLines
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -delta
		}
		if inLog {
			if j := p.selected(); j != nil {
				j.log.scroll(delta, p.logWidth(), p.logHeight(), p.wrap)
			}
			return p, nil
		}
		if delta > 0 {
			p.list.CursorDown()
		} else {
			p.list.CursorUp()
		}
		return p, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return p, nil
	}
	if !inLog {
		p.focus = focusList
		row := msg.Y - listTop
		idx := p.list.Paginator.Page*p.list.Paginator.PerPage + row
		if row >= 0 && row < p.listHeight() && idx < len(p.list.VisibleItems()) {
			p.list.Select(idx)
		}
		return p, nil
	}
	p.focus = focusLog
	count := p.clicks.Press(msg.X, msg.Y, time.Now())
	j := p.selected()
	row := msg.Y - 2
	if j == nil || row < 0 || row >= p.logHeight() {
		return p, nil
	}
	_, abs := j.log.render(p.logWidth(), p.logHeight(), p.wrap)
	if abs[row] >= 0 && j.log.selectProblemAt(abs[row]) && ui.IsDouble(count) {
		return p, p.openProblem(j)
	}
	return p, nil
}

func (p Pane) runJob(j *job) (Pane, tea.Cmd) {
	if j == nil || j.state == stateRunning {
		return p, nil
	}
	j.log.reset()
	j.stopped = false
	j.pendingRestart = false
	j.state = stateRunning
	j.gen++
	j.startedAt = time.Now()
	j.finishedAt = time.Time{}
	j.proc = startProc(p.jobIndex(j), j.gen, j.task, p.root)
	p.last = j
	cmd := j.proc.next()
	if !p.spinning {
		p.spinning = true
		p.list.SetDelegate(jobDelegate{spinnerFrame: p.spinner.View()})
		return p, tea.Batch(cmd, p.spinner.Tick)
	}
	return p, cmd
}

// restart stops a running job and queues a re-run for when it has exited; an
// idle job just runs.
func (p Pane) restart(j *job) (Pane, tea.Cmd) {
	if j == nil {
		return p, nil
	}
	if j.state != stateRunning {
		return p.runJob(j)
	}
	j.stopped = true
	j.pendingRestart = true
	j.proc.stop()
	return p, nil
}

// Rerun restarts the most recently started task, whichever is selected.
func (p Pane) Rerun() (Pane, tea.Cmd) {
	if p.last == nil {
		return p, ui.Toast(ui.LevelInfo, "no task has run yet")
	}
	p, cmd := p.restart(p.last)
	return p, tea.Batch(cmd, ui.Toast(ui.LevelInfo, "re-running "+p.last.name))
}

func (p *Pane) stop(j *job) {
	if j == nil || j.state != stateRunning {
		return
	}
	j.stopped = true
	j.pendingRestart = false
	j.proc.stop()
}

func (p Pane) View() string {
	left := p.list.View()
	if p.more > 0 {
		left = ui.Fit(left, p.listWidth(), p.listHeight()) + "\n" +
			ui.Muted.Render(fmt.Sprintf("  +%d more (add [[tasks]])", p.more))
	}
	left = ui.Fit(left, p.listWidth(), p.height)
	box := ui.Pane
	if p.focus == focusLog {
		box = ui.PaneActive
	}
	right := box.Render(ui.Fit(p.logBox(), p.logWidth(), max(p.height-2, 0)))
	return ui.Fit(lipgloss.JoinHorizontal(lipgloss.Top, left, right), p.width, p.height)
}

func (p Pane) logBox() string {
	j := p.selected()
	w := p.logWidth()
	if j == nil {
		msg := "no tasks — add [[tasks]] to config"
		if !p.detected {
			msg = "detecting tasks…"
		}
		return ui.Muted.Render(msg)
	}
	rows, _ := j.log.render(w, p.logHeight(), p.wrap)
	return ui.FitWidth(p.header(j), w, "…") + "\n" +
		strings.Join(rows, "\n") + "\n" +
		p.footerLine(j, w)
}

func (p Pane) header(j *job) string {
	h := j.state.style().Render(j.state.glyph()+" "+j.name) + ui.Muted.Render("  $ "+j.task.Cmd)
	var meta []string
	switch j.state {
	case stateDone:
		meta = append(meta, lipgloss.NewStyle().Foreground(ui.ColorString).Render("exit 0"))
	case stateFailed:
		meta = append(meta, ui.Err.Render(j.exitSummary()))
	case stateStopped:
		meta = append(meta, ui.Muted.Render("stopped"))
	}
	if e := j.elapsed(); e != "" {
		meta = append(meta, ui.Muted.Render(e))
	}
	if len(meta) > 0 {
		h += "  " + strings.Join(meta, ui.Muted.Render(" · "))
	}
	return h
}

func (p Pane) footerLine(j *job, w int) string {
	if p.searching {
		return ui.FitWidth(p.search.View(), w, "")
	}
	left, right := j.log.status()
	if left == "" && p.focus == focusLog && p.wrap {
		left = "wrap"
	}
	right = ui.Muted.Render(right)
	left = ui.FitWidth(ui.Muted.Render(left), max(w-ansi.StringWidth(right)-1, 0), "…")
	pad := max(w-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return left + strings.Repeat(" ", pad) + right
}
