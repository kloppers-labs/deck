package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/ui"
)

func (m Model) control(msg control.RequestMsg) (Model, tea.Cmd) {
	if !msg.Deadline.IsZero() && time.Now().After(msg.Deadline) {
		return m, nil
	}
	req := msg.Req
	if req.Cmd == "open" {
		if _, err := os.Stat(req.Path); err != nil {
			msg.Answer(control.Reply{Error: "no such file: " + req.Path})
			return m, nil
		}
	}
	if m.capturing() {
		m.held = &req
		msg.Answer(control.Reply{OK: true, Message: control.HeldMessage})
		return m, nil
	}
	m, cmd := m.applyRequest(req)
	msg.Answer(control.Reply{OK: true, Message: m.describe(req)})
	return m, cmd
}

func (m Model) applyRequest(req control.Request) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch req.Cmd {
	case "open":
		m.active = tabFiles
		if fi, err := os.Stat(req.Path); err == nil && fi.IsDir() {
			m.files, cmd = m.files.AgentReveal(req.Path)
		} else {
			m.files, cmd = m.files.AgentOpen(req.Path, req.Line, req.Col, req.EndLine)
		}
	case "review", "diff":
		m.active = tabGit
		review := git.ReviewMsg{Base: req.Base, Uncommitted: req.Uncommitted, Path: req.Path, Line: req.Line}
		cmd = func() tea.Msg { return review }
	default:
		return m, nil
	}
	toast := ui.ToastMsg{Level: ui.LevelInfo, Text: from(req) + " → " + m.describe(req)}
	next, t := m.addToast(toast)
	return next, tea.Batch(cmd, t)
}

func (m Model) applyHeld() (Model, tea.Cmd) {
	if m.held == nil {
		return m, nil
	}
	req := *m.held
	m.held = nil
	return m.applyRequest(req)
}

func from(req control.Request) string {
	if req.From == "" {
		return "agent"
	}
	return req.From
}

func (m Model) describe(req control.Request) string {
	target := ""
	if req.Path != "" {
		target = m.relPath(req.Path)
		if fi, err := os.Stat(req.Path); err == nil && fi.IsDir() {
			target += "/"
		} else if req.Line > 0 {
			target += ":" + strconv.Itoa(req.Line)
			if req.EndLine > req.Line {
				target += "-" + strconv.Itoa(req.EndLine)
			}
		}
	}
	switch req.Cmd {
	case "review":
		return "review"
	case "diff":
		return strings.TrimSpace("diff " + target)
	}
	return target
}

func (m Model) relPath(path string) string {
	if rel, err := filepath.Rel(m.files.Root(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func (m Model) heldStatus() string {
	if m.held == nil {
		return ""
	}
	text := from(*m.held) + ": " + m.describe(*m.held) + " · " + ui.Keys.AgentJump.Help().Key
	return lipgloss.NewStyle().Foreground(ui.ColorWarn).Render(text)
}

func touchCmd(dir string) tea.Cmd {
	pid := os.Getpid()
	return func() tea.Msg {
		control.Touch(dir, pid, time.Now())
		return nil
	}
}
