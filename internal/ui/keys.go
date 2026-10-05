package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is deck's global keymap. Pane-local movement (up/down/filter) is handled
// by the embedded Bubbles components; these are the cross-cutting bindings.
type KeyMap struct {
	Tab       key.Binding
	PrevTab   key.Binding
	Files     key.Binding
	Git       key.Binding
	AI        key.Binding
	Run       key.Binding
	HTTP      key.Binding
	Quit      key.Binding
	ForceQuit key.Binding
	Help      key.Binding
}

// Keys is the shared binding set.
var Keys = KeyMap{
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next tab"),
	),
	PrevTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev tab"),
	),
	Files: key.NewBinding(
		key.WithKeys("1"),
		key.WithHelp("1", "files"),
	),
	Git: key.NewBinding(
		key.WithKeys("2"),
		key.WithHelp("2", "git"),
	),
	AI: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "ai"),
	),
	Run: key.NewBinding(
		key.WithKeys("4"),
		key.WithHelp("4", "run"),
	),
	HTTP: key.NewBinding(
		key.WithKeys("5"),
		key.WithHelp("5", "http"),
	),
	// Quit also accepts ctrl+c so the help line reads "q", but ctrl+c is matched
	// separately via ForceQuit so it can bypass the capturing gate.
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	ForceQuit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}

// NavBindings groups the tab-navigation bindings for the full help view.
func (k KeyMap) NavBindings() []key.Binding {
	return []key.Binding{k.Tab, k.PrevTab, k.Files, k.Git, k.AI, k.Run, k.HTTP}
}

// GlobalBindings groups the always-available bindings shown in the footer and
// the full help view.
func (k KeyMap) GlobalBindings() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}
