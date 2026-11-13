package ide

import (
	"context"
	"encoding/json"
	"errors"

	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/presence"
)

type harness struct {
	t      *testing.T
	b      *Bridge
	store  *presence.Store
	root   string
	cfgDir string
	mu     sync.Mutex
	sent   []tea.Msg
	reply  control.Reply
}

func (h *harness) send(msg tea.Msg) {
	h.mu.Lock()
	h.sent = append(h.sent, msg)
	reply := h.reply
	h.mu.Unlock()
	if rm, ok := msg.(control.RequestMsg); ok {
		rm.Answer(reply)
	}
}

func (h *harness) messages() []tea.Msg {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]tea.Msg(nil), h.sent...)
}

func startHarness(t *testing.T) *harness {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	h := &harness{t: t, store: presence.New(), root: root, cfgDir: t.TempDir(), reply: control.Reply{OK: true}}
	b, err := Start(context.Background(), Options{
		Root: root, ConfigDir: h.cfgDir, PID: os.Getpid(), Store: h.store, Send: h.send,
		Debounce: 20 * time.Millisecond, Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.b = b
	t.Cleanup(b.Close)
	return h
}

func (h *harness) lockPath() string {
	return filepath.Join(h.cfgDir, "ide", strconv.Itoa(h.b.Port())+".lock")
}

func (h *harness) lock() lockFile {
	h.t.Helper()
	data, err := os.ReadFile(h.lockPath())
	if err != nil {
		h.t.Fatal(err)
	}
	var lf lockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		h.t.Fatal(err)
	}
	return lf
}

type rpcClient struct {
	t    *testing.T
	conn *websocket.Conn
	id   int
	note chan map[string]any
	resp chan map[string]any
}

func (h *harness) dial(header http.Header) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(h.b.Port()), &websocket.DialOptions{HTTPHeader: header})
}

func (h *harness) connect() *rpcClient {
	h.t.Helper()
	conn, _, err := h.dial(http.Header{"X-Claude-Code-Ide-Authorization": {h.lock().AuthToken}})
	if err != nil {
		h.t.Fatal(err)
	}
	c := &rpcClient{t: h.t, conn: conn, note: make(chan map[string]any, 32), resp: make(chan map[string]any, 32)}
	go func() {
		for {
			var msg map[string]any
			if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
				return
			}
			if _, ok := msg["id"]; ok {
				c.resp <- msg
			} else {
				c.note <- msg
			}
		}
	}()
	h.t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return c
}

func (c *rpcClient) call(method string, params any) map[string]any {
	c.t.Helper()
	c.id++
	req := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := wsjson.Write(context.Background(), c.conn, req); err != nil {
		c.t.Fatal(err)
	}
	select {
	case r := <-c.resp:
		return r
	case <-time.After(3 * time.Second):
		c.t.Fatalf("no response to %s", method)
		return nil
	}
}

