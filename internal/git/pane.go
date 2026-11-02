package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"deck/internal/editor"
	"deck/internal/ui"
)

const (
	diffDebounce  = 50 * time.Millisecond
	maxCachedDiff = 256
	wheelStep     = 3
)

// Options configures the Git tab. Dir is where the repository is looked up; "" is the cwd.
type Options struct {
	Theme         string
	CommitAI      bool
	ConfirmCommit bool
	Dir           string
}

type focus int

const (
	focusList focus = iota
	focusDiff
)

type mode int

const (
	modeChanges mode = iota
	modeLog
)

type cachedDiff struct{ raw, rendered string }

type confirmPrompt struct {
	text string
	run  func(Pane) (Pane, tea.Cmd)
}

// Pane is deck's Git tab: sectioned changes or the log on the left, the diff on
// the right. Every git call runs as a command; the pane never blocks.
type Pane struct {
	opts Options
	repo *Repo
	snap Snapshot

	items   []Item
	rows    []row
	cursor  int
	offset  int
	focus   focus
	mode    mode
	notRepo bool

	statusSeq  int
	loading    bool
	reloads    int
	indexStamp int64
	stamps     map[string]int64

	diff     viewport.Model
	diffSeq  int
	cache    map[diffKey]cachedDiff
	shownKey diffKey
	rawDiff  string

	committing bool
	confirming bool
	amend      bool
	box        textarea.Model
	commitText string
	confirm    *huh.Confirm
	confirmVal *bool
	generating bool
	cancelGen  *context.CancelFunc

	branching      bool
	branchesLoaded bool
	naming         bool
	branchList     list.Model
	nameInput      textinput.Model

	logList   list.Model
	logLoaded bool

	pending *confirmPrompt
	busy    string
	busyOp  string
	spin    spinner.Model

	status    string
	statusErr bool
	width     int
	height    int
}

// NewPane builds the Git tab. It makes no git calls; the first RefreshMsg loads the status.
func NewPane(opts Options) Pane {
	box := textarea.New()
	box.ShowLineNumbers = false
	box.SetHeight(4)
	box.Placeholder = "subject\n\nbody"
	if opts.CommitAI {
		box.Placeholder = "subject (ctrl+g for AI)\n\nbody"
	}
	confirmVal := new(bool)
	*confirmVal = true
	cf := huh.NewConfirm().
		Title("Commit this message?").
		Affirmative("Yes").
		Negative("No").
		Inline(true).
		Value(confirmVal)
	name := textinput.New()
	name.Prompt = "new branch: "
	name.Placeholder = "name"
	return Pane{
		opts:       opts,
		diff:       viewport.New(0, 0),
		cache:      map[diffKey]cachedDiff{},
		box:        box,
		confirm:    cf,
		confirmVal: confirmVal,
		cancelGen:  new(context.CancelFunc),
		branchList: newBranchList(),
		nameInput:  name,
		logList:    newLogList(),
		spin:       spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(ui.Active)),
	}
}

// SetSize sets the pane's outer box: the list column plus the bordered right column.
func (p *Pane) SetSize(w, h int) {
	p.width, p.height = w, h
	innerW, innerH := p.rightInner()
	p.diff.Width = innerW
	p.diff.Height = innerH
	p.box.SetWidth(max(innerW, 1))
	p.nameInput.Width = max(innerW-len(p.nameInput.Prompt)-1, 1)
	p.branchList.SetSize(innerW, max(innerH-2, 0))
	p.logList.SetSize(max(p.listWidth()-1, 0), max(h-p.listTop(), 0))
	p.offset = p.scrollOffset()
}

func (p Pane) listWidth() int { return max(p.width*2/5, 24) }

func (p Pane) rightInner() (int, int) {
	return max(p.width-p.listWidth()-4, 0), max(p.height-2, 0)
}

func (p Pane) listTop() int { return 2 }

func (p Pane) visibleRows() int { return max(p.height-p.listTop(), 1) }

func (p Pane) rowOf(item int) int {
	for i, r := range p.rows {
		if !r.header && r.item == item {
			return i
		}
	}
	return 0
}

