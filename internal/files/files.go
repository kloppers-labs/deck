// Package files is deck's Files tab: a lazily loaded directory tree beside the
// read-only code viewer, with git badges, metadata on hover and an info panel.
package files

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/httpc"
	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/ui/picker"
	"github.com/kloppers-labs/deck/internal/viewer"
)

// ListTop is the row, local to the pane, where the first tree row is drawn: the
// tree's top border, then the root header.
const ListTop = 2

const (
	hoverDelay   = 400 * time.Millisecond
	wheelRows    = 3
	tooltipWidth = 36
	defaultWidth = 32
)

// ReloadMsg re-reads the tree and the open file, keeping the position.
type ReloadMsg struct{}

// RevealMsg selects Target's file in the tree and opens it in the viewer at
// Target's position.
type RevealMsg struct{ Target editor.Target }

type hoverDueMsg struct{ seq int }

func (ReloadMsg) RouteTo() ui.Dest   { return ui.DestFiles }
func (RevealMsg) RouteTo() ui.Dest   { return ui.DestFiles }
func (hoverDueMsg) RouteTo() ui.Dest { return ui.DestFiles }

// ReloadCurrent is a command that reloads the Files tab in place.
func ReloadCurrent() tea.Msg { return ReloadMsg{} }

const (
	focusTree = iota
	focusViewer
)

const (
	treeAuto = iota
	treeShown
	treeHidden
)

type hoverState struct {
	path string
	y    int
	seq  int
	due  bool
	meta *Meta
}

// Model is the Files tab state.
type Model struct {
	tree     Tree
	viewer   viewer.Model
	focus    int
	treeMode int
	sidebar  int
	info     bool
	icons    string
	badgeSrc BadgeSource
	badgeSeq int
	badges   Badges
	agg      map[string]Badge
	gitTop   [2]string
	metas    *metaCache
	statSeq  int
	infoSeq  int
	infoPath string
	infoMeta *Meta
	hover    hoverState
	drag     bool
	clicks   ui.ClickTracker
	width    int
	height   int
	shown    string
	cardPath string
	initCmd  tea.Cmd
	spots    map[string]viewer.Spot
	history  *viewer.History
	pick     picker.Model
	origin   pickOrigin
	finder   finderState
	grep     grepState
	prompt   prompt
	yPending bool
	now      func() time.Time

	nav        LSP
	lspStatus  []viewer.ServerStatus
	lspLog     []string
	lspTicking bool
	ws         wsState

	offerInstall bool
	offer        *installOffer
	noOffer      map[string]bool
}

// New builds the Files tab rooted at dir (default: the working directory). Only
// the root is listed here; the first selection loads from Init.
func New(dir string, cfg config.Config) Model {
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		} else {
			dir = "."
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	tree, load := NewTree(dir, NewHidePolicy(cfg.Files.Hide, cfg.Files.ShowHidden), SortName)
	sidebar := cfg.Files.SidebarWidth
	if sidebar <= 0 {
		sidebar = defaultWidth
	}
	m := Model{
		tree: tree,
		viewer: viewer.New(viewer.Options{
			Root:              dir,
			Theme:             cfg.Theme,
			TabWidth:          cfg.Viewer.TabWidth,
			ScrollLines:       cfg.Viewer.ScrollLines,
			MaxHighlightBytes: cfg.Viewer.MaxHighlightBytes,
			LineNumbers:       cfg.Viewer.LineNumbers,
			Wrap:              cfg.Viewer.Wrap,
			HoverDelay:        time.Duration(cfg.Viewer.HoverDelayMS) * time.Millisecond,
			GotoClick:         cfg.Viewer.GotoClick,
			RequestTimeout:    time.Duration(cfg.LSP.RequestTimeoutMS) * time.Millisecond,
		}),
		sidebar: sidebar,
		info:    cfg.Files.InfoPanel,
		icons:   cfg.Files.Icons,
		metas:   newMetaCache(),
		spots:   map[string]viewer.Spot{},
		history: &viewer.History{},
		now:     time.Now,

		offerInstall: cfg.LSP.OfferInstall,
		noOffer:      map[string]bool{},
	}
	m.initCmd = tea.Batch(load, m.syncSelection())
	return m
}

