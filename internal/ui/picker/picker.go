// Package picker is deck's shared fuzzy overlay: a query line over a scrolling,
// optionally grouped list, used for find-file, grep and LSP result lists.
package picker

import (
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/kloppers-labs/deck/internal/ui"
)

// AsyncThreshold is the item count above which matching runs in a command.
var AsyncThreshold = 50_000

const (
	chromeRows = 4
	wheelRows  = 3
)

// Item is one entry. Highlight holds rune positions in Title to emphasise when
// the owner filters the list itself (Live pickers).
type Item struct {
	Title, Detail, Path string
	Line, Col           int
	Group               string
	Highlight           []int
}

// Match is an item that survived the query, with the rune positions in its
// Title that matched.
type Match struct {
	Index     int
	Score     int
	Positions []int
}

// MatchFunc filters and ranks items for a non-empty query.
type MatchFunc func(query string, items []Item) []Match

// Options configures a picker. Live pickers never filter: each query change is
// reported with QueryMsg and the owner replaces the items.
type Options struct {
	ID          string
	Dest        ui.Dest
	Title       string
	Placeholder string
	Live        bool
	Match       MatchFunc
}

// PeekMsg reports that the selection moved onto Item.
type PeekMsg struct {
	Dest ui.Dest
	ID   string
	Item Item
}

// AcceptMsg reports enter on Item; the picker has closed.
type AcceptMsg struct {
	Dest  ui.Dest
	ID    string
	Item  Item
	Query string
}

// CancelMsg reports esc; the picker has closed.
type CancelMsg struct {
	Dest ui.Dest
	ID   string
}

// QueryMsg reports a changed query in a Live picker.
type QueryMsg struct {
	Dest  ui.Dest
	ID    string
	Query string
}

func (m PeekMsg) RouteTo() ui.Dest   { return m.Dest }
func (m AcceptMsg) RouteTo() ui.Dest { return m.Dest }
func (m CancelMsg) RouteTo() ui.Dest { return m.Dest }
func (m QueryMsg) RouteTo() ui.Dest  { return m.Dest }

// Msg is a picker's own async result; owners forward it to Update.
type Msg interface {
	ui.Routed
	pickerMsg()
}

type matchedMsg struct {
	dest    ui.Dest
	seq     int64
	matches []Match
}

func (m matchedMsg) RouteTo() ui.Dest { return m.dest }
func (matchedMsg) pickerMsg()         {}

var seqGen atomic.Int64

// Model is an open picker; the zero value is closed.
type Model struct {
	opts    Options
	active  bool
	items   []Item
	all     bool
	matches []Match
	input   textinput.Model
	status  string
	sel     int
	top     int
	width   int
	height  int
	seq     int64
	clicks  ui.ClickTracker
}

// New returns an open, empty picker with the query focused.
func New(o Options) Model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = o.Placeholder
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Focus()
	return Model{opts: o, active: true, all: true, input: in}
}

func (m Model) Active() bool    { return m.active }
func (m Model) Capturing() bool { return m.active }
func (m Model) ID() string      { return m.opts.ID }
func (m Model) Query() string   { return m.input.Value() }
func (m Model) Total() int      { return len(m.items) }

func (m *Model) Close()             { m.active = false }
func (m *Model) SetStatus(s string) { m.status = s }

func (m *Model) SetSize(w, h int) {
	m.width, m.height = max(w, 0), max(h, 0)
	m.input.Width = max(m.innerWidth()-ansi.StringWidth(m.input.Prompt)-1, 1)
	m.ensureVisible()
}

func (m Model) listHeight() int      { return max(m.height-chromeRows, 0) }
func (m Model) innerWidth() int      { return max(m.width-ui.PaneActive.GetHorizontalFrameSize(), 0) }
func (m Model) itemIndex(i int) int  { return m.matchAt(i).Index }
func (m Model) groupOf(i int) string { return m.items[m.itemIndex(i)].Group }
func (m Model) hasGroups() bool      { return len(m.items) > 0 && m.items[0].Group != "" }
func (m Model) clampSel(i int) int   { return min(max(i, 0), max(m.Len()-1, 0)) }
func (m Model) filtering() bool      { return !m.opts.Live && m.input.Value() != "" }
func (m Model) pageSize() int        { return max(m.listHeight()-1, 1) }
func (m Model) countText() string    { return strconv.Itoa(m.Len()) + "/" + strconv.Itoa(len(m.items)) }

func (m Model) peekCmd(it Item) tea.Cmd {
	d, id := m.opts.Dest, m.opts.ID
	return func() tea.Msg { return PeekMsg{Dest: d, ID: id, Item: it} }
}

// Len is the number of items shown for the current query.
func (m Model) Len() int {
	if m.all {
		return len(m.items)
	}
	return len(m.matches)
}

func (m Model) matchAt(i int) Match {
	if m.all {
		return Match{Index: i}
	}
	return m.matches[i]
}

