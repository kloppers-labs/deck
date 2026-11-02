package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"deck/internal/lsp"
	"deck/internal/ui"
	"deck/internal/viewer"
)

type fakeClient struct {
	enc      lsp.Encoding
	pos      []lsp.Position
	locs     []lsp.Location
	hover    lsp.Hover
	syms     []lsp.Symbol
	md       lsp.MetadataDocument
	reloaded []string
}

func (c *fakeClient) Encoding() lsp.Encoding { return c.enc }
func (c *fakeClient) Hover(_ context.Context, _ string, p lsp.Position) (lsp.Hover, error) {
	c.pos = append(c.pos, p)
	return c.hover, nil
}
func (c *fakeClient) Definition(_ context.Context, _ string, p lsp.Position) ([]lsp.Location, error) {
	c.pos = append(c.pos, p)
	return c.locs, nil
}
func (c *fakeClient) TypeDefinition(_ context.Context, _ string, p lsp.Position) ([]lsp.Location, error) {
	return c.Definition(context.Background(), "", p)
}
func (c *fakeClient) Implementation(_ context.Context, _ string, p lsp.Position) ([]lsp.Location, error) {
	return c.Definition(context.Background(), "", p)
}
func (c *fakeClient) References(_ context.Context, _ string, p lsp.Position, _ bool) ([]lsp.Location, error) {
	return c.Definition(context.Background(), "", p)
}
func (c *fakeClient) DocumentSymbols(context.Context, string) ([]lsp.Symbol, error) {
	return c.syms, nil
}
func (c *fakeClient) WorkspaceSymbols(context.Context, string) ([]lsp.Symbol, error) {
	return c.syms, nil
}
func (c *fakeClient) Metadata(context.Context, string) (lsp.MetadataDocument, error) {
	return c.md, nil
}
func (c *fakeClient) Reload(_ context.Context, path string) error {
	c.reloaded = append(c.reloaded, path)
	return nil
}

type fakeBackend struct {
	client   *fakeClient
	err      error
	statuses []lsp.StatusEvent
	evs      chan any
	asked    []string
}

func (b *fakeBackend) clientFor(_ context.Context, path string) (navClient, error) {
	b.asked = append(b.asked, path)
	if b.err != nil {
		return nil, b.err
	}
	return b.client, nil
}

func (b *fakeBackend) specFor(path string) (lsp.ServerSpec, bool) {
	ext := filepath.Ext(path)
	for _, s := range lsp.DefaultSpecs() {
		for _, e := range s.Extensions {
			if e == ext {
				return s, true
			}
		}
	}
	return lsp.ServerSpec{}, false
}

func (b *fakeBackend) specs() []lsp.ServerSpec   { return lsp.DefaultSpecs() }
func (b *fakeBackend) status() []lsp.StatusEvent { return b.statuses }
func (b *fakeBackend) logs() []lsp.ServerLog {
	return []lsp.ServerLog{{Server: "csharp", Root: "/r", Stderr: "a\nb\n"}}
}
func (b *fakeBackend) events() <-chan any                     { return b.evs }
func (b *fakeBackend) prestart(context.Context, string) error { return nil }
func (b *fakeBackend) shutdown(context.Context) error         { return nil }

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterConvertsUTF16ColumnsAroundEmoji(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.cs")
	line := `var s = "😀" + Helper.Count();`
	writeFile(t, path, "// header\r\n"+line+"\r\n")
	runeCol := strings.Index(line, "Helper") - len("😀") + 1
	units := runeCol + 1
	c := &fakeClient{enc: lsp.UTF16, locs: []lsp.Location{{
		URI:   lsp.PathToURI(path),
		Range: lsp.Range{Start: lsp.Position{Line: 1, Character: units}, End: lsp.Position{Line: 1, Character: units + 6}},
	}}}
	a := newAdapter(&fakeBackend{client: c})
	locs, err := a.Definition(context.Background(), path, viewer.Loc{Path: path, Line: 1, Col: runeCol})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.pos[0]; got != (lsp.Position{Line: 1, Character: units}) {
		t.Errorf("sent %+v, want utf-16 column %d", got, units)
	}
	want := viewer.Location{Path: path, Range: viewer.Range{Start: viewer.Pos{Line: 1, Col: runeCol}, End: viewer.Pos{Line: 1, Col: runeCol + 6}}, Text: line}
	if len(locs) != 1 || locs[0] != want {
		t.Errorf("locations = %+v, want %+v", locs, want)
	}
	c.enc = lsp.UTF8
	c.pos = nil
	_, _ = a.Hover(context.Background(), path, viewer.Loc{Path: path, Line: 1, Col: runeCol})
	if got := c.pos[0].Character; got != strings.Index(line, "Helper") {
		t.Errorf("utf-8 column = %d, want byte offset %d", got, strings.Index(line, "Helper"))
	}
}

