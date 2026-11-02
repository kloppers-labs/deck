// Package files is deck's Files tab: a directory listing beside the read-only code
// viewer. Selecting a file loads it asynchronously; enter focuses the viewer.
package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/httpc"
	"deck/internal/ui"
	"deck/internal/viewer"
)

// ListTop is the row, local to the pane, where the first file row is drawn: the
// path header, then the list's reserved filter row.
const ListTop = 2

const (
	viewerInsetX = 2
	viewerInsetY = 1
)

// ReloadMsg re-reads the listing and the open file, keeping the position.
type ReloadMsg struct{}

// RevealMsg selects Target's file in the listing and opens it in the viewer at
// Target's position.
type RevealMsg struct{ Target editor.Target }

func (ReloadMsg) RouteTo() ui.Dest { return ui.DestFiles }
func (RevealMsg) RouteTo() ui.Dest { return ui.DestFiles }

// ReloadCurrent is a command that reloads the Files tab in place.
func ReloadCurrent() tea.Msg { return ReloadMsg{} }

type entry struct {
	name  string
	isDir bool
}

func (e entry) FilterValue() string { return e.name }

type fileDelegate struct{}

func (fileDelegate) Height() int                         { return 1 }
func (fileDelegate) Spacing() int                        { return 0 }
func (fileDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (fileDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	e, ok := item.(entry)
	if !ok {
		return
	}
	name := e.name
	if e.isDir {
		name += "/"
	}
	name = ui.FitWidth(name, m.Width()-2, "…")
	if index == m.Index() {
		fmt.Fprint(w, ui.Active.Render("› "+name))
		return
	}
	fmt.Fprint(w, "  "+name)
}

const (
	focusList = iota
	focusViewer
)

// Model is the Files tab state.
type Model struct {
	root    string
	dir     string
	list    list.Model
	viewer  viewer.Model
	focus   int
	clicks  ui.ClickTracker
	width   int
	height  int
	err     error
	shown   string
	initCmd tea.Cmd
	dirSel  map[string]string
	spots   map[string]viewer.Spot
}

// New builds the Files tab rooted at dir (default: the working directory). Only the
// directory is listed here; the first file loads from Init.
func New(dir string, cfg config.Config) Model {
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		} else {
			dir = "."
		}
	}
	l := list.New(nil, fileDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.KeyMap.Quit = key.Binding{}
	v := viewer.New(viewer.Options{
		Root:              dir,
		Theme:             cfg.Theme,
		TabWidth:          cfg.Viewer.TabWidth,
		ScrollLines:       cfg.Viewer.ScrollLines,
		MaxHighlightBytes: cfg.Viewer.MaxHighlightBytes,
		LineNumbers:       cfg.Viewer.LineNumbers,
		Wrap:              cfg.Viewer.Wrap,
	})
	m := Model{
		root:   dir,
		dir:    dir,
		list:   l,
		viewer: v,
		dirSel: map[string]string{},
		spots:  map[string]viewer.Spot{},
	}
	m.load("")
	m.initCmd = m.syncSelection()
	return m
}

// Init loads the initially selected file.
func (m Model) Init() tea.Cmd { return m.initCmd }

// readDir lists dir with directories first, each group sorted
// case-insensitively. Symlinks to directories count as directories.
func readDir(dir string) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(des))
	for _, de := range des {
		isDir := de.IsDir()
		if de.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, de.Name())); err == nil {
				isDir = fi.IsDir()
			}
		}
		entries = append(entries, entry{name: de.Name(), isDir: isDir})
	}
	sortEntries(entries)
	return entries, nil
}

func sortEntries(entries []entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
}

// load lists m.dir and selects prefer, else the entry remembered for this
// directory, else the first.
func (m *Model) load(prefer string) {
	entries, err := readDir(m.dir)
	m.err = err
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = e
	}
	if prefer == "" {
		prefer = m.dirSel[m.dir]
	}
	m.list.ResetFilter()
	m.list.SetItems(items)
	m.list.Select(0)
	for i, e := range entries {
		if e.name == prefer {
			m.list.Select(i)
			break
		}
	}
	m.list.SetHeight(m.listHeight())
}

func (m Model) listHeight() int {
	h := m.height - 1
	if m.err != nil {
		h--
	}
	return max(h, 0)
}

