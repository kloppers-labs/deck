package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStyleLogKeepsContent(t *testing.T) {
	in := "abc1234 (HEAD -> main, origin/main) fix things\ndef5678 add stuff\n"
	out := styleLog(in)
	for _, want := range []string{"abc1234", "fix things", "def5678", "add stuff", "(HEAD -> main, origin/main)"} {
		if !strings.Contains(out, want) {
			t.Errorf("styled log lost %q in %q", want, out)
		}
	}
	if got := strings.Count(out, "\n"); got != 2 {
		t.Errorf("styled log has %d newlines, want 2", got)
	}
}

func TestBranchPickerCaptures(t *testing.T) {
	p := NewPane("dracula", true)
	if p.Capturing() {
		t.Fatal("fresh pane should not capture")
	}
	p.branching = true
	if !p.Capturing() {
		t.Error("open branch picker should capture the keyboard")
	}
}

func committingPane(t *testing.T, commitAI bool) Pane {
	t.Helper()
	p := NewPane("dracula", commitAI)
	p.SetSize(120, 30)
	p.committing = true
	p.commitBox.Focus()
	return p
}

func ctrlG() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlG} }

func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collect(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestCommitMsgErrorKeepsTypedText(t *testing.T) {
	p := committingPane(t, true)
	p.commitBox.SetValue("wip")
	p, _ = p.Update(CommitMsgReady{Err: errors.New("x")})
	if got := p.commitBox.Value(); got != "wip" {
		t.Errorf("commit box = %q, want the typed text kept", got)
	}
	if !strings.Contains(p.status, "x") {
		t.Errorf("status = %q, want it to report the error", p.status)
	}
}

func TestCommitMsgSuccessSetsText(t *testing.T) {
	p := committingPane(t, true)
	p.commitBox.SetValue("wip")
	p, _ = p.Update(CommitMsgReady{Text: "  feat: add widget\n"})
	if got := p.commitBox.Value(); got != "feat: add widget" {
		t.Errorf("commit box = %q, want the generated message", got)
	}
}

func TestGenerateShowsSpinnerUntilReady(t *testing.T) {
	p := committingPane(t, true)
	p, cmd := p.Update(ctrlG())
	if !p.generating {
		t.Fatal("ctrl+g should start generating")
	}
	var req *GenCommitMsgRequest
	for _, msg := range collect(cmd) {
		if r, ok := msg.(GenCommitMsgRequest); ok {
			req = &r
		}
	}
	if req == nil || req.Ctx == nil {
		t.Fatal("ctrl+g should emit a GenCommitMsgRequest carrying a context")
	}
	if !strings.Contains(p.View(), p.spin.View()) {
		t.Error("the commit box should show the spinner while generating")
	}
	p, _ = p.Update(CommitMsgReady{Text: "feat: x"})
	if p.generating {
		t.Error("generating should stop once the message is ready")
	}
	if strings.Contains(p.View(), p.spin.View()) {
		t.Error("the spinner should disappear once the message is ready")
	}
}

func TestEscWhileGeneratingCancels(t *testing.T) {
	p := committingPane(t, true)
	p, cmd := p.Update(ctrlG())
	var ctx context.Context
	for _, msg := range collect(cmd) {
		if r, ok := msg.(GenCommitMsgRequest); ok {
			ctx = r.Ctx
		}
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ctx == nil || ctx.Err() == nil {
		t.Fatal("esc while generating should cancel the request context")
	}
	if p.generating {
		t.Error("esc should stop generating")
	}
	if !p.committing {
		t.Error("the first esc should only cancel generation, not the commit")
	}
	p.commitBox.SetValue("wip")
	p, _ = p.Update(CommitMsgReady{Err: context.Canceled})
	if p.commitBox.Value() != "wip" {
		t.Error("a cancelled reply must not touch the commit box")
	}
}

func TestCommitAIHintHiddenWhenUnavailable(t *testing.T) {
	hasHint := func(p Pane) bool {
		for _, b := range p.HelpBindings() {
			if b.Help().Key == "ctrl+g" {
				return true
			}
		}
		return false
	}
	if !hasHint(NewPane("dracula", true)) {
		t.Error("ctrl+g hint should show when the AI CLI is available")
	}
	off := NewPane("dracula", false)
	if hasHint(off) {
		t.Error("ctrl+g hint should be hidden when the AI CLI is missing")
	}
	if strings.Contains(off.commitBox.Placeholder, "ctrl+g") {
		t.Error("placeholder should not advertise ctrl+g when the AI CLI is missing")
	}
}

func TestRefreshMsgReloads(t *testing.T) {
	p := NewPane("dracula", true)
	before := p.Reloads()
	p, _ = p.Update(Refresh())
	if p.Reloads() != before+1 {
		t.Errorf("Reloads = %d, want %d", p.Reloads(), before+1)
	}
}