func TestAdapterStatus(t *testing.T) {
	b := &fakeBackend{}
	a := newAdapter(b)
	if st := a.Status("/r/notes.txt"); st.State != viewer.ServerNone || st.NoServerText() != "no language server for .txt" {
		t.Errorf("unknown extension: %+v", st)
	}
	if st := a.Status("/r/a.cs"); st.State != viewer.ServerIdle || st.Label != "C#" || !st.Usable() {
		t.Errorf("not yet started: %+v", st)
	}
	b.statuses = []lsp.StatusEvent{{Server: "csharp", State: lsp.StateFailed, Err: &lsp.MissingBinaryError{Binary: "csharp-ls"}, Hint: "dotnet tool install"}}
	st := a.Status("/r/a.cs")
	if st.State != viewer.ServerFailed || st.Usable() || st.NoServerText() != "no language server for .cs — csharp-ls not found" {
		t.Errorf("missing binary: %+v / %q", st, st.NoServerText())
	}
	since := time.Now()
	b.statuses = []lsp.StatusEvent{
		{Server: "csharp", Root: "/other", State: lsp.StateReady},
		{Server: "csharp", Root: "/r", State: lsp.StateIndexing, Title: "Loading", Percent: 42, Since: since},
	}
	if st := a.Status("/r/src/a.cs"); st.State != viewer.ServerIndexing || st.Project != "r" || st.Percent != 42 {
		t.Errorf("status should come from the server whose root holds the file: %+v", st)
	}
}

func TestAdapterEventsConverted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.cs")
	writeFile(t, path, "x\n\"😀\"bad\n")
	evs := make(chan any, 8)
	a := newAdapter(&fakeBackend{evs: evs})
	evs <- struct{}{}
	evs <- lsp.StatusEvent{Server: "csharp", Root: dir, State: lsp.StateReady}
	evs <- lsp.DiagnosticsEvent{Server: "csharp", Path: path, Diagnostics: []lsp.Diagnostic{{
		Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 4}, End: lsp.Position{Line: 1, Character: 7}}, Severity: lsp.SeverityWarning, Message: "m",
	}}}
	evs <- lsp.ShowMessageEvent{Server: "csharp", Type: lsp.MessageError, Message: "boom"}
	evs <- lsp.LogEvent{Server: "go", Message: "hi"}
	close(evs)
	var got []any
	for {
		ev, ok := a.NextEvent()
		if !ok {
			break
		}
		got = append(got, ev)
	}
	if len(got) != 4 {
		t.Fatalf("events = %#v", got)
	}
	if st := got[0].(viewer.ServerStatus); st.State != viewer.ServerReady || st.Label != "C#" {
		t.Errorf("status = %+v", st)
	}
	d := got[1].(DiagnosticsEvent)
	if d.Path != path || d.Diagnostics[0].Range.Start != (viewer.Pos{Line: 1, Col: 3}) || d.Diagnostics[0].Severity != viewer.SeverityWarning {
		t.Errorf("diagnostics = %+v", d)
	}
	if msg := got[2].(MessageEvent); msg.Level != ui.LevelError || msg.Server != "C#" {
		t.Errorf("message = %+v", msg)
	}
	if l := got[3].(LogEvent); l.Text != "hi" {
		t.Errorf("log = %+v", l)
	}
}

func TestAdapterReloadOnlyRunningServers(t *testing.T) {
	c := &fakeClient{enc: lsp.UTF16}
	b := &fakeBackend{client: c}
	a := newAdapter(b)
	_ = a.Reload(context.Background(), "/r/a.cs")
	if len(b.asked) != 0 {
		t.Error("reload must not start a server")
	}
	b.statuses = []lsp.StatusEvent{{Server: "csharp", Root: "/r", State: lsp.StateReady}}
	_ = a.Reload(context.Background(), "/r/a.cs")
	_ = a.Reload(context.Background(), "/r/notes.md")
	if strings.Join(c.reloaded, ",") != "/r/a.cs" {
		t.Errorf("reloaded = %v", c.reloaded)
	}
}

func TestAdapterDocumentAndWorkspaceProbe(t *testing.T) {
	uri := "csharp:/metadata/projects/P/assemblies/System.Runtime/symbols/System.String.cs"
	c := &fakeClient{enc: lsp.UTF16, md: lsp.MetadataDocument{SymbolName: "System.String", Source: "class 😀String {}\n"}}
	b := &fakeBackend{client: c}
	a := newAdapter(b)
	vd, err := a.Document(context.Background(), uri)
	if err != nil || vd.Title != "[metadata] System.String" {
		t.Fatalf("document = %+v, %v", vd, err)
	}
	_, _ = a.Hover(context.Background(), uri, viewer.Loc{Path: uri, Line: 0, Col: 7})
	if got := c.pos[0].Character; got != 8 {
		t.Errorf("columns in a virtual document use its cached text: %d, want 8", got)
	}
	if _, err := a.WorkspaceSymbols(context.Background(), "/r/README.md", "Str"); err == nil {
		t.Error("no running server should be an error")
	}
	b.statuses = []lsp.StatusEvent{{Server: "csharp", Root: "/r", State: lsp.StateReady}}
	if _, err := a.WorkspaceSymbols(context.Background(), "/r/README.md", "Str"); err != nil {
		t.Fatal(err)
	}
	if last := b.asked[len(b.asked)-1]; filepath.Dir(last) != "/r" || filepath.Ext(last) != ".cs" {
		t.Errorf("workspace symbols should probe the running server's root, asked %q", last)
	}
	b.err = errors.New("down")
	if _, err := a.Definition(context.Background(), "/r/a.cs", viewer.Loc{}); err == nil {
		t.Error("client errors should surface")
	}
	if logs := a.Logs(); len(logs) != 3 || logs[2] != "b" {
		t.Errorf("logs = %q", logs)
	}
}