// At returns the i-th shown item and the rune positions to highlight.
func (m Model) At(i int) (Item, []int) {
	mt := m.matchAt(i)
	it := m.items[mt.Index]
	if m.all {
		return it, it.Highlight
	}
	return it, mt.Positions
}

func (m Model) Selected() (Item, bool) {
	if m.Len() == 0 {
		return Item{}, false
	}
	it, _ := m.At(m.sel)
	return it, true
}

// SetItems replaces the items and re-applies the query.
func (m *Model) SetItems(items []Item) tea.Cmd {
	m.items = items
	m.sel, m.top = 0, 0
	return m.rematch(-1)
}

// Append adds streamed items, keeping the selection on the same item.
func (m *Model) Append(items []Item) tea.Cmd {
	if len(items) == 0 {
		return nil
	}
	keep := -1
	if m.Len() > 0 {
		keep = m.itemIndex(m.sel)
	}
	m.items = append(m.items, items...)
	if m.all {
		return nil
	}
	return m.rematch(keep)
}

func (m *Model) rematch(keep int) tea.Cmd {
	q := m.input.Value()
	if !m.filtering() {
		m.seq = 0
		m.all, m.matches = true, nil
		m.restoreSel(keep)
		return nil
	}
	match := m.opts.Match
	if match == nil {
		match = FuzzyTitles
	}
	if len(m.items) <= AsyncThreshold {
		m.apply(match(q, m.items), keep)
		return nil
	}
	m.seq = seqGen.Add(1)
	seq, dest, items := m.seq, m.opts.Dest, m.items[:len(m.items):len(m.items)]
	return func() tea.Msg {
		return matchedMsg{dest: dest, seq: seq, matches: match(q, items)}
	}
}

func (m *Model) apply(matches []Match, keep int) {
	m.all, m.matches = false, matches
	if m.hasGroups() {
		rank := map[string]int{}
		for _, it := range m.items {
			if _, ok := rank[it.Group]; !ok {
				rank[it.Group] = len(rank)
			}
		}
		sort.SliceStable(m.matches, func(a, b int) bool {
			return rank[m.items[m.matches[a].Index].Group] < rank[m.items[m.matches[b].Index].Group]
		})
	}
	m.restoreSel(keep)
}

func (m *Model) restoreSel(keep int) {
	m.sel = 0
	if keep >= 0 {
		for i := 0; i < m.Len(); i++ {
			if m.itemIndex(i) == keep {
				m.sel = i
				break
			}
		}
	}
	if keep < 0 {
		m.top = 0
	}
	m.ensureVisible()
}

// FuzzyTitles ranks items by a fuzzy match on Title, best first.
func FuzzyTitles(query string, items []Item) []Match {
	found := fuzzy.FindFrom(query, titleSource(items))
	out := make([]Match, len(found))
	for i, f := range found {
		out[i] = Match{Index: f.Index, Score: f.Score, Positions: RunePositions(f.Str, f.MatchedIndexes)}
	}
	return out
}

type titleSource []Item

func (s titleSource) String(i int) string { return s[i].Title }
func (s titleSource) Len() int            { return len(s) }

// RunePositions converts byte offsets into s to rune offsets.
func RunePositions(s string, bytes []int) []int {
	out := make([]int, 0, len(bytes))
	r, b := 0, 0
	for _, want := range bytes {
		for b < want && b < len(s) {
			_, size := utf8.DecodeRuneInString(s[b:])
			b += size
			r++
		}
		out = append(out, r)
	}
	return out
}

// Update handles keys, mouse events in picker-local cells, and match results.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	switch msg := msg.(type) {
	case matchedMsg:
		if msg.seq != m.seq || m.seq == 0 {
			return m, nil
		}
		keep := -1
		if m.Len() > 0 && !m.all {
			keep = m.itemIndex(m.sel)
		}
		m.apply(msg.matches, keep)
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) key(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "ctrl+p", "ctrl+k":
		return m.move(-1)
	case "down", "ctrl+n", "ctrl+j":
		return m.move(1)
	case "pgup":
		return m.move(-m.pageSize())
	case "pgdown":
		return m.move(m.pageSize())
	case "esc":
		m.active = false
		d, id := m.opts.Dest, m.opts.ID
		return m, func() tea.Msg { return CancelMsg{Dest: d, ID: id} }
	case "enter":
		return m.accept()
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	q := m.input.Value()
	if q == before {
		return m, cmd
	}
	if m.opts.Live {
		d, id := m.opts.Dest, m.opts.ID
		return m, tea.Batch(cmd, func() tea.Msg { return QueryMsg{Dest: d, ID: id, Query: q} })
	}
	m.sel, m.top = 0, 0
	return m, tea.Batch(cmd, m.rematch(-1))
}

func (m Model) accept() (Model, tea.Cmd) {
	it, ok := m.Selected()
	if !ok {
		return m, nil
	}
	m.active = false
	d, id, q := m.opts.Dest, m.opts.ID, m.input.Value()
	return m, func() tea.Msg { return AcceptMsg{Dest: d, ID: id, Item: it, Query: q} }
}