// WithBadges sets where git badges come from; without one the tree has none
// until a routed git.StatusLoadedMsg arrives.
func (m Model) WithBadges(src BadgeSource) Model {
	m.badgeSrc = src
	return m
}

// Init loads the initial selection and the first badges.
func (m Model) Init() tea.Cmd {
	var badges tea.Cmd
	if m.badgeSrc != nil {
		badges = badgesCmd(m.badgeSrc, m.tree.Launch(), m.badgeSeq)
	}
	return tea.Batch(m.initCmd, badges)
}

// SelectedPath is the absolute path of the selected tree entry, or "".
func (m Model) SelectedPath() string {
	if n := m.tree.Selected(); n != nil {
		return n.Path
	}
	return ""
}

// ViewerLoc is the viewer's cursor location.
func (m Model) ViewerLoc() viewer.Loc { return m.viewer.Loc() }

func (m Model) badgeFor(path string) Badge { return m.agg[path] }

func (m Model) treeVisible() bool {
	switch m.treeMode {
	case treeShown:
		return true
	case treeHidden:
		return false
	}
	return m.width >= autoCollapseCols
}

func (m Model) layout() Layout {
	return ComputeLayout(m.width, m.height, m.sidebar, m.treeVisible(), m.info)
}

func (m *Model) relayout() {
	l := m.layout()
	if !l.ShowTree {
		m.prompt = prompt{}
	}
	rows := l.TreeInner().H - 1
	if m.prompt.kind != promptNone {
		rows--
	}
	m.tree.SetHeight(rows)
	vi := l.ViewerInner()
	m.viewer.SetSize(vi.W, vi.H)
	r := m.pickerRect()
	m.pick.SetSize(r.W, r.H)
	if !l.ShowTree && m.focus == focusTree {
		m.setFocus(focusViewer)
	}
}

// SetSize sets the pane's outer box: the tree, the viewer and the info panel.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.relayout()
}

// ResizeCmd re-renders width-dependent viewer content after SetSize.
func (m Model) ResizeCmd() tea.Cmd { return m.viewer.ResizeCmd() }

func (m *Model) setFocus(f int) {
	m.focus = f
	m.viewer.SetFocused(f == focusViewer)
}

func (m *Model) setSidebar(w int) {
	m.sidebar = clampSidebar(w, m.width)
	m.relayout()
}

func (m *Model) rememberSpot() {
	if p := m.viewer.Path(); p != "" && m.viewer.Loaded() {
		m.spots[p] = m.viewer.Spot()
	}
}

func (m *Model) spotFor(path string) *viewer.Spot {
	if s, ok := m.spots[path]; ok {
		return &s
	}
	return nil
}

// syncSelection points the viewer at the selected entry: files load
// asynchronously at their last position, directories show a card.
func (m *Model) syncSelection() tea.Cmd {
	info := m.requestInfo()
	n := m.tree.Selected()
	path := ""
	if n != nil {
		path = n.Path
	}
	if path == m.shown && path != "" {
		return info
	}
	if m.viewer.Loaded() && m.cardPath == "" && m.viewer.Path() != "" && n != nil && !n.IsDir && n.Path != m.viewer.Path() {
		m.history.Push(m.viewer.Loc())
	}
	m.rememberSpot()
	m.shown, m.cardPath = path, ""
	var cmd tea.Cmd
	switch {
	case n == nil && m.tree.Root().err != nil:
		m.viewer = m.viewer.ShowNote("")
	case n == nil:
		m.viewer = m.viewer.ShowNote("empty directory")
	case n.IsDir:
		m.cardPath = path
		m.viewer, cmd = m.viewer.OpenVirtual(path, m.spotFor(path), dirCardDoc(path, m.tree.Hide(), m.agg, m.tree.Sort()))
	default:
		m.viewer, cmd = m.viewer.Open(path, m.spotFor(path))
	}
	return tea.Batch(cmd, info)
}

func (m *Model) requestInfo() tea.Cmd {
	path := m.SelectedPath()
	if !m.info || path == "" || path == m.infoPath {
		return nil
	}
	m.statSeq++
	m.infoSeq, m.infoPath, m.infoMeta = m.statSeq, path, nil
	return StatCmd(path, m.infoSeq, m.metas)
}

