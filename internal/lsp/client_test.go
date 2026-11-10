package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kloppers-labs/deck/internal/lsp/lsptest"
)

func states(evs []StatusEvent) string {
	var parts []string
	for _, ev := range evs {
		s := ev.State.String()
		if ev.Title != "" {
			s += "(" + ev.Title
			if ev.Percent >= 0 {
				s += fmt.Sprintf(" %d%%", ev.Percent)
			}
			s += ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestStateStartingIndexingReady(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	c := h.client(h.file("a.fk", "x"))
	s := h.servers[0]
	ctx := context.Background()
	if resp, err := s.Call(ctx, "window/workDoneProgress/create", map[string]any{"token": "load"}); err != nil || resp.Error != nil {
		t.Fatalf("create: %v %+v", err, resp.Error)
	}
	progress := func(kind string, pct int) {
		v := map[string]any{"kind": kind}
		if kind == "begin" {
			v["title"] = "Loading"
		}
		if pct >= 0 {
			v["percentage"] = pct
		}
		if err := s.Notify("$/progress", map[string]any{"token": "load", "value": v}); err != nil {
			t.Fatal(err)
		}
	}
	progress("begin", 0)
	progress("report", 50)
	progress("end", -1)

	evs := h.statusUntil(func(ev StatusEvent) bool { return ev.State == StateReady })
	want := "starting indexing indexing(Loading 0%) indexing(Loading 50%) ready"
	if got := states(evs); got != want {
		t.Fatalf("states = %q, want %q", got, want)
	}
	if c.State() != StateReady || evs[0].Server != "fake" || evs[0].Root != h.dir {
		t.Fatalf("state=%v first=%+v", c.State(), evs[0])
	}
	h.clock.Advance(5 * time.Second)
	if c.State() != StateReady {
		t.Fatal("timer moved a ready client")
	}

	progress("begin", -1)
	if ev := h.statusUntil(func(StatusEvent) bool { return true }); ev[0].State != StateIndexing || ev[0].Percent != -1 {
		t.Fatalf("re-index = %+v", ev[0])
	}
}

func TestReadyAfter2sWithoutProgress(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	c := h.client(h.file("a.fk", "x"))
	if c.State() != StateIndexing {
		t.Fatalf("state = %v, want indexing", c.State())
	}
	h.clock.Advance(1999 * time.Millisecond)
	if c.State() != StateIndexing {
		t.Fatalf("ready too early: %v", c.State())
	}
	h.clock.Advance(time.Millisecond)
	if c.State() != StateReady {
		t.Fatalf("state = %v, want ready", c.State())
	}
}

func TestReadyWaitsForOpenProgressAfter2s(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	c := h.client(h.file("a.fk", "x"))
	s := h.servers[0]
	_ = s.Notify("$/progress", map[string]any{"token": 7, "value": map[string]any{"kind": "begin", "title": "Indexing"}})
	waitFor(t, "progress begin", func() bool { return c.Status().Title == "Indexing" })
	h.clock.Advance(3 * time.Second)
	if c.State() != StateIndexing {
		t.Fatalf("state = %v, want indexing while a token is open", c.State())
	}
	_ = s.Notify("$/progress", map[string]any{"token": 7, "value": map[string]any{"kind": "end"}})
	waitFor(t, "ready", func() bool { return c.State() == StateReady })
}

func TestInitializeAdvertisesCapabilities(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	h.servers[0].Handle("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-8"}}, nil
	})
	c := h.client(h.file("sub/a.fk", "x"))
	if c.Encoding() != UTF8 || c.Root() != h.dir || c.Name() != "fake" {
		t.Fatalf("encoding=%v root=%q name=%q", c.Encoding(), c.Root(), c.Name())
	}
	msgs, err := h.servers[0].WaitFor("initialized", 1, time.Second)
	if err != nil || len(msgs) != 1 {
		t.Fatal(err)
	}
	init := h.servers[0].Received()[0]
	if init.Method != "initialize" {
		t.Fatalf("first message = %s", init.Method)
	}
	var p struct {
		RootURI      string `json:"rootUri"`
		Capabilities struct {
			General struct {
				PositionEncodings []string `json:"positionEncodings"`
			} `json:"general"`
			TextDocument struct {
				Hover struct {
					ContentFormat []string `json:"contentFormat"`
				} `json:"hover"`
				Definition struct {
					LinkSupport bool `json:"linkSupport"`
				} `json:"definition"`
				DocumentSymbol struct {
					Hierarchical bool `json:"hierarchicalDocumentSymbolSupport"`
				} `json:"documentSymbol"`
			} `json:"textDocument"`
			Window struct {
				WorkDoneProgress bool `json:"workDoneProgress"`
			} `json:"window"`
			Workspace struct {
				Configuration bool `json:"configuration"`
			} `json:"workspace"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(init.Params, &p); err != nil {
		t.Fatal(err)
	}
	caps := p.Capabilities
	if p.RootURI != PathToURI(h.dir) || strings.Join(caps.General.PositionEncodings, ",") != "utf-8,utf-16" ||
		caps.TextDocument.Hover.ContentFormat[0] != "markdown" || !caps.TextDocument.Definition.LinkSupport ||
		!caps.TextDocument.DocumentSymbol.Hierarchical || !caps.Window.WorkDoneProgress || !caps.Workspace.Configuration {
		t.Fatalf("initialize params = %s", init.Params)
	}
	cmd := h.starts[0]
	if cmd.Path != "/bin/fakels" || strings.Join(cmd.Args, " ") != "--stdio" || cmd.Dir != h.dir || cmd.Stderr == nil {
		t.Fatalf("command = %+v", cmd)
	}
}

func TestServerRequestsAnswered(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	h.client(h.file("a.fk", "x"))
	s := h.servers[0]
	ctx := context.Background()
	for method, want := range map[string]string{
		"workspace/configuration":     `[null,null]`,
		"client/registerCapability":   `null`,
		"client/unregisterCapability": `null`,
		"window/showMessageRequest":   `null`,
		"workspace/workspaceFolders":  fmt.Sprintf(`[{"uri":%q,"name":%q}]`, PathToURI(h.dir), filepath.Base(h.dir)),
	} {
		params := map[string]any{"items": []map[string]string{{"section": "a"}, {"section": "b"}}}
		resp, err := s.Call(ctx, method, params)
		if err != nil || resp.Error != nil || string(resp.Result) != want {
			t.Errorf("%s: result=%s err=%v rpc=%+v, want %s", method, resp.Result, err, resp.Error, want)
		}
	}
	resp, err := s.Call(ctx, "workspace/applyEdit", map[string]any{})
	if err != nil || resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("unknown request: %+v %v", resp, err)
	}
}

func TestNotificationsBecomeEvents(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	a := h.file("a.fk", "x")
	h.client(a)
	s := h.servers[0]
	_ = s.Notify("textDocument/publishDiagnostics", map[string]any{"uri": PathToURI(a), "diagnostics": []map[string]any{{
		"range": rng(1, 0, 1, 4), "severity": 1, "message": "boom",
	}}})
	_ = s.Notify("window/logMessage", map[string]any{"type": 4, "message": "chatty"})
	_ = s.Notify("window/showMessage", map[string]any{"type": 1, "message": "load failed"})
	var diag *DiagnosticsEvent
	var log *LogEvent
	var show *ShowMessageEvent
	for diag == nil || log == nil || show == nil {
		switch ev := h.nextEvent().(type) {
		case DiagnosticsEvent:
			diag = &ev
		case LogEvent:
			log = &ev
		case ShowMessageEvent:
			show = &ev
		}
	}
	if diag.Path != a || diag.Server != "fake" || len(diag.Diagnostics) != 1 || diag.Diagnostics[0].Severity != SeverityError {
		t.Fatalf("diag = %+v", diag)
	}
	if log.Message != "chatty" || log.Type != MessageLog || show.Message != "load failed" || show.Type != MessageError {
		t.Fatalf("log=%+v show=%+v", log, show)
	}
}

func didOpenURIs(t *testing.T, msgs []lsptest.Message) []string {
	t.Helper()
	var out []string
	for _, m := range msgs {
		var p DidOpenTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p.TextDocument.URI)
	}
	return out
}

func TestDidOpenOnceAndLRUClose(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	s := h.servers[0]
	var files []string
	for i := range 31 {
		files = append(files, h.file(fmt.Sprintf("f%02d.fk", i), fmt.Sprintf("content %d\n", i)))
	}
	c := h.client(files[0])
	ctx := context.Background()
	for _, f := range files[:30] {
		if _, err := c.Hover(ctx, f, Position{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Hover(ctx, files[0], Position{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Hover(ctx, files[30], Position{}); err != nil {
		t.Fatal(err)
	}
	opens, err := s.WaitFor("textDocument/didOpen", 31, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var first DidOpenTextDocumentParams
	_ = json.Unmarshal(opens[0].Params, &first)
	if first.TextDocument.Text != "content 0\n" || first.TextDocument.Version != 1 || first.TextDocument.LanguageID != "fk" {
		t.Fatalf("didOpen = %+v", first.TextDocument)
	}
	closes, err := s.WaitFor("textDocument/didClose", 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var closed DidCloseTextDocumentParams
	_ = json.Unmarshal(closes[0].Params, &closed)
	if closed.TextDocument.URI != PathToURI(files[1]) {
		t.Fatalf("evicted %s, want the least recently used %s", closed.TextDocument.URI, PathToURI(files[1]))
	}
	if _, err := c.Hover(ctx, files[5], Position{}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.WaitFor("textDocument/didOpen", 0, 0)
	seen := map[string]int{}
	for _, uri := range didOpenURIs(t, all) {
		seen[uri]++
	}
	if len(all) != 31 || seen[PathToURI(files[5])] != 1 {
		t.Fatalf("didOpen count = %d, files[5] opened %d times", len(all), seen[PathToURI(files[5])])
	}
}

func TestReloadSendsDidChangeVersionBump(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	s := h.servers[0]
	a := h.file("a.fk", "one")
	c := h.client(a)
	ctx := context.Background()
	if err := c.Open(ctx, a); err != nil {
		t.Fatal(err)
	}
	h.file("a.fk", "two")
	if err := c.Reload(ctx, a); err != nil {
		t.Fatal(err)
	}
	h.file("a.fk", "three!")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(a, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Hover(ctx, a, Position{}); err != nil {
		t.Fatal(err)
	}
	changes, err := s.WaitFor("textDocument/didChange", 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		version int
		text    string
	}{{2, "two"}, {3, "three!"}} {
		var p DidChangeTextDocumentParams
		_ = json.Unmarshal(changes[i].Params, &p)
		if p.TextDocument.Version != want.version || len(p.ContentChanges) != 1 || p.ContentChanges[0].Text != want.text {
			t.Fatalf("didChange %d = %+v", i, p)
		}
	}
	other := h.file("b.fk", "b")
	if err := c.Reload(ctx, other); err != nil {
		t.Fatal(err)
	}
	if opens, _ := s.WaitFor("textDocument/didOpen", 0, 0); len(opens) != 1 {
		t.Fatalf("Reload of an unopened doc sent didOpen")
	}
}

func TestRequestsDecodeResults(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	s := h.servers[0]
	a := h.file("a.fk", "hello")
	c := h.client(a)
	ctx := context.Background()
	link := map[string]any{"targetUri": "file:///b.fk", "targetRange": rng(0, 0, 9, 0), "targetSelectionRange": rng(2, 1, 2, 4)}
	s.Handle("textDocument/definition", func(json.RawMessage) (any, error) { return []any{link}, nil })
	s.Handle("textDocument/typeDefinition", func(json.RawMessage) (any, error) { return link, nil })
	s.Handle("textDocument/implementation", func(json.RawMessage) (any, error) { return nil, nil })
	s.Handle("textDocument/references", func(p json.RawMessage) (any, error) {
		var rp ReferenceParams
		_ = json.Unmarshal(p, &rp)
		if !rp.Context.IncludeDeclaration || rp.Position != (Position{3, 7}) {
			return nil, errors.New("bad reference params")
		}
		return []any{map[string]any{"uri": PathToURI(a), "range": rng(3, 7, 3, 9)}}, nil
	})
	s.Handle("textDocument/hover", func(json.RawMessage) (any, error) {
		return map[string]any{"contents": map[string]any{"kind": "markdown", "value": "**hi**"}}, nil
	})
	s.Handle("textDocument/documentSymbol", func(json.RawMessage) (any, error) {
		return []any{map[string]any{"name": "F", "kind": 12, "range": rng(0, 0, 1, 0), "selectionRange": rng(0, 0, 0, 1)}}, nil
	})
	s.Handle("workspace/symbol", func(p json.RawMessage) (any, error) {
		return []any{map[string]any{"name": "Q", "kind": 5, "location": map[string]any{"uri": "file:///q.fk", "range": rng(1, 0, 2, 0)}}}, nil
	})
	s.Handle("csharp/metadata", func(json.RawMessage) (any, error) {
		return map[string]any{"projectName": "Api", "assemblyName": "System.Runtime", "symbolName": "System.String", "source": "class String {}"}, nil
	})

	defs, err := c.Definition(ctx, a, Position{0, 1})
	if err != nil || len(defs) != 1 || defs[0].URI != "file:///b.fk" || defs[0].Range != rng(2, 1, 2, 4) {
		t.Fatalf("Definition = %+v, %v", defs, err)
	}
	if tds, err := c.TypeDefinition(ctx, a, Position{}); err != nil || len(tds) != 1 {
		t.Fatalf("TypeDefinition = %+v, %v", tds, err)
	}
	if impls, err := c.Implementation(ctx, a, Position{}); err != nil || impls != nil {
		t.Fatalf("Implementation = %+v, %v", impls, err)
	}
	if refs, err := c.References(ctx, a, Position{3, 7}, true); err != nil || len(refs) != 1 {
		t.Fatalf("References = %+v, %v", refs, err)
	}
	if hv, err := c.Hover(ctx, a, Position{}); err != nil || hv.Markdown != "**hi**" {
		t.Fatalf("Hover = %+v, %v", hv, err)
	}
	if syms, err := c.DocumentSymbols(ctx, a); err != nil || len(syms) != 1 || syms[0].URI != PathToURI(a) {
		t.Fatalf("DocumentSymbols = %+v, %v", syms, err)
	}
	if ws, err := c.WorkspaceSymbols(ctx, "Q"); err != nil || len(ws) != 1 || ws[0].Name != "Q" {
		t.Fatalf("WorkspaceSymbols = %+v, %v", ws, err)
	}
	const meta = "csharp:/metadata/projects/Api/assemblies/System.Runtime/symbols/System.String.cs"
	if md, err := c.Metadata(ctx, meta); err != nil || md.Source != "class String {}" || md.SymbolName != "System.String" {
		t.Fatalf("Metadata = %+v, %v", md, err)
	}
	if _, err := c.Hover(ctx, meta, Position{}); err != nil {
		t.Fatal(err)
	}
	opens, _ := s.WaitFor("textDocument/didOpen", 0, 0)
	if len(opens) != 1 {
		t.Fatalf("virtual document was didOpen'ed: %d opens", len(opens))
	}
}

func TestRequestTimeout(t *testing.T) {
	cfg := fakeConfig()
	cfg.RequestTimeout = 50 * time.Millisecond
	h := newHarness(t, 1, cfg)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h.servers[0].Handle("textDocument/hover", func(json.RawMessage) (any, error) { <-release; return nil, nil })
	a := h.file("a.fk", "x")
	_, err := h.client(a).Hover(context.Background(), a, Position{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestShutdownSequenceThenKill(t *testing.T) {
	for _, ignoreExit := range []bool{false, true} {
		t.Run(fmt.Sprintf("ignoreExit=%v", ignoreExit), func(t *testing.T) {
			h := newHarness(t, 1, fakeConfig())
			s := h.servers[0]
			s.IgnoreExit = ignoreExit
			h.client(h.file("a.fk", "x"))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			start := time.Now()
			if err := h.mgr.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if el := time.Since(start); el > time.Second {
				t.Fatalf("shutdown took %v", el)
			}
			var tail []string
			for _, m := range s.Methods() {
				if m == "shutdown" || m == "exit" {
					tail = append(tail, m)
				}
			}
			if strings.Join(tail, ",") != "shutdown,exit" || !s.Killed() {
				t.Fatalf("sequence = %v killed=%v", tail, s.Killed())
			}
			if _, err := h.mgr.ClientFor(context.Background(), h.file("b.fk", "y")); !errors.Is(err, ErrShutdown) {
				t.Fatalf("ClientFor after shutdown = %v", err)
			}
			if _, open := <-h.mgr.Events(); open {
				for range h.mgr.Events() {
				}
			}
		})
	}
}

func TestMissingBinaryFailsOnceNoRetry(t *testing.T) {
	lookups := 0
	h := newHarness(t, 0, fakeConfig(), WithEnv(Env{
		LookPath: func(string) (string, error) { lookups++; return "", errors.New("not found") },
		Home:     t.TempDir(),
		IsExec:   func(string) bool { return false },
	}))
	a := h.file("a.fk", "x")
	for range 3 {
		_, err := h.mgr.ClientFor(context.Background(), a)
		var missing *MissingBinaryError
		if !errors.As(err, &missing) || missing.Binary != "fakels" {
			t.Fatalf("err = %v", err)
		}
	}
	if lookups != 1 || h.startCount() != 0 {
		t.Fatalf("lookups=%d starts=%d", lookups, h.startCount())
	}
	ev := h.nextEvent().(StatusEvent)
	if ev.State != StateFailed || ev.Server != "fake" {
		t.Fatalf("event = %+v", ev)
	}
	select {
	case extra := <-h.mgr.Events():
		t.Fatalf("unexpected second event %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
	st := h.mgr.Status()
	if len(st) != 1 || st[0].State != StateFailed {
		t.Fatalf("Status = %+v", st)
	}
}

func TestCrashRestartsOnce(t *testing.T) {
	h := newHarness(t, 2, fakeConfig())
	a := h.file("a.fk", "x")
	c := h.client(a)
	s1 := h.servers[0]
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s1.Handle("textDocument/hover", func(json.RawMessage) (any, error) { <-release; return nil, nil })
	done := make(chan error, 1)
	go func() { _, err := c.Hover(context.Background(), a, Position{}); done <- err }()
	if _, err := s1.WaitFor("textDocument/hover", 1, time.Second); err != nil {
		t.Fatal(err)
	}
	s1.Crash()
	if err := <-done; !errors.Is(err, ErrServerExited) {
		t.Fatalf("pending err = %v", err)
	}
	waitFor(t, "exited state", func() bool { return c.State() == StateExited })

	if _, err := c.Hover(context.Background(), a, Position{}); err != nil {
		t.Fatalf("after restart: %v", err)
	}
	if h.startCount() != 2 {
		t.Fatalf("starts = %d, want 2", h.startCount())
	}
	if opens, _ := h.servers[1].WaitFor("textDocument/didOpen", 1, time.Second); len(opens) != 1 {
		t.Fatal("document not reopened on the restarted server")
	}

	h.servers[1].Crash()
	waitFor(t, "failed state", func() bool { return c.State() == StateFailed })
	if _, err := c.Hover(context.Background(), a, Position{}); !errors.Is(err, ErrServerExited) {
		t.Fatalf("after second crash err = %v", err)
	}
	if _, err := h.mgr.ClientFor(context.Background(), a); !errors.Is(err, ErrServerExited) {
		t.Fatalf("ClientFor after second crash = %v", err)
	}
	if h.startCount() != 2 {
		t.Fatalf("starts = %d, want no third start", h.startCount())
	}
}

func TestPrestartOnlyMatchingRoot(t *testing.T) {
	specs := []ServerSpec{
		{Name: "sln", Command: []string{"slnls"}, Extensions: []string{".cs"}, RootMarkers: []string{"*.sln", "*.csproj"},
			PrestartMarkers: []string{"*.sln"}, SolutionFlag: "--solution", Enabled: true},
		{Name: "lazy", Command: []string{"lazyls"}, Extensions: []string{".go"}, RootMarkers: []string{"go.mod"}, Enabled: true},
	}

	h := newHarness(t, 1, DefaultConfig(), withSpecs(specs))
	touch(t, filepath.Join(h.dir, "Shop.sln"), filepath.Join(h.dir, "go.mod"))
	if err := h.mgr.Prestart(context.Background(), filepath.Join(h.dir)); err != nil {
		t.Fatal(err)
	}
	if h.startCount() != 1 || h.starts[0].Path != "/bin/slnls" || h.starts[0].Dir != h.dir ||
		strings.Join(h.starts[0].Args, " ") != "--solution "+filepath.Join(h.dir, "Shop.sln") {
		t.Fatalf("starts = %+v", h.starts)
	}
	cs := h.file("src/Api/Program.cs", "class P {}")
	touch(t, filepath.Join(h.dir, "src", "Api", "Api.csproj"))
	if c := h.client(cs); c.Root() != h.dir || h.startCount() != 1 {
		t.Fatalf("ClientFor did not reuse the prestarted client: root=%q starts=%d", c.Root(), h.startCount())
	}

	goOnly := newHarness(t, 1, DefaultConfig(), withSpecs(specs))
	touch(t, filepath.Join(goOnly.dir, "go.mod"))
	if err := goOnly.mgr.Prestart(context.Background(), goOnly.dir); err != nil {
		t.Fatal(err)
	}
	if goOnly.startCount() != 0 {
		t.Fatalf("lazy server prestarted: %+v", goOnly.starts)
	}

	cfg := DefaultConfig()
	cfg.Prestart = false
	off := newHarness(t, 1, cfg, withSpecs(specs))
	touch(t, filepath.Join(off.dir, "Shop.sln"))
	if err := off.mgr.Prestart(context.Background(), off.dir); err != nil || off.startCount() != 0 {
		t.Fatalf("prestart disabled but started %d (%v)", off.startCount(), err)
	}
}

func TestDisabledManager(t *testing.T) {
	cfg := fakeConfig()
	cfg.Enabled = false
	h := newHarness(t, 1, cfg)
	if _, err := h.mgr.ClientFor(context.Background(), h.file("a.fk", "x")); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
	if err := h.mgr.Prestart(context.Background(), h.dir); err != nil || h.startCount() != 0 {
		t.Fatalf("prestart on disabled manager: %v starts=%d", err, h.startCount())
	}
}

func TestConcurrentClientForStartsOnce(t *testing.T) {
	h := newHarness(t, 2, fakeConfig())
	a := h.file("a.fk", "x")
	errs := make(chan error, 10)
	for range 10 {
		go func() { _, err := h.mgr.ClientFor(context.Background(), a); errs <- err }()
	}
	for range 10 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if h.startCount() != 1 {
		t.Fatalf("starts = %d", h.startCount())
	}
}

func TestClientForVirtualURIReusesRunningClient(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	const virtual = "fake:/metadata/decompiled/Thing.fk"
	var noServer *NoServerError
	if _, err := h.mgr.ClientFor(context.Background(), virtual); !errors.As(err, &noServer) {
		t.Fatalf("before any client: err = %v", err)
	}
	c := h.client(h.file("a.fk", "x"))
	got, err := h.mgr.ClientFor(context.Background(), virtual)
	if err != nil || got != c || h.startCount() != 1 {
		t.Fatalf("virtual ClientFor = %p, %v (want %p), starts=%d", got, err, c, h.startCount())
	}
}

func TestStderrCapturedInLogs(t *testing.T) {
	h := newHarness(t, 1, fakeConfig())
	h.client(h.file("a.fk", "x"))
	_, _ = h.starts[0].Stderr.Write([]byte("csharp-ls: loading solution\n"))
	logs := h.mgr.Logs()
	if len(logs) != 1 || logs[0].Server != "fake" || !strings.Contains(logs[0].Stderr, "loading solution") {
		t.Fatalf("Logs = %+v", logs)
	}
}
