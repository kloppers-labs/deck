// Command deck is a terminal command center: browse files, drive git, run tasks,
// and send HTTP requests, handing off to micro for editing.
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
