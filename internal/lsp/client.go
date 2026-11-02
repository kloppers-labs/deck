package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrShutdown = errors.New("lsp: manager shut down")
	ErrDisabled = errors.New("lsp: disabled")
)

const (
	maxRestarts = 1
	openDocsMax = 30
	stderrSize  = 64 * 1024
)

type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }

type timings struct {
	backoff      time.Duration
	shutdownWait time.Duration
	exitWait     time.Duration
	initTimeout  time.Duration
	readyAfter   time.Duration
}

func defaultTimings() timings {
	return timings{
		backoff:      time.Second,
		shutdownWait: time.Second,
		exitWait:     time.Second,
		initTimeout:  60 * time.Second,
		readyAfter:   2 * time.Second,
	}
}

type openDoc struct {
	path    string
	version int
	mtime   time.Time
	size    int64
}

// Client owns one server process for a (spec, root) pair and restarts it once after a crash.
type Client struct {
	res     Resolved
	starter Starter
	clock   clock
	timing  timings
	timeout time.Duration
	emit    func(any)
	stderr  *Ring

	startMu sync.Mutex
	docMu   sync.Mutex
	docs    []*openDoc

	mu           sync.Mutex
	state        State
	err          error
	since        time.Time
	conn         *Conn
	proc         Process
	gen          int
	encoding     Encoding
	active       map[string]bool
	title        string
	message      string
	percent      int
	restarts     int
	crashedAt    time.Time
	shuttingDown bool
}

func newClient(res Resolved, starter Starter, clk clock, t timings, timeout time.Duration, emit func(any)) *Client {
	return &Client{
		res: res, starter: starter, clock: clk, timing: t, timeout: timeout, emit: emit,
		stderr: NewRing(stderrSize), encoding: UTF16, percent: -1, since: clk.Now(),
	}
}

func (c *Client) Name() string   { return c.res.Spec.Name }
func (c *Client) Root() string   { return c.res.Root }
func (c *Client) Stderr() string { return c.stderr.String() }

func (c *Client) Encoding() Encoding {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoding
}

func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Client) Status() StatusEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

func (c *Client) statusLocked() StatusEvent {
	ev := StatusEvent{
		Server: c.res.Spec.Name, Root: c.res.Root, State: c.state,
		Title: c.title, Message: c.message, Percent: c.percent, Err: c.err, Since: c.since,
	}
	if c.state == StateFailed {
		ev.Hint = c.res.Spec.InstallHint
	}
	return ev
}

func (c *Client) setStateLocked(s State, err error) {
	if c.state != s {
		c.since = c.clock.Now()
	}
	c.state, c.err = s, err
	c.emit(c.statusLocked())
}

func (c *Client) setState(s State, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStateLocked(s, err)
}

func (c *Client) ensureRunning() (*Conn, error) {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	c.mu.Lock()
	state, conn, err, crashedAt := c.state, c.conn, c.err, c.crashedAt
	if c.shuttingDown {
		c.mu.Unlock()
		return nil, ErrShutdown
	}
	if state == StateExited {
		c.restarts++
	}
	c.mu.Unlock()
	switch state {
	case StateIndexing, StateReady:
		return conn, nil
	case StateFailed:
		return nil, err
	case StateStopped:
		return nil, ErrShutdown
	case StateExited:
		if wait := c.timing.backoff - c.clock.Now().Sub(crashedAt); wait > 0 {
			time.Sleep(wait)
		}
	}
	return c.start()
}

