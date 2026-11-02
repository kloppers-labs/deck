package lsp

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultSpecsInstallAndLanguageIDs(t *testing.T) {
	languageIDs := map[string]string{
		".cs": "csharp", ".csx": "csharp", ".go": "go",
		".ts": "typescript", ".tsx": "typescriptreact", ".js": "javascript", ".jsx": "javascriptreact",
		".mjs": "javascript", ".cjs": "javascript",
		".py": "python", ".pyi": "python", ".rs": "rust", ".sh": "shellscript", ".bash": "shellscript",
		".lua": "lua", ".yaml": "yaml", ".yml": "yaml", ".json": "json", ".jsonc": "jsonc",
	}
	installers := map[string]bool{"dotnet": true, "go": true, "npm": true, "rustup": true, "brew": true}
	seen := map[string]bool{}
	for _, s := range DefaultSpecs() {
		if seen[s.Name] {
			t.Errorf("duplicate spec %q", s.Name)
		}
		seen[s.Name] = true
		if len(s.Install) < 2 || !installers[s.Install[0]] {
			t.Errorf("%s: install = %v, want an argv led by a known installer", s.Name, s.Install)
		}
		if s.InstallHint() != strings.Join(s.Install, " ") {
			t.Errorf("%s: hint %q does not match the install argv", s.Name, s.InstallHint())
		}
		if s.Language == "" || len(s.Command) == 0 || len(s.Extensions) == 0 || len(s.RootMarkers) == 0 || !s.Enabled {
			t.Errorf("%s: incomplete spec %+v", s.Name, s)
		}
		for _, ext := range s.Extensions {
			want, ok := languageIDs[ext]
			if !ok {
				t.Errorf("%s: no expected languageId for %s", s.Name, ext)
				continue
			}
			if got := LanguageID("/x/file" + ext); got != want {
				t.Errorf("LanguageID(%s) = %q, want %q", ext, got, want)
			}
		}
	}
	for _, name := range []string{"csharp", "go", "typescript", "python", "rust", "bash", "lua", "yaml", "json"} {
		if !seen[name] {
			t.Errorf("missing built-in spec %q", name)
		}
	}
	if strings.Join(TSInstall, " ") != TSInstallHint {
		t.Errorf("TSInstall %v disagrees with TSInstallHint %q", TSInstall, TSInstallHint)
	}
}

func TestForgetInvalidatesBinaryCache(t *testing.T) {
	var installed atomic.Bool
	lookups := 0
	r := NewRegistry(nil, Env{
		LookPath: func(n string) (string, error) {
			lookups++
			if installed.Load() {
				return "/bin/" + n, nil
			}
			return "", exec.ErrNotFound
		},
		IsExec: func(string) bool { return false },
	})
	if _, err := r.LookupBinary("pyright-langserver"); err == nil {
		t.Fatal("binary should be missing")
	}
	installed.Store(true)
	if _, err := r.LookupBinary("pyright-langserver"); err == nil || lookups != 1 {
		t.Fatalf("a failed lookup should be cached: err=%v lookups=%d", err, lookups)
	}
	r.Forget("pyright-langserver")
	if got, err := r.LookupBinary("pyright-langserver"); err != nil || got != "/bin/pyright-langserver" {
		t.Fatalf("after Forget = %q, %v", got, err)
	}
}

func TestFailingProbeCountsAsMissing(t *testing.T) {
	repo := realTemp(t)
	var probeOK atomic.Bool
	probes := 0
	specs := []ServerSpec{{Name: "rust", Command: []string{"rust-analyzer"}, Extensions: []string{".rs"}, Enabled: true,
		Install: []string{"rustup", "component", "add", "rust-analyzer"}, Probe: []string{"--version"}}}
	r := NewRegistry(specs, Env{
		LookPath: func(n string) (string, error) { return "/bin/" + n, nil },
		Home:     repo,
		Probe: func(bin string, args []string) error {
			probes++
			if bin != "/bin/rust-analyzer" || !reflect.DeepEqual(args, []string{"--version"}) {
				t.Errorf("probe ran %s %v", bin, args)
			}
			if probeOK.Load() {
				return nil
			}
			return errors.New("exit status 1")
		},
	})
	file := filepath.Join(repo, "main.rs")
	_, err := r.Resolve(file)
	var missing *MissingBinaryError
	if !errors.As(err, &missing) || missing.Binary != "rust-analyzer" || !reflect.DeepEqual(missing.Install, specs[0].Install) {
		t.Fatalf("err = %#v, want rust-analyzer missing with its install argv", err)
	}
	probeOK.Store(true)
	if _, err := r.Resolve(file); err == nil || probes != 1 {
		t.Fatalf("probe results should be cached: err=%v probes=%d", err, probes)
	}
	if _, err := r.LookupServer("rust-analyzer"); err == nil {
		t.Fatal("LookupServer should apply the cached failed probe")
	}
	r.Forget("rust-analyzer")
	if res, err := r.Resolve(file); err != nil || res.Binary != "/bin/rust-analyzer" || res.LanguageID != "rust" {
		t.Fatalf("after Forget = %+v, %v", res, err)
	}
	if got, err := r.LookupServer("rust-analyzer"); err != nil || got != "/bin/rust-analyzer" {
		t.Fatalf("LookupServer = %q, %v", got, err)
	}
	if got, err := r.LookupServer("other-tool"); err != nil || got != "/bin/other-tool" {
		t.Fatalf("LookupServer without a spec = %q, %v", got, err)
	}
}

