package runner

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/config"
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

type job struct {
	name           string
	cmd            string
	state          jobState
	out            logBuf
	proc           *proc
	stopped        bool
	gen            int
	pendingRestart bool
	startedAt      time.Time
	finishedAt     time.Time
}

// elapsed renders the job's runtime: live while running, total once finished,
// empty if it never ran.
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

// jobItem adapts a job to list.Item; FilterValue is the task name so "/"
// filtering matches on names.
type jobItem struct{ job *job }

func (i jobItem) FilterValue() string { return i.job.name }

// jobDelegate renders each task row: the state glyph (or the live spinner frame
// for a running task) plus the name, with a cursor marker on the selected row.
// spinnerFrame is refreshed on every spinner tick via list.SetDelegate.
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

// Pane is the Run tab: a task list plus the selected task's live output.
type Pane struct {
	jobs     []*job
	list     list.Model
	view     viewport.Model
	spinner  spinner.Model
	spinning bool
	width    int
	height   int
}

// NewPane builds the Run tab from configured tasks, falling back to tasks
// auto-detected from dir when none are configured.
func NewPane(tasks []config.Task, dir string) Pane {
	if len(tasks) == 0 {
		tasks = detect(dir)
	}
	jobs := make([]*job, len(tasks))
	items := make([]list.Item, len(tasks))
	for i, t := range tasks {
		jobs[i] = &job{name: t.Name, cmd: t.Cmd, state: stateIdle}
		items[i] = jobItem{job: jobs[i]}
	}
	l := list.New(items, jobDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.KeyMap.Quit = key.Binding{} // deck's global "q" owns quit
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)
	return Pane{jobs: jobs, list: l, view: viewport.New(0, 0), spinner: sp}
}

// anyRunning reports whether any job is currently executing.
func (p Pane) anyRunning() bool { return p.RunningCount() > 0 }

// RunningCount reports how many jobs are executing, shown as the tab-bar badge
// and in the status bar.
func (p Pane) RunningCount() int {
	n := 0
	for _, j := range p.jobs {
		if j.state == stateRunning {
			n++
		}
	}
	return n
}

// SetSize sets the pane's outer box: the task list plus the bordered output.
func (p *Pane) SetSize(w, h int) {
	p.width, p.height = w, h
	listW := p.listWidth()
	p.list.SetSize(listW, h)
	p.view.Width = max(w-listW-4, 0)
	p.view.Height = max(h-2, 0)
	p.refreshView()
}

func (p Pane) listWidth() int { return max(p.width/3, 24) }

// Capturing reports whether raw keys should bypass global nav. The Run tab only
// captures while the list's "/" filter is active.
func (p Pane) Capturing() bool { return p.list.FilterState() == list.Filtering }

// HelpBindings returns the Run-pane key hints shown in the help footer.
func (p Pane) HelpBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("enter/r", "run")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
		key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	}
}

func (p *Pane) selected() *job {
	if it, ok := p.list.SelectedItem().(jobItem); ok {
		return it.job
	}
	return nil
}

// jobIndex returns j's position in the jobs slice, used to tag streamed output.
// The list's own index shifts under filtering, so output is keyed off this.
func (p Pane) jobIndex(j *job) int {
	for i, jj := range p.jobs {
		if jj == j {
			return i
		}
	}
	return -1
}

func (p *Pane) refreshView() {
	j := p.selected()
	if j == nil {
		p.view.SetContent(ui.Muted.Render("no tasks — add [[tasks]] to ~/.config/deck/config.toml"))
		return
	}
	header := j.state.style().Render(j.state.glyph()+" "+j.name) + ui.Muted.Render("  $ "+j.cmd)
	if e := j.elapsed(); e != "" {
		header += ui.Muted.Render("  ·  " + e)
	}
	p.view.SetContent(header + "\n\n" + j.out.String())
}

