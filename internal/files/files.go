// Package files is deck's file navigator: a directory listing on the left and a
// syntax-highlighted preview on the right. Selecting a file emits an OpenMsg so
// the root model can hand off to micro.
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
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/httpc"
	"deck/internal/ui"
)

// ListTop is the row, local to the pane, where the first file row is drawn: the
// path header, then the list's reserved filter row.
const ListTop = 2

const wheelLines = 3

// OpenMsg asks the root model to open Path in the editor.
type OpenMsg struct{ Path string }

// entry is one row in the listing; it doubles as the list.Item, filtering on
// the file name.
type entry struct {
	name  string
	isDir bool
}

func (e entry) FilterValue() string { return e.name }

// fileDelegate renders one listing row: the name (with a trailing "/" for
// directories) and a cursor marker on the selected row.
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

// focus tracks which half of the pane owns the keyboard: the file list or the
// preview viewport.
const (
	focusList = iota
	focusPreview
)

// Model is the navigator state.
type Model struct {
	root    string
	dir     string
	list    list.Model
	preview viewport.Model
	focus   int
	theme   string
	clicks  ui.ClickTracker
	width   int
	height  int
	err     error
}

// New builds a navigator rooted at dir (defaulting to the working directory).
// theme is the chroma/glamour style name used for the preview.
func New(dir, theme string) Model {
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
	l.KeyMap.Quit = key.Binding{} // deck's global "q" owns quit
	m := Model{root: dir, dir: dir, theme: theme, preview: viewport.New(0, 0), list: l}
	m.load()
	return m
}

// readDir lists dir with directories first, each group sorted case-insensitively.
// Dotfiles are included; "." is not, but ".." navigation is handled separately.
func readDir(dir string) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(des))
	for _, de := range des {
		entries = append(entries, entry{name: de.Name(), isDir: de.IsDir()})
	}
	sortEntries(entries)
	return entries, nil
}

// sortEntries orders directories before files, then alphabetically (fold-case).
func sortEntries(entries []entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
}

func (m *Model) load() {
	entries, err := readDir(m.dir)
	m.err = err
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = e
	}
	m.list.ResetFilter()
	m.list.SetItems(items)
	m.list.Select(0)
	m.list.SetHeight(m.listHeight())
	m.refreshPreview()
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

func (m Model) selected() (entry, bool) {
	if e, ok := m.list.SelectedItem().(entry); ok {
		return e, true
	}
	return entry{}, false
}

// selectedName is the current selection's file name, or "" when none, used to
// detect selection changes after the list handles a key.
func (m Model) selectedName() string {
	if e, ok := m.selected(); ok {
		return e.name
	}
	return ""
}

func (m *Model) refreshPreview() {
	e, ok := m.selected()
	if !ok {
		m.preview.SetContent("")
		return
	}
	if e.isDir {
		m.preview.SetContent(ui.Muted.Render("📁 " + e.name + "/"))
		return
	}
	m.preview.SetContent(Preview(filepath.Join(m.dir, e.name), m.preview.Width, m.theme))
	m.preview.GotoTop()
}

// SetSize sets the pane's outer box: the list column plus the bordered preview.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	listW := m.listWidth()
	m.list.SetSize(listW, m.listHeight())
	m.preview.Width = max(w-listW-4, 0)
	m.preview.Height = max(h-2, 0)
	m.refreshPreview()
}

// Update handles navigation keys and forwards scrolling to the preview pane.
// "f" hands the keyboard to the preview viewport so the user can scroll it
// without moving the file cursor; "esc" returns focus to the list.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if mm, ok := msg.(tea.MouseMsg); ok {
		return m.mouse(mm)
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	filtering := m.list.FilterState() == list.Filtering

	// f/esc focus toggle. While filtering these keys belong to the filter box
	// (esc cancels the filter), so the toggle is suppressed.
	if !filtering {
		switch k.String() {
		case "f":
			m.focus = focusPreview
			return m, nil
		case "esc":
			m.focus = focusList
			return m, nil
		}
	}

	// Preview focus: every key scrolls the viewport; the file cursor never moves.
	if m.focus == focusPreview {
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}

	// List focus: intercept deck navigation semantics the list doesn't know,
	// unless we're typing into the filter.
	if !filtering {
		if model, cmd, handled := m.listKey(k); handled {
			return model, cmd
		}
	}

	// Everything else (up/down/j/k, g/G, "/", page keys) drives the list,
	// yielding filtering and pagination for free.
	prev := m.selectedName()
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if m.selectedName() != prev {
		m.refreshPreview()
	}
	return m, cmd
}

// listKey handles the deck-specific list keys (activate, up-a-dir), reporting
// whether it consumed the key; movement and filtering are left to the list.
func (m Model) listKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "enter", "l", "right":
		model, cmd := m.activate()
		return model, cmd, true
	case "backspace", "h", "left", "-":
		m.dir = filepath.Dir(m.dir)
		m.load()
		return m, nil, true
	case "x":
		if e, ok := m.selected(); ok && !e.isDir && strings.EqualFold(filepath.Ext(e.name), ".http") {
			return m, httpc.LoadRequest(filepath.Join(m.dir, e.name), 0), true
		}
	}
	return m, nil, false
}

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	listW := m.listWidth()
	if tea.MouseEvent(msg).IsWheel() {
		down := msg.Button == tea.MouseButtonWheelDown
		if msg.X >= listW {
			if down {
				m.preview.ScrollDown(wheelLines)
			} else {
				m.preview.ScrollUp(wheelLines)
			}
			return m, nil
		}
		prev := m.selectedName()
		if down {
			m.list.CursorDown()
		} else {
			m.list.CursorUp()
		}
		if m.selectedName() != prev {
			m.refreshPreview()
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if msg.X >= listW {
		m.focus = focusPreview
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
	m.focus = focusList
	prev := m.selectedName()
	m.list.Select(idx)
	if m.selectedName() != prev {
		m.refreshPreview()
	}
	if ui.IsDouble(m.clicks.Press(msg.X, msg.Y, time.Now())) {
		return m.activate()
	}
	return m, nil
}

// Capturing reports that the "/" filter owns the keyboard. Reading the preview
// never captures.
func (m Model) Capturing() bool {
	return m.list.FilterState() == list.Filtering
}

// HelpBindings returns the Files-pane key hints shown in the help footer.
func (m Model) HelpBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "up dir")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "focus preview")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "send .http to HTTP")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "list")),
	}
}

func (m Model) activate() (Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok {
		return m, nil
	}
	if e.isDir {
		m.dir = filepath.Join(m.dir, e.name)
		m.load()
		return m, nil
	}
	path := filepath.Join(m.dir, e.name)
	return m, func() tea.Msg { return OpenMsg{Path: path} }
}

// View renders the listing and preview side by side, filling exactly the box
// given to SetSize.
func (m Model) View() string {
	listW := m.listWidth()
	header := ui.Muted.Render(headerPath(m.root, m.dir, listW-1))
	col := ui.Fit(header+"\n"+m.list.View(), listW, m.listHeight()+1)
	if m.err != nil {
		col += "\n" + ui.Err.Render(ui.FitWidth(loadError(m.err), listW-1, "…"))
	}
	paneStyle := ui.Pane
	if m.focus == focusPreview {
		paneStyle = ui.PaneActive
	}
	preview := paneStyle.Render(ui.Fit(m.preview.View(), m.preview.Width, m.preview.Height))
	body := lipgloss.JoinHorizontal(lipgloss.Top, ui.Fit(col, listW, m.height), preview)
	return ui.Fit(body, m.width, m.height)
}

// loadError drops the path from a listing error; the header already shows it.
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
