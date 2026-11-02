package files

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
	"deck/internal/viewer"
)

const (
	lspTickEvery  = 500 * time.Millisecond
	lspLogMax     = 500
	reloadTimeout = 5 * time.Second
)

// LSP is what the Files tab needs from the language-server layer.
type LSP interface {
	viewer.Navigator
	Reload(ctx context.Context, path string) error
	NextEvent() (any, bool)
	Logs() []string
}

// DiagnosticsEvent replaces the diagnostics of Path.
type DiagnosticsEvent struct {
	Path        string
	Diagnostics []viewer.Diagnostic
}

// LogEvent is a line from a server's log.
type LogEvent struct{ Server, Text string }

// MessageEvent is a message a server wants shown to the user.
type MessageEvent struct {
	Server string
	Level  ui.Level
	Text   string
}

// LSPEventMsg carries one event from the language servers: a
// viewer.ServerStatus, DiagnosticsEvent, LogEvent or MessageEvent.
type LSPEventMsg struct{ Event any }

type lspTickMsg struct{}

func (LSPEventMsg) RouteTo() ui.Dest { return ui.DestFiles }
func (lspTickMsg) RouteTo() ui.Dest  { return ui.DestFiles }

// ListenLSP waits for the next language-server event; the Files tab re-arms it
// after each one, and it stops when the servers shut down.
func ListenLSP(l LSP) tea.Cmd {
	if l == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := l.NextEvent()
		if !ok {
			return nil
		}
		return LSPEventMsg{Event: ev}
	}
}

// LSPStatus lists the latest state of every server deck has heard from.
func (m Model) LSPStatus() []viewer.ServerStatus { return m.lspStatus }

func (m Model) lspEvent(msg LSPEventMsg) (Model, tea.Cmd) {
	next := ListenLSP(m.nav)
	switch ev := msg.Event.(type) {
	case viewer.ServerStatus:
		m.lspStatus = upsertStatus(m.lspStatus, ev)
		return m, tea.Batch(next, m.startLSPTick())
	case DiagnosticsEvent:
		m.viewer.SetDiagnostics(ev.Path, ev.Diagnostics)
	case LogEvent:
		m.appendLog("[" + ev.Server + "] " + ev.Text)
	case MessageEvent:
		m.appendLog("[" + ev.Server + "] " + ev.Text)
		return m, tea.Batch(next, ui.Toast(ev.Level, ev.Server+": "+ev.Text))
	}
	return m, next
}

func upsertStatus(list []viewer.ServerStatus, st viewer.ServerStatus) []viewer.ServerStatus {
	out := make([]viewer.ServerStatus, 0, len(list)+1)
	replaced := false
	for _, s := range list {
		if s.Server == st.Server && s.Project == st.Project {
			s, replaced = st, true
		}
		out = append(out, s)
	}
	if !replaced {
		out = append(out, st)
	}
	return out
}

func (m *Model) appendLog(line string) {
	m.lspLog = append(m.lspLog, line)
	if over := len(m.lspLog) - lspLogMax; over > 0 {
		m.lspLog = append([]string(nil), m.lspLog[over:]...)
	}
}

func (m Model) lspLoading() bool {
	for _, s := range m.lspStatus {
		if s.State == viewer.ServerStarting || s.State == viewer.ServerIndexing {
			return true
		}
	}
	return false
}

// startLSPTick keeps the status bar's elapsed time moving while a server loads.
func (m *Model) startLSPTick() tea.Cmd {
	if m.lspTicking || !m.lspLoading() {
		return nil
	}
	m.lspTicking = true
	return tea.Tick(lspTickEvery, func(time.Time) tea.Msg { return lspTickMsg{} })
}

func (m Model) lspTick() (Model, tea.Cmd) {
	m.lspTicking = false
	return m, m.startLSPTick()
}

func reloadDocsCmd(l LSP, paths []string) tea.Cmd {
	if l == nil || len(paths) == 0 {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reloadTimeout)
		defer cancel()
		for _, p := range paths {
			_ = l.Reload(ctx, p)
		}
		return nil
	}
}
