package lsp

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type Command struct {
	Path   string
	Args   []string
	Dir    string
	Stderr io.Writer
}

// Process is a running server: stdio as a ReadWriteCloser, plus exit notification and a group kill.
type Process interface {
	io.ReadWriteCloser
	Exited() <-chan struct{}
	Kill() error
}

type Starter func(Command) (Process, error)

// StartProcess runs the server in its own process group so Kill also reaches its children.
func StartProcess(c Command) (Process, error) {
	cmd := exec.Command(c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = stdoutW, stderrW
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{stdoutR, stdoutW, stderrR, stderrW} {
			f.Close()
		}
		return nil, err
	}
	stdoutW.Close()
	stderrW.Close()
	stderr := c.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	go func() {
		_, _ = io.Copy(stderr, stderrR)
		stderrR.Close()
	}()
	p := &execProcess{cmd: cmd, stdin: stdin, stdout: stdoutR, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.exited)
	}()
	return p, nil
}

type execProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File
	exited chan struct{}
	once   sync.Once
}

func (p *execProcess) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *execProcess) Write(b []byte) (int, error) { return p.stdin.Write(b) }
func (p *execProcess) Exited() <-chan struct{}     { return p.exited }

func (p *execProcess) Close() error {
	var err error
	p.once.Do(func() { err = errors.Join(p.stdin.Close(), p.stdout.Close()) })
	return err
}

func (p *execProcess) Kill() error {
	err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