// Update handles selection, running, stopping, and streamed output.
func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// run/stop/restart/clear are deck semantics the list doesn't know;
		// intercept them before the list, but not while typing into the "/"
		// filter. Page keys scroll the output viewport, which otherwise never
		// sees keystrokes.
		if p.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "enter", "r":
				return p.runJob(p.selected())
			case "R":
				return p.restart()
			case "x":
				p.stop()
				return p, nil
			case "c":
				if j := p.selected(); j != nil {
					j.out.Reset()
					p.refreshView()
				}
				return p, nil
			case "pgup", "pgdown", "ctrl+u", "ctrl+d":
				var cmd tea.Cmd
				p.view, cmd = p.view.Update(msg)
				return p, cmd
			}
		}
		prev := p.selected()
		var cmd tea.Cmd
		p.list, cmd = p.list.Update(msg)
		if p.selected() != prev {
			p.view.GotoTop()
			p.refreshView()
		}
		return p, cmd
	case OutputMsg:
		if msg.idx >= 0 && msg.idx < len(p.jobs) {
			j := p.jobs[msg.idx]
			if msg.gen != j.gen {
				return p, nil // late output from a superseded run
			}
			j.out.Append(msg.text)
			if p.selected() == j {
				// Only auto-follow when the user is at the bottom, so
				// scroll-back through history isn't yanked away.
				follow := p.view.AtBottom()
				p.refreshView()
				if follow {
					p.view.GotoBottom()
				}
			}
			if j.proc != nil {
				return p, j.proc.next()
			}
		}
		return p, nil
	case ExitMsg:
		if msg.idx >= 0 && msg.idx < len(p.jobs) {
			j := p.jobs[msg.idx]
			if msg.gen != j.gen {
				return p, nil // late exit from a superseded run
			}
			j.finishedAt = time.Now()
			switch {
			case j.stopped:
				j.state = stateStopped
			case msg.err != nil:
				j.state = stateFailed
				j.out.Append("\n" + ui.Err.Render("exited: "+msg.err.Error()) + "\n")
			default:
				j.state = stateDone
				j.out.Append("\n" + lipgloss.NewStyle().Foreground(ui.ColorString).Render("done") + "\n")
			}
			j.proc = nil
			if j.pendingRestart {
				// Restart was queued while this run wound down; relaunch now
				// that the old process has fully exited.
				return p.runJob(j)
			}
			if p.selected() == j {
				p.refreshView()
			}
		}
		return p, nil
	case spinner.TickMsg:
		if p.anyRunning() {
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			p.list.SetDelegate(jobDelegate{spinnerFrame: p.spinner.View()})
			return p, cmd
		}
		p.spinning = false
		return p, nil
	}

	var cmd tea.Cmd
	p.view, cmd = p.view.Update(msg)
	return p, cmd
}

// runJob (re)launches j with a fresh generation. It takes the job rather than
// reading the selection because a queued restart fires from ExitMsg, where the
// exiting job may no longer be the selected one.
func (p Pane) runJob(j *job) (Pane, tea.Cmd) {
	if j == nil || j.state == stateRunning {
		return p, nil
	}
	j.out.Reset()
	j.stopped = false
	j.pendingRestart = false
	j.state = stateRunning
	j.gen++
	j.startedAt = time.Now()
	j.finishedAt = time.Time{}
	j.proc = startProc(p.jobIndex(j), j.gen, j.cmd)
	if p.selected() == j {
		p.refreshView()
		p.view.GotoTop()
	}
	cmd := j.proc.next()
	if !p.spinning {
		p.spinning = true
		p.list.SetDelegate(jobDelegate{spinnerFrame: p.spinner.View()})
		return p, tea.Batch(cmd, p.spinner.Tick)
	}
	return p, cmd
}

// restart stops the selected job and queues an immediate re-run once its
// process exits; on an idle job it's a plain run.
func (p Pane) restart() (Pane, tea.Cmd) {
	j := p.selected()
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

func (p *Pane) stop() {
	j := p.selected()
	if j == nil || j.state != stateRunning {
		return
	}
	j.stopped = true
	j.proc.stop()
}

// View renders the task list beside the selected task's output, filling exactly
// the box given to SetSize.
func (p Pane) View() string {
	left := ui.Fit(p.list.View(), p.listWidth(), p.height)
	right := ui.Pane.Render(ui.Fit(p.view.View(), p.view.Width, p.view.Height))
	return ui.Fit(lipgloss.JoinHorizontal(lipgloss.Top, left, right), p.width, p.height)
}
