package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kloppers-labs/deck/internal/lsp/lsptest"
)

func newTestConn(t *testing.T, opts ConnOptions) (*Conn, *lsptest.Server) {
	t.Helper()
	s := lsptest.New()
	c := NewConn(s.Process(), opts)
	t.Cleanup(func() { c.Close(); s.Crash() })
	return c, s
}

func TestFramingSplitReads(t *testing.T) {
	got := make(chan string, 1)
	c, s := newTestConn(t, ConnOptions{OnNotify: func(method string, params json.RawMessage) {
		got <- method + " " + string(params)
	}})
	_ = c
	body := `{"jsonrpc":"2.0","method":"window/logMessage","params":{"type":3,"message":"hi"}}`
	frame := fmt.Sprintf("Content-Length: %d\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n%s", len(body), body)
	for _, chunk := range []string{frame[:3], frame[3:17], frame[17:40], frame[40 : len(frame)-5], frame[len(frame)-5:]} {
		if err := s.WriteRaw([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case m := <-got:
		want := `window/logMessage {"type":3,"message":"hi"}`
		if m != want {
			t.Fatalf("got %q, want %q", m, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification not delivered")
	}
}

func TestConcurrentCallsMatched(t *testing.T) {
	c, s := newTestConn(t, ConnOptions{})
	s.Handle("echo", func(p json.RawMessage) (any, error) { return json.RawMessage(p), nil })
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out int
			if err := c.Call(context.Background(), "echo", i, &out); err != nil {
				errs <- err
				return
			}
			if out != i {
				errs <- fmt.Errorf("call %d got %d", i, out)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCancelSendsCancelRequest(t *testing.T) {
	c, s := newTestConn(t, ConnOptions{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s.Handle("slow", func(json.RawMessage) (any, error) { <-release; return nil, nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Call(ctx, "slow", nil, nil) }()
	reqs, err := s.WaitFor("slow", 1, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Call err = %v, want context.Canceled", err)
	}
	cancels, err := s.WaitFor("$/cancelRequest", 1, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(cancels[0].Params, &p); err != nil {
		t.Fatal(err)
	}
	if string(p.ID) != string(reqs[0].ID) {
		t.Fatalf("cancel id = %s, want %s", p.ID, reqs[0].ID)
	}
}

func TestServerRequestConfigurationAnswered(t *testing.T) {
	_, s := newTestConn(t, ConnOptions{OnRequest: func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method != "workspace/configuration" {
			return nil, ErrMethodNotFound
		}
		var p ConfigurationParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return make([]any, len(p.Items)), nil
	}})
	resp, err := s.Call(context.Background(), "workspace/configuration", map[string]any{
		"items": []map[string]string{{"section": "gopls"}, {"section": "csharp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil || string(resp.Result) != "[null,null]" {
		t.Fatalf("result = %s, error = %v", resp.Result, resp.Error)
	}
}

func TestUnknownServerRequestMethodNotFound(t *testing.T) {
	for name, opts := range map[string]ConnOptions{
		"no handler": {},
		"handler declines": {OnRequest: func(context.Context, string, json.RawMessage) (any, error) {
			return nil, ErrMethodNotFound
		}},
	} {
		t.Run(name, func(t *testing.T) {
			_, s := newTestConn(t, opts)
			resp, err := s.Call(context.Background(), "custom/thing", nil)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
				t.Fatalf("error = %+v, want code %d", resp.Error, CodeMethodNotFound)
			}
		})
	}
}

func TestEOFFailsPending(t *testing.T) {
	c, s := newTestConn(t, ConnOptions{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s.Handle("slow", func(json.RawMessage) (any, error) { <-release; return nil, nil })
	done := make(chan error, 1)
	go func() { done <- c.Call(context.Background(), "slow", nil, nil) }()
	if _, err := s.WaitFor("slow", 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	s.Crash()
	select {
	case err := <-done:
		if !errors.Is(err, ErrServerExited) {
			t.Fatalf("err = %v, want ErrServerExited", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call not failed")
	}
	<-c.Done()
	if err := c.Call(context.Background(), "slow", nil, nil); !errors.Is(err, ErrServerExited) {
		t.Fatalf("call after exit = %v, want ErrServerExited", err)
	}
}

func TestRPCErrorCancelledIsQuiet(t *testing.T) {
	var logged atomic.Int32
	c, s := newTestConn(t, ConnOptions{Logf: func(string, ...any) { logged.Add(1) }})
	s.Handle("cancelled", func(json.RawMessage) (any, error) {
		return nil, &lsptest.Error{Code: CodeRequestCancelled, Message: "cancelled"}
	})
	s.Handle("modified", func(json.RawMessage) (any, error) {
		return nil, &lsptest.Error{Code: CodeContentModified, Message: "modified"}
	})
	s.Handle("broken", func(json.RawMessage) (any, error) { return nil, errors.New("boom") })

	for _, method := range []string{"cancelled", "modified"} {
		err := c.Call(context.Background(), method, nil, nil)
		if err == nil || !IsQuiet(err) {
			t.Fatalf("%s: err = %v, want quiet error", method, err)
		}
	}
	if err := c.Call(context.Background(), "broken", nil, nil); err == nil || IsQuiet(err) {
		t.Fatalf("broken: err = %v, want loud error", err)
	}

	release := make(chan struct{})
	s.Handle("slow", func(json.RawMessage) (any, error) {
		<-release
		return nil, &lsptest.Error{Code: CodeRequestCancelled, Message: "cancelled"}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Call(ctx, "slow", nil, nil) }()
	if _, err := s.WaitFor("slow", 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !IsQuiet(err) {
		t.Fatalf("cancelled call err = %v, want quiet", err)
	}
	close(release)
	if err := c.Call(context.Background(), "cancelled", nil, nil); !IsQuiet(err) {
		t.Fatal(err)
	}
	if n := logged.Load(); n != 0 {
		t.Fatalf("logged %d lines, want 0", n)
	}
}