func (c *Client) start() (*Conn, error) {
	c.setState(StateStarting, nil)
	proc, err := c.starter(Command{Path: c.res.Binary, Args: c.res.Args, Dir: c.res.Root, Stderr: c.stderr})
	if err != nil {
		c.setState(StateFailed, err)
		return nil, err
	}
	conn := NewConn(proc, ConnOptions{OnRequest: c.onRequest, OnNotify: c.onNotify})
	c.mu.Lock()
	if c.shuttingDown {
		c.mu.Unlock()
		_ = proc.Kill()
		conn.Close()
		return nil, ErrShutdown
	}
	c.gen++
	gen := c.gen
	c.conn, c.proc = conn, proc
	c.active = map[string]bool{}
	c.title, c.message, c.percent = "", "", -1
	c.mu.Unlock()
	c.resetDocs()

	ctx, cancel := context.WithTimeout(context.Background(), c.timing.initTimeout)
	defer cancel()
	var init InitializeResult
	if err := conn.Call(ctx, "initialize", c.initializeParams(), &init); err != nil {
		_ = proc.Kill()
		conn.Close()
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.shuttingDown {
			return nil, ErrShutdown
		}
		err = fmt.Errorf("initialize %s: %w", c.res.Spec.Name, err)
		c.conn, c.proc = nil, nil
		c.setStateLocked(StateFailed, err)
		return nil, err
	}
	c.mu.Lock()
	c.encoding = negotiatedEncoding(init.Capabilities.PositionEncoding)
	c.setStateLocked(StateIndexing, nil)
	c.mu.Unlock()
	if err := conn.Notify("initialized", struct{}{}); err != nil {
		return nil, err
	}
	c.clock.AfterFunc(c.timing.readyAfter, func() { c.readyTimeout(gen) })
	go c.watch(conn, proc, gen)
	return conn, nil
}

func (c *Client) watch(conn *Conn, proc Process, gen int) {
	select {
	case <-conn.Done():
	case <-proc.Exited():
		conn.Close()
	}
	c.mu.Lock()
	if c.gen != gen || c.shuttingDown {
		c.mu.Unlock()
		return
	}
	c.conn, c.proc = nil, nil
	c.crashedAt = c.clock.Now()
	c.title, c.message, c.percent = "", "", -1
	if c.restarts >= maxRestarts {
		c.setStateLocked(StateFailed, ErrServerExited)
	} else {
		c.setStateLocked(StateExited, ErrServerExited)
	}
	c.mu.Unlock()
	_ = proc.Kill()
	c.resetDocs()
}

func (c *Client) readyTimeout(gen int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen == gen && c.state == StateIndexing && len(c.active) == 0 {
		c.setStateLocked(StateReady, nil)
	}
}

func (c *Client) initializeParams() InitializeParams {
	root := PathToURI(c.res.Root)
	return InitializeParams{
		ProcessID:             os.Getpid(),
		ClientInfo:            ClientInfo{Name: "deck"},
		RootURI:               root,
		RootPath:              c.res.Root,
		WorkspaceFolders:      []WorkspaceFolder{{URI: root, Name: filepath.Base(c.res.Root)}},
		InitializationOptions: c.res.InitOptions,
		Capabilities: map[string]any{
			"general": map[string]any{"positionEncodings": []Encoding{UTF8, UTF16}},
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"dynamicRegistration": false},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": true},
				"declaration":        map[string]any{"linkSupport": true},
				"typeDefinition":     map[string]any{"linkSupport": true},
				"implementation":     map[string]any{"linkSupport": true},
				"references":         map[string]any{},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"publishDiagnostics": map[string]any{"versionSupport": true},
			},
			"workspace": map[string]any{"configuration": true, "workspaceFolders": true, "symbol": map[string]any{}},
			"window":    map[string]any{"workDoneProgress": true, "showMessage": map[string]any{}},
		},
	}
}

func (c *Client) onRequest(_ context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "workspace/configuration":
		var p ConfigurationParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		out := make([]any, len(p.Items))
		for i, item := range p.Items {
			out[i] = c.res.Spec.Settings[item.Section]
		}
		return out, nil
	case "window/workDoneProgress/create":
		var p WorkDoneProgressCreateParams
		if err := json.Unmarshal(params, &p); err == nil {
			c.mu.Lock()
			if c.active != nil {
				c.active[string(p.Token)] = true
			}
			c.mu.Unlock()
		}
		return nil, nil
	case "client/registerCapability", "client/unregisterCapability", "window/showMessageRequest":
		return nil, nil
	case "workspace/workspaceFolders":
		return []WorkspaceFolder{{URI: PathToURI(c.res.Root), Name: filepath.Base(c.res.Root)}}, nil
	}
	return nil, ErrMethodNotFound
}

