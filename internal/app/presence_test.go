package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/files"
	"github.com/kloppers-labs/deck/internal/presence"
	"github.com/kloppers-labs/deck/internal/viewer"
)

func runQuick(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, runQuick(c)...)
			}
			return out
		}
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			var out []tea.Msg
			for i := range v.Len() {
				out = append(out, runQuick(v.Index(i).Interface().(tea.Cmd))...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func drive(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	queue := append([]tea.Msg(nil), msgs...)
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		msg := queue[0]
		queue = queue[1:]
		next, cmd := m.Update(msg)
		m = next.(Model)
		queue = append(queue, runQuick(cmd)...)
	}
	return m
}

func agentFixture(t *testing.T, cfg config.Config) (Model, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "ab\tc\nxyz\n😀é\n")
	write("b.go", "package b\n")
	write("docs/x.md", "# Title\n\nintro\n\n## Goals\n\ngoal one\ngoal two\n\n## Plan\n\nstep\n")
	t.Chdir(dir)
	m := New(cfg, nil, nil).WithAgent(presence.New(), "")
	m = drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	return m, dir
}

func openFile(t *testing.T, m Model, path string, line int) Model {
	t.Helper()
	return drive(t, m, files.RevealMsg{Target: editor.Target{Path: path, Line: line}})
}

func keys(t *testing.T, m Model, ks ...string) Model {
	t.Helper()
	for _, k := range ks {
		m = drive(t, m, keyMsg(k))
	}
	return m
}

func TestPresenceFollowsViewerCursor(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = openFile(t, m, filepath.Join(dir, "a.go"), 1)
	m = keys(t, m, "j")
	got := m.pres.Get()
	if got.Tab != "files" || got.File != filepath.Join(dir, "a.go") || got.Line != 2 || got.Col != 1 {
		t.Errorf("presence = %+v, want files a.go:2:1", got)
	}
	if got.Root != dir {
		t.Errorf("Root = %q, want %q", got.Root, dir)
	}
}

func TestPresenceTabSwitch(t *testing.T) {
	m, _ := agentFixture(t, config.Default())
	m = keys(t, m, "2")
	if got := m.pres.Get().Tab; got != "git" {
		t.Errorf("Tab = %q, want git", got)
	}
}

func TestPresenceSelection(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = openFile(t, m, filepath.Join(dir, "a.go"), 1)
	m = keys(t, m, "V", "j")
	got := m.pres.Get()
	if got.Sel == nil || *got.Sel != (presence.Range{StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 4}) {
		t.Fatalf("Sel = %+v, want 1:1-2:4", got.Sel)
	}
	if got.SelText != "ab\tc\nxyz" {
		t.Errorf("SelText = %q", got.SelText)
	}
}

func TestPresenceSelTextCapped(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat(strings.Repeat("x", 99)+"\n", 210)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openFile(t, m, big, 1)
	m = keys(t, m, "V", "G")
	if got := len(m.pres.Get().SelText); got != presence.MaxSelText {
		t.Errorf("len(SelText) = %d, want %d", got, presence.MaxSelText)
	}
}

func TestPresenceRenderedMarkdownSection(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	m = openFile(t, m, filepath.Join(dir, "docs", "x.md"), 0)
	for range 12 {
		if got := m.pres.Get(); got.Section == "Goals" {
			if got.Line != 5 || got.Sel == nil || got.Sel.StartLine != 5 || got.Sel.EndLine != 9 {
				t.Errorf("section presence = %+v sel %+v, want lines 5-9", got, got.Sel)
			}
			return
		}
		m = keys(t, m, "j")
	}
	t.Errorf("never reached the Goals section: %+v", m.pres.Get())
}

func TestPresenceDirectory(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	for range 4 {
		if m.files.SelectedPath() == filepath.Join(dir, "docs") {
			break
		}
		m = keys(t, m, "k")
	}
	got := m.pres.Get()
	if got.Dir != filepath.Join(dir, "docs") || got.File != "" {
		t.Errorf("presence = %+v, want Dir docs and no file", got)
	}
}

func TestPresenceDiagnosticsStored(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	path := filepath.Join(dir, "a.go")
	d := viewer.Diagnostic{Range: viewer.Range{Start: viewer.Pos{Line: 1, Col: 0}, End: viewer.Pos{Line: 1, Col: 2}},
		Severity: viewer.SeverityError, Message: "boom", Source: "gopls"}
	m = drive(t, m, files.LSPEventMsg{Event: files.DiagnosticsEvent{Path: path, Diagnostics: []viewer.Diagnostic{d}}})
	got := m.pres.Diagnostics(path)[path]
	want := presence.Diagnostic{Line: 2, Col: 1, EndLine: 2, EndCol: 3, Severity: "error", Message: "boom", Source: "gopls"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("diagnostics = %+v, want %+v", got, want)
	}
}

func TestNoStoreNoPanic(t *testing.T) {
	m := New(config.Default(), nil, nil)
	m = drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40}, keyMsg("j"), keyMsg("2"))
	if m.pres != nil {
		t.Error("no store was given")
	}
}
