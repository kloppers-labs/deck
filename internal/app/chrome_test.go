package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/ui"
)

func TestAltDigitJumpsWhileCapturing(t *testing.T) {
	m := update(t, newModel(), keyMsg("4"))
	if !m.capturing() {
		t.Fatal("the HTTP tab should capture the keyboard")
	}
	if got := update(t, m, keyMsg("alt+1")).active; got != tabFiles {
		t.Errorf("alt+1 while capturing: active = %d, want tabFiles", got)
	}
	if got := update(t, m, keyMsg("alt+3")).active; got != tabRun {
		t.Errorf("alt+3 while capturing: active = %d, want tabRun", got)
	}
}

func TestF1HelpWhileCapturing(t *testing.T) {
	m := update(t, newModel(), keyMsg("4"), keyMsg("f1"))
	if !m.help.open {
		t.Fatal("F1 should open help even while the HTTP input captures")
	}
	if update(t, m, keyMsg("f1")).help.open {
		t.Error("F1 again should close help")
	}
}

func TestFilesPreviewDoesNotCapture(t *testing.T) {
	m := update(t, sized(t, 100, 30), keyMsg("f"))
	if m.capturing() {
		t.Fatal("focusing the preview should not capture the keyboard")
	}
	if _, cmd := m.Update(keyMsg("q")); !isQuit(cmd) {
		t.Error("'q' should quit while the preview has focus")
	}
}

func TestFooterWhileCapturing(t *testing.T) {
	m := update(t, sized(t, 160, 30), keyMsg("4"))
	footer := ansi.Strip(m.footer())
	if !strings.Contains(footer, "esc normal") {
		t.Errorf("capturing footer %q should offer esc normal", footer)
	}
	if strings.Contains(footer, "q quit") || strings.Contains(footer, "? help") {
		t.Errorf("capturing footer %q should not offer q or ?", footer)
	}
	if files := ansi.Strip(update(t, m, keyMsg("alt+1")).footer()); !strings.Contains(files, "q quit") {
		t.Errorf("non-capturing footer %q should offer q quit", files)
	}
}

func TestAltMTogglesMouse(t *testing.T) {
	m := sized(t, 120, 30)
	if !m.mouseOn {
		t.Fatal("mouse should start on with the default config")
	}
	next, cmd := m.Update(keyMsg("alt+m"))
	m = next.(Model)
	if m.mouseOn || cmd == nil {
		t.Fatal("alt+m should turn the mouse off and return a command")
	}
	if got := fmt.Sprintf("%T", cmd()); got != fmt.Sprintf("%T", tea.DisableMouse()) {
		t.Errorf("alt+m off returned %s, want DisableMouse", got)
	}
	if footer := ansi.Strip(m.footer()); !strings.Contains(footer, "mouse off") {
		t.Errorf("footer %q should say mouse off", footer)
	}
	next, cmd = m.Update(keyMsg("alt+m"))
	if !next.(Model).mouseOn || fmt.Sprintf("%T", cmd()) != fmt.Sprintf("%T", tea.EnableMouseAllMotion()) {
		t.Error("alt+m again should re-enable all-motion mouse reporting")
	}
}

func TestMouseOffConfigStartsOff(t *testing.T) {
	cfg := config.Default()
	cfg.Mouse = "click"
	m := New(cfg, nil)
	if !m.mouseOn {
		t.Fatal("click mode should start with the mouse on")
	}
	next, _ := m.Update(keyMsg("alt+m"))
	_, cmd := next.(Model).Update(keyMsg("alt+m"))
	if fmt.Sprintf("%T", cmd()) != fmt.Sprintf("%T", tea.EnableMouseCellMotion()) {
		t.Error("click mode should re-enable cell-motion reporting")
	}
	cfg.Mouse = "off"
	if New(cfg, nil).mouseOn {
		t.Error("mouse = off should start with the mouse off")
	}
}

func TestToastExpires(t *testing.T) {
	m := sized(t, 120, 30)
	next, cmd := m.Update(ui.ToastMsg{Level: ui.LevelInfo, Text: "copied"})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("a toast should schedule its expiry")
	}
	if !strings.Contains(ansi.Strip(m.statusBar()), "copied") {
		t.Fatal("the status bar should show the toast")
	}
	m = update(t, m, toastExpireMsg{id: m.toasts[0].id})
	if strings.Contains(ansi.Strip(m.statusBar()), "copied") || len(m.toasts) != 0 {
		t.Error("the toast should be gone once it expires")
	}
}

