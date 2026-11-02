package ui

import tea "github.com/charmbracelet/bubbletea"

// FocusTabMsg asks the root model to switch to the tab that owns Dest.
type FocusTabMsg struct{ Dest Dest }

func (FocusTabMsg) RouteTo() Dest { return DestApp }

func FocusTab(d Dest) tea.Cmd { return func() tea.Msg { return FocusTabMsg{Dest: d} } }
