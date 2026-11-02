package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is deck's global keymap. Pane-local movement (up/down/filter) is handled
// by the embedded Bubbles components; these are the cross-cutting bindings.
type KeyMap struct {
	Tab         key.Binding
	PrevTab     key.Binding
	Files       key.Binding
	Git         key.Binding
	Run         key.Binding
	HTTP        key.Binding
	AltFiles    key.Binding
	AltGit      key.Binding
	AltRun      key.Binding
	AltHTTP     key.Binding
	Quit        key.Binding
	ForceQuit   key.Binding
	Help        key.Binding
	HelpAlways  key.Binding
	ToggleMouse key.Binding
	Normal      key.Binding
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
	Run: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "run"),
	),
	HTTP: key.NewBinding(
		key.WithKeys("4"),
		key.WithHelp("4", "http"),
	),
	AltFiles: key.NewBinding(
		key.WithKeys("alt+1"),
		key.WithHelp("alt+1", "files (while typing)"),
	),
	AltGit: key.NewBinding(
		key.WithKeys("alt+2"),
		key.WithHelp("alt+2", "git (while typing)"),
	),
	AltRun: key.NewBinding(
		key.WithKeys("alt+3"),
		key.WithHelp("alt+3", "run (while typing)"),
	),
	AltHTTP: key.NewBinding(
		key.WithKeys("alt+4"),
		key.WithHelp("alt+4", "http (while typing)"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	ForceQuit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit (always)"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	HelpAlways: key.NewBinding(
		key.WithKeys("f1"),
		key.WithHelp("F1", "help (always)"),
	),
	ToggleMouse: key.NewBinding(
		key.WithKeys("alt+m"),
		key.WithHelp("alt+m", "toggle mouse"),
	),
	Normal: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "normal"),
	),
}

// GlobalBindings lists every app-level binding, for the help overlay.
func (k KeyMap) GlobalBindings() []key.Binding {
	return []key.Binding{
		k.Tab, k.PrevTab, k.Files, k.Git, k.Run, k.HTTP,
		k.AltFiles, k.AltGit, k.AltRun, k.AltHTTP,
		k.Help, k.HelpAlways, k.ToggleMouse, k.Quit, k.ForceQuit,
	}
}