func (c *Client) onNotify(method string, params json.RawMessage) {
	name := c.res.Spec.Name
	switch method {
	case "$/progress":
		c.onProgress(params)
	case "textDocument/publishDiagnostics":
		var p PublishDiagnosticsParams
		if json.Unmarshal(params, &p) != nil {
			return
		}
		path, _ := URIToPath(p.URI)
		c.emit(DiagnosticsEvent{Server: name, URI: p.URI, Path: path, Version: p.Version, Diagnostics: p.Diagnostics})
	case "window/logMessage":
		var p ShowMessageParams
		if json.Unmarshal(params, &p) == nil {
			c.emit(LogEvent{Server: name, Type: p.Type, Message: p.Message})
		}
	case "window/showMessage":
		var p ShowMessageParams
		if json.Unmarshal(params, &p) == nil {
			c.emit(ShowMessageEvent{Server: name, Type: p.Type, Message: p.Message})
		}
	}
}

func (c *Client) onProgress(params json.RawMessage) {
	var p ProgressParams
	var v WorkDoneProgress
	if json.Unmarshal(params, &p) != nil || json.Unmarshal(p.Value, &v) != nil {
		return
	}
	token := string(p.Token)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		return
	}
	switch v.Kind {
	case "begin":
		c.active[token] = true
		c.title, c.message, c.percent = v.Title, v.Message, percentOf(v.Percentage)
		if c.state == StateReady {
			c.setStateLocked(StateIndexing, nil)
			return
		}
	case "report":
		if v.Message != "" {
			c.message = v.Message
		}
		if v.Percentage != nil {
			c.percent = *v.Percentage
		}
	case "end":
		delete(c.active, token)
		if len(c.active) == 0 {
			c.title, c.message, c.percent = "", "", -1
			if c.state == StateIndexing {
				c.setStateLocked(StateReady, nil)
				return
			}
		}
	default:
		return
	}
	c.emit(c.statusLocked())
}

func percentOf(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func (c *Client) shutdown(ctx context.Context) {
	c.mu.Lock()
	c.shuttingDown = true
	conn, proc, state := c.conn, c.proc, c.state
	c.mu.Unlock()
	if proc == nil {
		c.setState(StateStopped, nil)
		return
	}
	if state == StateIndexing || state == StateReady {
		sctx, cancel := context.WithTimeout(ctx, c.timing.shutdownWait)
		_ = conn.Call(sctx, "shutdown", nil, nil)
		cancel()
		_ = conn.Notify("exit", nil)
		select {
		case <-proc.Exited():
		case <-time.After(c.timing.exitWait):
		case <-ctx.Done():
		}
	}
	_ = proc.Kill()
	_ = conn.Close()
	c.setState(StateStopped, nil)
}

func (c *Client) resetDocs() {
	c.docMu.Lock()
	c.docs = nil
	c.docMu.Unlock()
}

func (c *Client) findDoc(path string) int {
	for i, d := range c.docs {
		if d.path == path {
			return i
		}
	}
	return -1
}

func (c *Client) ensureOpen(conn *Conn, path string) (string, error) {
	if IsVirtual(path) {
		return path, nil
	}
	uri := PathToURI(path)
	c.docMu.Lock()
	defer c.docMu.Unlock()
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if i := c.findDoc(path); i >= 0 {
		d := c.docs[i]
		c.docs = append(append(c.docs[:i:i], c.docs[i+1:]...), d)
		if d.mtime.Equal(fi.ModTime()) && d.size == fi.Size() {
			return uri, nil
		}
		return uri, c.sendChange(conn, d)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	d := &openDoc{path: path, version: 1, mtime: fi.ModTime(), size: fi.Size()}
	err = conn.Notify("textDocument/didOpen", DidOpenTextDocumentParams{TextDocument: TextDocumentItem{
		URI: uri, LanguageID: LanguageID(path), Version: d.version, Text: string(text),
	}})
	if err != nil {
		return "", err
	}
	c.docs = append(c.docs, d)
	if len(c.docs) > openDocsMax {
		old := c.docs[0]
		c.docs = c.docs[1:]
		_ = conn.Notify("textDocument/didClose", DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: PathToURI(old.path)}})
	}
	return uri, nil
}