func (m *Model) refreshBadges() tea.Cmd {
	if m.badgeSrc == nil {
		return nil
	}
	m.badgeSeq++
	return badgesCmd(m.badgeSrc, m.tree.Launch(), m.badgeSeq)
}

func (m *Model) applyBadges(b Badges) {
	m.badges = b
	m.agg = Aggregate(b.Status)
	m.tree.SetIgnored(b.Ignored)
}

func (m *Model) applyStatus(msg git.StatusLoadedMsg) {
	if msg.Err != nil || msg.Repo == nil {
		return
	}
	if m.gitTop[0] != msg.Repo.Top {
		m.gitTop = [2]string{msg.Repo.Top, LogicalTop(m.tree.Launch(), msg.Repo.Top)}
	}
	b := BadgesFromEntries(m.gitTop[1], msg.Snapshot.Entries)
	if len(b.Ignored) == 0 {
		b.Ignored = m.badges.Ignored
	}
	m.applyBadges(b)
}

func (m *Model) clearHover() {
	if m.hover.path == "" && !m.hover.due {
		return
	}
	m.statSeq++
	m.hover = hoverState{seq: m.statSeq}
}

// Update routes viewer messages to the viewer, keys by focus and mouse events
// by region, and applies the tab's own async results.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case viewer.Msg:
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		if _, ok := msg.(viewer.DocLoadedMsg); ok {
			m.maybeOffer()
		}
		return m, cmd
	case installDoneMsg:
		return m.installDone(msg)
	case viewer.GotoMsg:
		return m.gotoTarget(msg)
	case viewer.ChooseMsg:
		return m.choose(msg)
	case viewer.OutlineMsg:
		return m.outline(msg)
	case viewer.JumpedMsg:
		m.history.Push(msg.From)
		return m, nil
	case LSPEventMsg:
		return m.lspEvent(msg)
	case lspTickMsg:
		return m.lspTick()
	case wsDueMsg:
		return m.wsDue(msg)
	case wsResultMsg:
		return m.wsResult(msg)
	case picker.PeekMsg, picker.AcceptMsg, picker.CancelMsg, picker.QueryMsg, picker.Msg:
		return m.picked(msg)
	case FinderIndexMsg:
		return m.finderIndexed(msg)
	case grepDueMsg, GrepChunkMsg, GrepDoneMsg:
		return m.grepEvent(msg)
	case opDoneMsg:
		return m.opDone(msg)
	case shellClosedMsg:
		return m.shellClosed(msg)
	case ReloadMsg:
		return m.reload()
	case RevealMsg:
		m.finder.stale = true
		return m.reveal(msg.Target)
	case dirLoadedMsg:
		m.tree.Loaded(msg)
		return m, m.syncSelection()
	case BadgesMsg:
		if msg.Seq == m.badgeSeq {
			m.applyBadges(msg.Badges)
		}
		return m, nil
	case RefreshBadgesMsg, tea.FocusMsg:
		m.finder.stale = true
		return m, m.refreshBadges()
	case git.StatusLoadedMsg:
		m.applyStatus(msg)
		return m, nil
	case StatMsg:
		meta := msg.Meta
		if msg.Seq == m.infoSeq {
			m.infoMeta = &meta
		}
		if msg.Seq == m.hover.seq && m.hover.due {
			m.hover.meta = &meta
		}
		return m, nil
	case hoverDueMsg:
		if msg.seq != m.hover.seq || m.hover.path == "" {
			return m, nil
		}
		m.hover.due = true
		return m, StatCmd(m.hover.path, m.hover.seq, m.metas)
	case InvalidateMsg:
		return m.invalidate(msg.Saved)
	case tea.MouseMsg:
		if m.pick.Active() {
			return m.pickerMouse(msg)
		}
		return m.mouse(msg)
	case tea.KeyMsg:
		m.clearHover()
		switch {
		case m.offer != nil:
			return m.offerKey(msg)
		case m.pick.Active():
			return m.pickerKey(msg)
		case m.prompt.kind != promptNone:
			return m.promptKey(msg)
		case m.focus == focusViewer:
			return m.viewerKey(msg)
		}
		return m.treeKey(msg)
	}
	var cmd tea.Cmd
	switch {
	case m.pick.Active():
		m.pick, cmd = m.pick.Update(msg)
	case m.prompt.kind != promptNone:
		m.prompt.input, cmd = m.prompt.input.Update(msg)
	}
	return m, cmd
}

