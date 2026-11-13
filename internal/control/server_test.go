package control

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/presence"
)

func serveForTest(t *testing.T, sock string, query func(Request) Reply, send func(tea.Msg)) func() {
	t.Helper()
	if query == nil {
		query = func(Request) Reply { return Reply{OK: true} }
	}
	if send == nil {
		send = func(tea.Msg) {}
	}
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ServeListener(ctx, ln, query, send)
		close(done)
	}()
	return func() {
		cancel()
		<-done
	}
}

func TestPickOrder(t *testing.T) {
	at := func(s int) time.Time { return time.Unix(int64(s), 0) }
	insts := []Instance{
		{PID: 1, Root: "/r", Started: at(1)},
		{PID: 2, Root: "/r", Started: at(2), Focused: at(5)},
		{PID: 3, Root: "/r", Started: at(9)},
		{PID: 4, Root: "/r/nested", Started: at(1)},
		{PID: 5, Root: "/other", Started: at(1)},
	}
	cases := []struct {
		name         string
		pid          int
		root, target string
		want         int
	}{
		{"explicit pid wins", 5, "/r", "/r/a.go", 5},
		{"exact root, most recently focused", 0, "/r", "/r/a.go", 2},
		{"exact nested root beats its parent", 0, "/r/nested", "/r/nested/x.go", 4},
		{"deepest root containing the target", 0, "/elsewhere", "/r/nested/x.go", 4},
		{"root containing the target", 0, "/elsewhere", "/r/a.go", 2},
		{"nothing matches", 0, "/nope", "/nope/x", 0},
		{"unknown pid", 77, "/r", "/r/a.go", 0},
	}
	for _, tc := range cases {
		got, ok := Pick(insts, tc.pid, tc.root, tc.target)
		if tc.want == 0 {
			if ok {
				t.Errorf("%s: picked %d, want none", tc.name, got.PID)
			}
			continue
		}
		if !ok || got.PID != tc.want {
			t.Errorf("%s: picked %d (%v), want %d", tc.name, got.PID, ok, tc.want)
		}
	}
	tie := []Instance{{PID: 1, Root: "/r", Started: at(1)}, {PID: 2, Root: "/r", Started: at(3)}}
	if got, _ := Pick(tie, 0, "/r", "/r"); got.PID != 2 {
		t.Errorf("unfocused tie: picked %d, want the newer start 2", got.PID)
	}
}

func TestServeRoundTripQuery(t *testing.T) {
	sock, _ := SocketPath(shortDir(t), 1)
	snap := presence.Snapshot{Tab: "files", File: "/r/a.go", Line: 3}
	stop := serveForTest(t, sock, func(r Request) Reply {
		if r.Cmd != "where" {
			t.Errorf("query got %q", r.Cmd)
		}
		return Reply{OK: true, Where: &snap}
	}, nil)
	defer stop()
	got, err := Call(sock, Request{V: 1, Cmd: "where"}, time.Second)
	if err != nil || !got.OK || got.Where == nil || got.Where.Line != 3 {
		t.Errorf("Call = %+v, %v", got, err)
	}
}

func TestServeNavigateViaSend(t *testing.T) {
	sock, _ := SocketPath(shortDir(t), 1)
	stop := serveForTest(t, sock, nil, func(msg tea.Msg) {
		rm, ok := msg.(RequestMsg)
		if !ok {
			t.Errorf("send got %T", msg)
			return
		}
		if rm.Req.Path != "/r/a.go" || rm.Req.Line != 4 || rm.Req.From != "Claude" {
			t.Errorf("request = %+v", rm.Req)
		}
		rm.Reply <- Reply{OK: true, Message: "opened a.go:4"}
	})
	defer stop()
	got, err := Call(sock, Request{V: 1, Cmd: "open", Path: "/r/a.go", Line: 4, From: "Claude"}, time.Second)
	if err != nil || !got.OK || got.Message != "opened a.go:4" {
		t.Errorf("Call = %+v, %v", got, err)
	}
}

func TestServeBusyTimeout(t *testing.T) {
	prev := navTimeout
	navTimeout = 50 * time.Millisecond
	t.Cleanup(func() { navTimeout = prev })
	sock, _ := SocketPath(shortDir(t), 1)
	block := make(chan struct{})
	defer close(block)
	var deadline time.Time
	sent := make(chan struct{}, 1)
	stop := serveForTest(t, sock, nil, func(msg tea.Msg) {
		deadline = msg.(RequestMsg).Deadline
		sent <- struct{}{}
		<-block
	})
	defer stop()
	start := time.Now()
	got, err := Call(sock, Request{V: 1, Cmd: "open", Path: "/r/a.go"}, time.Second)
	if err != nil || got.OK || got.Error != "deck is busy: editor open" {
		t.Fatalf("Call = %+v, %v; want the busy error", got, err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("busy reply took %v", took)
	}
	<-sent
	if d := deadline.Sub(start); d < 0 || d > 200*time.Millisecond {
		t.Errorf("Deadline is %v after the call, want about navTimeout", d)
	}
}

func TestUnknownCmdAndVersion(t *testing.T) {
	sock, _ := SocketPath(shortDir(t), 1)
	stop := serveForTest(t, sock, nil, nil)
	defer stop()
	got, _ := Call(sock, Request{V: 1, Cmd: "nope"}, time.Second)
	if got.OK || got.Error != `unknown command "nope"` {
		t.Errorf("unknown command reply = %+v", got)
	}
	got, _ = Call(sock, Request{V: 2, Cmd: "where"}, time.Second)
	if got.OK || got.Error != "unsupported protocol version 2" {
		t.Errorf("version reply = %+v", got)
	}
}

func TestSocketPerms(t *testing.T) {
	sock, _ := SocketPath(shortDir(t), 1)
	stop := serveForTest(t, sock, nil, nil)
	defer stop()
	fi, err := os.Stat(sock)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
}

func TestServeRemovesSocketOnStop(t *testing.T) {
	sock, _ := SocketPath(shortDir(t), 1)
	stop := serveForTest(t, sock, nil, nil)
	stop()
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Error("stopping should remove the socket")
	}
}

func TestServeMalformedRequest(t *testing.T) {
	sock, _ := SocketPath(shortDir(t), 1)
	stop := serveForTest(t, sock, nil, nil)
	defer stop()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("not json\n"))
	var r Reply
	if err := json.NewDecoder(c).Decode(&r); err != nil || r.OK || r.Error == "" {
		t.Errorf("malformed request reply = %+v, %v", r, err)
	}
}
