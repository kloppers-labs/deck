// Package aicommit generates a git commit message in one shot by piping the
// staged diff into the user's already-authenticated claude, codex or crush CLI.
package aicommit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	maxDiffBytes = 60 * 1024
	instruction  = "Write a concise git commit message for the staged diff provided on stdin. " +
		"Use an imperative subject under 60 characters; add a short body only if it helps. " +
		"Output only the message."
)

var (
	execCommand = exec.CommandContext
	lookPath    = exec.LookPath
)

// BuildArgs maps a backend to its CLI invocation; "mods" is a deprecated alias for crush.
func BuildArgs(backend, model, instruction string) (string, []string) {
	switch backend {
	case "codex":
		return "codex", []string{"exec", instruction}
	case "crush", "mods":
		args := []string{"run", "--quiet"}
		if model != "" {
			args = append(args, "-m", model)
		}
		return "crush", append(args, instruction)
	default:
		return "claude", []string{"-p", instruction, "--model", model, "--output-format", "text"}
	}
}

// Available reports whether the backend's CLI is on PATH.
func Available(backend string) bool {
	name, _ := BuildArgs(backend, "", "")
	_, err := lookPath(name)
	return err == nil
}

// Generate asks the backend for a commit message describing diff, which is sent on stdin.
func Generate(ctx context.Context, backend, model, diff string) (string, error) {
	name, args := BuildArgs(backend, model, instruction)
	cmd := execCommand(ctx, name, args...)
	cmd.Stdin = strings.NewReader(capDiff(diff))
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("%s: %w: %s", name, err, detail)
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}
	msg := stripFences(stdout.String())
	if msg == "" {
		return "", errors.New(name + " returned an empty message")
	}
	return msg, nil
}

func capDiff(diff string) string {
	if len(diff) <= maxDiffBytes {
		return diff
	}
	cut := strings.LastIndexByte(diff[:maxDiffBytes], '\n')
	if cut < 0 {
		cut = maxDiffBytes
	}
	return diff[:cut] + fmt.Sprintf("\n[diff truncated; %d more bytes]", len(diff)-cut)
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if _, rest, ok := strings.Cut(s, "\n"); ok {
		s = rest
	} else {
		s = ""
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}
