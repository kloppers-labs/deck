package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/ide"
	"github.com/kloppers-labs/deck/internal/presence"
)

type agentSetup struct {
	dir   string
	ln    net.Listener
	store *presence.Store
	root  string
}

func prepareAgent(cfg config.Config) (*agentSetup, []config.Warning) {
	if !cfg.Agent.Enabled {
		return nil, nil
	}
	home, _ := os.UserHomeDir()
	dir, err := control.Dir(os.Getenv, home)
	if err != nil {
		return nil, []config.Warning{{Message: "agent control off: " + err.Error()}}
	}
	sock, err := control.SocketPath(dir, os.Getpid())
	if err != nil {
		return nil, []config.Warning{{Message: "agent control off: " + err.Error()}}
	}
	ln, err := control.Listen(sock)
	if err != nil {
		return nil, []config.Warning{{Message: "agent control off: " + err.Error()}}
	}
	return &agentSetup{dir: dir, ln: ln, store: presence.New(), root: worktreeRoot()}, nil
}

func worktreeRoot() string {
	cwd, _ := os.Getwd()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if r, err := git.OpenRepo(ctx, cwd); err == nil {
		return r.Top
	}
	return cwd
}

// serve registers this deck and answers agents until the returned func runs.
func (a *agentSetup) serve(p *tea.Program) func() {
	cwd, _ := os.Getwd()
	inst := control.Instance{
		PID: os.Getpid(), Root: a.root, Cwd: cwd, Version: versionString(),
		Socket: a.ln.Addr().String(), Started: time.Now(), Focused: time.Now(),
	}
	cleanup, err := control.Register(a.dir, inst)
	if err != nil {
		a.ln.Close()
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		control.ServeListener(ctx, a.ln, a.query, p.Send)
		close(done)
	}()
	return func() {
		cancel()
		<-done
		cleanup()
	}
}

func (a *agentSetup) query(req control.Request) control.Reply {
	if req.Cmd == "ping" {
		return control.Reply{OK: true, Message: versionString()}
	}
	snap := a.store.Get()
	return control.Reply{OK: true, Where: &snap}
}

type programRef struct{ p atomic.Pointer[tea.Program] }

func (r *programRef) send(msg tea.Msg) {
	if p := r.p.Load(); p != nil {
		p.Send(msg)
	}
}

func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func (a *agentSetup) startBridge(send func(tea.Msg)) (*ide.Bridge, []config.Warning) {
	b, err := ide.Start(context.Background(), ide.Options{
		Root: a.root, ConfigDir: claudeConfigDir(), PID: os.Getpid(),
		Store: a.store, Send: send, Version: versionString(),
	})
	switch {
	case errors.Is(err, ide.ErrNoClaude):
		return nil, nil
	case err != nil:
		return nil, []config.Warning{{Message: "Claude bridge off: " + err.Error()}}
	}
	return b, nil
}
