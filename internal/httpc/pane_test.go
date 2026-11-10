package httpc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

func kp(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "ctrl+e":
		return tea.KeyMsg{Type: tea.KeyCtrlE}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(p Pane, keys ...string) Pane {
	for _, k := range keys {
		p, _ = p.Update(kp(k))
	}
	return p
}

func typeText(p Pane, s string) Pane {
	for _, r := range s {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return p
}

func sizedPane() Pane {
	p := NewPane("dracula")
	p.SetSize(100, 40)
	return p
}

func plain(p Pane) string { return ansi.Strip(p.View()) }

func TestRespMsgRoutesToHTTP(t *testing.T) {
	for _, m := range []ui.Routed{RespMsg{}, LoadRequestMsg{}, httpFilesMsg{}, requestLoadedMsg{}} {
		if got := m.RouteTo(); got != ui.DestHTTP {
			t.Errorf("%T routes to %v, want DestHTTP", m, got)
		}
	}
}

func TestViewIsExactlyTheOuterBox(t *testing.T) {
	for _, size := range [][2]int{{60, 11}, {80, 19}, {120, 35}, {200, 45}} {
		for _, sidebar := range []bool{false, true} {
			p := NewPane("dracula")
			p.SetSize(size[0], size[1])
			p, _ = p.Update(RespMsg{status: "200 OK", code: 200, contentType: "text/plain",
				body: strings.Repeat(strings.Repeat("x", 300)+"\n", 200), truncated: true})
			if sidebar {
				p, _ = p.Update(httpFilesMsg{items: []sidebarItem{{file: strings.Repeat("long/", 30) + "a.http", req: fileRequest{Method: "GET", URL: strings.Repeat("u", 300)}}}})
				p = press(p, "ctrl+o")
			}
			v := p.View()
			if got := lipgloss.Height(v); got != size[1] {
				t.Errorf("%v sidebar=%v: height = %d, want %d", size, sidebar, got, size[1])
			}
			if got := lipgloss.Width(v); got > size[0] {
				t.Errorf("%v sidebar=%v: width = %d, want <= %d", size, sidebar, got, size[0])
			}
		}
	}
}

func TestResponseHeightFollowsPane(t *testing.T) {
	p := NewPane("dracula")
	p.SetSize(100, 40)
	tall := p.resp.Height
	p.SetSize(100, 16)
	if p.resp.Height >= tall || p.resp.Height < 1 {
		t.Errorf("response height %d (was %d) should shrink with the pane and stay >= 1", p.resp.Height, tall)
	}
	p.SetSize(100, 5)
	if p.resp.Height != 1 {
		t.Errorf("response height at a tiny pane = %d, want the minimum of 1", p.resp.Height)
	}
	if v := ansi.Strip(p.View()); lipgloss.Height(v) != 5 {
		t.Errorf("tiny pane view height = %d, want 5", lipgloss.Height(v))
	}
}

func TestNormalModeNotCapturing(t *testing.T) {
	p := sizedPane()
	if p.Capturing() {
		t.Error("the HTTP tab should start in normal mode")
	}
	p = press(p, "x", "y")
	if p.url.Value() != "" {
		t.Error("normal-mode keys should not type into the URL")
	}
}

func TestIEntersInsert(t *testing.T) {
	p := press(sizedPane(), "i")
	if !p.Capturing() {
		t.Fatal("i should enter insert mode")
	}
	p = typeText(p, "localhost:5000")
	if p.url.Value() != "localhost:5000" {
		t.Errorf("url = %q, want typed text", p.url.Value())
	}
}

func TestEscToNormal(t *testing.T) {
	p := press(sizedPane(), "i", "esc")
	if p.Capturing() {
		t.Error("esc should return to normal mode")
	}
}

func TestCtrlNMovesFieldsInInsert(t *testing.T) {
	p := press(sizedPane(), "i", "ctrl+n")
	if p.focus != focusHeaders || !p.Capturing() {
		t.Fatalf("focus = %d, want headers in insert mode", p.focus)
	}
	p = typeText(p, "X-A: 1")
	if p.headers.Value() != "X-A: 1" {
		t.Errorf("headers = %q", p.headers.Value())
	}
}

func TestMethodCycles(t *testing.T) {
	p := sizedPane()
	p.setFocus(focusMethod)
	p = press(p, "right")
	if p.method != "POST" {
		t.Errorf("method = %q, want POST after →", p.method)
	}
}

func longResponse() RespMsg {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return RespMsg{seq: 0, status: "200 OK", code: 200, contentType: "text/plain", body: b.String(),
		headers: http.Header{"X-Test": {"yes"}}}
}

func TestResponseScrollsJK(t *testing.T) {
	p, _ := sizedPane().Update(longResponse())
	p = press(p, "j", "j")
	if p.resp.YOffset != 2 {
		t.Errorf("offset = %d after jj, want 2", p.resp.YOffset)
	}
	p = press(p, "k")
	if p.resp.YOffset != 1 {
		t.Errorf("offset = %d after k, want 1", p.resp.YOffset)
	}
	p = press(p, "G")
	if !p.resp.AtBottom() {
		t.Error("G should go to the bottom")
	}
	p = press(p, "g")
	if p.resp.YOffset != 0 {
		t.Error("g should go to the top")
	}
}

func TestWheelScrollsResponse(t *testing.T) {
	p, _ := sizedPane().Update(longResponse())
	p, _ = p.Update(tea.MouseMsg{X: 10, Y: 30, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if p.resp.YOffset != 3 {
		t.Errorf("offset = %d, want 3 after one wheel notch", p.resp.YOffset)
	}
}

func TestClickFieldEntersInsert(t *testing.T) {
	p := sizedPane()
	p, _ = p.Update(tea.MouseMsg{X: 10, Y: bodyTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if p.focus != focusBody || !p.Capturing() {
		t.Errorf("focus = %d capturing = %v, want body in insert mode", p.focus, p.Capturing())
	}
}

func TestHeadersTab(t *testing.T) {
	p, _ := sizedPane().Update(longResponse())
	if strings.Contains(plain(p), "X-Test: yes") {
		t.Fatal("the body tab is shown first")
	}
	p = press(p, "]")
	if !strings.Contains(plain(p), "X-Test: yes") {
		t.Errorf("] should show the response headers:\n%s", plain(p))
	}
	p = press(p, "[")
	if !strings.Contains(plain(p), "line 0") {
		t.Error("[ should return to the body")
	}
}

func TestTruncatedBadge(t *testing.T) {
	p, _ := sizedPane().Update(RespMsg{status: "200 OK", code: 200, body: "x", truncated: true})
	if !strings.Contains(plain(p), "truncated at 1 MiB") {
		t.Errorf("view should flag a truncated body:\n%s", plain(p))
	}
}

func TestAutoContentTypeShown(t *testing.T) {
	p := press(sizedPane(), "i")
	p = typeText(p, "localhost:1")
	p.body.SetValue(`{"a":1}`)
	p, _ = p.send()
	if !strings.Contains(plain(p), "Content-Type: application/json (auto)") {
		t.Errorf("the auto header should be shown:\n%s", plain(p))
	}
	if p.cancel != nil {
		p.cancel()
	}
}

func TestEscCancelsInFlight(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	p := press(sizedPane(), "i")
	p = typeText(p, srv.URL)
	p = press(p, "esc")
	p, cmd := p.Update(kp("ctrl+s"))
	if !p.sending || cmd == nil {
		t.Fatal("ctrl+s should send")
	}
	got := make(chan tea.Msg, 1)
	go func() {
		for _, m := range runCmds(cmd) {
			if r, ok := m.(RespMsg); ok {
				got <- r
			}
		}
	}()
	p = press(p, "esc")
	if p.sending || !strings.Contains(plain(p), "cancelled") {
		t.Errorf("esc should cancel: sending=%v\n%s", p.sending, plain(p))
	}
	select {
	case m := <-got:
		p, _ = p.Update(m)
		if !strings.Contains(plain(p), "cancelled") {
			t.Error("the cancelled request's late response should be ignored")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the request was not cancelled")
	}
}

func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmds(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestSendResolvesVariablesFromLoadedFile(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "Api.http")
	src := "@host = " + srv.URL + "\n\nGET {{host}}/weatherforecast/\nAuthorization: Bearer {{token}}\n\n###\nGET {{host}}/second\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"token":"t-dev"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := sizedPane()
	p, cmd := p.Update(LoadRequestMsg{Path: path})
	var focused bool
	for _, m := range runCmds(cmd) {
		if f, ok := m.(ui.FocusTabMsg); ok && f.Dest == ui.DestHTTP {
			focused = true
		}
		p, _ = p.Update(m)
	}
	if !focused {
		t.Error("loading a request should focus the HTTP tab")
	}
	if p.url.Value() != "{{host}}/weatherforecast/" || p.env != "dev" {
		t.Fatalf("url = %q env = %q", p.url.Value(), p.env)
	}
	p, cmd = p.send()
	for _, m := range runCmds(cmd) {
		p, _ = p.Update(m)
	}
	if gotPath != "/weatherforecast/" || gotAuth != "Bearer t-dev" {
		t.Errorf("server saw path %q auth %q", gotPath, gotAuth)
	}
	p = press(p, "ctrl+e")
	if p.env != "" {
		t.Errorf("ctrl+e should cycle to no environment, got %q", p.env)
	}
}

func TestLoadRequestPicksBlockAtLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.http")
	_ = os.WriteFile(path, []byte("GET http://h/1\n\n###\nPOST http://h/2\nX-A: 1\n\nbody\n"), 0o644)
	p := sizedPane()
	_, cmd := p.Update(LoadRequestMsg{Path: path, Line: 5})
	for _, m := range runCmds(cmd) {
		p, _ = p.Update(m)
	}
	if p.method != "POST" || p.url.Value() != "http://h/2" || p.headers.Value() != "X-A: 1" || p.body.Value() != "body" {
		t.Errorf("loaded %s %q headers %q body %q", p.method, p.url.Value(), p.headers.Value(), p.body.Value())
	}
}

func TestSidebarListsAndLoads(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "src", "Api"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "node_modules", "x"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "src", "Api", "Api.http"), []byte("GET http://h/a\n\n###\n# @name second\nGET http://h/b\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "node_modules", "x", "x.http"), []byte("GET http://h/skip\n"), 0o644)
	msg := discoverCmd(root)().(httpFilesMsg)
	if len(msg.items) != 2 {
		t.Fatalf("items = %+v, want the two requests outside node_modules", msg.items)
	}
	p := sizedPane()
	p, _ = p.Update(msg)
	p = press(p, "ctrl+o")
	if !p.Capturing() || !strings.Contains(plain(p), "second") {
		t.Fatalf("ctrl+o should open the sidebar:\n%s", plain(p))
	}
	p, cmd := p.Update(kp("j"))
	p, cmd = p.Update(kp("enter"))
	for _, m := range runCmds(cmd) {
		p, _ = p.Update(m)
	}
	if p.sidebar.open || p.url.Value() != "http://h/b" {
		t.Errorf("enter should load the request and close: open=%v url=%q", p.sidebar.open, p.url.Value())
	}
}

func TestDiscoverBounded(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c", "d", "e")
	_ = os.MkdirAll(deep, 0o755)
	_ = os.WriteFile(filepath.Join(deep, "deep.http"), []byte("GET http://h/x\n"), 0o644)
	if got := discoverCmd(root)().(httpFilesMsg); len(got.items) != 0 {
		t.Errorf("items = %+v, want files beyond the depth limit skipped", got.items)
	}
}
