package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kloppers-labs/deck/internal/lsp"
	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/viewer"
)

type navClient interface {
	Encoding() lsp.Encoding
	Hover(ctx context.Context, path string, pos lsp.Position) (lsp.Hover, error)
	Definition(ctx context.Context, path string, pos lsp.Position) ([]lsp.Location, error)
	TypeDefinition(ctx context.Context, path string, pos lsp.Position) ([]lsp.Location, error)
	Implementation(ctx context.Context, path string, pos lsp.Position) ([]lsp.Location, error)
	References(ctx context.Context, path string, pos lsp.Position, includeDeclaration bool) ([]lsp.Location, error)
	DocumentSymbols(ctx context.Context, path string) ([]lsp.Symbol, error)
	WorkspaceSymbols(ctx context.Context, query string) ([]lsp.Symbol, error)
	Metadata(ctx context.Context, uri string) (lsp.MetadataDocument, error)
	Reload(ctx context.Context, path string) error
}

type lspBackend interface {
	clientFor(ctx context.Context, path string) (navClient, error)
	specFor(path string) (lsp.ServerSpec, bool)
	specs() []lsp.ServerSpec
	status() []lsp.StatusEvent
	logs() []lsp.ServerLog
	events() <-chan any
	prestart(ctx context.Context, cwd string) error
	shutdown(ctx context.Context) error
	install(ctx context.Context, server string) (string, error)
}

type managerBackend struct{ m *lsp.Manager }

