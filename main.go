// Command deck is a terminal command center: browse files, drive git, run tasks,
// and send HTTP requests, handing off to micro for editing.
package main

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/app"
	"deck/internal/config"
	"deck/internal/files"
	"deck/internal/lsp"
)

const (
	stopTimeout        = 5 * time.Second
	lspShutdownTimeout = 3 * time.Second
)

func main() { os.Exit(run()) }

func run() int {
	cfg, warnings := config.Load()
	mgr := lsp.NewManager(app.LSPConfig(cfg.LSP))
	defer app.ShutdownLSP(mgr, lspShutdownTimeout)
	var nav app.LSP
	if cfg.LSP.Enabled {
		nav = files.NewLSPAdapter(mgr)
	}
	p := tea.NewProgram(app.New(cfg, warnings, nav), programOptions(cfg.Mouse)...)
	final, err := p.Run()
	if m, ok := final.(app.Model); ok {
		m.StopAll(stopTimeout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "deck:", err)
		return 1
	}
	return 0
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