// paneKey handles the keys that work from both regions.
func (m *Model) paneKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+b":
		if m.layout().ShowTree {
			m.treeMode = treeHidden
		} else {
			m.treeMode = treeShown
		}
		m.relayout()
		return m.viewer.ResizeCmd(), true
	case "i":
		m.info = !m.info
		m.infoPath = ""
		m.relayout()
		return tea.Batch(m.viewer.ResizeCmd(), m.requestInfo()), true
	case "ctrl+p":
		return m.openFinder(), true
	case "ctrl+f":
		return m.openGrep(), true
	case "ctrl+t":
		var cmd tea.Cmd
		*m, cmd = m.openSymbols()
		return cmd, true
	case "ctrl+o", "alt+left":
		var cmd tea.Cmd
		*m, cmd = m.back()
		return cmd, true
	case "ctrl+]", "alt+right":
		var cmd tea.Cmd
		*m, cmd = m.forward()
		return cmd, true
	}
	return nil, false
}

func (m Model) viewerKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc && !m.viewer.WantsEsc() {
		if m.layout().ShowTree {
			m.setFocus(focusTree)
		}
		return m, nil
	}
	if !m.viewer.Capturing() {
		if cmd, ok := m.paneKey(msg); ok {
			return m, cmd
		}
		if msg.String() == "e" && m.cardPath != "" && m.cardPath == m.viewer.Path() {
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return m, cmd
}

func (m Model) treeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.yPending {
		m.yPending = false
		switch msg.String() {
		case "y":
			return m, m.copyPath(false)
		case "Y":
			return m, m.copyPath(true)
		case "esc":
			return m, nil
		}
	}
	if cmd, ok := m.paneKey(msg); ok {
		return m, cmd
	}
	n := m.tree.Selected()
	page := max(m.tree.height, 1)
	var cmd tea.Cmd
	switch msg.String() {
	case "j", "down":
		m.tree.Move(1)
	case "k", "up":
		m.tree.Move(-1)
	case "g", "home":
		m.tree.Select(0)
	case "G", "end":
		m.tree.Select(len(m.tree.Rows()) - 1)
	case "ctrl+d":
		m.tree.Move(max(page/2, 1))
	case "ctrl+u":
		m.tree.Move(-max(page/2, 1))
	case "pgdown":
		m.tree.Move(page)
	case "pgup":
		m.tree.Move(-page)
	case "enter":
		return m.activate()
	case "l", "right":
		switch {
		case n == nil:
		case !n.IsDir:
			return m.activate()
		case !n.Expanded:
			cmd = m.tree.Expand(n)
		case m.tree.sel+1 < len(m.tree.Rows()) && m.tree.Rows()[m.tree.sel+1].Depth > n.Depth:
			m.tree.Move(1)
		}
	case "h", "left":
		if n != nil && n.IsDir && n.Expanded {
			m.tree.Collapse(n)
		} else {
			m.tree.SelectParent()
		}
	case "-", "backspace":
		m.tree.RerootUp()
	case "=":
		m.tree.RerootAt(n)
	case "~":
		m.tree.RerootLaunch()
	case ".":
		m.tree.ToggleDotfiles()
	case "I":
		m.tree.ToggleIgnored()
	case "s":
		m.tree.SetSort(m.tree.Sort().Next())
	case "R":
		return m.reload()
	case "L":
		return m.openLog()
	case "<":
		m.setSidebar(m.layout().Tree.W - sidebarStep)
		return m, m.viewer.ResizeCmd()
	case ">":
		m.setSidebar(m.layout().Tree.W + sidebarStep)
		return m, m.viewer.ResizeCmd()
	case "e":
		return m, m.openEditor()
	case "x":
		return m, m.sendHTTPFile()
	case "a":
		return m.openPrompt(promptNew)
	case "r":
		return m.openPrompt(promptRename)
	case "D":
		return m.openPrompt(promptTrash)
	case "y":
		m.yPending = n != nil
		return m, nil
	case "o":
		if n != nil {
			return m, revealCmd(n.Path)
		}
		return m, nil
	case "!":
		dir := m.tree.Root().Path
		if n != nil {
			dir = n.Path
		}
		return m, shellCmd(dir)
	default:
		return m, nil
	}
	return m, tea.Batch(cmd, m.syncSelection())
}

