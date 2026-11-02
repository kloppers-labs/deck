package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// InstallRunner runs an install argv and returns its combined output.
type InstallRunner func(ctx context.Context, argv []string) ([]byte, error)

const installKillDelay = 2 * time.Second

// RunInstall runs argv in its own process group with no stdin; cancelling ctx kills the group.
func RunInstall(ctx context.Context, argv []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty install command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	cmd.WaitDelay = installKillDelay
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ctxErr := ctx.Err(); err != nil && ctxErr != nil {
		err = ctxErr
	}
	return out.Bytes(), err
}

// InstallCommand is the argv that installs name's server: the one recorded by
// its missing-binary failure, else the spec's.
func (m *Manager) InstallCommand(name string) []string {
	m.mu.Lock()
	ev, failed := m.failed[name]
	m.mu.Unlock()
	if failed && len(ev.Install) > 0 {
		return ev.Install
	}
	for _, s := range m.reg.Specs() {
		if s.Name == name {
			return s.Install
		}
	}
	return nil
}

// Install runs name's install command and, when it succeeds, lets the server
// start again. Shutdown cancels a running install.
func (m *Manager) Install(ctx context.Context, name string) (string, error) {
	argv := m.InstallCommand(name)
	if len(argv) == 0 {
		return "", fmt.Errorf("no install command for %s", name)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrShutdown
	}
	id := m.nextID
	m.nextID++
	m.installs[id] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.installs, id)
		m.mu.Unlock()
	}()
	out, err := m.installer(ctx, argv)
	if err != nil {
		return string(out), err
	}
	m.Retry(name)
	return string(out), nil
}

// Retry forgets name's recorded failure and cached binary lookup, so the next
// request resolves and starts it afresh.
func (m *Manager) Retry(name string) {
	for _, s := range m.reg.Specs() {
		if s.Name == name && len(s.Command) > 0 {
			m.reg.Forget(s.Command[0])
		}
	}
	m.mu.Lock()
	_, failed := m.failed[name]
	delete(m.failed, name)
	m.mu.Unlock()
	if failed {
		m.emit(StatusEvent{Server: name, State: StateIdle, Percent: -1, Since: m.clock.Now()})
	}
}
