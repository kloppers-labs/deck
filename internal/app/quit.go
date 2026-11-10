package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/ui"
)

const stopTimeout = 5 * time.Second

type quitState struct {
	asking   bool
	stopping bool
}

// StopAll stops every running task and waits up to timeout for them to exit.
func (m Model) StopAll(timeout time.Duration) bool { return m.run.StopAll(timeout) }

// requestQuit quits at once when nothing is running; otherwise it asks, and a
// second ctrl+c forces the quit.
func (m *Model) requestQuit(force bool) tea.Cmd {
	if m.run.RunningCount() == 0 || (force && (m.quit.asking || m.quit.stopping)) {
		return tea.Quit
	}
	m.quit.asking = true
	return nil
}

func (m *Model) quitKey(msg tea.KeyMsg) tea.Cmd {
	if m.quit.stopping {
		return nil
	}
	if s := msg.String(); s == "y" || s == "Y" {
		m.quit.asking = false
		m.quit.stopping = true
		return m.run.StopAllCmd(stopTimeout, tea.QuitMsg{})
	}
	m.quit.asking = false
	return nil
}

func (m Model) quitPrompt() string {
	names := m.run.RunningNames()
	if m.quit.stopping {
		return fmt.Sprintf("stopping %s…", tasksPhrase(len(names)))
	}
	return fmt.Sprintf("%s running (%s): quit and stop them? [y/N]", tasksPhrase(len(names)), strings.Join(names, ", "))
}

func tasksPhrase(n int) string {
	if n == 1 {
		return "1 task"
	}
	return fmt.Sprintf("%d tasks", n)
}

func (m Model) quitOverlay(body string) string {
	w := min(lipgloss.Width(m.quitPrompt())+4, m.width-4)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorWarn).
		Padding(0, 1).
		Render(ui.Fit(m.quitPrompt(), w-4, 1))
	x := max((m.width-w)/2, 0)
	y := max((m.bodyHeight()-3)/2, 0)
	return ui.Overlay(ui.Dimmed(body), box, x, y)
}
