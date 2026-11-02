package aicommit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		backend  string
		wantName string
		wantArgs []string
		stdin    string
	}{
		{"claude", "claude", []string{"-p", "hello", "--model", "sonnet", "--output-format", "text"}, "claude -p reads piped stdin as context"},
		{"", "claude", []string{"-p", "hello", "--model", "sonnet", "--output-format", "text"}, "default backend is claude"},
		{"codex", "codex", []string{"exec", "hello"}, "codex exec appends piped stdin as a <stdin> block"},
		{"crush", "crush", []string{"run", "--quiet", "-m", "sonnet", "hello"}, "crush run reads piped stdin"},
		{"mods", "crush", []string{"run", "--quiet", "-m", "sonnet", "hello"}, "mods is a deprecated alias for crush"},
	}
	for _, tt := range tests {
		name, args := BuildArgs(tt.backend, "sonnet", "hello")
		if name != tt.wantName {
			t.Errorf("backend %q: name = %q, want %q", tt.backend, name, tt.wantName)
		}
		if !reflect.DeepEqual(args, tt.wantArgs) {
			t.Errorf("backend %q: args = %v, want %v", tt.backend, args, tt.wantArgs)
		}
	}
}

func TestBuildArgsCrushWithoutModel(t *testing.T) {
	_, args := BuildArgs("crush", "", "hello")
	if want := []string{"run", "--quiet", "hello"}; !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestBuildArgsPreservesMultilineInstruction(t *testing.T) {
	instr := "line one\nline two\n```go\nfmt.Println()\n```"
	_, args := BuildArgs("claude", "sonnet", instr)
	if args[1] != instr {
		t.Errorf("instruction was altered:\n got %q\nwant %q", args[1], instr)
	}
}

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("DECK_AICOMMIT_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "fenced":
		fmt.Fprint(os.Stdout, "```text\nfeat: add widget\n\nbody line\n```\n")
	case "sleep":
		time.Sleep(10 * time.Second)
	case "fail":
		fmt.Fprint(os.Stderr, "not logged in")
		os.Exit(3)
	case "empty":
	}
	os.Exit(0)
}

type fakeCall struct {
	name string
	args []string
}

func useHelper(t *testing.T, mode string) *fakeCall {
	t.Helper()
	call := &fakeCall{}
	orig := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		call.name, call.args = name, args
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(), "DECK_AICOMMIT_HELPER="+mode)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	return call
}

func TestGenerateSendsDiffOnStdin(t *testing.T) {
	call := useHelper(t, "echo")
	diff := "diff --git a/x.go b/x.go\n+func X() {}\n"
	out, err := Generate(context.Background(), "claude", "sonnet", diff)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != strings.TrimSpace(diff) {
		t.Errorf("stdin echo = %q, want %q", out, strings.TrimSpace(diff))
	}
	if call.name != "claude" {
		t.Errorf("ran %q, want claude", call.name)
	}
	for _, a := range call.args {
		if strings.Contains(a, "func X()") {
			t.Errorf("diff leaked into argv: %q", a)
		}
	}
}

func TestGenerateStripsFences(t *testing.T) {
	useHelper(t, "fenced")
	out, err := Generate(context.Background(), "claude", "sonnet", "diff")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if want := "feat: add widget\n\nbody line"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestGenerateTimeout(t *testing.T) {
	useHelper(t, "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Generate(ctx, "claude", "sonnet", "diff")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Generate took %v after the deadline", elapsed)
	}
}

func TestGenerateIncludesStderrInError(t *testing.T) {
	useHelper(t, "fail")
	_, err := Generate(context.Background(), "claude", "sonnet", "diff")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v, want it to carry stderr", err)
	}
}

func TestGenerateEmptyOutputIsError(t *testing.T) {
	useHelper(t, "empty")
	if _, err := Generate(context.Background(), "claude", "sonnet", "diff"); err == nil {
		t.Fatal("expected an error for an empty response")
	}
}

func TestStripFences(t *testing.T) {
	cases := map[string]string{
		"feat: x":                          "feat: x",
		"  feat: x\n":                      "feat: x",
		"```\nfeat: x\n```":                "feat: x",
		"```git-commit\nfeat: x\n\nb\n```": "feat: x\n\nb",
		"```\nfeat: x":                     "feat: x",
	}
	for in, want := range cases {
		if got := stripFences(in); got != want {
			t.Errorf("stripFences(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapDiff(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	diff := strings.Repeat(line, 700)
	got := capDiff(diff)
	head, tail, ok := strings.Cut(got, "\n[diff truncated; ")
	if !ok {
		t.Fatalf("missing truncation marker in %d-byte result", len(got))
	}
	if len(head) > maxDiffBytes {
		t.Errorf("kept %d bytes, limit %d", len(head), maxDiffBytes)
	}
	if !strings.HasPrefix(diff, head) || diff[len(head)] != '\n' {
		t.Error("cut is not at a line boundary")
	}
	if want := fmt.Sprintf("%d more bytes]", len(diff)-len(head)); tail != want {
		t.Errorf("marker tail = %q, want %q", tail, want)
	}
}

func TestCapDiffLeavesSmallDiffAlone(t *testing.T) {
	if got := capDiff("small\n"); got != "small\n" {
		t.Errorf("capDiff changed a small diff: %q", got)
	}
}

func TestAvailable(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	var asked string
	lookPath = func(name string) (string, error) {
		asked = name
		if name == "crush" {
			return "/bin/crush", nil
		}
		return "", exec.ErrNotFound
	}
	if !Available("mods") || asked != "crush" {
		t.Errorf("Available(mods) should look up crush, asked %q", asked)
	}
	if Available("claude") {
		t.Error("Available(claude) should be false when claude is missing")
	}
}