func (m Model) listWidth() int {
	return max(m.width/3, 20)
}

// SelectedPath is the absolute path of the highlighted entry, or "" when the
// directory is empty.
func (m Model) SelectedPath() string {
	if e, ok := m.selected(); ok {
		return filepath.Join(m.dir, e.name)
	}
	return ""
}

// ViewerLoc is the viewer's cursor location.
func (m Model) ViewerLoc() viewer.Loc { return m.viewer.Loc() }

func (m Model) selected() (entry, bool) {
	if e, ok := m.list.SelectedItem().(entry); ok {
		return e, true
	}
	return entry{}, false
}

func (m Model) selectedName() string {
	if e, ok := m.selected(); ok {
		return e.name
	}
	return ""
}

func (m *Model) rememberSpot() {
	if p := m.viewer.Path(); p != "" && m.viewer.Loaded() {
		m.spots[p] = m.viewer.Spot()
	}
}

// syncSelection points the viewer at the selected entry, loading files
// asynchronously and restoring the last position seen in each.
func (m *Model) syncSelection() tea.Cmd {
	path := m.SelectedPath()
	if path == m.shown {
		return nil
	}
	m.rememberSpot()
	m.shown = path
	e, ok := m.selected()
	switch {
	case !ok && m.err == nil:
		m.viewer = m.viewer.ShowNote("empty directory")
		return nil
	case !ok:
		m.viewer = m.viewer.ShowNote("")
		return nil
	case e.isDir:
		m.viewer = m.viewer.ShowNote("📁 " + e.name + "/")
		return nil
	}
	var at *viewer.Spot
	if s, ok := m.spots[path]; ok {
		at = &s
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Open(path, at)
	return cmd
}

func (m *Model) chdir(dir, prefer string) tea.Cmd {
	if name := m.selectedName(); name != "" {
		m.dirSel[m.dir] = name
	}
	m.dir = dir
	m.load(prefer)
	return m.syncSelection()
}

func (m *Model) setFocus(f int) {
	m.focus = f
	m.viewer.SetFocused(f == focusViewer)
}

// SetSize sets the pane's outer box: the list column plus the bordered viewer.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.list.SetSize(m.listWidth(), m.listHeight())
	m.viewer.SetSize(max(w-m.listWidth()-2*viewerInsetX, 0), max(h-2*viewerInsetY, 0))
}

// ResizeCmd re-renders width-dependent viewer content after SetSize.
func (m Model) ResizeCmd() tea.Cmd { return m.viewer.ResizeCmd() }

// Update routes viewer messages to the viewer, keys by focus and mouse events
// by region.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case viewer.Msg:
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return m, cmd
	case ReloadMsg:
		return m.reload()
	case RevealMsg:
		return m.reveal(msg.Target)
	case tea.MouseMsg:
		return m.mouse(msg)
	case tea.KeyMsg:
		if m.focus == focusViewer {
			return m.viewerKey(msg)
		}
		return m.listKey(msg)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) viewerKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc && !m.viewer.WantsEsc() {
		m.setFocus(focusList)
		return m, nil
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return m, cmd
}