func (p Pane) scrollOffset() int {
	vis, off := p.visibleRows(), p.offset
	target := p.rowOf(p.cursor)
	top := target
	if top > 0 && p.rows[top-1].header {
		top--
	}
	if top < off {
		off = top
	}
	if target >= off+vis {
		off = target - vis + 1
	}
	return max(min(off, len(p.rows)-vis), 0)
}

func (p Pane) selected() (Item, bool) {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return Item{}, false
	}
	return p.items[p.cursor], true
}

func (p Pane) spinning() bool { return p.loading || p.busy != "" || p.generating }

func (p *Pane) setStatus(text string, isErr bool) { p.status, p.statusErr = text, isErr }

func (p Pane) withSpin(spinningBefore bool, cmd tea.Cmd) tea.Cmd {
	if spinningBefore || !p.spinning() {
		return cmd
	}
	return tea.Batch(cmd, p.spin.Tick)
}

// Update handles status and diff loads, the change list, the diff, the commit
// box, the branch picker, the log and remote operations.
func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	p, cmd := p.update(msg)
	p.offset = p.scrollOffset()
	return p, cmd
}

func (p Pane) update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case RefreshMsg:
		return p.reload()
	case StatusLoadedMsg:
		return p.applyStatus(msg)
	case diffDueMsg:
		if msg.seq != p.diffSeq || p.mode != modeChanges {
			return p, nil
		}
		return p.fetchDiff()
	case DiffLoadedMsg:
		return p.diffLoaded(msg), nil
	case opDoneMsg:
		return p.opDone(msg)
	case commitDoneMsg:
		return p.commitDone(msg)
	case headMessageMsg:
		if msg.err == nil && p.committing && p.amend && strings.TrimSpace(p.box.Value()) == "" {
			p.box.SetValue(strings.TrimSpace(msg.text))
		}
		return p, nil
	case branchesLoadedMsg:
		return p.applyBranches(msg), nil
	case logLoadedMsg:
		return p.applyLog(msg), nil
	case showLoadedMsg:
		return p.showLoaded(msg), nil
	case CommitMsgReady:
		return p.commitMsgReady(msg)
	case spinner.TickMsg:
		if !p.spinning() {
			return p, nil
		}
		var cmd tea.Cmd
		p.spin, cmd = p.spin.Update(msg)
		return p, cmd
	case tea.MouseMsg:
		return p.mouse(msg)
	case tea.KeyMsg:
		return p.key(msg)
	}
	var cmd tea.Cmd
	switch {
	case p.committing && !p.confirming:
		p.box, cmd = p.box.Update(msg)
	case p.naming:
		p.nameInput, cmd = p.nameInput.Update(msg)
	}
	return p, cmd
}

func (p Pane) reload() (Pane, tea.Cmd) {
	was := p.spinning()
	p.reloads++
	p.statusSeq++
	p.loading = true
	return p, p.withSpin(was, loadStatus(p.statusSeq, p.repo, p.opts.Dir))
}

func loadStatus(seq int, repo *Repo, dir string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if repo == nil {
			r, err := OpenRepo(ctx, dir)
			if err != nil {
				return StatusLoadedMsg{Seq: seq, Err: err}
			}
			repo = r
		}
		snap, err := repo.StatusSnapshot(ctx, false)
		if err != nil {
			return StatusLoadedMsg{Seq: seq, Repo: repo, Err: err}
		}
		return StatusLoadedMsg{Seq: seq, Repo: repo, Snapshot: snap,
			IndexStamp: repo.IndexStamp(), Stamps: fileStamps(repo, snap.Entries)}
	}
}