func (m Model) activate() (Model, tea.Cmd) {
	n := m.tree.Selected()
	if n == nil {
		return m, nil
	}
	if n.IsDir {
		cmd := m.tree.Toggle(n)
		return m, tea.Batch(cmd, m.syncSelection())
	}
	cmd := m.syncSelection()
	m.setFocus(focusViewer)
	return m, cmd
}

func (m Model) copyPath(abs bool) tea.Cmd {
	n := m.tree.Selected()
	if n == nil {
		return nil
	}
	p := n.Path
	if rel, err := filepath.Rel(m.tree.Launch(), p); !abs && err == nil && filepath.IsLocal(rel) {
		p = rel
	}
	return copyCmd(p, p)
}

func (m Model) sendHTTPFile() tea.Cmd {
	n := m.tree.Selected()
	if n == nil || n.IsDir || !strings.EqualFold(filepath.Ext(n.Name), ".http") {
		return nil
	}
	return httpc.LoadRequest(n.Path, 0)
}

func (m Model) openEditor() tea.Cmd {
	n := m.tree.Selected()
	if n == nil || n.IsDir {
		return nil
	}
	t := editor.Target{Path: n.Path, Line: 1, Col: 1}
	if m.viewer.Path() == n.Path && m.viewer.Loaded() {
		loc := m.viewer.Loc()
		t.Line, t.Col = loc.Line+1, loc.Col+1
	}
	return func() tea.Msg { return editor.OpenMsg{Target: t} }
}

func (m Model) reload() (Model, tea.Cmd) {
	m.tree.Reload()
	m.finder.stale = true
	m.infoPath = ""
	badges := m.refreshBadges()
	n := m.tree.Selected()
	if n != nil && !n.IsDir && n.Path == m.shown && n.Path == m.viewer.Path() {
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Reload()
		return m, tea.Batch(cmd, badges, m.requestInfo())
	}
	m.shown = ""
	return m, tea.Batch(m.syncSelection(), badges)
}

func (m Model) reveal(t editor.Target) (Model, tea.Cmd) {
	if t.Path == "" {
		return m, nil
	}
	m.rememberSpot()
	return m.show(t.Path, &viewer.Spot{Line: max(t.Line-1, 0), Col: max(t.Col-1, 0), Top: t.Top - 1})
}

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	l := m.layout()
	if m.drag {
		switch msg.Action {
		case tea.MouseActionMotion:
			m.setSidebar(msg.X + 1)
		case tea.MouseActionRelease:
			m.drag = false
		}
		return m, m.viewer.ResizeCmd()
	}
	if msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonBackward:
			return m.back()
		case tea.MouseButtonForward:
			return m.forward()
		}
	}
	press := msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
	if press && l.OnDivider(msg.X, msg.Y) {
		m.clearHover()
		m.drag = true
		return m, nil
	}
	if l.ShowTree && l.Tree.Contains(msg.X, msg.Y) {
		leave := m.viewer.MouseLeave()
		m, cmd := m.treeMouse(msg, l)
		return m, tea.Batch(cmd, leave)
	}
	m.clearHover()
	if !l.Viewer.Contains(msg.X, msg.Y) {
		leave := m.viewer.MouseLeave()
		return m, leave
	}
	if press {
		m.setFocus(focusViewer)
	}
	local := msg
	local.X, local.Y = l.ViewerInner().Local(msg.X, msg.Y)
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(local)
	return m, cmd
}

func (m Model) treeMouse(msg tea.MouseMsg, l Layout) (Model, tea.Cmd) {
	inner := l.TreeInner()
	rows, top := m.tree.Visible()
	row := msg.Y - inner.Y - 1
	var n *Node
	if row >= 0 && row < len(rows) && inner.Contains(msg.X, msg.Y) {
		n = rows[row]
	}
	switch {
	case tea.MouseEvent(msg).IsWheel():
		m.clearHover()
		switch msg.Button {
		case tea.MouseButtonWheelDown:
			m.tree.ScrollBy(wheelRows)
		case tea.MouseButtonWheelUp:
			m.tree.ScrollBy(-wheelRows)
		}
		return m, nil
	case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonNone:
		return m.hoverRow(n, msg.Y)
	case msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft:
		return m, nil
	}
	m.clearHover()
	if n == nil {
		return m, nil
	}
	m.setFocus(focusTree)
	m.tree.Select(top + row)
	var cmd tea.Cmd
	x := msg.X - inner.X - n.Depth*2
	double := ui.IsDouble(m.clicks.Press(msg.X, msg.Y, time.Now()))
	switch {
	case n.IsDir && x >= 0 && x < 2:
		cmd = m.tree.Toggle(n)
	case double:
		return m.activate()
	}
	return m, tea.Batch(cmd, m.syncSelection())
}