func installSpecs() []ServerSpec {
	return []ServerSpec{{
		Name: "fake", Language: "Fake", Command: []string{"fakels", "--stdio"}, Extensions: []string{".fk"},
		RootMarkers: []string{"fake.root"}, Enabled: true, Install: []string{"fakepm", "install", "fakels"},
	}}
}

func toggledEnv(installed *atomic.Bool, home string) Env {
	return Env{
		LookPath: func(n string) (string, error) {
			if installed.Load() {
				return "/bin/" + n, nil
			}
			return "", exec.ErrNotFound
		},
		Home:   home,
		IsExec: func(string) bool { return false },
	}
}

func TestRetryAfterMissingBinary(t *testing.T) {
	var installed atomic.Bool
	h := newHarness(t, 1, fakeConfig(), withSpecs(installSpecs()), WithEnv(toggledEnv(&installed, t.TempDir())))
	a := h.file("a.fk", "x")
	if _, err := h.mgr.ClientFor(context.Background(), a); err == nil {
		t.Fatal("want a missing binary")
	}
	ev := h.nextEvent().(StatusEvent)
	if ev.State != StateFailed || !reflect.DeepEqual(ev.Install, []string{"fakepm", "install", "fakels"}) {
		t.Fatalf("failure event = %+v, want the install argv", ev)
	}
	installed.Store(true)
	if _, err := h.mgr.ClientFor(context.Background(), a); err == nil {
		t.Fatal("a recorded failure is not retried on its own")
	}
	h.mgr.Retry("fake")
	if ev := h.nextEvent().(StatusEvent); ev.Server != "fake" || ev.State != StateIdle {
		t.Fatalf("Retry should report the server idle, got %+v", ev)
	}
	for _, st := range h.mgr.Status() {
		if st.State == StateFailed {
			t.Fatalf("failure still listed: %+v", st)
		}
	}
	if c := h.client(a); c == nil || h.startCount() != 1 {
		t.Fatalf("server should start after Retry, starts=%d", h.startCount())
	}
}

func TestInstallRunsCommandThenRetries(t *testing.T) {
	var installed atomic.Bool
	var mu sync.Mutex
	var ran [][]string
	installer := func(_ context.Context, argv []string) ([]byte, error) {
		mu.Lock()
		ran = append(ran, argv)
		mu.Unlock()
		installed.Store(true)
		return []byte("added 1 package\n"), nil
	}
	h := newHarness(t, 1, fakeConfig(), withSpecs(installSpecs()), WithEnv(toggledEnv(&installed, t.TempDir())), WithInstaller(installer))
	a := h.file("a.fk", "x")
	if _, err := h.mgr.ClientFor(context.Background(), a); err == nil {
		t.Fatal("want a missing binary")
	}
	out, err := h.mgr.Install(context.Background(), "fake")
	if err != nil || out != "added 1 package\n" {
		t.Fatalf("Install = %q, %v", out, err)
	}
	if len(ran) != 1 || !reflect.DeepEqual(ran[0], []string{"fakepm", "install", "fakels"}) {
		t.Fatalf("ran %v", ran)
	}
	if c := h.client(a); c == nil {
		t.Fatal("server should start after a successful install")
	}
}

func TestInstallFailureKeepsFailure(t *testing.T) {
	var installed atomic.Bool
	installer := func(context.Context, []string) ([]byte, error) {
		return []byte("npm ERR! code EACCES\n"), errors.New("exit status 243")
	}
	h := newHarness(t, 1, fakeConfig(), withSpecs(installSpecs()), WithEnv(toggledEnv(&installed, t.TempDir())), WithInstaller(installer))
	a := h.file("a.fk", "x")
	_, _ = h.mgr.ClientFor(context.Background(), a)
	out, err := h.mgr.Install(context.Background(), "fake")
	if err == nil || !strings.Contains(out, "EACCES") {
		t.Fatalf("Install = %q, %v; want the failure and its output", out, err)
	}
	if _, err := h.mgr.ClientFor(context.Background(), a); err == nil {
		t.Fatal("a failed install keeps the server failed")
	}
}

func TestInstallUnknownServer(t *testing.T) {
	h := newHarness(t, 0, fakeConfig())
	if _, err := h.mgr.Install(context.Background(), "nope"); err == nil {
		t.Fatal("want an error for a server without an install command")
	}
}

func TestShutdownCancelsInstall(t *testing.T) {
	started := make(chan struct{})
	installer := func(ctx context.Context, _ []string) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	h := newHarness(t, 0, fakeConfig(), withSpecs(installSpecs()), WithInstaller(installer))
	done := make(chan error, 1)
	go func() {
		_, err := h.mgr.Install(context.Background(), "fake")
		done <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = h.mgr.Shutdown(ctx)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want cancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown should cancel a running install")
	}
}

func TestRunInstallOutputAndGroupKill(t *testing.T) {
	out, err := RunInstall(context.Background(), []string{"sh", "-c", "echo installing; echo done >&2"})
	if err != nil || !strings.Contains(string(out), "installing") || !strings.Contains(string(out), "done") {
		t.Fatalf("RunInstall = %q, %v", out, err)
	}
	marker := "30.734215"
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	begin := time.Now()
	_, err = RunInstall(ctx, []string{"sh", "-c", "sleep " + marker + " & wait"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(begin) > 3*time.Second {
		t.Fatalf("err = %v after %v, want a prompt deadline error", err, time.Since(begin))
	}
	waitFor(t, "the install's process group to die", func() bool {
		return exec.Command("pgrep", "-f", "sleep "+marker).Run() != nil
	})
}
