package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"deck/internal/lsp/lsptest"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at   time.Time
	f    func()
	done bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.done
		t.done = true
		return was
	}
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	for _, t := range c.timers {
		if !t.done && !t.at.After(c.now) {
			t.done = true
			due = append(due, t.f)
		}
	}
	c.mu.Unlock()
	for _, f := range due {
		f()
	}
}

type harness struct {
	t       *testing.T
	dir     string
	clock   *fakeClock
	mgr     *Manager
	servers []*lsptest.Server

	mu     sync.Mutex
	starts []Command
}

func fakeConfig() Config {
	cfg := DefaultConfig()
	cfg.Servers = map[string]ServerOverride{
		"fake": {Command: []string{"fakels", "--stdio"}, Extensions: []string{".fk"}, RootMarkers: []string{"fake.root"}},
	}
	return cfg
}

func newHarness(t *testing.T, servers int, cfg Config, opts ...Option) *harness {
	t.Helper()
	h := &harness{t: t, dir: realTemp(t), clock: newFakeClock()}
	touch(t, filepath.Join(h.dir, ".git", "HEAD"), filepath.Join(h.dir, "fake.root"))
	for range servers {
		h.servers = append(h.servers, lsptest.New())
	}
	starter := func(cmd Command) (Process, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		i := len(h.starts)
		h.starts = append(h.starts, cmd)
		if i >= len(h.servers) {
			return nil, errors.New("no more fake servers")
		}
		return h.servers[i].Process(), nil
	}
	base := []Option{
		WithEnv(Env{LookPath: func(n string) (string, error) { return "/bin/" + n, nil }, Home: h.dir}),
		WithStarter(starter),
		withClock(h.clock),
		withTimings(timings{backoff: 0, shutdownWait: 100 * time.Millisecond, exitWait: 100 * time.Millisecond, initTimeout: 2 * time.Second, readyAfter: 2 * time.Second}),
	}
	h.mgr = NewManager(cfg, append(base, opts...)...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.mgr.Shutdown(ctx)
		for _, s := range h.servers {
			s.Crash()
		}
	})
	return h
}

func (h *harness) startCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.starts)
}

func (h *harness) file(name, content string) string {
	h.t.Helper()
	p := filepath.Join(h.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) client(path string) *Client {
	h.t.Helper()
	c, err := h.mgr.ClientFor(context.Background(), path)
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func (h *harness) nextEvent() any {
	h.t.Helper()
	select {
	case ev := <-h.mgr.Events():
		return ev
	case <-time.After(2 * time.Second):
		h.t.Fatal("timed out waiting for an event")
		return nil
	}
}

func (h *harness) statusUntil(done func(StatusEvent) bool) []StatusEvent {
	h.t.Helper()
	var got []StatusEvent
	for {
		if ev, ok := h.nextEvent().(StatusEvent); ok {
			got = append(got, ev)
			if done(ev) {
				return got
			}
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
