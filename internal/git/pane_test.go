package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/ui"
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

func fakeChanges(n int) []Change {
	out := make([]Change, n)
	for i := range out {
		out[i] = Change{Staged: " ", Unstaged: "M", Path: fmt.Sprintf("dir/%s-%03d.go", strings.Repeat("long-name-", 6), i)}
	}
	return out
}

func TestViewIsExactlyTheOuterBox(t *testing.T) {
	for _, size := range [][2]int{{60, 11}, {80, 19}, {200, 45}} {
		p := NewPane("dracula", true)
		p.changes = fakeChanges(80)
		p.status = strings.Repeat("a very long status message ", 10)
		p.SetSize(size[0], size[1])
		for _, mode := range []string{"list", "commit"} {
			if mode == "commit" {
				p.committing = true
			}
			v := p.View()
			if got := lipgloss.Height(v); got != size[1] {
				t.Errorf("%v %s: height = %d, want %d", size, mode, got, size[1])
			}
			if got := lipgloss.Width(v); got > size[0] {
				t.Errorf("%v %s: width = %d, want <= %d", size, mode, got, size[0])
			}
		}
	}
}

func TestCursorRowStaysVisible(t *testing.T) {
	p := NewPane("dracula", true)
	p.changes = fakeChanges(60)
	for i := range p.changes {
		p.changes[i].Path = fmt.Sprintf("f-%03d.go", i)
	}
	p.SetSize(100, 14)
	for range 40 {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if p.cursor != 40 {
		t.Fatalf("cursor = %d, want 40", p.cursor)
	}
	if v := ansi.Strip(p.View()); !strings.Contains(v, "f-040.go") {
		t.Errorf("the cursor row scrolled out of view:\n%s", v)
	}
	for range 40 {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	}
	if v := ansi.Strip(p.View()); !strings.Contains(v, "f-000.go") {
		t.Errorf("moving back to the top should show the first row:\n%s", v)
	}
}

func TestCommitMsgErrorRaisesToast(t *testing.T) {
	p := committingPane(t, true)
	_, cmd := p.Update(CommitMsgReady{Err: errors.New("cli missing")})
	var toast *ui.ToastMsg
	for _, msg := range collect(cmd) {
		if tm, ok := msg.(ui.ToastMsg); ok {
			toast = &tm
		}
	}
	if toast == nil || toast.Level != ui.LevelError || !strings.Contains(toast.Text, "cli missing") {
		t.Errorf("a failed commit message should raise an error toast, got %+v", toast)
	}
	if _, cmd := p.Update(CommitMsgReady{Err: context.Canceled}); cmd != nil {
		t.Error("a cancelled request should not raise a toast")
	}
}

func TestMessagesRouteToGit(t *testing.T) {
	for _, msg := range []ui.Routed{RefreshMsg{}, CommitMsgReady{}} {
		if msg.RouteTo() != ui.DestGit {
			t.Errorf("%T routes to %v, want DestGit", msg, msg.RouteTo())
		}
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