func (m Model) move(d int) (Model, tea.Cmd) {
	next := m.clampSel(m.sel + d)
	if next == m.sel || m.Len() == 0 {
		return m, nil
	}
	m.sel = next
	m.ensureVisible()
	it, _ := m.Selected()
	return m, m.peekCmd(it)
}

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		return m.move(wheelRows)
	case tea.MouseButtonWheelUp:
		return m.move(-wheelRows)
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	row := msg.Y - (chromeRows - 1)
	rows := m.displayRows()
	if row < 0 || row >= len(rows) || rows[row].match < 0 {
		return m, nil
	}
	idx := rows[row].match
	if ui.IsDouble(m.clicks.Press(msg.X, msg.Y, time.Now())) && idx == m.sel {
		return m.accept()
	}
	if idx == m.sel {
		return m, nil
	}
	m.sel = idx
	it, _ := m.Selected()
	return m, m.peekCmd(it)
}

type dispRow struct {
	match int
	group string
}

// displayRows lays out the visible window from top: items, each preceded by a
// header when its group starts (the first row always shows its group).
func (m Model) displayRows() []dispRow {
	h := m.listHeight()
	var out []dispRow
	groups := m.hasGroups()
	for i := m.top; i < m.Len() && len(out) < h; i++ {
		if groups && (i == m.top || m.groupOf(i) != m.groupOf(i-1)) {
			out = append(out, dispRow{match: -1, group: m.groupOf(i)})
			if len(out) >= h {
				break
			}
		}
		out = append(out, dispRow{match: i})
	}
	return out
}

func (m *Model) ensureVisible() {
	m.sel = m.clampSel(m.sel)
	if m.sel < m.top {
		m.top = m.sel
	}
	for m.top < m.sel && !m.rowShown(m.sel) {
		m.top++
	}
}

func (m Model) rowShown(i int) bool {
	for _, r := range m.displayRows() {
		if r.match == i {
			return true
		}
	}
	return false
}

var (
	rowSelected = lipgloss.NewStyle().Bold(true).Foreground(ui.ColorFg).Background(ui.ColorDim)
	groupStyle  = lipgloss.NewStyle().Bold(true).Foreground(ui.ColorInfo)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(ui.ColorAccent)
)

// View renders the picker box at exactly the size given to SetSize.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	w := m.innerWidth()
	h := max(m.height-ui.PaneActive.GetVerticalFrameSize(), 0)
	lines := []string{m.header(w), m.input.View()}
	rows := m.displayRows()
	if len(rows) == 0 && (m.input.Value() != "" || len(m.items) > 0) {
		lines = append(lines, ui.Muted.Render("no matches"))
	}
	for _, r := range rows {
		if r.match < 0 {
			lines = append(lines, groupStyle.Render(ansi.Truncate(r.group, w, "…")))
			continue
		}
		lines = append(lines, m.renderRow(r.match, w))
	}
	return ui.PaneActive.Render(ui.Fit(strings.Join(lines, "\n"), w, h))
}

func (m Model) header(w int) string {
	count := ui.Muted.Render(m.countText())
	room := max(w-ansi.StringWidth(m.countText())-1, 0)
	left := titleStyle.Render(m.opts.Title)
	if m.status != "" {
		left += " " + ui.Muted.Render(m.status)
	}
	left = ansi.Truncate(left, room, "…")
	gap := max(w-ansi.StringWidth(left)-ansi.StringWidth(m.countText()), 1)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+count, w, "")
}

func (m Model) renderRow(i, w int) string {
	it, pos := m.At(i)
	selected := i == m.sel
	prefix := "  "
	if selected {
		prefix = "› "
	}
	if m.hasGroups() {
		prefix += "  "
	}
	line := prefix + highlight(it.Title, pos)
	if it.Detail != "" {
		line += "  " + ui.Muted.Render(it.Detail)
	}
	line = ansi.Truncate(line, w, "…")
	if selected {
		line += strings.Repeat(" ", max(w-ansi.StringWidth(line), 0))
		return rowSelected.Render(line)
	}
	return line
}

func highlight(s string, pos []int) string {
	if len(pos) == 0 {
		return s
	}
	marked := make(map[int]bool, len(pos))
	for _, p := range pos {
		marked[p] = true
	}
	var b, run strings.Builder
	i := 0
	for _, r := range s {
		if marked[i] {
			run.WriteRune(r)
		} else {
			if run.Len() > 0 {
				b.WriteString(ui.Active.Render(run.String()))
				run.Reset()
			}
			b.WriteRune(r)
		}
		i++
	}
	if run.Len() > 0 {
		b.WriteString(ui.Active.Render(run.String()))
	}
	return b.String()
}

// HelpBindings lists the picker's keys.
func (m Model) HelpBindings() []key.Binding { return Bindings }

var Bindings = []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}