func (c *Client) sendChange(conn *Conn, d *openDoc) error {
	fi, err := os.Stat(d.path)
	if err != nil {
		return err
	}
	text, err := os.ReadFile(d.path)
	if err != nil {
		return err
	}
	d.version++
	d.mtime, d.size = fi.ModTime(), fi.Size()
	return conn.Notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: PathToURI(d.path), Version: d.version},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: string(text)}},
	})
}

// Open sends didOpen for path unless the server already has it.
func (c *Client) Open(ctx context.Context, path string) error {
	conn, err := c.ensureRunning()
	if err != nil {
		return err
	}
	_, err = c.ensureOpen(conn, path)
	return err
}

// Reload re-sends an open document's full text from disk; unopened documents are left alone.
func (c *Client) Reload(ctx context.Context, path string) error {
	conn, err := c.ensureRunning()
	if err != nil {
		return err
	}
	c.docMu.Lock()
	defer c.docMu.Unlock()
	if i := c.findDoc(path); i >= 0 {
		return c.sendChange(conn, c.docs[i])
	}
	return nil
}

func (c *Client) call(ctx context.Context, path, method string, params func(uri string) any) (json.RawMessage, error) {
	conn, err := c.ensureRunning()
	if err != nil {
		return nil, err
	}
	uri := ""
	if path != "" {
		if uri, err = c.ensureOpen(conn, path); err != nil {
			return nil, err
		}
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	var raw json.RawMessage
	err = conn.Call(ctx, method, params(uri), &raw)
	return raw, err
}

func positionParams(pos Position) func(string) any {
	return func(uri string) any {
		return TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos}
	}
}

func (c *Client) locations(ctx context.Context, method, path string, pos Position) ([]Location, error) {
	raw, err := c.call(ctx, path, method, positionParams(pos))
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw)
}

func (c *Client) Definition(ctx context.Context, path string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/definition", path, pos)
}

func (c *Client) TypeDefinition(ctx context.Context, path string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/typeDefinition", path, pos)
}

func (c *Client) Implementation(ctx context.Context, path string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/implementation", path, pos)
}

func (c *Client) References(ctx context.Context, path string, pos Position, includeDeclaration bool) ([]Location, error) {
	raw, err := c.call(ctx, path, "textDocument/references", func(uri string) any {
		p := ReferenceParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos,
		}}
		p.Context.IncludeDeclaration = includeDeclaration
		return p
	})
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw)
}

func (c *Client) Hover(ctx context.Context, path string, pos Position) (Hover, error) {
	raw, err := c.call(ctx, path, "textDocument/hover", positionParams(pos))
	if err != nil {
		return Hover{}, err
	}
	return decodeHover(raw)
}

func (c *Client) DocumentSymbols(ctx context.Context, path string) ([]Symbol, error) {
	var docURI string
	raw, err := c.call(ctx, path, "textDocument/documentSymbol", func(uri string) any {
		docURI = uri
		return map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}}
	})
	if err != nil {
		return nil, err
	}
	return decodeSymbols(raw, docURI)
}

func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]Symbol, error) {
	raw, err := c.call(ctx, "", "workspace/symbol", func(string) any { return map[string]string{"query": query} })
	if err != nil {
		return nil, err
	}
	return decodeWorkspaceSymbols(raw)
}

// MetadataDocument is csharp-ls's decompiled source for a csharp:/metadata URI.
type MetadataDocument struct {
	ProjectName  string `json:"projectName"`
	AssemblyName string `json:"assemblyName"`
	SymbolName   string `json:"symbolName"`
	Source       string `json:"source"`
}

func (c *Client) Metadata(ctx context.Context, uri string) (MetadataDocument, error) {
	var md MetadataDocument
	raw, err := c.call(ctx, "", "csharp/metadata", func(string) any {
		return map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}}
	})
	if err != nil || isNull(raw) {
		return md, err
	}
	err = json.Unmarshal(raw, &md)
	return md, err
}