func (p Pane) applyStatus(msg StatusLoadedMsg) (Pane, tea.Cmd) {
	if msg.Seq != p.statusSeq {
		return p, nil
	}
	p.loading = false
	if errors.Is(msg.Err, ErrNotRepo) {
		p.notRepo, p.repo, p.snap, p.items, p.rows = true, nil, Snapshot{}, nil, nil
		p.cursor = 0
		p.setDiff(diffKey{}, cachedDiff{})
		return p, nil
	}
	if msg.Err != nil {
		p.setStatus("status: "+msg.Err.Error(), true)
		return p, nil
	}
	prev, had := p.selected()
	pos := p.posInSection()
	p.notRepo = false
	p.repo = msg.Repo
	p.snap = msg.Snapshot
	p.indexStamp = msg.IndexStamp
	p.stamps = msg.Stamps
	p.items = Items(msg.Snapshot.Entries)
	p.rows = layoutRows(p.items)
	p.cursor = p.reselect(prev, had, pos)
	if p.mode != modeChanges {
		return p, nil
	}
	return p.requestDiff()
}

func (p Pane) posInSection() int {
	it, ok := p.selected()
	if !ok {
		return 0
	}
	n := 0
	for _, other := range p.items[:p.cursor] {
		if other.Section == it.Section {
			n++
		}
	}
	return n
}

// reselect keeps the cursor on the same entry, or on its neighbour in the same
// section when the entry moved (staging walks down the Unstaged list).
func (p Pane) reselect(prev Item, had bool, pos int) int {
	if !had {
		return 0
	}
	var same []int
	for i, it := range p.items {
		if it.Section == prev.Section {
			if it.Entry.Path == prev.Entry.Path {
				return i
			}
			same = append(same, i)
		}
	}
	if len(same) > 0 {
		return same[min(pos, len(same)-1)]
	}
	for i, it := range p.items {
		if it.Entry.Path == prev.Entry.Path {
			return i
		}
	}
	return max(min(p.cursor, len(p.items)-1), 0)
}

func (p Pane) move(delta int) (Pane, tea.Cmd) {
	return p.moveTo(p.cursor + delta)
}

func (p Pane) moveTo(i int) (Pane, tea.Cmd) {
	i = max(min(i, len(p.items)-1), 0)
	if i == p.cursor || len(p.items) == 0 {
		return p, nil
	}
	p.cursor = i
	return p.scheduleDiff()
}

func (p Pane) key(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch {
	case p.pending != nil:
		return p.pendingKey(msg)
	case p.committing:
		return p.commitKey(msg)
	case p.branching:
		return p.branchKey(msg)
	case p.focus == focusDiff:
		return p.diffKeys(msg)
	case p.mode == modeLog:
		return p.logKey(msg)
	}
	return p.listKey(msg)
}

func (p Pane) listKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		return p.move(1)
	case "k", "up":
		return p.move(-1)
	case "g", "home":
		return p.moveTo(0)
	case "G", "end":
		return p.moveTo(len(p.items) - 1)
	case "enter", "right":
		if _, ok := p.selected(); ok {
			p.focus = focusDiff
		}
	case " ", "s":
		return p.toggleStage()
	case "a":
		return p.localOp("stage all", true, func(ctx context.Context, r *Repo) error { return r.StageAll(ctx) })
	case "A":
		initial := p.snap.Initial
		return p.localOp("unstage all", true, func(ctx context.Context, r *Repo) error { return r.UnstageAll(ctx, initial) })
	case "X":
		return p.askDiscard(), nil
	case "c":
		return p.openCommit()
	case "b":
		return p.openBranches()
	case "f":
		return p.startBusy("fetch", "fetching…", func(ctx context.Context, r *Repo) error { return r.Fetch(ctx) })
	case "p":
		return p.startBusy("pull", "pulling…", func(ctx context.Context, r *Repo) error { return r.Pull(ctx) })
	case "P":
		branch, upstream := p.snap.Branch, p.snap.Upstream
		return p.startBusy("push", "pushing…", func(ctx context.Context, r *Repo) error { return r.Push(ctx, branch, upstream) })
	case "l":
		return p.openLog()
	case "e":
		return p.edit()
	case "r":
		return p.reload()
	}
	return p, nil
}

