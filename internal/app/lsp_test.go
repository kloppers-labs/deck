package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/files"
	"deck/internal/viewer"
)

type fakeLSP struct {
	mu         sync.Mutex
	events     []any
	prestarted []string
	shutdown   bool
	deadline   bool
}

func (f *fakeLSP) Hover(context.Context, string, viewer.Loc) (viewer.Hover, error) {
	return viewer.Hover{}, nil
}
func (f *fakeLSP) Definition(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	return nil, nil
}
func (f *fakeLSP) TypeDefinition(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	return nil, nil
}
func (f *fakeLSP) Implementation(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	return nil, nil
}
func (f *fakeLSP) References(context.Context, string, viewer.Loc) ([]viewer.Location, error) {
	return nil, nil
}
func (f *fakeLSP) DocumentSymbols(context.Context, string) ([]viewer.Symbol, error) { return nil, nil }
func (f *fakeLSP) WorkspaceSymbols(context.Context, string, string) ([]viewer.Symbol, error) {
	return nil, nil
}
func (f *fakeLSP) Document(context.Context, string) (viewer.VirtualDoc, error) {
	return viewer.VirtualDoc{}, nil
}
func (f *fakeLSP) Status(string) viewer.ServerStatus    { return viewer.ServerStatus{} }
func (f *fakeLSP) Reload(context.Context, string) error { return nil }
func (f *fakeLSP) Logs() []string                       { return nil }
func (f *fakeLSP) NextEvent() (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		return nil, false
	}
	ev := f.events[0]
	f.events = f.events[1:]
	return ev, true
}
func (f *fakeLSP) Prestart(_ context.Context, cwd string) error {
	f.mu.Lock()
	f.prestarted = append(f.prestarted, cwd)
	f.mu.Unlock()
	return nil
}
func (f *fakeLSP) Shutdown(ctx context.Context) error {
	_, ok := ctx.Deadline()
	f.mu.Lock()
	f.shutdown, f.deadline = true, ok
	f.mu.Unlock()
	return nil
}

func statusEvent(state viewer.ServerState, pct int) files.LSPEventMsg {
	return files.LSPEventMsg{Event: viewer.ServerStatus{
		Server: "csharp", Label: "C#", Project: "Foo.sln", State: state, Title: "Loading Foo.sln",
		Percent: pct, Since: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
	}}
}

func TestInitListensAndPrestarts(t *testing.T) {
	fake := &fakeLSP{events: []any{viewer.ServerStatus{Server: "csharp", Label: "C#", State: viewer.ServerReady}}}
	m := New(config.Default(), nil, fake)
	var got bool
	for _, msg := range collect(m.Init()) {
		if _, ok := msg.(files.LSPEventMsg); ok {
			got = true
		}
	}
	if !got || len(fake.prestarted) != 1 {
		t.Errorf("Init should listen for events (%v) and prestart once (%v)", got, fake.prestarted)
	}
}

func TestLSPEventsRoutedWhileOnGit(t *testing.T) {
	fake := &fakeLSP{}
	m := update(t, New(config.Default(), nil, fake), tea.WindowSizeMsg{Width: 140, Height: 30}, keyMsg("2"))
	m = update(t, m, statusEvent(viewer.ServerReady, -1))
	if m.active != tabGit {
		t.Fatal("setup: Git should be active")
	}
	if st := m.files.LSPStatus(); len(st) != 1 || st[0].State != viewer.ServerReady {
		t.Errorf("the event should reach Files while Git is active: %+v", st)
	}
	if !strings.Contains(ansi.Strip(m.statusBar()), "C# ●") {
		t.Errorf("status bar = %q", ansi.Strip(m.statusBar()))
	}
}

func TestStatusBarLSPBadges(t *testing.T) {
	m := update(t, New(config.Default(), nil, &fakeLSP{}), tea.WindowSizeMsg{Width: 160, Height: 30})
	m.now = func() time.Time { return time.Date(2026, 11, 2, 9, 0, 18, 0, time.UTC) }
	for _, c := range []struct {
		state viewer.ServerState
		pct   int
		want  []string
	}{
		{viewer.ServerIndexing, 42, []string{"C# ", "Loading Foo.sln 42% (18s)"}},
		{viewer.ServerIndexing, -1, []string{"Loading Foo.sln (18s)"}},
		{viewer.ServerReady, -1, []string{"C# ●"}},
		{viewer.ServerFailed, -1, []string{"C# ✗"}},
	} {
		m = update(t, m, statusEvent(c.state, c.pct))
		bar := ansi.Strip(m.statusBar())
		for _, w := range c.want {
			if !strings.Contains(bar, w) {
				t.Errorf("state %v: status bar %q lacks %q", c.state, bar, w)
			}
		}
		if c.pct < 0 && strings.Contains(bar, "%") {
			t.Errorf("no percent should show without one: %q", bar)
		}
	}
	installing := viewer.ServerStatus{Label: "Py", State: viewer.ServerStarting, Title: "installing pyright", Percent: -1, Since: m.now().Add(-3 * time.Second)}
	if got := ansi.Strip(lspBadge(installing, m.now())); !strings.HasPrefix(got, "Py ") || !strings.HasSuffix(got, " installing pyright (3s)") {
		t.Errorf("install progress badge = %q", got)
	}
	if lspBadge(viewer.ServerStatus{Label: "Go", State: viewer.ServerIdle}, time.Now()) != "" {
		t.Error("an idle server has no badge")
	}
}

func TestShutdownCalledOnQuit(t *testing.T) {
	fake := &fakeLSP{}
	m := sized(t, 120, 30)
	m.lsp = fake
	_, cmd := m.Update(keyMsg("q"))
	if !isQuit(cmd) {
		t.Fatal("q should quit")
	}
	if err := ShutdownLSP(fake, time.Second); err != nil || !fake.shutdown || !fake.deadline {
		t.Errorf("shutdown called %v with deadline %v", fake.shutdown, fake.deadline)
	}
	if ShutdownLSP(nil, time.Second) != nil {
		t.Error("no servers, nothing to stop")
	}
}

func TestLSPConfig(t *testing.T) {
	off := false
	c := config.Default().LSP
	c.Servers = map[string]config.LSPServer{"csharp": {Solution: "Foo.sln", Enabled: &off}}
	got := LSPConfig(c)
	if !got.Enabled || !got.Prestart || got.RequestTimeout != 8*time.Second {
		t.Errorf("config = %+v", got)
	}
	if s := got.Servers["csharp"]; s.Solution != "Foo.sln" || s.Enabled == nil || *s.Enabled {
		t.Errorf("override = %+v", s)
	}
}

func TestResyncMsgSyncsBundleQuietly(t *testing.T) {
	cfg := config.Default()
	cfg.MicroBundle = false
	if _, cmd := New(cfg, nil, nil).Update(editor.ResyncMsg{}); cmd != nil {
		t.Error("no bundle, nothing to resync")
	}
	m := New(config.Default(), nil, nil)
	if !m.ed.resyncConfig().Quiet {
		t.Error("a resync must not show issues again")
	}
	if _, cmd := m.Update(editor.ResyncMsg{}); cmd == nil {
		t.Error("ResyncMsg should run a bundle sync")
	}
}