func (m Model) hoverRow(n *Node, y int) (Model, tea.Cmd) {
	if n == nil {
		m.clearHover()
		return m, nil
	}
	if n.Path == m.hover.path {
		return m, nil
	}
	m.statSeq++
	seq := m.statSeq
	m.hover = hoverState{path: n.Path, y: y, seq: seq}
	return m, tea.Tick(hoverDelay, func(time.Time) tea.Msg { return hoverDueMsg{seq: seq} })
}

// Capturing reports whether typed text belongs to the pane: the install offer,
// the picker, an inline prompt, a pending y chord, or a viewer prompt or chord.
func (m Model) Capturing() bool {
	return m.offer != nil || m.pick.Active() || m.prompt.kind != promptNone || m.yPending ||
		(m.focus == focusViewer && m.viewer.Capturing())
}

var (
	findBinding  = key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "find file"))
	grepBinding  = key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "grep"))
	treeBindings = []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("l", "h"), key.WithHelp("l/h", "fold")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		findBinding,
		grepBinding,
		symbolsBinding,
		key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "lsp log")),
		key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "new")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "trash")),
		key.NewBinding(key.WithKeys("y"), key.WithHelp("yy/yY", "copy path")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "reveal")),
		key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "shell")),
		key.NewBinding(key.WithKeys("-", "=", "~"), key.WithHelp("-/=/~", "root")),
		key.NewBinding(key.WithKeys(".", "I"), key.WithHelp("./I", "hide")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "info")),
		key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "tree")),
		key.NewBinding(key.WithKeys("<", ">"), key.WithHelp("</>", "width")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "http")),
	}
	symbolsBinding = key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "symbols"))
	viewerExtras   = []key.Binding{
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		findBinding,
		grepBinding,
		symbolsBinding,
		key.NewBinding(key.WithKeys("ctrl+o", "ctrl+]"), key.WithHelp("ctrl+o/ctrl+]", "back/forward")),
		key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "info")),
		key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "tree")),
	}
	grepToggles = []key.Binding{
		key.NewBinding(key.WithKeys("alt+r"), key.WithHelp(ui.Alt("r"), "regex")),
		key.NewBinding(key.WithKeys("alt+c"), key.WithHelp(ui.Alt("c"), "case")),
		key.NewBinding(key.WithKeys("alt+g"), key.WithHelp(ui.Alt("g"), "glob (last word)")),
	}
	promptBindings = []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
	confirmBindings = []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "move to trash")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("any other key", "cancel")),
	}
)

// HelpBindings returns the key hints for whatever has the keyboard.
func (m Model) HelpBindings() []key.Binding {
	switch {
	case m.offer != nil:
		return offerBindings
	case m.pick.Active() && m.pick.ID() == pickGrep:
		return append(append([]key.Binding{}, picker.Bindings...), grepToggles...)
	case m.pick.Active():
		return picker.Bindings
	case m.prompt.kind == promptTrash:
		return confirmBindings
	case m.prompt.kind != promptNone:
		return promptBindings
	}
	if m.focus == focusViewer {
		return append(append([]key.Binding{}, viewerExtras...), m.viewer.HelpBindings()...)
	}
	return treeBindings
}

func (m Model) regionStyle(region int) lipgloss.Style {
	s := ui.Pane
	if m.focus == region {
		s = ui.PaneActive
	}
	if region == focusTree {
		s = s.Padding(0)
	}
	return s
}

var tooltipStyle = ui.PaneActive

// tooltip is the hover box for the row under the mouse, once its dwell is due
// and its metadata has arrived.
func (m Model) tooltip() string {
	if !m.hover.due || m.hover.meta == nil {
		return ""
	}
	lines := FormatMeta(*m.hover.meta, m.badgeFor(m.hover.path), tooltipWidth-4, m.now())
	return tooltipStyle.Render(strings.Join(lines, "\n"))
}

