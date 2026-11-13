package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/viewer"
)

func request(req control.Request) (control.RequestMsg, chan control.Reply) {
	ch := make(chan control.Reply, 1)
	req.V = control.Version
	if req.From == "" {
		req.From = "Claude"
	}
	return control.RequestMsg{Req: req, Reply: ch, Deadline: time.Now().Add(time.Minute)}, ch
}

func replyOf(t *testing.T, ch chan control.Reply) control.Reply {
	t.Helper()
	select {
	case r := <-ch:
		return r
	default:
		t.Fatal("no reply was sent")
		return control.Reply{}
	}
}

func TestControlOpenSwitchesToFiles(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = keys(t, m, "2")
	msg, ch := request(control.Request{Cmd: "open", Path: filepath.Join(dir, "a.go"), Line: 2})
	m = drive(t, m, msg)
	if m.active != tabFiles {
		t.Errorf("active = %d, want Files", m.active)
	}
	if loc := m.files.ViewerLoc(); loc.Path != filepath.Join(dir, "a.go") || loc.Line != 1 {
		t.Errorf("viewer at %+v, want a.go line index 1", loc)
	}
	if r := replyOf(t, ch); !r.OK {
		t.Errorf("reply = %+v", r)
	}
	if !strings.Contains(m.View(), "Claude → a.go:2") {
		t.Error("an agent jump should toast where it went")
	}
}

func TestControlOpenPushesHistory(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = openFile(t, m, filepath.Join(dir, "b.go"), 1)
	msg, _ := request(control.Request{Cmd: "open", Path: filepath.Join(dir, "a.go"), Line: 2})
	m = drive(t, m, msg, tea.KeyMsg{Type: tea.KeyCtrlO})
	if got := m.files.ViewerLoc().Path; got != filepath.Join(dir, "b.go") {
		t.Errorf("ctrl+o after an agent jump went to %q, want b.go", got)
	}
}

func TestControlOpenRangeSelects(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	msg, _ := request(control.Request{Cmd: "open", Path: filepath.Join(dir, "a.go"), Line: 1, EndLine: 2})
	m = drive(t, m, msg)
	got, ok := m.files.Selection()
	want := viewer.Range{Start: viewer.Pos{Line: 0, Col: 0}, End: viewer.Pos{Line: 1, Col: 3}}
	if !ok || got != want {
		t.Errorf("selection = %+v, %v; want %+v", got, ok, want)
	}
}

func TestControlOpenDirReveals(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = openFile(t, m, filepath.Join(dir, "a.go"), 1)
	msg, ch := request(control.Request{Cmd: "open", Path: filepath.Join(dir, "docs")})
	m = drive(t, m, msg)
	if got := m.files.SelectedPath(); got != filepath.Join(dir, "docs") {
		t.Errorf("selected %q, want docs", got)
	}
	if m.files.ShownFile() != "" {
		t.Error("a directory should show its card, not a file")
	}
	if r := replyOf(t, ch); !r.OK {
		t.Errorf("reply = %+v", r)
	}
}

func TestControlHeldWhileCapturing(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = openFile(t, m, filepath.Join(dir, "a.go"), 1)
	m = keys(t, m, "/")
	if !m.capturing() {
		t.Fatal("the search prompt should capture")
	}
	msg, ch := request(control.Request{Cmd: "open", Path: filepath.Join(dir, "b.go"), Line: 1})
	m = drive(t, m, msg)
	if r := replyOf(t, ch); !r.OK || r.Message != "held: user is typing" {
		t.Errorf("reply = %+v, want held", r)
	}
	if got := m.files.ViewerLoc().Path; got != filepath.Join(dir, "a.go") {
		t.Fatalf("a held jump moved the viewer to %q", got)
	}
	if !strings.Contains(m.View(), "Claude: b.go:1 · "+ui.Alt("j")) {
		t.Error("the status bar should offer the held jump")
	}
	m = keys(t, m, "esc", "alt+j")
	if got := m.files.ViewerLoc().Path; got != filepath.Join(dir, "b.go") {
		t.Errorf("alt+j went to %q, want b.go", got)
	}
	if strings.Contains(m.View(), ui.Alt("j")) {
		t.Error("the held jump should be gone once applied")
	}
}

