package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/config"
)

const (
	maxBatch     = 256
	batchWindow  = 16 * time.Millisecond
	maxChunk     = 64 * 1024
	pollInterval = 25 * time.Millisecond
)

var killDelay = 3 * time.Second

var colorEnv = map[string]string{
	"FORCE_COLOR":    "1",
	"CLICOLOR_FORCE": "1",
	"DOTNET_SYSTEM_CONSOLE_ALLOW_ANSI_COLOR_REDIRECTION": "1",
}

// chunk is one segment of output; cr marks a segment ended by a bare carriage
// return, which the next segment overwrites.
type chunk struct {
	text string
	cr   bool
}

// proc is one running task: its process group, its output and its exit.
type proc struct {
	idx, gen int
	lines    chan chunk
	exit     chan ExitMsg
	cancel   context.CancelFunc
	done     chan struct{}
}

var startProc = start

// start runs t under "sh -c" in its own process group, stdout and stderr
// merged; stopping sends the group SIGTERM, then SIGKILL after killDelay.
func start(idx, gen int, t config.Task, root string) *proc {
	ctx, cancel := context.WithCancel(context.Background())
	p := &proc{
		idx: idx, gen: gen,
		lines:  make(chan chunk, 1024),
		exit:   make(chan ExitMsg, 1),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", t.Cmd)
	cmd.Dir = taskDir(root, t.Dir)
	cmd.Env = taskEnv(os.Environ(), t.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return signalGroup(cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = killDelay
	go p.run(ctx, cmd)
	return p
}

func (p *proc) run(ctx context.Context, cmd *exec.Cmd) {
	defer close(p.done)
	defer p.cancel()
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		close(p.lines)
		p.finish(err)
		return
	}
	exited := make(chan struct{})
	reaped := make(chan struct{})
	go p.reap(ctx, cmd.Process.Pid, exited, reaped)
	errc := make(chan error, 1)
	go func() {
		errc <- cmd.Wait()
		_ = pw.Close()
	}()
	scanChunks(pr, func(c chunk) {
		select {
		case p.lines <- c:
			return
		default:
		}
		select {
		case p.lines <- c:
		case <-ctx.Done():
		}
	})
	_, _ = io.Copy(io.Discard, pr)
	close(p.lines)
	err := <-errc
	close(exited)
	<-reaped
	p.finish(err)
}

func (p *proc) finish(err error) {
	p.exit <- ExitMsg{idx: p.idx, gen: p.gen, err: err, code: exitCode(err)}
	close(p.exit)
}

// reap escalates a stop: once the context is cancelled it waits for the group
// to empty, and sends SIGKILL to whatever is left after killDelay.
func (p *proc) reap(ctx context.Context, pid int, exited, reaped chan struct{}) {
	defer close(reaped)
	select {
	case <-exited:
		return
	case <-ctx.Done():
	}
	deadline := time.Now().Add(killDelay)
	for groupAlive(pid) {
		if time.Now().After(deadline) {
			_ = signalGroup(pid, syscall.SIGKILL)
			return
		}
		time.Sleep(pollInterval)
	}
}

func signalGroup(pid int, sig syscall.Signal) error {
	err := syscall.Kill(-pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func groupAlive(pid int) bool { return syscall.Kill(-pid, 0) == nil }

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// next returns a command that waits for the next batch of output (up to
// maxBatch lines or batchWindow of time), then the exit, then nil.
func (p *proc) next() tea.Cmd {
	return func() tea.Msg {
		first, ok := <-p.lines
		if !ok {
			if ex, ok := <-p.exit; ok {
				return ex
			}
			return nil
		}
		out := OutputMsg{idx: p.idx, gen: p.gen, lines: []chunk{first}}
		timer := time.NewTimer(batchWindow)
		defer timer.Stop()
		for len(out.lines) < maxBatch {
			select {
			case c, ok := <-p.lines:
				if !ok {
					return out
				}
				out.lines = append(out.lines, c)
			case <-timer.C:
				return out
			}
		}
		return out
	}
}

func (p *proc) stop() {
	if p != nil && p.cancel != nil {
		p.cancel()
	}
}

// wait reports whether the task fully exited within timeout.
func (p *proc) wait(timeout time.Duration) bool {
	if p == nil || p.done == nil {
		return true
	}
	select {
	case <-p.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// scanChunks splits r on "\n", "\r\n" and bare "\r", calling emit for each
// segment. Overlong segments are cut at maxChunk.
func scanChunks(r io.Reader, emit func(chunk)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxChunk*2)
	sc.Split(splitChunks)
	for sc.Scan() {
		tok := sc.Text()
		switch {
		case strings.HasSuffix(tok, "\n"):
			emit(chunk{text: strings.TrimSuffix(strings.TrimSuffix(tok, "\n"), "\r")})
		case strings.HasSuffix(tok, "\r"):
			emit(chunk{text: strings.TrimSuffix(tok, "\r"), cr: true})
		default:
			emit(chunk{text: tok})
		}
	}
}

func splitChunks(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			return i + 2, data[:i+2], nil
		}
		return i + 1, data[:i+1], nil
	}
	if atEOF || len(data) >= maxChunk {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func taskDir(root, dir string) string {
	switch {
	case dir == "":
		return root
	case filepath.IsAbs(dir):
		return dir
	}
	return filepath.Join(root, dir)
}

// taskEnv is base with deck's colour variables and the task's own env applied
// over it, later keys winning.
func taskEnv(base []string, extra map[string]string) []string {
	over := maps.Clone(colorEnv)
	maps.Copy(over, extra)
	env := make([]string, 0, len(base)+len(over))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := over[k]; !ok {
			env = append(env, kv)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(over)) {
		env = append(env, k+"="+over[k])
	}
	return env
}
