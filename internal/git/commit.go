package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/ui"
)

const subjectLimit = 72

func subjectLen(text string) (int, bool) {
	subject, _, _ := strings.Cut(text, "\n")
	n := utf8.RuneCountInString(subject)
	return n, n > subjectLimit
}

func (p Pane) openCommit() (Pane, tea.Cmd) {
	if p.repo == nil {
		p.setStatus("not a git repository", true)
		return p, nil
	}
	p.committing = true
	p.focus = focusList
	if sectionCounts(p.items)[SectionStaged] == 0 {
		p.setStatus("nothing staged: "+ui.Alt("a")+" amends the last commit", false)
	}
	return p, p.box.Focus()
}

func (p Pane) commitKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	if p.confirming {
		return p.confirmKey(msg)
	}
	switch msg.String() {
	case "esc":
		if p.generating {
			p.stopGenerating()
			p.setStatus("commit message generation cancelled", false)
			return p, nil
		}
		return p.cancelCommit(), nil
	case "ctrl+g":
		return p.startGenerating()
	case "alt+a":
		return p.toggleAmend()
	case "ctrl+s":
		return p.submit()
	}
	var cmd tea.Cmd
	p.box, cmd = p.box.Update(msg)
	return p, cmd
}

func (p Pane) toggleAmend() (Pane, tea.Cmd) {
	p.amend = !p.amend
	if !p.amend || strings.TrimSpace(p.box.Value()) != "" || p.repo == nil {
		return p, nil
	}
	repo := p.repo
	return p, func() tea.Msg {
		text, err := repo.HeadMessage(context.Background())
		return headMessageMsg{text: text, err: err}
	}
}

func (p Pane) submit() (Pane, tea.Cmd) {
	text := strings.TrimSpace(p.box.Value())
	switch {
	case text == "":
		p.setStatus("empty commit message", true)
		return p, nil
	case !p.amend && sectionCounts(p.items)[SectionStaged] == 0:
		p.setStatus("nothing staged to commit", true)
		return p, nil
	}
	p.commitText = text
	if !p.opts.ConfirmCommit {
		return p.doCommit()
	}
	p.confirming = true
	*p.confirmVal = true
	p.box.Blur()
	p.confirm.Focus()
	return p, nil
}

func (p Pane) confirmKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "y":
		return p.doCommit()
	case "n", "esc":
		return p.backToEdit()
	case "enter":
		if v, ok := p.confirm.GetValue().(bool); ok && v {
			return p.doCommit()
		}
		return p.backToEdit()
	}
	model, cmd := p.confirm.Update(msg)
	p.confirm = model.(*huh.Confirm)
	return p, cmd
}

func (p Pane) backToEdit() (Pane, tea.Cmd) {
	p.confirming = false
	p.confirm.Blur()
	return p, p.box.Focus()
}

func (p Pane) doCommit() (Pane, tea.Cmd) {
	p.confirming = false
	p.confirm.Blur()
	if p.busy != "" || p.repo == nil {
		p.setStatus("wait for "+p.busyOp+" to finish", true)
		return p, nil
	}
	was := p.spinning()
	p.busy, p.busyOp = "committing…", "commit"
	repo, text, amend := p.repo, p.commitText, p.amend
	subject, _, _ := strings.Cut(text, "\n")
	cmd := func() tea.Msg {
		_, err := repo.Commit(context.Background(), text+"\n", amend)
		return commitDoneMsg{subject: subject, err: err}
	}
	return p, p.withSpin(was, cmd)
}

func (p Pane) commitDone(msg commitDoneMsg) (Pane, tea.Cmd) {
	if p.busyOp == "commit" {
		p.busy, p.busyOp = "", ""
	}
	if msg.err != nil {
		p.setStatus("commit failed: "+msg.err.Error(), true)
		return p, tea.Batch(p.box.Focus(), ui.Toast(ui.LevelError, p.status))
	}
	verb := "committed: "
	if p.amend {
		verb = "amended: "
	}
	p = p.cancelCommit()
	p.setStatus(verb+msg.subject, false)
	return p.reload()
}

func (p Pane) startGenerating() (Pane, tea.Cmd) {
	if p.generating {
		return p, nil
	}
	was := p.spinning()
	ctx, cancel := context.WithCancel(context.Background())
	*p.cancelGen = cancel
	p.generating = true
	p.setStatus("generating commit message…", false)
	repo := p.repo
	req := func() tea.Msg { return GenCommitMsgRequest{Ctx: ctx, Repo: repo} }
	return p, p.withSpin(was, req)
}

func (p *Pane) stopGenerating() {
	p.generating = false
	if p.cancelGen != nil && *p.cancelGen != nil {
		(*p.cancelGen)()
		*p.cancelGen = nil
	}
}

func (p Pane) commitMsgReady(msg CommitMsgReady) (Pane, tea.Cmd) {
	if errors.Is(msg.Err, context.Canceled) {
		return p, nil
	}
	p.stopGenerating()
	if msg.Err != nil {
		p.setStatus("AI commit message failed: "+msg.Err.Error(), true)
		return p, ui.Toast(ui.LevelError, p.status)
	}
	p.setStatus("", false)
	if p.committing {
		p.box.SetValue(strings.TrimSpace(msg.Text))
	}
	return p, nil
}

func (p Pane) cancelCommit() Pane {
	p.stopGenerating()
	p.committing = false
	p.confirming = false
	p.amend = false
	p.commitText = ""
	if !p.statusErr {
		p.setStatus("", false)
	}
	p.box.Reset()
	p.box.Blur()
	p.confirm.Blur()
	return p
}

func (p Pane) commitView() string {
	title := ui.Active.Render("commit")
	if p.amend {
		title += " " + lipgloss.NewStyle().Foreground(ui.ColorWarn).Bold(true).Render("amend")
	}
	n, over := subjectLen(p.box.Value())
	counter := ui.Muted.Render(fmt.Sprintf("%d/%d", n, subjectLimit))
	if over {
		counter = ui.Err.Render(fmt.Sprintf("%d/%d", n, subjectLimit))
	}
	title += "  " + counter
	if p.generating {
		title += "  " + p.spin.View() + "generating…"
	}
	hint := "ctrl+s commit · " + ui.Alt("a") + " amend · esc cancel"
	if p.opts.CommitAI {
		hint = "ctrl+s commit · " + ui.Alt("a") + " amend · ctrl+g AI · esc cancel"
	}
	parts := []string{title, p.box.View()}
	if p.confirming {
		parts = append(parts, p.confirm.View()+ui.Muted.Render("  y/n"))
	} else {
		parts = append(parts, ui.Muted.Render(hint))
	}
	return strings.Join(parts, "\n")
}
