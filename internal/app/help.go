package app

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/ui"
)

const (
	helpMaxWidth = 72
	helpKeyWidth = 16
)

type helpState struct {
	open      bool
	filtering bool
	scroll    int
	filter    textinput.Model
}

func newHelpState() helpState {
	f := textinput.New()
	f.Prompt = "/"
	f.Placeholder = "filter"
	return helpState{filter: f}
}

var helpFooter = []key.Binding{
	key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")),
	key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
}

var mouseHelp = []key.Binding{
	key.NewBinding(key.WithHelp("click tab", "switch tab")),
	key.NewBinding(key.WithHelp("click", "select / focus")),
	key.NewBinding(key.WithHelp("double-click", "open")),
	key.NewBinding(key.WithHelp("wheel", "scroll what is under the pointer")),
	key.NewBinding(key.WithHelp("shift+drag", "select text (option+drag in iTerm2)")),
	key.NewBinding(key.WithHelp("alt+m", "toggle mouse capture")),
}

func (m *Model) openHelp() {
	m.help.open = true
	m.help.scroll = 0
	m.help.filtering = false
	m.help.filter.Reset()
	m.help.filter.Blur()
}

func (m *Model) helpKey(msg tea.KeyMsg) tea.Cmd {
	h := &m.help
	if h.filtering {
		switch msg.String() {
		case "esc":
			h.filtering = false
			h.filter.Reset()
			h.filter.Blur()
		case "enter":
			h.filtering = false
			h.filter.Blur()
		default:
			var cmd tea.Cmd
			h.filter, cmd = h.filter.Update(msg)
			h.scroll = 0
			return cmd
		}
		return nil
	}
	switch msg.String() {
	case "esc", "?", "f1", "q":
		if h.filter.Value() != "" && msg.String() == "esc" {
			h.filter.Reset()
			return nil
		}
		h.open = false
	case "/":
		h.filtering = true
		return h.filter.Focus()
	case "j", "down":
		m.scrollHelp(1)
	case "k", "up":
		m.scrollHelp(-1)
	case "g", "home":
		h.scroll = 0
	case "G", "end":
		m.scrollHelp(len(m.helpLines()))
	}
	return nil
}

func (m *Model) scrollHelp(delta int) {
	maxScroll := max(len(m.helpLines())-m.helpContentRows(), 0)
	m.help.scroll = min(max(m.help.scroll+delta, 0), maxScroll)
}

func (m Model) helpSections() []struct {
	title    string
	bindings []key.Binding
} {
	return []struct {
		title    string
		bindings []key.Binding
	}{
		{"Global", ui.Keys.GlobalBindings()},
		{tabNames[m.active], m.paneBindings()},
		{"Mouse", mouseHelp},
	}
}

func (m Model) helpLines() []string {
	query := strings.ToLower(m.help.filter.Value())
	var lines []string
	for _, s := range m.helpSections() {
		var rows []string
		for _, b := range s.bindings {
			k, d := b.Help().Key, b.Help().Desc
			if k == "" || !strings.Contains(strings.ToLower(k+" "+d), query) {
				continue
			}
			pad := strings.Repeat(" ", max(helpKeyWidth-ansi.StringWidth(k), 1))
			rows = append(rows, "  "+ui.Active.Render(k)+pad+ui.Muted.Render(d))
		}
		if len(rows) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, ui.Title.Render(s.title))
		lines = append(lines, rows...)
	}
	if len(lines) == 0 {
		lines = []string{ui.Muted.Render("no matching keys")}
	}
	return lines
}

func (m Model) helpBox() (w, h int) {
	w = min(helpMaxWidth, m.width-4)
	h = min(len(m.helpLines())+4, m.bodyHeight())
	return w, max(h, 5)
}

func (m Model) helpContentRows() int {
	_, h := m.helpBox()
	return max(h-4, 1)
}

func (m Model) helpOverlay(body string) string {
	w, h := m.helpBox()
	innerW := w - 4
	lines := m.helpLines()
	start := min(m.help.scroll, max(len(lines)-m.helpContentRows(), 0))
	end := min(start+m.helpContentRows(), len(lines))

	head := "help"
	switch {
	case m.help.filtering || m.help.filter.Value() != "":
		head = m.help.filter.View()
	case len(lines) > m.helpContentRows():
		head += ui.Muted.Render("  j/k scroll · / filter · esc close")
	default:
		head += ui.Muted.Render("  / filter · esc close")
	}
	content := ui.FitWidth(head, innerW, "…") + "\n" + strings.Join(lines[start:end], "\n")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorAccent).
		Padding(0, 1).
		Render(ui.Fit(content, innerW, h-2))
	x := (m.width - w) / 2
	y := max((m.bodyHeight()-h)/2, 0)
	return ui.Overlay(ui.Dimmed(body), box, x, y)
}
