package git

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/ui"
)

type branchItem string

func (b branchItem) FilterValue() string { return string(b) }

type branchDelegate struct{ current string }

func (branchDelegate) Height() int                         { return 1 }
func (branchDelegate) Spacing() int                        { return 0 }
func (branchDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d branchDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	b, ok := item.(branchItem)
	if !ok {
		return
	}
	name := string(b)
	if name == d.current {
		name += lipgloss.NewStyle().Foreground(ui.ColorString).Render(" ✓")
	}
	if index == m.Index() {
		fmt.Fprint(w, ui.FitWidth(ui.Active.Render("› "+name), m.Width(), "…"))
		return
	}
	fmt.Fprint(w, ui.FitWidth("  "+name, m.Width(), "…"))
}

func newBranchList() list.Model {
	l := list.New(nil, branchDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.KeyMap.Quit = key.Binding{}
	l.KeyMap.ForceQuit = key.Binding{}
	return l
}

func (p Pane) openBranches() (Pane, tea.Cmd) {
	if p.repo == nil {
		p.setStatus("not a git repository", true)
		return p, nil
	}
	p.branching, p.branchesLoaded, p.naming = true, false, false
	p.branchList.ResetFilter()
	p.branchList.SetItems(nil)
	repo := p.repo
	return p, func() tea.Msg {
		names, err := repo.Branches(context.Background())
		return branchesLoadedMsg{names: names, err: err}
	}
}

func (p Pane) applyBranches(msg branchesLoadedMsg) Pane {
	if !p.branching {
		return p
	}
	if msg.err != nil {
		p.branching = false
		p.setStatus("branches: "+msg.err.Error(), true)
		return p
	}
	items := make([]list.Item, len(msg.names))
	current := 0
	for i, n := range msg.names {
		items[i] = branchItem(n)
		if n == p.snap.Branch {
			current = i
		}
	}
	p.branchList.SetDelegate(branchDelegate{current: p.snap.Branch})
	p.branchList.SetItems(items)
	p.branchList.Select(current)
	p.branchesLoaded = true
	return p
}

func (p Pane) branchKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	if p.naming {
		return p.nameKey(msg)
	}
	if p.branchList.FilterState() != list.Filtering {
		switch msg.String() {
		case "esc":
			if p.branchList.FilterState() == list.FilterApplied {
				p.branchList.ResetFilter()
				return p, nil
			}
			p.branching = false
			return p, nil
		case "n":
			p.naming = true
			p.nameInput.Reset()
			return p, p.nameInput.Focus()
		case "enter":
			return p.switchSelected()
		case "D":
			return p.askDeleteBranch(), nil
		}
	}
	var cmd tea.Cmd
	p.branchList, cmd = p.branchList.Update(msg)
	return p, cmd
}

func (p Pane) selectedBranch() (string, bool) {
	b, ok := p.branchList.SelectedItem().(branchItem)
	return string(b), ok
}

func (p Pane) switchSelected() (Pane, tea.Cmd) {
	name, ok := p.selectedBranch()
	if !ok {
		return p, nil
	}
	p.branching = false
	if name == p.snap.Branch {
		p.setStatus("already on "+name, false)
		return p, nil
	}
	return p.startBusy("switch "+name, "switching to "+name+"…", func(ctx context.Context, r *Repo) error {
		return r.Switch(ctx, name)
	})
}

func (p Pane) nameKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.naming = false
		p.nameInput.Blur()
		return p, nil
	case "enter":
		name := strings.TrimSpace(p.nameInput.Value())
		p.naming = false
		p.nameInput.Blur()
		if name == "" {
			return p, nil
		}
		p.branching = false
		return p.startBusy("create "+name, "creating "+name+"…", func(ctx context.Context, r *Repo) error {
			return r.CreateBranch(ctx, name)
		})
	}
	var cmd tea.Cmd
	p.nameInput, cmd = p.nameInput.Update(msg)
	return p, cmd
}

func (p Pane) askDeleteBranch() Pane {
	name, ok := p.selectedBranch()
	if !ok {
		return p
	}
	p.pending = &confirmPrompt{
		text: "delete branch " + name + "? y/n",
		run: func(p Pane) (Pane, tea.Cmd) {
			p.branching = false
			return p.startBusy("delete "+name, "deleting "+name+"…", func(ctx context.Context, r *Repo) error {
				return r.DeleteBranch(ctx, name)
			})
		},
	}
	return p
}

func (p Pane) branchView() string {
	title := ui.Active.Render("branches")
	var second string
	switch {
	case p.naming:
		second = p.nameInput.View()
	case !p.branchesLoaded:
		second = ui.Muted.Render("loading…")
	default:
		second = ui.Muted.Render("enter switch · n new · D delete · / filter · esc close")
	}
	return title + "\n" + second + "\n" + p.branchList.View()
}
