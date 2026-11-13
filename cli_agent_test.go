package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/presence"
)

type fakeDeck struct {
	mu   sync.Mutex
	reqs []control.Request
	snap presence.Snapshot
}

func (f *fakeDeck) requests() []control.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]control.Request(nil), f.reqs...)
}

func shortTemp(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	dir, _ = filepath.EvalSymlinks(dir)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startFakeDeck(t *testing.T, runtime, root string, f *fakeDeck) {
	t.Helper()
	dir := filepath.Join(runtime, "deck")
	sock, err := control.SocketPath(dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := control.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := control.Register(dir, control.Instance{PID: os.Getpid(), Root: root, Socket: sock, Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		control.ServeListener(ctx, ln, func(control.Request) control.Reply {
			f.mu.Lock()
			defer f.mu.Unlock()
			snap := f.snap
			return control.Reply{OK: true, Where: &snap}
		}, func(msg tea.Msg) {
			rm := msg.(control.RequestMsg)
			f.mu.Lock()
			f.reqs = append(f.reqs, rm.Req)
			f.mu.Unlock()
			rm.Answer(control.Reply{OK: true, Message: "done"})
		})
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		cleanup()
	})
}

func testEnv(runtime, cwd string, vars ...string) agentEnv {
	m := map[string]string{"XDG_RUNTIME_DIR": runtime}
	for i := 0; i+1 < len(vars); i += 2 {
		m[vars[i]] = vars[i+1]
	}
	return agentEnv{
		Getenv: func(k string) string { return m[k] },
		Getwd:  func() (string, error) { return cwd, nil },
		Home:   "/nonexistent-home",
		Now:    time.Now,
	}
}

func runCLI(t *testing.T, env agentEnv, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runAgent(args[0], args[1:], env, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestSubcommandBeatsDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, "where"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", shortTemp(t))
	var out, errOut bytes.Buffer
	code, launch := cli([]string{"where", "--no-launch"}, &out, &errOut)
	if launch || code != 3 {
		t.Errorf("deck where = (%d, %v), want the where subcommand (exit 3, no deck)", code, launch)
	}
}

func TestDoubleDashOpensDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, "open"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code, launch := cli([]string{"--", "open"}, &out, &errOut)
	wd, _ := os.Getwd()
	if code != 0 || !launch || filepath.Base(wd) != "open" {
		t.Errorf("deck -- open = (%d, %v) in %q, want a launch inside ./open", code, launch, wd)
	}
}

func TestOpenResolvesRelativeToCallerCwd(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	f := &fakeDeck{}
	startFakeDeck(t, runtime, root, f)
	code, out, errOut := runCLI(t, testEnv(runtime, filepath.Join(root, "sub")), "open", "x.go:12:3")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	reqs := f.requests()
	want := control.Request{V: 1, Cmd: "open", Path: filepath.Join(root, "sub", "x.go"), Line: 12, Col: 3, From: "agent"}
	if len(reqs) != 1 || reqs[0] != want {
		t.Errorf("requests = %+v, want %+v", reqs, want)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("success output %q should be one line", out)
	}
}

func TestOpenRangeAndReview(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	f := &fakeDeck{}
	startFakeDeck(t, runtime, root, f)
	env := testEnv(runtime, root)
	runCLI(t, env, "open", "a.go:10-20")
	runCLI(t, env, "review", "--uncommitted", "--base", "origin/dev")
	runCLI(t, env, "diff", "pkg/b.go:7")
	reqs := f.requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %+v", reqs)
	}
	if reqs[0].Line != 10 || reqs[0].EndLine != 20 {
		t.Errorf("open range = %+v", reqs[0])
	}
	if reqs[1].Cmd != "review" || !reqs[1].Uncommitted || reqs[1].Base != "origin/dev" {
		t.Errorf("review = %+v", reqs[1])
	}
	if reqs[2].Cmd != "diff" || reqs[2].Path != filepath.Join(root, "pkg", "b.go") || reqs[2].Line != 7 {
		t.Errorf("diff = %+v", reqs[2])
	}
}

func TestWhereHumanFormat(t *testing.T) {
	root := "/r"
	cases := []struct {
		name string
		snap presence.Snapshot
		want string
	}{
		{"selection", presence.Snapshot{Root: root, Tab: "files", File: "/r/a.go", Line: 118, Col: 5,
			Sel: &presence.Range{StartLine: 118, StartCol: 1, EndLine: 130, EndCol: 2}, SelText: "func x() {\n}"},
			"files  a.go:118:5  selection 118-130\nfunc x() {\n}\n"},
		{"cursor", presence.Snapshot{Root: root, Tab: "files", File: "/r/a.go", Line: 3, Col: 1}, "files  a.go:3:1\n"},
		{"section", presence.Snapshot{Root: root, Tab: "files", File: "/r/docs/x.md", Line: 5, Col: 1, Section: "Goals",
			Sel: &presence.Range{StartLine: 5, StartCol: 1, EndLine: 9, EndCol: 1}}, "files  docs/x.md  section \"Goals\" 5-9\n"},
		{"directory", presence.Snapshot{Root: root, Tab: "files", Dir: "/r/docs"}, "files  docs/\n"},
		{"git review", presence.Snapshot{Root: root, Tab: "git", File: "/r/a.go", Line: 42,
			Git: &presence.GitSpot{Mode: "review", Base: "origin/main", Side: "new"}}, "git review  a.go:42 (new)  vs origin/main\n"},
		{"git changes, no file", presence.Snapshot{Root: root, Tab: "git", Git: &presence.GitSpot{Mode: "changes"}}, "git changes\n"},
		{"other tab", presence.Snapshot{Root: root, Tab: "run"}, "run\n"},
		{"outside cwd", presence.Snapshot{Root: root, Tab: "files", File: "/elsewhere/z.go", Line: 1, Col: 1}, "files  /elsewhere/z.go:1:1\n"},
	}
	for _, tc := range cases {
		if got := formatWhere(tc.snap, "/r"); got != tc.want {
			t.Errorf("%s: formatWhere = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWhereJSON(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	f := &fakeDeck{snap: presence.Snapshot{Root: root, Tab: "files", File: filepath.Join(root, "a.go"), Line: 2, Col: 1}}
	startFakeDeck(t, runtime, root, f)
	code, out, _ := runCLI(t, testEnv(runtime, root), "where", "--json")
	if code != 0 || !strings.Contains(out, `"line": 2`) || !strings.Contains(out, `"tab": "files"`) {
		t.Errorf("where --json = %d %q", code, out)
	}
	code, out, _ = runCLI(t, testEnv(runtime, root), "where")
	if code != 0 || out != "files  a.go:2:1\n" {
		t.Errorf("where = %d %q", code, out)
	}
}

func TestWhereNoDeckExit3(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	code, _, errOut := runCLI(t, testEnv(runtime, root), "where", "--no-launch")
	if code != 3 || !strings.Contains(errOut, "no deck running for "+root) {
		t.Errorf("exit %d, stderr %q; want 3 and the root", code, errOut)
	}
}

func TestFromDefaultsToClaude(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	f := &fakeDeck{}
	startFakeDeck(t, runtime, root, f)
	runCLI(t, testEnv(runtime, root, "CLAUDECODE", "1"), "open", "a.go")
	runCLI(t, testEnv(runtime, root, "CLAUDECODE", "1"), "open", "--from", "Codex", "a.go")
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].From != "Claude" || reqs[1].From != "Codex" {
		t.Errorf("from = %+v", reqs)
	}
}

func TestLs(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	startFakeDeck(t, runtime, root, &fakeDeck{})
	code, out, _ := runCLI(t, testEnv(runtime, root), "ls")
	if code != 0 || !strings.Contains(out, "PID") || !strings.Contains(out, root) {
		t.Errorf("ls = %d %q", code, out)
	}
}

func TestUsageListsSubcommands(t *testing.T) {
	for _, sub := range []string{"deck open", "deck review", "deck diff", "deck where", "deck ls"} {
		if !strings.Contains(usage, sub) {
			t.Errorf("usage lacks %q", sub)
		}
	}
}

func TestAutoLaunchThenSends(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	f := &fakeDeck{}
	env := testEnv(runtime, root)
	var launched []string
	env.Launch = func(dir, mode string) error {
		launched = append(launched, dir+"|"+mode)
		startFakeDeck(t, runtime, root, f)
		return nil
	}
	code, _, errOut := runCLI(t, env, "open", "a.go")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if len(launched) != 1 || launched[0] != root+"|" {
		t.Errorf("launched = %q, want one launch for the root with the configured mode", launched)
	}
	if len(f.requests()) != 1 {
		t.Errorf("the request should reach the new deck, got %+v", f.requests())
	}
}

func TestLaunchOffExit3(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	env := testEnv(runtime, root)
	env.Launch = func(string, string) error { return errors.New("starting deck automatically is off") }
	code, _, errOut := runCLI(t, env, "open", "a.go")
	if code != 3 || !strings.Contains(errOut, "is off") {
		t.Errorf("exit %d, stderr %q; want 3 and the reason", code, errOut)
	}
}

func TestLaunchCommand(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	env := testEnv(runtime, root)
	var mode string
	env.Launch = func(dir, m string) error {
		mode = m
		startFakeDeck(t, runtime, root, &fakeDeck{})
		return nil
	}
	code, out, errOut := runCLI(t, env, "launch", "--tab")
	if code != 0 || mode != "tab" || !strings.Contains(out, "started deck for "+root) {
		t.Errorf("launch = %d %q %q, mode %q", code, out, errOut, mode)
	}
	code, out, _ = runCLI(t, env, "launch")
	if code != 0 || !strings.Contains(out, "already running") {
		t.Errorf("second launch = %d %q, want already running", code, out)
	}
}

func TestGuideCLI(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	code, out, _ := runCLI(t, testEnv(runtime, root), "guide")
	if code != 0 || !strings.Contains(out, "deck where") {
		t.Errorf("guide = %d %q", code, out)
	}
	cfg := t.TempDir()
	code, out, errOut := runCLI(t, testEnv(runtime, root, "CLAUDE_CONFIG_DIR", cfg), "guide", "--install", "claude")
	if code != 0 || !strings.Contains(out, filepath.Join(cfg, "skills", "deck", "SKILL.md")) {
		t.Errorf("guide --install = %d %q %q", code, out, errOut)
	}
	if code, _, _ := runCLI(t, testEnv(runtime, root), "guide", "--install", "vim"); code != 2 {
		t.Errorf("an unknown agent should be a usage error, got %d", code)
	}
}

func TestWhereNeverLaunches(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	env := testEnv(runtime, root)
	launched := 0
	env.Launch = func(string, string) error { launched++; return nil }
	code, _, errOut := runCLI(t, env, "where")
	if code != 3 || launched != 0 || !strings.Contains(errOut, "no deck running") {
		t.Errorf("where with no deck: exit %d, launches %d, stderr %q; want 3, 0 and the reason", code, launched, errOut)
	}
}

func TestFlagsAfterThePath(t *testing.T) {
	runtime, root := shortTemp(t), shortTemp(t)
	code, _, errOut := runCLI(t, testEnv(runtime, root), "open", "x.go:12", "--no-launch")
	if code != 3 {
		t.Errorf("open x.go:12 --no-launch = %d (%q), want 3 (no deck), not a usage error", code, errOut)
	}
	f := &fakeDeck{}
	startFakeDeck(t, runtime, root, f)
	if code, _, errOut := runCLI(t, testEnv(runtime, root), "open", "a.go:3", "--from", "Codex"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, testEnv(runtime, root), "review", "--base", "main", "--uncommitted"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].From != "Codex" || reqs[0].Line != 3 || reqs[1].Base != "main" || !reqs[1].Uncommitted {
		t.Errorf("requests = %+v", reqs)
	}
}
