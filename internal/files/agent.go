package files

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/viewer"
)

// Root is the launch directory of the tree.
func (m Model) Root() string { return m.tree.Launch() }

// ShownFile is the file in the viewer, or "" for a directory card, a note or a virtual document.
func (m Model) ShownFile() string {
	if !m.viewer.ShowingDocument() || (m.cardPath != "" && m.cardPath == m.viewer.Path()) {
		return ""
	}
	return m.viewer.Path()
}

// SelectedDir is the tree selection when it is a directory.
func (m Model) SelectedDir() string {
	if n := m.tree.Selected(); n != nil && n.IsDir {
		return n.Path
	}
	return ""
}

func (m Model) Selection() (viewer.Range, bool) { return m.viewer.Selection() }

func (m Model) SelectionText() string { return m.viewer.SelectionText() }

func (m Model) Section() (string, int, int, bool) { return m.viewer.Section() }

func (m Model) VisibleLines() (int, int) { return m.viewer.VisibleLines() }

func (m Model) RecentFiles(n int) []string { return m.history.Recent(n) }

// Diagnostics unpacks a diagnostics event.
func (e LSPEventMsg) Diagnostics() (string, []viewer.Diagnostic, bool) {
	d, ok := e.Event.(DiagnosticsEvent)
	return d.Path, d.Diagnostics, ok
}

func (m Model) SourceLine(i int) string { return m.viewer.SourceLine(i) }

// AgentOpen shows path for an agent: 1-based line and col (0 = unset), and
// whole lines line..endLine selected when endLine >= line. The jump is undoable with ctrl+o.
func (m Model) AgentOpen(path string, line, col, endLine int) (Model, tea.Cmd) {
	if m.ShownFile() != "" {
		m.history.Push(m.viewer.Loc())
	}
	var spot *viewer.Spot
	if line > 0 {
		spot = &viewer.Spot{Line: line - 1, Col: max(col-1, 0), Top: -1}
		if viewer.IsMarkdown(path) {
			m.viewer = m.viewer.SetRawMarkdown(true)
		}
	}
	m, cmd := m.openAt(path, spot)
	if line > 0 && endLine >= line {
		m.viewer = m.viewer.SelectLinesWhenLoaded(line-1, endLine-1)
	}
	return m, cmd
}

// AgentReveal selects dir in the tree and shows its card.
func (m Model) AgentReveal(dir string) (Model, tea.Cmd) {
	if m.ShownFile() != "" {
		m.history.Push(m.viewer.Loc())
	}
	m.rememberSpot()
	m.tree.Reload()
	m.tree.Reveal(dir)
	m.setFocus(focusTree)
	return m, tea.Batch(m.syncSelection(), m.refreshBadges())
}
