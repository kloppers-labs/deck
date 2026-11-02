package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Level int

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
)

// ToastMsg asks the root model to show a transient notice in the status bar.
// A zero TTL uses a default that depends on Level.
type ToastMsg struct {
	Level Level
	Text  string
	TTL   time.Duration
}

func (ToastMsg) RouteTo() Dest { return DestApp }

// Toast returns a command that raises a toast.
func Toast(level Level, text string) tea.Cmd {
	return func() tea.Msg { return ToastMsg{Level: level, Text: text} }
}

// Lifetime is how long the toast stays visible.
func (t ToastMsg) Lifetime() time.Duration {
	if t.TTL > 0 {
		return t.TTL
	}
	switch t.Level {
	case LevelError:
		return 8 * time.Second
	case LevelWarn:
		return 6 * time.Second
	}
	return 4 * time.Second
}

func (t ToastMsg) Render() string {
	switch t.Level {
	case LevelError:
		return lipgloss.NewStyle().Foreground(ColorError).Render("✗ " + t.Text)
	case LevelWarn:
		return lipgloss.NewStyle().Foreground(ColorWarn).Render("! " + t.Text)
	}
	return lipgloss.NewStyle().Foreground(ColorInfo).Render(t.Text)
}
