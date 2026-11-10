package files

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/ui"
)

// InvalidateMsg reports files changed outside deck, e.g. saved in the editor.
type InvalidateMsg struct{ Saved []string }

func (InvalidateMsg) RouteTo() ui.Dest { return ui.DestFiles }

func (m Model) invalidate(saved []string) (Model, tea.Cmd) {
	resync := reloadDocsCmd(m.nav, saved)
	if !m.viewer.Loaded() || !slices.Contains(saved, m.viewer.Path()) {
		return m, resync
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Reload()
	return m, tea.Batch(resync, cmd)
}