func (m Model) listKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.list.FilterState() != list.Filtering {
		switch msg.String() {
		case "enter", "l", "right":
			return m.activate()
		case "backspace", "h", "left", "-":
			cmd := m.chdir(filepath.Dir(m.dir), filepath.Base(m.dir))
			return m, cmd
		case "e":
			return m, m.openEditor()
		case "x":
			if cmd := m.sendHTTPFile(); cmd != nil {
				return m, cmd
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	sync := m.syncSelection()
	return m, tea.Batch(cmd, sync)
}

func (m Model) sendHTTPFile() tea.Cmd {
	e, ok := m.selected()
	if !ok || e.isDir || !strings.EqualFold(filepath.Ext(e.name), ".http") {
		return nil
	}
	return httpc.LoadRequest(filepath.Join(m.dir, e.name), 0)
}

func (m Model) openEditor() tea.Cmd {
	e, ok := m.selected()
	if !ok || e.isDir {
		return nil
	}
	t := editor.Target{Path: filepath.Join(m.dir, e.name), Line: 1, Col: 1}
	return func() tea.Msg { return editor.OpenMsg{Target: t} }
}

func (m Model) activate() (Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok {
		return m, nil
	}
	if e.isDir {
		cmd := m.chdir(filepath.Join(m.dir, e.name), "")
		return m, cmd
	}
	cmd := m.syncSelection()
	m.setFocus(focusViewer)
	return m, cmd
}

func (m Model) reload() (Model, tea.Cmd) {
	path := m.SelectedPath()
	m.load(m.selectedName())
	if p := m.SelectedPath(); p == path && p != "" && p == m.viewer.Path() {
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Reload()
		return m, cmd
	}
	cmd := m.syncSelection()
	return m, cmd
}

func (m Model) reveal(t editor.Target) (Model, tea.Cmd) {
	if t.Path == "" {
		return m, nil
	}
	if dir := filepath.Dir(t.Path); dir != m.dir {
		if name := m.selectedName(); name != "" {
			m.dirSel[m.dir] = name
		}
		m.dir = dir
	}
	m.load(filepath.Base(t.Path))
	m.rememberSpot()
	m.shown = t.Path
	spot := viewer.Spot{Line: max(t.Line-1, 0), Col: max(t.Col-1, 0), Top: t.Top - 1}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Open(t.Path, &spot)
	m.setFocus(focusViewer)
	return m, cmd
}

func (m Model) viewerRect() ui.Rect {
	return ui.Rect{X: m.listWidth() + viewerInsetX, Y: viewerInsetY, W: max(m.width-m.listWidth()-2*viewerInsetX, 0), H: max(m.height-2*viewerInsetY, 0)}
}

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	listW := m.listWidth()
	if msg.X >= listW {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.setFocus(focusViewer)
		}
		local := msg
		local.X, local.Y = m.viewerRect().Local(msg.X, msg.Y)
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(local)
		return m, cmd
	}
	if tea.MouseEvent(msg).IsWheel() {
		switch msg.Button {
		case tea.MouseButtonWheelDown:
			m.list.CursorDown()
		case tea.MouseButtonWheelUp:
			m.list.CursorUp()
		}
		cmd := m.syncSelection()
		return m, cmd
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if m.list.FilterState() == list.Filtering {
		return m, nil
	}
	row := msg.Y - ListTop
	idx := m.list.Paginator.Page*m.list.Paginator.PerPage + row
	if row < 0 || row >= m.list.Paginator.PerPage || idx >= len(m.list.VisibleItems()) {
		return m, nil
	}
	m.setFocus(focusList)
	m.list.Select(idx)
	cmd := m.syncSelection()
	if ui.IsDouble(m.clicks.Press(msg.X, msg.Y, time.Now())) {
		next, open := m.activate()
		return next, tea.Batch(cmd, open)
	}
	return m, cmd
}

// Capturing reports whether typed text belongs to the pane: the list filter,
// or a viewer prompt or pending chord. Reading never captures.
func (m Model) Capturing() bool {
	if m.list.FilterState() == list.Filtering {
		return true
	}
	return m.focus == focusViewer && m.viewer.Capturing()
}

var (
	listBindings = []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		key.NewBinding(key.WithKeys("enter", "l"), key.WithHelp("enter", "view")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "up dir")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "send .http to HTTP")),
	}
	backToList = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "list"))
)

// HelpBindings returns the key hints for whichever region has focus.
func (m Model) HelpBindings() []key.Binding {
	if m.focus == focusViewer {
		return append(m.viewer.HelpBindings(), backToList)
	}
	return listBindings
}

// View renders the listing and viewer side by side, filling exactly the box
// given to SetSize.
func (m Model) View() string {
	listW := m.listWidth()
	header := ui.Muted.Render(headerPath(m.root, m.dir, listW-1))
	col := ui.Fit(header+"\n"+m.list.View(), listW, m.listHeight()+1)
	if m.err != nil {
		col += "\n" + ui.Err.Render(ui.FitWidth(loadError(m.err), listW-1, "…"))
	}
	paneStyle := ui.Pane
	if m.focus == focusViewer {
		paneStyle = ui.PaneActive
	}
	r := m.viewerRect()
	pane := paneStyle.Render(ui.Fit(m.viewer.View(), r.W, r.H))
	body := lipgloss.JoinHorizontal(lipgloss.Top, ui.Fit(col, listW, m.height), pane)
	return ui.Fit(body, m.width, m.height)
}

func loadError(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
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