func (c *rpcClient) tool(name string, args map[string]any) string {
	c.t.Helper()
	r := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	res, ok := r["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("tools/call %s: %v", name, r)
	}
	content := res["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}

func (c *rpcClient) nextNote(method string, wait time.Duration) (map[string]any, bool) {
	deadline := time.After(wait)
	for {
		select {
		case n := <-c.note:
			if n["method"] == method {
				return n["params"].(map[string]any), true
			}
		case <-deadline:
			return nil, false
		}
	}
}

func TestLockfileWrittenAndRemoved(t *testing.T) {
	h := startHarness(t)
	lf := h.lock()
	if lf.PID != os.Getpid() || lf.IDEName != "deck" || lf.Transport != "ws" || len(lf.AuthToken) != 32 ||
		len(lf.WorkspaceFolders) != 1 || lf.WorkspaceFolders[0] != h.root {
		t.Errorf("lockfile = %+v", lf)
	}
	if fi, _ := os.Stat(h.lockPath()); fi.Mode().Perm() != 0o600 {
		t.Errorf("lockfile mode = %v, want 0600", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Dir(h.lockPath())); fi.Mode().Perm() != 0o700 {
		t.Errorf("ide dir mode = %v, want 0700", fi.Mode().Perm())
	}
	if p := h.b.Port(); p < 10000 || p > 65535 {
		t.Errorf("port %d outside 10000-65535", p)
	}
	h.b.Close()
	if _, err := os.Stat(h.lockPath()); !os.IsNotExist(err) {
		t.Error("Close should remove the lockfile")
	}
}

func TestSweepDeadDeckLockfiles(t *testing.T) {
	dir := t.TempDir()
	ideDir := filepath.Join(dir, "ide")
	os.MkdirAll(ideDir, 0o700)
	write := func(name, ideName string, pid int) string {
		p := filepath.Join(ideDir, name)
		data, _ := json.Marshal(lockFile{PID: pid, IDEName: ideName, Transport: "ws"})
		os.WriteFile(p, data, 0o600)
		return p
	}
	dead := write("11111.lock", "deck", 999999)
	vscode := write("22222.lock", "Visual Studio Code", 999999)
	live := write("33333.lock", "deck", os.Getpid())
	sweep(ideDir)
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Error("a dead deck's lockfile should be removed")
	}
	for _, p := range []string{vscode, live} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should be kept: %v", filepath.Base(p), err)
		}
	}
}