func (p Pane) diffKeys(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		p.diff.ScrollDown(1)
	case "k", "up":
		p.diff.ScrollUp(1)
	case "ctrl+d":
		p.diff.HalfPageDown()
	case "ctrl+u":
		p.diff.HalfPageUp()
	case "pgdown", " ", "f":
		p.diff.PageDown()
	case "pgup", "b":
		p.diff.PageUp()
	case "g", "home":
		p.diff.GotoTop()
	case "G", "end":
		p.diff.GotoBottom()
	case "esc", "h", "left":
		p.focus = focusList
	case "e":
		if p.mode == modeChanges {
			return p.edit()
		}
	}
	return p, nil
}

func (p Pane) pendingKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		run := p.pending.run
		p.pending = nil
		return run(p)
	case "n", "N", "esc":
		p.pending = nil
	}
	return p, nil
}

func (p Pane) toggleStage() (Pane, tea.Cmd) {
	it, ok := p.selected()
	if !ok {
		return p, nil
	}
	if it.Section == SectionStaged {
		initial := p.snap.Initial
		return p.localOp("unstage", true, func(ctx context.Context, r *Repo) error { return r.Unstage(ctx, initial, it.Entry) })
	}
	return p.localOp("stage", true, func(ctx context.Context, r *Repo) error { return r.Stage(ctx, it.Entry.Path) })
}

func (p Pane) askDiscard() Pane {
	it, ok := p.selected()
	if !ok {
		return p
	}
	if it.Section == SectionConflicts {
		p.setStatus("resolve conflicts before discarding", true)
		return p
	}
	initial := p.snap.Initial
	p.pending = &confirmPrompt{
		text: "discard " + it.Section.String() + " changes to " + it.Entry.Path + "? y/n",
		run: func(p Pane) (Pane, tea.Cmd) {
			return p.localOp("discard "+it.Entry.Path, false, func(ctx context.Context, r *Repo) error {
				return r.Discard(ctx, initial, it)
			})
		},
	}
	return p
}

func (p Pane) edit() (Pane, tea.Cmd) {
	it, ok := p.selected()
	if !ok || p.repo == nil {
		return p, nil
	}
	if it.Entry.Deleted() && it.Section != SectionStaged {
		p.setStatus(it.Entry.Path+" is deleted", true)
		return p, nil
	}
	line := 1
	if p.shownKey == p.keyFor(it) {
		line = firstHunkLine(p.rawDiff)
	}
	msg := editor.OpenMsg{Target: editor.Target{Path: p.repo.Abs(it.Entry.Path), Line: line}}
	return p, func() tea.Msg { return msg }
}

func (p Pane) localOp(label string, quiet bool, fn func(context.Context, *Repo) error) (Pane, tea.Cmd) {
	if p.repo == nil {
		p.setStatus("not a git repository", true)
		return p, nil
	}
	repo := p.repo
	return p, func() tea.Msg { return opDoneMsg{label: label, quiet: quiet, err: fn(context.Background(), repo)} }
}

func (p Pane) startBusy(label, text string, fn func(context.Context, *Repo) error) (Pane, tea.Cmd) {
	if p.repo == nil {
		p.setStatus("not a git repository", true)
		return p, nil
	}
	if p.busy != "" {
		return p, nil
	}
	was := p.spinning()
	p.busy, p.busyOp = text, label
	repo := p.repo
	cmd := func() tea.Msg { return opDoneMsg{label: label, err: fn(context.Background(), repo)} }
	return p, p.withSpin(was, cmd)
}

func (p Pane) opDone(msg opDoneMsg) (Pane, tea.Cmd) {
	if msg.label == p.busyOp {
		p.busy, p.busyOp = "", ""
	}
	var toast tea.Cmd
	switch {
	case msg.err != nil:
		p.setStatus(msg.label+": "+msg.err.Error(), true)
		toast = ui.Toast(ui.LevelError, p.status)
	case !msg.quiet:
		p.setStatus(msg.label+" ✓", false)
	}
	p, cmd := p.reload()
	return p, tea.Batch(cmd, toast)
}

