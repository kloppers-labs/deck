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
	cfg, warnings := config.Load()
	p := tea.NewProgram(app.New(cfg, warnings), programOptions(cfg.Mouse)...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "deck:", err)
		os.Exit(1)
	}
}

func programOptions(mouse string) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus()}
	switch mouse {
	case "hover":
		opts = append(opts, tea.WithMouseAllMotion())
	case "click":
		opts = append(opts, tea.WithMouseCellMotion())
	}
	return opts
}
