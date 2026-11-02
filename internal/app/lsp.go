package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/config"
	"deck/internal/files"
	"deck/internal/lsp"
	"deck/internal/ui"
	"deck/internal/viewer"
)

// LSP is the language-server layer as the root model sees it.
type LSP interface {
	files.LSP
	Prestart(ctx context.Context, cwd string) error
	Shutdown(ctx context.Context) error
}

// LSPConfig turns the [lsp] config section into the lsp package's settings.
func LSPConfig(c config.LSP) lsp.Config {
	out := lsp.Config{
		Enabled:        c.Enabled,
		Prestart:       c.Prestart,
		RequestTimeout: time.Duration(c.RequestTimeoutMS) * time.Millisecond,
		Servers:        map[string]lsp.ServerOverride{},
	}
	for name, s := range c.Servers {
		out.Servers[name] = lsp.ServerOverride{
			Command: s.Command, RootMarkers: s.RootMarkers, Solution: s.Solution, Enabled: s.Enabled,
		}
	}
	return out
}

// ShutdownLSP stops every language server, waiting at most timeout.
func ShutdownLSP(l interface{ Shutdown(context.Context) error }, timeout time.Duration) error {
	if l == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return l.Shutdown(ctx)
}

func prestartCmd(l LSP, cwd string) tea.Cmd {
	if l == nil {
		return nil
	}
	return func() tea.Msg {
		_ = l.Prestart(context.Background(), cwd)
		return nil
	}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// lspBadge renders one server for the status bar, or "" when it has nothing to say.
func lspBadge(st viewer.ServerStatus, now time.Time) string {
	switch st.State {
	case viewer.ServerReady:
		return lipgloss.NewStyle().Foreground(ui.ColorString).Render(st.Label + " ●")
	case viewer.ServerFailed, viewer.ServerExited:
		return ui.Err.Render(st.Label + " ✗")
	case viewer.ServerStarting, viewer.ServerIndexing:
	default:
		return ""
	}
	frame := spinFrames[int(now.UnixMilli()/100)%len(spinFrames)]
	parts := []string{st.Label, frame}
	switch {
	case st.Title != "":
		parts = append(parts, st.Title)
	case st.Project != "":
		parts = append(parts, "loading "+st.Project)
	}
	if st.Percent >= 0 {
		parts = append(parts, fmt.Sprintf("%d%%", st.Percent))
	}
	if !st.Since.IsZero() {
		parts = append(parts, fmt.Sprintf("(%ds)", int(now.Sub(st.Since).Seconds())))
	}
	return lipgloss.NewStyle().Foreground(ui.ColorWarn).Render(strings.Join(parts, " "))
}

func lspBadges(list []viewer.ServerStatus, now time.Time) []string {
	var out []string
	for _, st := range list {
		if b := lspBadge(st, now); b != "" {
			out = append(out, b)
		}
	}
	return out
}
