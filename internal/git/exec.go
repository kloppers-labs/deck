// Package git drives the user's git CLI for the Git tab: porcelain v2 status,
// staging, diffs, commits, branches and remotes, always against the repo root.
package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

const (
	localTimeout  = 10 * time.Second
	remoteTimeout = 120 * time.Second
)

var execCount atomic.Int64

// ExecError is a failed git invocation with its separated output.
type ExecError struct {
	Args   []string
	Stdout string
	Stderr string
	Err    error
}

func (e *ExecError) Error() string {
	for _, s := range []string{e.Stderr, e.Stdout} {
		if line := firstLine(s); line != "" {
			return line
		}
	}
	if e.Err != nil {
		return "git " + strings.Join(e.Args, " ") + ": " + e.Err.Error()
	}
	return "git " + strings.Join(e.Args, " ") + " failed"
}

func (e *ExecError) Unwrap() error { return e.Err }

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

func gitEnv(base []string) []string {
	forced := map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_OPTIONAL_LOCKS":  "0",
		"LC_ALL":              "C",
	}
	env := make([]string, 0, len(base)+len(forced))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := forced[k]; !ok {
			env = append(env, kv)
		}
	}
	for _, k := range []string{"GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS", "LC_ALL"} {
		env = append(env, k+"="+forced[k])
	}
	return env
}

// runGit runs `git -C dir args...` with stdout and stderr kept apart. A context
// without a deadline gets the local timeout.
func runGit(ctx context.Context, dir string, args ...string) (stdout, stderr string, err error) {
	return runGitInput(ctx, dir, "", args...)
}

func runGitInput(ctx context.Context, dir, stdin string, args ...string) (string, string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, localTimeout)
		defer cancel()
	}
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	execCount.Add(1)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = gitEnv(os.Environ())
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return string(out), errBuf.String(), &ExecError{Args: args, Stdout: string(out), Stderr: errBuf.String(), Err: err}
	}
	return string(out), errBuf.String(), nil
}
