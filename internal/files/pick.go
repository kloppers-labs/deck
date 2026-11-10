package files

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/ui/picker"
	"github.com/kloppers-labs/deck/internal/viewer"
)

const (
	pickFind = "find"
	pickGrep = "grep"

	pickerMaxWidth  = 110
	pickerMaxHeight = 24
	pickerMinHeight = 8
)

// pickOrigin is where the viewer stood when a picker opened, so esc can put it
// back after peeks and enter can record the jump.
type pickOrigin struct {
	loc      viewer.Loc
	spot     viewer.Spot
	card     bool
	peeked   bool
	markdown bool
}

func (m *Model) openPicker(o picker.Options) {
	m.clearHover()
	m.yPending = false
	m.rememberSpot()
	m.origin = pickOrigin{
		loc:  m.viewer.Loc(),
		spot: m.viewer.Spot(),
		card: m.cardPath != "" && m.cardPath == m.viewer.Path(),
	}
	if !m.viewer.Loaded() {
		m.origin.loc.Path = ""
	}
	m.pick = picker.New(o)
	r := m.pickerRect()
	m.pick.SetSize(r.W, r.H)
}

// pickerRect is the picker box: centred across the bottom of the pane so the
// viewer rows above it stay visible for peeks.
func (m Model) pickerRect() ui.Rect {
	w := min(max(m.width-4, 0), pickerMaxWidth)
	h := min(max(m.height*3/5, pickerMinHeight), pickerMaxHeight, m.height)
	return ui.Rect{X: (m.width - w) / 2, Y: m.height - h, W: w, H: h}
}

// peekSpot places line (1-based) inside the viewer rows left above the picker.
func (m Model) peekSpot(line, col int) *viewer.Spot {
	if line <= 0 {
		return nil
	}
	above := m.pickerRect().Y - 2
	top := -1
	if above >= 3 {
		top = max(line-1-above/2, 0)
	}
	return &viewer.Spot{Line: line - 1, Col: max(col, 0), Top: top}
}

func (m Model) peek(path string, spot *viewer.Spot) (Model, tea.Cmd) {
	if path == "" {
		return m, nil
	}
	m.origin.peeked = true
	var cmd tea.Cmd
	switch {
	case path == m.viewer.Path() && m.viewer.Loaded() && spot != nil:
		m.viewer = m.viewer.JumpTo(*spot)
	case viewer.IsVirtual(path):
		m.viewer, cmd = m.viewer.OpenURI(path, spot)
	default:
		m.viewer, cmd = m.viewer.Open(path, spot)
	}
	return m, cmd
}

func (m Model) restoreOrigin() (Model, tea.Cmd) {
	if m.origin.markdown {
		m.viewer = m.viewer.SetRawMarkdown(false)
	}
	if !m.origin.peeked {
		return m, nil
	}
	m.origin.peeked = false
	if m.origin.card || m.origin.loc.Path == "" {
		m.viewer = m.viewer.ShowNote("")
		m.shown = ""
		return m, m.syncSelection()
	}
	spot := m.origin.spot
	if m.origin.loc.Path == m.viewer.Path() && m.viewer.Loaded() {
		m.viewer = m.viewer.JumpTo(spot)
		return m, nil
	}
	var cmd tea.Cmd
	if viewer.IsVirtual(m.origin.loc.Path) {
		m.viewer, cmd = m.viewer.OpenURI(m.origin.loc.Path, &spot)
	} else {
		m.viewer, cmd = m.viewer.Open(m.origin.loc.Path, &spot)
	}
	return m, cmd
}

// jump records the picker's origin in the history, then reveals path in the
// tree and opens it in the viewer at spot.
func (m Model) jump(path string, spot *viewer.Spot) (Model, tea.Cmd) {
	if o := m.origin; o.loc.Path != "" && !o.card {
		m.history.Push(o.loc)
	}
	m.origin = pickOrigin{}
	if path == "" {
		return m, nil
	}
	return m.openAt(path, spot)
}

func (m Model) show(path string, spot *viewer.Spot) (Model, tea.Cmd) {
	m.tree.Reload()
	m.tree.Reveal(path)
	m.shown, m.cardPath = path, ""
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Open(path, spot)
	m.setFocus(focusViewer)
	return m, tea.Batch(cmd, m.refreshBadges(), m.requestInfo())
}

func (m Model) pickerKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.pick.ID() == pickGrep {
		if toggled := m.grep.toggle(msg.String()); toggled {
			return m.startSearch()
		}
	}
	var cmd tea.Cmd
	m.pick, cmd = m.pick.Update(msg)
	if !m.pick.Active() {
		m.stopGrep()
	}
	return m, cmd
}

func (m Model) pickerMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	r := m.pickerRect()
	if !r.Contains(msg.X, msg.Y) {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			return m.pickerKey(tea.KeyMsg{Type: tea.KeyEsc})
		}
		return m, nil
	}
	local := msg
	local.X, local.Y = r.Local(msg.X, msg.Y)
	var cmd tea.Cmd
	m.pick, cmd = m.pick.Update(local)
	if !m.pick.Active() {
		m.stopGrep()
	}
	return m, cmd
}

func (m Model) picked(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case picker.PeekMsg:
		if msg.ID != m.pick.ID() || !m.pick.Active() {
			return m, nil
		}
		if msg.ID == pickFind {
			_, line, col := splitLineSuffix(m.pick.Query())
			spot := m.peekSpot(line, col-1)
			if spot == nil {
				spot = m.spotFor(msg.Item.Path)
			}
			return m.peek(msg.Item.Path, spot)
		}
		return m.peek(msg.Item.Path, m.peekSpot(msg.Item.Line, msg.Item.Col))
	case picker.AcceptMsg:
		if msg.ID != m.pick.ID() {
			return m, nil
		}
		m.stopGrep()
		if msg.ID == pickFind {
			_, line, col := splitLineSuffix(msg.Query)
			spot := m.spotFor(msg.Item.Path)
			if line > 0 {
				spot = &viewer.Spot{Line: line - 1, Col: max(col-1, 0), Top: -1}
			}
			return m.jump(msg.Item.Path, spot)
		}
		return m.jump(msg.Item.Path, &viewer.Spot{Line: max(msg.Item.Line-1, 0), Col: msg.Item.Col, Top: -1})
	case picker.CancelMsg:
		if msg.ID != m.pick.ID() {
			return m, nil
		}
		m.stopGrep()
		return m.restoreOrigin()
	case picker.QueryMsg:
		if !m.pick.Active() || msg.ID != m.pick.ID() {
			return m, nil
		}
		switch msg.ID {
		case pickGrep:
			return m.grepQuery()
		case pickSymbols:
			return m.wsQuery(msg.Query)
		}
		return m, nil
	case picker.Msg:
		var cmd tea.Cmd
		m.pick, cmd = m.pick.Update(msg)
		return m, cmd
	}
	return m, nil
}
