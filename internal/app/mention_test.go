package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ide"
	"github.com/kloppers-labs/deck/internal/ui"
)

func TestMentionFallsBackToClipboard(t *testing.T) {
	var buf bytes.Buffer
	prev := clipboardOut
	clipboardOut = &buf
	t.Cleanup(func() { clipboardOut = prev })

	m := sized(t, 120, 40)
	next, cmd := m.Update(ui.MentionMsg{Path: filepath.Join(m.ed.cwd, "a.go"), LineStart: 39, LineEnd: 51})
	m = next.(Model)
	for _, msg := range collect(cmd) {
		m = update(t, m, msg)
	}
	if buf.String() != ansi.SetSystemClipboard("@a.go#L40-52") {
		t.Errorf("clipboard = %q, want OSC52 of @a.go#L40-52", buf.String())
	}
	if !strings.Contains(m.View(), "copied @a.go#L40-52") {
		t.Error("the fallback should confirm with a toast")
	}
}

type fakeIDE struct{ got []ui.MentionMsg }

func (f *fakeIDE) Mention(path string, start, end int) bool {
	f.got = append(f.got, ui.MentionMsg{Path: path, LineStart: start, LineEnd: end})
	return true
}

func TestMentionUsesBridgeWhenConnected(t *testing.T) {
	var buf bytes.Buffer
	prev := clipboardOut
	clipboardOut = &buf
	t.Cleanup(func() { clipboardOut = prev })

	f := &fakeIDE{}
	m := sized(t, 120, 40).WithIDE(f)
	m = drive(t, m, ide.ClientsMsg{N: 1}, ui.MentionMsg{Path: filepath.Join(m.ed.cwd, "a.go"), LineStart: 39, LineEnd: 51})
	if len(f.got) != 1 || f.got[0].LineStart != 39 {
		t.Errorf("bridge got %+v", f.got)
	}
	if buf.Len() != 0 {
		t.Error("a connected Claude should not need the clipboard")
	}
	if !strings.Contains(m.View(), "sent @a.go#L40-52 to Claude") {
		t.Error("sending should confirm with a toast")
	}
}

func TestClaudeBadgeAndToast(t *testing.T) {
	m := sized(t, 120, 40).WithIDE(&fakeIDE{})
	m = drive(t, m, ide.ClientsMsg{N: 1})
	if v := m.View(); !strings.Contains(v, "✦ Claude") || !strings.Contains(v, "Claude connected") {
		t.Errorf("a connection should show the badge and a toast")
	}
	m = drive(t, m, ide.ClientsMsg{N: 2})
	if !strings.Contains(m.View(), "✦ Claude ×2") {
		t.Error("two sessions should show ×2")
	}
	m = drive(t, m, ide.ClientsMsg{N: 0})
	if strings.Contains(m.statusBar(), "✦") {
		t.Error("the badge should go when Claude disconnects")
	}
}

func TestClaudeBadgeFitsFrame(t *testing.T) {
	cfg := longNameFixture(t)
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 40}, {200, 50}} {
		w, h := size[0], size[1]
		m := update(t, New(cfg, nil, nil), tea.WindowSizeMsg{Width: w, Height: h})
		m.claudeClients = 3
		view := m.View()
		if got := lipgloss.Height(view); got != h {
			t.Errorf("%dx%d: frame height = %d", w, h, got)
		}
		if got := lipgloss.Width(view); got > w {
			t.Errorf("%dx%d: frame width = %d", w, h, got)
		}
	}
}