func TestToastNewestWins(t *testing.T) {
	m := update(t, sized(t, 120, 30),
		ui.ToastMsg{Level: ui.LevelInfo, Text: "first"},
		ui.ToastMsg{Level: ui.LevelError, Text: "second"})
	bar := ansi.Strip(m.statusBar())
	if !strings.Contains(bar, "second") || strings.Contains(bar, "first") {
		t.Fatalf("status bar %q should show only the newest toast", bar)
	}
	m = update(t, m, toastExpireMsg{id: m.toasts[1].id})
	if bar := ansi.Strip(m.statusBar()); !strings.Contains(bar, "first") {
		t.Errorf("after the newest expires, status bar %q should fall back to the older toast", bar)
	}
}

func TestStatusBarShowsToastRight(t *testing.T) {
	got := statusLine([]string{"deck", "⎇ main"}, []string{"saved"}, 40)
	if w := ansi.StringWidth(got); w != 40 {
		t.Errorf("width = %d, want 40: %q", w, got)
	}
	if !strings.HasPrefix(got, "deck · ⎇ main") || !strings.HasSuffix(got, "saved") {
		t.Errorf("statusLine = %q, want project left and toast right", got)
	}
	tight := statusLine([]string{"a-very-long-project-name", "⎇ feature/branch"}, []string{"build failed"}, 24)
	if w := ansi.StringWidth(tight); w != 24 {
		t.Errorf("tight width = %d, want 24: %q", w, tight)
	}
	if !strings.HasSuffix(tight, "build failed") || !strings.Contains(tight, "…") {
		t.Errorf("tight statusLine = %q, want the left side truncated and the toast intact", tight)
	}
}

func TestEditorErrorBecomesToast(t *testing.T) {
	_, cmd := sized(t, 120, 30).Update(editorClosedMsg{err: errors.New("exit status 1")})
	var toast *ui.ToastMsg
	for _, msg := range collect(cmd) {
		if tm, ok := msg.(ui.ToastMsg); ok {
			toast = &tm
		}
	}
	if toast == nil || toast.Level != ui.LevelError || !strings.Contains(toast.Text, "exit status 1") {
		t.Errorf("editor error should raise an error toast, got %+v", toast)
	}
}

func TestConfigWarningsBecomeToasts(t *testing.T) {
	m := New(config.Default(), []config.Warning{
		{Path: "/x/config.toml", Message: "bad"},
		{Path: "/x/config.toml", Message: "worse"},
	})
	var toast *ui.ToastMsg
	for _, msg := range collect(m.Init()) {
		if tm, ok := msg.(ui.ToastMsg); ok {
			toast = &tm
		}
	}
	if toast == nil || toast.Level != ui.LevelWarn || !strings.Contains(toast.Text, "bad") || !strings.Contains(toast.Text, "+1") {
		t.Errorf("config warnings should raise one warning toast, got %+v", toast)
	}
}

func TestHelpIsModal(t *testing.T) {
	m := update(t, sized(t, 120, 30), keyMsg("?"))
	view := ansi.Strip(m.View())
	first := strings.SplitN(view, "\n", 2)[0] + strings.SplitN(view, "\n", 3)[1]
	if !strings.Contains(first, "Files") || !strings.Contains(first, "HTTP") {
		t.Errorf("the tab bar should stay visible above the help modal:\n%s", view)
	}
	if !strings.Contains(view, "Global") {
		t.Fatalf("help should open on the Global group:\n%s", view)
	}
	if end := ansi.Strip(update(t, m, keyMsg("G")).View()); !strings.Contains(end, "Mouse") || !strings.Contains(end, "double-click") {
		t.Fatalf("scrolling to the end should reach the Mouse group:\n%s", end)
	}
	if _, cmd := m.Update(keyMsg("q")); isQuit(cmd) {
		t.Error("help is modal: q should close it, not quit")
	}
	m = update(t, m, keyMsg("/"))
	for _, r := range "mouse" {
		m = update(t, m, keyMsg(string(r)))
	}
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "toggle mouse") {
		t.Errorf("filtering by mouse should keep the alt+m row:\n%s", view)
	}
	if strings.Contains(view, "next tab") {
		t.Errorf("filtering by mouse should hide unrelated rows:\n%s", view)
	}
	m = update(t, m, keyMsg("esc"), keyMsg("esc"))
	if m.help.open {
		t.Error("esc should clear the filter, then close help")
	}
}

func TestHelpScrolls(t *testing.T) {
	m := update(t, sized(t, 80, 16), keyMsg("?"))
	before := ansi.Strip(m.View())
	m = update(t, m, keyMsg("j"), keyMsg("j"))
	if m.help.scroll != 2 {
		t.Fatalf("scroll = %d, want 2", m.help.scroll)
	}
	if ansi.Strip(m.View()) == before {
		t.Error("j should scroll the help content")
	}
	m = update(t, m, keyMsg("k"), keyMsg("k"), keyMsg("k"))
	if m.help.scroll != 0 {
		t.Errorf("scroll = %d, want clamped at 0", m.help.scroll)
	}
}

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