// View renders the tree, viewer and info panel side by side, filling exactly
// the box given to SetSize, with the hover tooltip composed on top.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	l := m.layout()
	var cols []string
	if l.ShowTree {
		ti := l.TreeInner()
		cols = append(cols, m.regionStyle(focusTree).Render(ui.Fit(m.treeView(ti.W, ti.H), ti.W, ti.H)))
	}
	vi := l.ViewerInner()
	cols = append(cols, m.regionStyle(focusViewer).Render(ui.Fit(m.viewer.View(), vi.W, vi.H)))
	if l.ShowInfo {
		ii := l.InfoInner()
		cols = append(cols, ui.Pane.Render(ui.Fit(m.infoView(ii.W), ii.W, ii.H)))
	}
	body := ui.Fit(lipgloss.JoinHorizontal(lipgloss.Top, cols...), m.width, m.height)
	if tip := m.tooltip(); tip != "" {
		anchor := ui.Rect{X: l.Tree.X + l.Tree.W, Y: m.hover.y}
		r := viewer.PlacePopup(anchor, lipgloss.Width(tip), lipgloss.Height(tip), ui.Rect{W: m.width, H: m.height})
		body = ui.Overlay(body, tip, r.X, r.Y)
	}
	if m.pick.Active() {
		r := m.pickerRect()
		body = ui.Overlay(body, m.pick.View(), r.X, r.Y)
	}
	if m.offer != nil {
		body = m.overlayOffer(body)
	}
	return ui.Fit(body, m.width, m.height)
}

func (m Model) treeView(w, h int) string {
	if m.prompt.kind != promptNone && h > 1 {
		return ui.Fit(m.treeRows(w, h-1), w, h-1) + "\n" + m.prompt.view(w)
	}
	return m.treeRows(w, h)
}

func (m Model) treeRows(w, h int) string {
	out := []string{m.treeHeader(w)}
	root := m.tree.Root()
	rows, top := m.tree.Visible()
	switch {
	case root.err != nil:
		out = append(out, ui.Err.Render(ui.FitWidth(loadError(root.err), w, "…")))
	case root.loading:
		out = append(out, ui.Muted.Render("loading…"))
	case len(rows) == 0:
		out = append(out, ui.Muted.Render("empty directory"))
	}
	hide := m.tree.Hide()
	for i, n := range rows {
		out = append(out, renderRow(n, rowStyle{
			width:    w,
			icons:    m.icons,
			badge:    m.agg[n.Path],
			selected: top+i == m.tree.sel,
			focused:  m.focus == focusTree,
			ignored:  hide.ShowIgnored && hide.IsIgnored(n.Path),
		}))
		if len(out) >= h {
			break
		}
	}
	return strings.Join(out, "\n")
}

func (m Model) treeHeader(w int) string {
	var flags []string
	if s := m.tree.Sort(); s != SortName {
		flags = append(flags, "↓"+s.String())
	}
	hide := m.tree.Hide()
	if hide.ShowDot {
		flags = append(flags, ".")
	}
	if hide.ShowIgnored {
		flags = append(flags, "I")
	}
	right := strings.Join(flags, " ")
	room := w
	if right != "" {
		room -= ansi.StringWidth(right) + 1
	}
	left := headerPath(m.tree.Launch(), m.tree.Root().Path, max(room, 0))
	gap := max(w-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return ui.Muted.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) infoView(w int) string {
	if m.infoMeta == nil || m.infoMeta.Path != m.SelectedPath() {
		return ui.Muted.Render("…")
	}
	return strings.Join(FormatMeta(*m.infoMeta, m.badgeFor(m.infoMeta.Path), w, m.now()), "\n")
}

// headerPath shows dir relative to the launch root (prefixed with the root's
// name), left-truncated to w columns so the deepest part stays visible.
func headerPath(root, dir string, w int) string {
	shown := dir
	rel, err := filepath.Rel(root, dir)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		shown = filepath.Join(filepath.Base(root), rel)
	}
	if over := ansi.StringWidth(shown) - w; over > 0 {
		shown = ansi.TruncateLeft(shown, over+1, "…")
	}
	return shown
}