func (p Pane) mouse(msg tea.MouseMsg) (Pane, tea.Cmd) {
	if p.committing || p.branching || p.pending != nil {
		return p, nil
	}
	inList := msg.X < p.listWidth()
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		delta := 1
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -1
		}
		switch {
		case !inList && delta > 0:
			p.diff.ScrollDown(wheelStep)
		case !inList:
			p.diff.ScrollUp(wheelStep)
		case p.mode == modeLog:
			p.logStep(delta)
		default:
			return p.move(delta)
		}
		return p, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return p, nil
		}
		if !inList {
			p.focus = focusDiff
			return p, nil
		}
		p.focus = focusList
		if p.mode == modeLog {
			return p, nil
		}
		r := p.offset + msg.Y - p.listTop()
		if r >= 0 && r < len(p.rows) && !p.rows[r].header {
			return p.moveTo(p.rows[r].item)
		}
	}
	return p, nil
}

// Capturing reports whether a modal flow owns the keyboard: the commit box,
// the branch picker or a y/n prompt.
func (p Pane) Capturing() bool { return p.committing || p.branching || p.pending != nil }

// Branch reports the checked-out branch with ahead/behind counts for the status bar.
func (p Pane) Branch() string {
	if p.snap.Branch == "" || p.snap.Upstream == "" {
		return p.snap.Branch
	}
	return fmt.Sprintf("%s ↑%d ↓%d", p.snap.Branch, p.snap.Ahead, p.snap.Behind)
}

// Top is the repository root once the first status load has resolved it.
func (p Pane) Top() string {
	if p.repo == nil {
		return ""
	}
	return p.repo.Top
}

// Reloads counts status reloads so callers can observe that a refresh landed.
func (p Pane) Reloads() int { return p.reloads }

// DirtyCount reports the number of changed paths for the status bar.
func (p Pane) DirtyCount() int { return len(p.snap.Entries) }

var (
	bindMove     = key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move"))
	bindDiff     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "diff"))
	bindStage    = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "stage/unstage"))
	bindAll      = key.NewBinding(key.WithKeys("a", "A"), key.WithHelp("a/A", "stage/unstage all"))
	bindDiscard  = key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "discard"))
	bindCommit   = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "commit"))
	bindBranches = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branches"))
	bindRemote   = key.NewBinding(key.WithKeys("f", "p", "P"), key.WithHelp("f/p/P", "fetch/pull/push"))
	bindLog      = key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log"))
	bindEdit     = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	bindRefresh  = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	bindScroll   = key.NewBinding(key.WithKeys("j", "k", "ctrl+d", "ctrl+u"), key.WithHelp("j/k ctrl+d/u", "scroll"))
	bindEnds     = key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom"))
	bindBack     = key.NewBinding(key.WithKeys("esc", "h"), key.WithHelp("esc", "back"))
	bindShow     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "show commit"))
	bindYes      = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm"))
	bindNo       = key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel"))
	bindSave     = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "commit"))
	bindAmend    = key.NewBinding(key.WithKeys("alt+a"), key.WithHelp(ui.Alt("a"), "amend"))
	bindAI       = key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "AI msg"))
	bindCancel   = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	bindSwitch   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch"))
	bindNew      = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new"))
	bindDelete   = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete"))
	bindFilter   = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
)

// HelpBindings returns the key hints for the pane's current state.
func (p Pane) HelpBindings() []key.Binding {
	switch {
	case p.pending != nil:
		return []key.Binding{bindYes, bindNo}
	case p.committing:
		b := []key.Binding{bindSave, bindAmend}
		if p.opts.CommitAI {
			b = append(b, bindAI)
		}
		return append(b, bindCancel)
	case p.branching:
		return []key.Binding{bindSwitch, bindNew, bindDelete, bindFilter, bindCancel}
	case p.focus == focusDiff:
		return []key.Binding{bindScroll, bindEnds, bindEdit, bindBack}
	case p.mode == modeLog:
		return []key.Binding{bindMove, bindShow, bindBack}
	}
	return []key.Binding{bindMove, bindDiff, bindStage, bindAll, bindDiscard, bindCommit,
		bindBranches, bindRemote, bindLog, bindEdit, bindRefresh}
}
