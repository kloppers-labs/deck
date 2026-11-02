package git

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/ui"
)

const logLimit = 200

// LogEntry is one commit of the log list.
type LogEntry struct {
	Hash    string
	Age     string
	Author  string
	Refs    string
	Subject string
}

func (e LogEntry) FilterValue() string { return e.Subject }

func parseLog(out string) []LogEntry {
	var entries []LogEntry
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.SplitN(line, "\x00", 5)
		if len(f) != 5 {
			continue
		}
		entries = append(entries, LogEntry{Hash: f[0], Age: f[1], Author: f[2], Refs: f[3], Subject: f[4]})
	}
	return entries
}

func (r *Repo) Log(ctx context.Context, n int) ([]LogEntry, error) {
	out, err := r.git(ctx, "log", "--format=%h%x00%ar%x00%an%x00%D%x00%s", "-n", strconv.Itoa(n))
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

func (r *Repo) Show(ctx context.Context, hash string) (string, error) {
	return r.git(ctx, "show", "--no-color", "--no-ext-diff", "--stat", "-p", hash)
}

type logDelegate struct{}

func (logDelegate) Height() int                         { return 1 }
func (logDelegate) Spacing() int                        { return 0 }
func (logDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (logDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	e, ok := item.(LogEntry)
	if !ok {
		return
	}
	hash := lipgloss.NewStyle().Foreground(ui.ColorAccent).Render(e.Hash)
	line := hash + " " + e.Subject
	if e.Refs != "" {
		line += lipgloss.NewStyle().Foreground(ui.ColorString).Render(" (" + e.Refs + ")")
	}
	line += ui.Muted.Render(" · " + e.Age + " · " + e.Author)
	prefix := "  "
	if index == m.Index() {
		prefix = ui.Active.Render("› ")
	}
	fmt.Fprint(w, ui.FitWidth(prefix+line, m.Width(), "…"))
}

func newLogList() list.Model {
	l := list.New(nil, logDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.KeyMap.Quit = key.Binding{}
	l.KeyMap.ForceQuit = key.Binding{}
	l.KeyMap.ShowFullHelp = key.Binding{}
	l.KeyMap.CloseFullHelp = key.Binding{}
	return l
}

func (p Pane) openLog() (Pane, tea.Cmd) {
	if p.repo == nil {
		p.setStatus("not a git repository", true)
		return p, nil
	}
	p.mode, p.focus, p.logLoaded = modeLog, focusList, false
	p.rawDiff, p.shownKey = "", diffKey{}
	p.diff.SetContent(ui.Muted.Render("enter shows the selected commit"))
	repo := p.repo
	return p, func() tea.Msg {
		entries, err := repo.Log(context.Background(), logLimit)
		return logLoadedMsg{entries: entries, err: err}
	}
}

func (p Pane) applyLog(msg logLoadedMsg) Pane {
	if p.mode != modeLog {
		return p
	}
	if msg.err != nil {
		p.setStatus("log: "+msg.err.Error(), true)
		return p
	}
	items := make([]list.Item, len(msg.entries))
	for i, e := range msg.entries {
		items[i] = e
	}
	p.logList.SetItems(items)
	p.logList.Select(0)
	p.logLoaded = true
	return p
}

func (p *Pane) logStep(delta int) {
	if delta > 0 {
		p.logList.CursorDown()
	} else {
		p.logList.CursorUp()
	}
}

func (p Pane) logKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		p.logStep(1)
	case "k", "up":
		p.logStep(-1)
	case "g", "home":
		p.logList.Select(0)
	case "G", "end":
		p.logList.Select(max(len(p.logList.Items())-1, 0))
	case "enter", "right":
		e, ok := p.logList.SelectedItem().(LogEntry)
		if !ok || p.repo == nil {
			return p, nil
		}
		p.focus = focusDiff
		p.rawDiff, p.shownKey = "", diffKey{}
		p.diff.SetContent(ui.Muted.Render("loading " + e.Hash + "…"))
		repo, hash := p.repo, e.Hash
		return p, func() tea.Msg {
			text, err := repo.Show(context.Background(), hash)
			return showLoadedMsg{hash: hash, text: text, err: err}
		}
	case "esc", "l", "h", "left":
		p.mode = modeChanges
		return p.requestDiff()
	case "r":
		return p.openLog()
	}
	return p, nil
}

func (p Pane) showLoaded(msg showLoadedMsg) Pane {
	if p.mode != modeLog {
		return p
	}
	if msg.err != nil {
		p.diff.SetContent(ui.Err.Render(msg.err.Error()))
		return p
	}
	p.diff.SetContent(highlightDiff(p.opts.Theme, capDiff(msg.text, diffCap)))
	p.diff.GotoTop()
	return p
}
