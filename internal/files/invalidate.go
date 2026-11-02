package files

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
)

// InvalidateMsg reports files changed outside deck, e.g. saved in the editor.
type InvalidateMsg struct{ Saved []string }

func (InvalidateMsg) RouteTo() ui.Dest { return ui.DestFiles }

func (m Model) invalidate(saved []string) (Model, tea.Cmd) {
	if !m.viewer.Loaded() || !slices.Contains(saved, m.viewer.Path()) {
		return m, nil
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Reload()
	return m, cmd
}
