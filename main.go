// Command deck is a terminal command center: browse files, drive git, and ask
// AI — handing off to micro for editing. It reuses the user's authenticated
// claude/codex/mods CLIs rather than calling any API directly.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/app"
	"deck/internal/config"
)

func main() {
	cfg := config.Load()
	p := tea.NewProgram(app.New(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "deck:", err)
		os.Exit(1)
	}
}