func (b managerBackend) clientFor(ctx context.Context, path string) (navClient, error) {
	c, err := b.m.ClientFor(ctx, path)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (b managerBackend) specFor(path string) (lsp.ServerSpec, bool) {
	return b.m.Registry().SpecFor(path)
}
func (b managerBackend) specs() []lsp.ServerSpec   { return b.m.Registry().Specs() }
func (b managerBackend) status() []lsp.StatusEvent { return b.m.Status() }
func (b managerBackend) logs() []lsp.ServerLog     { return b.m.Logs() }
func (b managerBackend) events() <-chan any        { return b.m.Events() }
func (b managerBackend) prestart(ctx context.Context, cwd string) error {
	return b.m.Prestart(ctx, cwd)
}
func (b managerBackend) shutdown(ctx context.Context) error { return b.m.Shutdown(ctx) }
func (b managerBackend) install(ctx context.Context, server string) (string, error) {
	return b.m.Install(ctx, server)
}

// LSPAdapter presents an *lsp.Manager as the viewer's Navigator, converting
// between rune columns and the server's position encoding.
type LSPAdapter struct {
	b lspBackend

	mu      sync.Mutex
	virtual map[string][]string
	enc     map[string]lsp.Encoding
}

func NewLSPAdapter(m *lsp.Manager) *LSPAdapter { return newAdapter(managerBackend{m}) }

func newAdapter(b lspBackend) *LSPAdapter {
	return &LSPAdapter{b: b, virtual: map[string][]string{}, enc: map[string]lsp.Encoding{}}
}

var serverLabels = map[string]string{
	"csharp": "C#", "go": "Go", "typescript": "TS", "python": "Py", "rust": "Rust",
	"bash": "Sh", "lua": "Lua", "yaml": "YAML", "json": "JSON",
}

func serverLabel(name string) string {
	if l, ok := serverLabels[name]; ok {
		return l
	}
	return name
}

var serverStates = map[lsp.State]viewer.ServerState{
	lsp.StateIdle:     viewer.ServerIdle,
	lsp.StateStarting: viewer.ServerStarting,
	lsp.StateIndexing: viewer.ServerIndexing,
	lsp.StateReady:    viewer.ServerReady,
	lsp.StateExited:   viewer.ServerExited,
	lsp.StateFailed:   viewer.ServerFailed,
	lsp.StateStopped:  viewer.ServerIdle,
}

func (a *LSPAdapter) language(name string) string {
	for _, s := range a.b.specs() {
		if s.Name == name && s.Language != "" {
			return s.Language
		}
	}
	return serverLabel(name)
}

func (a *LSPAdapter) statusOf(ev lsp.StatusEvent) viewer.ServerStatus {
	st := viewer.ServerStatus{
		Server: ev.Server, Label: serverLabel(ev.Server), Language: a.language(ev.Server), State: serverStates[ev.State],
		Title: ev.Title, Message: ev.Message, Percent: ev.Percent, Since: ev.Since, Hint: ev.Hint, Install: ev.Install,
	}
	if ev.Root != "" {
		st.Project = filepath.Base(ev.Root)
	}
	if ev.Err != nil {
		st.Err = ev.Err.Error()
	}
	return st
}

func (a *LSPAdapter) Status(path string) viewer.ServerStatus {
	ext := filepath.Ext(path)
	spec, ok := a.b.specFor(path)
	if !ok {
		return viewer.ServerStatus{Ext: ext}
	}
	var found *viewer.ServerStatus
	for _, ev := range a.b.status() {
		if ev.Server != spec.Name {
			continue
		}
		st := a.statusOf(ev)
		if ev.Root == "" || virtualOrUnder(path, ev.Root) || found == nil {
			found = &st
		}
	}
	if found == nil {
		return viewer.ServerStatus{Server: spec.Name, Label: serverLabel(spec.Name), Language: a.language(spec.Name), Ext: ext, State: viewer.ServerIdle, Percent: -1}
	}
	found.Ext = ext
	return *found
}

func virtualOrUnder(path, root string) bool {
	if viewer.IsVirtual(path) {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(rel)
}

func (a *LSPAdapter) client(ctx context.Context, path string) (navClient, lsp.Encoding, error) {
	c, err := a.b.clientFor(ctx, path)
	if err != nil {
		return nil, "", err
	}
	enc := c.Encoding()
	if spec, ok := a.b.specFor(path); ok {
		a.mu.Lock()
		a.enc[spec.Name] = enc
		a.mu.Unlock()
	}
	return c, enc, nil
}

func (a *LSPAdapter) linesOf(path string) []string {
	if viewer.IsVirtual(path) {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.virtual[path]
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return splitLines(string(data))
}

func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

func lineAt(lines []string, i int) string {
	if i >= 0 && i < len(lines) {
		return lines[i]
	}
	return ""
}

func (a *LSPAdapter) position(path string, at viewer.Loc, enc lsp.Encoding) lsp.Position {
	line := lineAt(a.linesOf(path), at.Line)
	return lsp.Position{Line: at.Line, Character: lsp.RuneToUnits(line, at.Col, enc)}
}

type lineSource struct {
	a     *LSPAdapter
	files map[string][]string
}

func (s *lineSource) lines(path string) []string {
	if l, ok := s.files[path]; ok {
		return l
	}
	l := s.a.linesOf(path)
	s.files[path] = l
	return l
}

func (s *lineSource) pos(path string, p lsp.Position, enc lsp.Encoding) viewer.Pos {
	line := lineAt(s.lines(path), p.Line)
	if line == "" {
		return viewer.Pos{Line: p.Line, Col: p.Character}
	}
	return viewer.Pos{Line: p.Line, Col: lsp.UnitsToRune(line, p.Character, enc)}
}

func (s *lineSource) rng(path string, r lsp.Range, enc lsp.Encoding) viewer.Range {
	return viewer.Range{Start: s.pos(path, r.Start, enc), End: s.pos(path, r.End, enc)}
}

func uriPath(uri string) string {
	if p, ok := lsp.URIToPath(uri); ok {
		return p
	}
	return uri
}

func (a *LSPAdapter) convertLocations(locs []lsp.Location, enc lsp.Encoding) []viewer.Location {
	src := &lineSource{a: a, files: map[string][]string{}}
	out := make([]viewer.Location, 0, len(locs))
	for _, l := range locs {
		path := uriPath(l.URI)
		out = append(out, viewer.Location{
			Path:  path,
			Range: src.rng(path, l.Range, enc),
			Text:  strings.TrimSpace(lineAt(src.lines(path), l.Range.Start.Line)),
		})
	}
	return out
}

func (a *LSPAdapter) convertSymbols(syms []lsp.Symbol, path string, enc lsp.Encoding, exact bool) []viewer.Symbol {
	src := &lineSource{a: a, files: map[string][]string{}}
	out := make([]viewer.Symbol, len(syms))
	for i, s := range syms {
		p := path
		if s.URI != "" {
			p = uriPath(s.URI)
		}
		vs := viewer.Symbol{
			Name: s.Name, Detail: s.Detail, Container: s.Container, Kind: viewer.SymbolKind(s.Kind),
			Path: p, Depth: s.Depth, Parent: s.Parent,
		}
		if exact {
			vs.Range, vs.Selection = src.rng(p, s.Range, enc), src.rng(p, s.SelectionRange, enc)
		} else {
			vs.Range = viewer.Range{Start: viewer.Pos{Line: s.Range.Start.Line, Col: s.Range.Start.Character}, End: viewer.Pos{Line: s.Range.End.Line, Col: s.Range.End.Character}}
			vs.Selection = vs.Range
		}
		out[i] = vs
	}
	return out
}

type locFunc func(navClient, context.Context, string, lsp.Position) ([]lsp.Location, error)

func (a *LSPAdapter) locations(ctx context.Context, path string, at viewer.Loc, f locFunc) ([]viewer.Location, error) {
	c, enc, err := a.client(ctx, path)
	if err != nil {
		return nil, err
	}
	locs, err := f(c, ctx, path, a.position(path, at, enc))
	if err != nil {
		return nil, err
	}
	return a.convertLocations(locs, enc), nil
}

func (a *LSPAdapter) Definition(ctx context.Context, path string, at viewer.Loc) ([]viewer.Location, error) {
	return a.locations(ctx, path, at, navClient.Definition)
}

func (a *LSPAdapter) TypeDefinition(ctx context.Context, path string, at viewer.Loc) ([]viewer.Location, error) {
	return a.locations(ctx, path, at, navClient.TypeDefinition)
}

func (a *LSPAdapter) Implementation(ctx context.Context, path string, at viewer.Loc) ([]viewer.Location, error) {
	return a.locations(ctx, path, at, navClient.Implementation)
}

func (a *LSPAdapter) References(ctx context.Context, path string, at viewer.Loc) ([]viewer.Location, error) {
	return a.locations(ctx, path, at, func(c navClient, ctx context.Context, p string, pos lsp.Position) ([]lsp.Location, error) {
		return c.References(ctx, p, pos, true)
	})
}

func (a *LSPAdapter) Hover(ctx context.Context, path string, at viewer.Loc) (viewer.Hover, error) {
	c, enc, err := a.client(ctx, path)
	if err != nil {
		return viewer.Hover{}, err
	}
	h, err := c.Hover(ctx, path, a.position(path, at, enc))
	if err != nil {
		return viewer.Hover{}, err
	}
	out := viewer.Hover{Markdown: h.Markdown}
	if h.Range != nil {
		src := &lineSource{a: a, files: map[string][]string{}}
		r := src.rng(path, *h.Range, enc)
		out.Range = &r
	}
	return out, nil
}

func (a *LSPAdapter) DocumentSymbols(ctx context.Context, path string) ([]viewer.Symbol, error) {
	c, enc, err := a.client(ctx, path)
	if err != nil {
		return nil, err
	}
	syms, err := c.DocumentSymbols(ctx, path)
	if err != nil {
		return nil, err
	}
	return a.convertSymbols(syms, path, enc, true), nil
}

// WorkspaceSymbols asks path's server, or else any running server, by query.
func (a *LSPAdapter) WorkspaceSymbols(ctx context.Context, path, query string) ([]viewer.Symbol, error) {
	target := path
	if _, ok := a.b.specFor(path); !ok {
		target = a.runningProbe()
	}
	if target == "" {
		return nil, errors.New("no language server running")
	}
	c, enc, err := a.client(ctx, target)
	if err != nil {
		return nil, err
	}
	syms, err := c.WorkspaceSymbols(ctx, query)
	if err != nil {
		return nil, err
	}
	return a.convertSymbols(syms, "", enc, false), nil
}

// runningProbe is a path inside the root of the first running server, which
// resolves to that server.
func (a *LSPAdapter) runningProbe() string {
	exts := map[string]string{}
	for _, s := range a.b.specs() {
		if len(s.Extensions) > 0 {
			exts[s.Name] = s.Extensions[0]
		}
	}
	for _, ev := range a.b.status() {
		if (ev.State == lsp.StateReady || ev.State == lsp.StateIndexing) && ev.Root != "" && exts[ev.Server] != "" {
			return filepath.Join(ev.Root, "deck-probe"+exts[ev.Server])
		}
	}
	return ""
}

// Document fetches a virtual document such as csharp-ls decompiled metadata.
func (a *LSPAdapter) Document(ctx context.Context, uri string) (viewer.VirtualDoc, error) {
	c, err := a.b.clientFor(ctx, uri)
	if err != nil {
		return viewer.VirtualDoc{}, err
	}
	md, err := c.Metadata(ctx, uri)
	if err != nil {
		return viewer.VirtualDoc{}, err
	}
	if md.Source == "" {
		return viewer.VirtualDoc{}, errors.New("no source for " + uri)
	}
	a.mu.Lock()
	a.virtual[uri] = splitLines(md.Source)
	a.mu.Unlock()
	title := md.SymbolName
	if title == "" {
		title = filepath.Base(uri)
	}
	return viewer.VirtualDoc{Title: "[metadata] " + title, Text: md.Source}, nil
}

// Reload re-sends path to a server that already runs for it; it never starts one.
func (a *LSPAdapter) Reload(ctx context.Context, path string) error {
	spec, ok := a.b.specFor(path)
	if !ok {
		return nil
	}
	running := false
	for _, ev := range a.b.status() {
		if ev.Server == spec.Name && (ev.State == lsp.StateReady || ev.State == lsp.StateIndexing) && virtualOrUnder(path, ev.Root) {
			running = true
		}
	}
	if !running {
		return nil
	}
	c, err := a.b.clientFor(ctx, path)
	if err != nil {
		return err
	}
	return c.Reload(ctx, path)
}

var messageLevels = map[lsp.MessageType]ui.Level{
	lsp.MessageError:   ui.LevelError,
	lsp.MessageWarning: ui.LevelWarn,
}

// NextEvent blocks for the next server event, converted for the Files tab.
func (a *LSPAdapter) NextEvent() (any, bool) {
	for ev := range a.b.events() {
		if out, ok := a.convertEvent(ev); ok {
			return out, true
		}
	}
	return nil, false
}

func (a *LSPAdapter) convertEvent(ev any) (any, bool) {
	switch ev := ev.(type) {
	case lsp.StatusEvent:
		return a.statusOf(ev), true
	case lsp.DiagnosticsEvent:
		path := ev.Path
		if path == "" {
			path = ev.URI
		}
		a.mu.Lock()
		enc, ok := a.enc[ev.Server]
		a.mu.Unlock()
		if !ok {
			enc = lsp.UTF16
		}
		src := &lineSource{a: a, files: map[string][]string{}}
		ds := make([]viewer.Diagnostic, len(ev.Diagnostics))
		for i, d := range ev.Diagnostics {
			ds[i] = viewer.Diagnostic{
				Range: src.rng(path, d.Range, enc), Severity: viewer.Severity(d.Severity),
				Message: d.Message, Source: d.Source,
			}
		}
		return DiagnosticsEvent{Path: path, Diagnostics: ds}, true
	case lsp.LogEvent:
		return LogEvent{Server: ev.Server, Text: ev.Message}, true
	case lsp.ShowMessageEvent:
		if ev.Type == lsp.MessageLog {
			return LogEvent{Server: ev.Server, Text: ev.Message}, true
		}
		level, ok := messageLevels[ev.Type]
		if !ok {
			level = ui.LevelInfo
		}
		return MessageEvent{Server: serverLabel(ev.Server), Level: level, Text: ev.Message}, true
	}
	return nil, false
}

// Logs returns each server's captured stderr, one line per entry.
func (a *LSPAdapter) Logs() []string {
	var out []string
	for _, l := range a.b.logs() {
		out = append(out, "── "+l.Server+" · "+l.Root+" (stderr)")
		for _, line := range strings.Split(strings.TrimRight(l.Stderr, "\n"), "\n") {
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

func (a *LSPAdapter) Prestart(ctx context.Context, cwd string) error { return a.b.prestart(ctx, cwd) }

func (a *LSPAdapter) Shutdown(ctx context.Context) error { return a.b.shutdown(ctx) }

// Install runs server's install command and lets it start again on success.
func (a *LSPAdapter) Install(ctx context.Context, server string) (string, error) {
	return a.b.install(ctx, server)
}
