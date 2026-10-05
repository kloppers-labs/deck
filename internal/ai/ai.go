// Package ai runs one-shot AI requests by shelling out to the user's already
// authenticated CLIs (claude / codex / crush), reusing their OAuth logins rather
// than calling any API directly. Output is streamed back as Bubble Tea messages.
package ai

import (
	"bufio"
	"context"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ChunkMsg carries one streamed piece of AI output.
type ChunkMsg struct{ Text string }

// DoneMsg signals the request finished (Err non-nil on failure).
type DoneMsg struct {
	Err    error
	Stderr string
}

// BuildArgs maps a backend + prompt to the exact CLI invocation. Passing the
// prompt as a single argv entry avoids any shell-quoting issues with newlines.
//
// "mods" is accepted as a deprecated alias for "crush": Charm archived mods in
// 2026 and folded its one-shot mode into `crush run`, so existing configs keep
// working.
func BuildArgs(backend, model, prompt string) (string, []string) {
	switch backend {
	case "codex":
		return "codex", []string{"exec", prompt}
	case "crush", "mods":
		args := []string{"run", "--quiet"}
		if model != "" {
			args = append(args, "-m", model)
		}
		return "crush", append(args, prompt)
	default: // claude
		return "claude", []string{"-p", prompt, "--model", model, "--output-format", "text"}
	}
}

// Run executes a one-shot request and returns the full output. Used for
// non-streaming needs like generating a commit message from a diff.
func Run(backend, model, prompt string) (string, error) {
	name, args := BuildArgs(backend, model, prompt)
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// Stream is a running AI request. Feed Next() back into Bubble Tea until a
// DoneMsg arrives; call Cancel() to abort early.
type Stream struct {
	ch     chan tea.Msg
	cancel context.CancelFunc
}

// Start launches the request in a goroutine and returns the stream plus the
// first command to wait on.
func Start(backend, model, prompt string) (*Stream, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	name, args := BuildArgs(backend, model, prompt)
	ch := make(chan tea.Msg, 64)
	s := &Stream{ch: ch, cancel: cancel}

	go func() {
		defer close(ch)
		cmd := exec.CommandContext(ctx, name, args...)
		var errBuf strings.Builder
		cmd.Stderr = &errBuf

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ch <- DoneMsg{Err: err}
			return
		}
		if err := cmd.Start(); err != nil {
			ch <- DoneMsg{Err: err, Stderr: errBuf.String()}
			return
		}

		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			ch <- ChunkMsg{Text: sc.Text() + "\n"}
		}
		ch <- DoneMsg{Err: cmd.Wait(), Stderr: errBuf.String()}
	}()

	return s, s.Next()
}

// Next returns a command that blocks for the next message from the stream.
func (s *Stream) Next() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-s.ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (s *Stream) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}