func TestControlExpiredRequestDropped(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = openFile(t, m, filepath.Join(dir, "a.go"), 1)
	msg, ch := request(control.Request{Cmd: "open", Path: filepath.Join(dir, "b.go"), Line: 1})
	msg.Deadline = time.Now().Add(-time.Second)
	m = drive(t, m, msg)
	if got := m.files.ViewerLoc().Path; got != filepath.Join(dir, "a.go") {
		t.Errorf("an expired request moved the viewer to %q", got)
	}
	select {
	case r := <-ch:
		t.Errorf("an expired request should not be answered, got %+v", r)
	default:
	}
}

func TestControlReviewSwitchesToGit(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	msg, ch := request(control.Request{Cmd: "diff", Path: filepath.Join(dir, "a.go"), Line: 3, Base: "main"})
	next, cmd := m.Update(msg)
	m = next.(Model)
	if m.active != tabGit {
		t.Errorf("active = %d, want Git", m.active)
	}
	var got *git.ReviewMsg
	for _, out := range runQuick(cmd) {
		if rm, ok := out.(git.ReviewMsg); ok {
			got = &rm
		}
	}
	want := git.ReviewMsg{Base: "main", Path: filepath.Join(dir, "a.go"), Line: 3}
	if got == nil || *got != want {
		t.Errorf("review msg = %+v, want %+v", got, want)
	}
	if r := replyOf(t, ch); !r.OK {
		t.Errorf("reply = %+v", r)
	}
}

func TestControlMissingFile(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	missing := filepath.Join(dir, "nope.go")
	msg, ch := request(control.Request{Cmd: "open", Path: missing})
	drive(t, m, msg)
	if r := replyOf(t, ch); r.OK || r.Error != "no such file: "+missing {
		t.Errorf("reply = %+v", r)
	}
}

func TestHeldJumpFitsFrame(t *testing.T) {
	cfg := longNameFixture(t)
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 40}, {200, 50}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			m := update(t, New(cfg, nil, nil), tea.WindowSizeMsg{Width: w, Height: h})
			m.held = &control.Request{Cmd: "open", Path: strings.Repeat("deep/", 30) + "file.go", Line: 120, From: "Claude"}
			view := m.View()
			if got := lipgloss.Height(view); got != h {
				t.Errorf("frame height = %d, want %d", got, h)
			}
			if got := lipgloss.Width(view); got > w {
				t.Errorf("frame width = %d, want <= %d", got, w)
			}
		})
	}
}

func TestReviewModeFitsFrame(t *testing.T) {
	cfg := longNameFixture(t)
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 40}, {200, 50}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			m := drive(t, New(cfg, nil, nil), tea.WindowSizeMsg{Width: w, Height: h}, keyMsg("2"), keyMsg("v"))
			if !strings.Contains(m.View(), "Review") {
				t.Fatal("v should open the review")
			}
			view := m.View()
			if got := lipgloss.Height(view); got != h {
				t.Errorf("frame height = %d, want %d", got, h)
			}
			if got := lipgloss.Width(view); got > w {
				t.Errorf("frame width = %d, want <= %d", got, w)
			}
		})
	}
}

func TestShowFileSwitchesToFiles(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = keys(t, m, "2")
	m = drive(t, m, ui.ShowFileMsg{Path: filepath.Join(dir, "a.go"), Line: 2})
	if m.active != tabFiles {
		t.Errorf("active = %d, want Files", m.active)
	}
	if loc := m.files.ViewerLoc(); loc.Path != filepath.Join(dir, "a.go") || loc.Line != 1 {
		t.Errorf("viewer at %+v, want a.go line index 1", loc)
	}
}

func TestPresenceFromGitReview(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init"},
		{"switch", "-qc", "feature"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, _ := request(control.Request{Cmd: "diff", Path: filepath.Join(dir, "b.go"), Line: 3})
	m = drive(t, m, msg)
	got := m.pres.Get()
	if got.Tab != "git" || got.Git == nil || got.Git.Mode != "review" || got.Git.Base != "main" {
		t.Fatalf("presence = %+v git %+v, want the review", got, got.Git)
	}
	if got.File != filepath.Join(dir, "b.go") || got.Line != 3 || got.Git.Side != "new" {
		t.Errorf("presence = %+v git %+v, want b.go:3 new side", got, got.Git)
	}
	m = keys(t, m, "v", "k")
	got = m.pres.Get()
	if got.Sel == nil || got.Sel.StartLine != 2 || got.Sel.EndLine != 3 || got.SelText != "\nfunc B() {}" {
		t.Errorf("selection presence = %+v text %q", got.Sel, got.SelText)
	}
}
