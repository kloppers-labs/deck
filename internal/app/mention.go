package app

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ide"
	"github.com/kloppers-labs/deck/internal/ui"
)

var clipboardOut io.Writer = os.Stdout

func mentionRef(root string, msg ui.MentionMsg) string {
	path := msg.Path
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		path = rel
	}
	ref := "@" + filepath.ToSlash(path) + "#L" + strconv.Itoa(msg.LineStart+1)
	if msg.LineEnd > msg.LineStart {
		ref += "-" + strconv.Itoa(msg.LineEnd+1)
	}
	return ref
}

// IDE is the Claude Code bridge as the app sees it.
type IDE interface {
	Mention(path string, lineStart, lineEnd int) bool
}

// WithIDE sends @ mentions to connected Claude sessions before falling back to the clipboard.
func (m Model) WithIDE(b IDE) Model {
	m.ide = b
	return m
}

func (m Model) mention(msg ui.MentionMsg) (Model, tea.Cmd) {
	ref := mentionRef(m.ed.cwd, msg)
	if m.ide != nil && m.claudeClients > 0 && m.ide.Mention(msg.Path, msg.LineStart, msg.LineEnd) {
		return m.addToast(ui.ToastMsg{Level: ui.LevelInfo, Text: "sent " + ref + " to Claude"})
	}
	return m, func() tea.Msg {
		if _, err := io.WriteString(clipboardOut, ansi.SetSystemClipboard(ref)); err != nil {
			return ui.ToastMsg{Level: ui.LevelError, Text: "copy: " + err.Error()}
		}
		return ui.ToastMsg{Level: ui.LevelInfo, Text: "copied " + ref}
	}
}

func (m Model) clientsChanged(msg ide.ClientsMsg) (Model, tea.Cmd) {
	was := m.claudeClients
	m.claudeClients = msg.N
	if was == 0 && msg.N > 0 {
		return m.addToast(ui.ToastMsg{Level: ui.LevelInfo, Text: "Claude connected"})
	}
	return m, nil
}

func (m Model) claudeBadge() string {
	switch {
	case m.claudeClients == 0:
		return ""
	case m.claudeClients == 1:
		return ui.Active.Render("✦ Claude")
	}
	return ui.Active.Render("✦ Claude ×" + strconv.Itoa(m.claudeClients))
}
