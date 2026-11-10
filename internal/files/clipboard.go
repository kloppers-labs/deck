package files

import (
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

var clipboardOut io.Writer = os.Stdout

// copyCmd puts text on the system clipboard through OSC52 and toasts label.
func copyCmd(text, label string) tea.Cmd {
	return func() tea.Msg {
		if _, err := io.WriteString(clipboardOut, ansi.SetSystemClipboard(text)); err != nil {
			return ui.ToastMsg{Level: ui.LevelError, Text: "copy: " + err.Error()}
		}
		return ui.ToastMsg{Level: ui.LevelInfo, Text: "copied " + ui.FitWidth(label, 60, "…")}
	}
}