func TestRejectsBadToken(t *testing.T) {
	h := startHarness(t)
	_, resp, err := h.dial(http.Header{"X-Claude-Code-Ide-Authorization": {"nope"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token: err %v, resp %+v; want 401", err, resp)
	}
}

func TestRejectsOrigin(t *testing.T) {
	h := startHarness(t)
	_, resp, err := h.dial(http.Header{"X-Claude-Code-Ide-Authorization": {h.lock().AuthToken}, "Origin": {"https://evil.example"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("browser origin: err %v, resp %+v; want 403", err, resp)
	}
}

func TestInitializeAndToolsList(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	init := c.call("initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}})
	res := init["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" || res["serverInfo"].(map[string]any)["name"] != "deck" {
		t.Errorf("initialize = %v", init)
	}
	list := c.call("tools/list", nil)["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, tl := range list {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	want := []string{"openFile", "getCurrentSelection", "getLatestSelection", "getOpenEditors", "getWorkspaceFolders",
		"getDiagnostics", "checkDocumentDirty", "saveDocument", "close_tab", "closeAllDiffTabs"}
	got := strings.Join(names, ",")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("tools %q lack %s", got, w)
		}
	}
	if strings.Contains(got, "executeCode") {
		t.Error("executeCode must not be listed")
	}
}

func TestUnknownMethod(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	r := c.call("nope/nope", nil)
	if e, ok := r["error"].(map[string]any); !ok || e["code"] != float64(-32601) {
		t.Errorf("unknown method = %v, want -32601", r)
	}
}

func TestGetCurrentSelectionShape(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	if text := c.tool("getCurrentSelection", nil); !strings.Contains(text, `"success":false`) {
		t.Errorf("no file open: %s", text)
	}
	path := filepath.Join(h.root, "a.go")
	os.WriteFile(path, []byte("package a\nfunc A() {}\n"), 0o644)
	h.store.Publish(presence.Snapshot{Tab: "files", File: path, Line: 2, Col: 1,
		Sel: &presence.Range{StartLine: 2, StartCol: 1, EndLine: 2, EndCol: 5}, SelText: "func"})
	var got struct {
		Success   bool   `json:"success"`
		Text      string `json:"text"`
		FilePath  string `json:"filePath"`
		Selection struct {
			Start   struct{ Line, Character int } `json:"start"`
			End     struct{ Line, Character int } `json:"end"`
			IsEmpty bool                          `json:"isEmpty"`
		} `json:"selection"`
	}
	json.Unmarshal([]byte(c.tool("getCurrentSelection", nil)), &got)
	if !got.Success || got.Text != "func" || got.FilePath != path || got.Selection.Start.Line != 1 ||
		got.Selection.End.Character != 4 || got.Selection.IsEmpty {
		t.Errorf("selection = %+v", got)
	}
}

func TestGetWorkspaceFolders(t *testing.T) {
	h := startHarness(t)
	text := h.connect().tool("getWorkspaceFolders", nil)
	if !strings.Contains(text, `"rootPath":"`+h.root+`"`) || !strings.Contains(text, `"uri":"file://`+h.root+`"`) {
		t.Errorf("workspace folders = %s", text)
	}
}

func TestGetDiagnosticsShape(t *testing.T) {
	h := startHarness(t)
	path := filepath.Join(h.root, "a.go")
	h.store.SetDiagnostics(path, []presence.Diagnostic{{Line: 2, Col: 3, EndLine: 2, EndCol: 5, Severity: "error", Message: "boom", Source: "gopls"}})
	var got []struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Message  string `json:"message"`
			Severity string `json:"severity"`
			Source   string `json:"source"`
			Range    struct {
				Start struct{ Line, Character int } `json:"start"`
			} `json:"range"`
		} `json:"diagnostics"`
	}
	json.Unmarshal([]byte(h.connect().tool("getDiagnostics", nil)), &got)
	if len(got) != 1 || got[0].URI != "file://"+path || got[0].Diagnostics[0].Severity != "Error" ||
		got[0].Diagnostics[0].Range.Start.Line != 1 || got[0].Diagnostics[0].Range.Start.Character != 2 {
		t.Errorf("diagnostics = %+v", got)
	}
}

func TestCheckDocumentDirtyFalse(t *testing.T) {
	h := startHarness(t)
	text := h.connect().tool("checkDocumentDirty", map[string]any{"filePath": "/x/a.go"})
	if !strings.Contains(text, `"isDirty":false`) || !strings.Contains(text, `"success":true`) {
		t.Errorf("checkDocumentDirty = %s", text)
	}
}

func TestSaveDocumentReadOnly(t *testing.T) {
	h := startHarness(t)
	text := h.connect().tool("saveDocument", map[string]any{"filePath": "/x/a.go"})
	if !strings.Contains(text, `"saved":false`) || !strings.Contains(text, "deck is read-only") {
		t.Errorf("saveDocument = %s", text)
	}
}

func TestCloseTabs(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	if got := c.tool("close_tab", map[string]any{"tab_name": "x"}); got != "TAB_CLOSED" {
		t.Errorf("close_tab = %q", got)
	}
	if got := c.tool("closeAllDiffTabs", nil); got != "CLOSED_0_DIFF_TABS" {
		t.Errorf("closeAllDiffTabs = %q", got)
	}
}

func TestSelectionChangedDebounced(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	path := filepath.Join(h.root, "a.go")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644)
	for line := 1; line <= 3; line++ {
		h.store.Publish(presence.Snapshot{Tab: "files", File: path, Line: line, Col: 1})
		time.Sleep(5 * time.Millisecond)
	}
	params, ok := c.nextNote("selection_changed", time.Second)
	if !ok {
		t.Fatal("no selection_changed")
	}
	sel := params["selection"].(map[string]any)
	if sel["start"].(map[string]any)["line"] != float64(2) || sel["isEmpty"] != true || params["filePath"] != path ||
		params["fileUrl"] != "file://"+path {
		t.Errorf("selection_changed = %v", params)
	}
	if extra, ok := c.nextNote("selection_changed", 100*time.Millisecond); ok {
		t.Errorf("one burst should send one notification, also got %v", extra)
	}
	h.store.Publish(presence.Snapshot{Tab: "files", File: path, Line: 3, Col: 1, Recent: []string{"x"}})
	if extra, ok := c.nextNote("selection_changed", 100*time.Millisecond); ok {
		t.Errorf("an unchanged position should not resend, got %v", extra)
	}
}

func TestSelectionChangedUTF16(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	path := filepath.Join(h.root, "e.go")
	os.WriteFile(path, []byte("😀x\n"), 0o644)
	h.store.Publish(presence.Snapshot{Tab: "files", File: path, Line: 1, Col: 2})
	params, ok := c.nextNote("selection_changed", time.Second)
	if !ok {
		t.Fatal("no selection_changed")
	}
	if ch := params["selection"].(map[string]any)["start"].(map[string]any)["character"]; ch != float64(2) {
		t.Errorf("character = %v, want 2 UTF-16 units after the emoji", ch)
	}
}

func TestUTF16Col(t *testing.T) {
	for _, tc := range []struct {
		line string
		col  int
		want int
	}{{"abc", 2, 2}, {"😀x", 1, 2}, {"😀x", 2, 3}, {"é", 1, 1}, {"ab", 5, 5}} {
		if got := utf16Col(tc.line, tc.col); got != tc.want {
			t.Errorf("utf16Col(%q, %d) = %d, want %d", tc.line, tc.col, got, tc.want)
		}
	}
}

func TestNoNotificationForRunTab(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	h.store.Publish(presence.Snapshot{Tab: "run"})
	if n, ok := c.nextNote("selection_changed", 150*time.Millisecond); ok {
		t.Errorf("the Run tab has no file to report, got %v", n)
	}
}

func TestAtMentioned(t *testing.T) {
	h := startHarness(t)
	if h.b.Mention("/x/a.go", 39, 51) {
		t.Error("Mention without a client should report false")
	}
	c := h.connect()
	waitClients(t, h.b, 1)
	if !h.b.Mention("/x/a.go", 39, 51) {
		t.Fatal("Mention with a client should report true")
	}
	params, ok := c.nextNote("at_mentioned", time.Second)
	if !ok || params["filePath"] != "/x/a.go" || params["lineStart"] != float64(39) || params["lineEnd"] != float64(51) {
		t.Errorf("at_mentioned = %v", params)
	}
}

func TestOpenFileStartTextSendsRequest(t *testing.T) {
	h := startHarness(t)
	path := filepath.Join(h.root, "m.go")
	os.WriteFile(path, []byte("package main\n\nfunc main() {\n}\n"), 0o644)
	text := h.connect().tool("openFile", map[string]any{"filePath": path, "startText": "func main", "endText": "}"})
	if text != "Opened file: "+path {
		t.Errorf("openFile = %q", text)
	}
	var req *control.Request
	for _, msg := range h.messages() {
		if rm, ok := msg.(control.RequestMsg); ok {
			req = &rm.Req
		}
	}
	want := control.Request{V: control.Version, Cmd: "open", Path: path, Line: 3, Col: 1, EndLine: 4, From: "Claude"}
	if req == nil || *req != want {
		t.Errorf("request = %+v, want %+v", req, want)
	}
}

func waitClients(t *testing.T, b *Bridge, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for b.Clients() != n {
		if time.Now().After(deadline) {
			t.Fatalf("clients = %d, want %d", b.Clients(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestClientsMsgOnConnectDisconnect(t *testing.T) {
	h := startHarness(t)
	c := h.connect()
	waitClients(t, h.b, 1)
	c.conn.Close(websocket.StatusNormalClosure, "")
	waitClients(t, h.b, 0)
	var ns []int
	for _, msg := range h.messages() {
		if cm, ok := msg.(ClientsMsg); ok {
			ns = append(ns, cm.N)
		}
	}
	if len(ns) != 2 || ns[0] != 1 || ns[1] != 0 {
		t.Errorf("ClientsMsg sequence = %v, want [1 0]", ns)
	}
}

func TestNegotiatesMCPSubprotocol(t *testing.T) {
	h := startHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(h.b.Port()), &websocket.DialOptions{
		HTTPHeader:   http.Header{"X-Claude-Code-Ide-Authorization": {h.lock().AuthToken}},
		Subprotocols: []string{"mcp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if got := conn.Subprotocol(); got != "mcp" {
		t.Errorf("subprotocol = %q, want mcp (Claude's client requires it)", got)
	}
}

func TestStartWithoutClaudeConfig(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-claude-here")
	_, err := Start(context.Background(), Options{Root: t.TempDir(), ConfigDir: missing, PID: os.Getpid(), Store: presence.New()})
	if !errors.Is(err, ErrNoClaude) {
		t.Errorf("err = %v, want ErrNoClaude", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("deck must not create Claude's config directory")
	}
}
