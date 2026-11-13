package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestDirPrefersXDGRuntime(t *testing.T) {
	got, err := Dir(env("XDG_RUNTIME_DIR", "/run/user/501", "XDG_STATE_HOME", "/s"), "/h")
	if err != nil || got != "/run/user/501/deck" {
		t.Errorf("Dir = %q, %v; want /run/user/501/deck", got, err)
	}
}

func TestDirIgnoresTMPDIR(t *testing.T) {
	got, err := Dir(env("TMPDIR", "/var/folders/x/T/"), "/h")
	if err != nil || got != "/h/.local/state/deck/run" {
		t.Errorf("Dir = %q, %v; want /h/.local/state/deck/run", got, err)
	}
}

func TestDirStateHome(t *testing.T) {
	got, err := Dir(env("XDG_STATE_HOME", "/s"), "/h")
	if err != nil || got != "/s/deck/run" {
		t.Errorf("Dir = %q, %v; want /s/deck/run", got, err)
	}
}

func TestDirNeedsAHome(t *testing.T) {
	if _, err := Dir(env(), ""); err == nil {
		t.Error("no home and no XDG dirs should fail")
	}
}

func TestSocketPathTooLong(t *testing.T) {
	if _, err := SocketPath("/"+strings.Repeat("x", 120), 42); err == nil {
		t.Error("a socket path over the platform limit should fail")
	}
	got, err := SocketPath("/d", 42)
	if err != nil || got != "/d/42.sock" {
		t.Errorf("SocketPath = %q, %v", got, err)
	}
}

func TestRegisterListCleanup(t *testing.T) {
	dir := filepath.Join(shortDir(t), "run")
	sock, _ := SocketPath(dir, os.Getpid())
	inst := Instance{PID: os.Getpid(), Root: "/r", Cwd: "/r/sub", Version: "dev", Socket: sock, Started: time.Unix(100, 0)}
	cleanup, err := Register(dir, inst)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
	side := filepath.Join(dir, sidecarName(os.Getpid()))
	if fi, err := os.Stat(side); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("sidecar mode = %v, %v; want 0600", fi, err)
	}
	stop := serveForTest(t, sock, nil, nil)
	defer stop()
	got, err := List(dir)
	if err != nil || len(got) != 1 || got[0].Root != "/r" || got[0].Cwd != "/r/sub" {
		t.Fatalf("List = %+v, %v", got, err)
	}
	if err := Touch(dir, os.Getpid(), time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	if got, _ := List(dir); !got[0].Focused.Equal(time.Unix(200, 0)) {
		t.Errorf("Focused = %v, want the touched time", got[0].Focused)
	}
	cleanup()
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Error("cleanup should remove the sidecar")
	}
}

func TestListSweepsDeadPid(t *testing.T) {
	dir := shortDir(t)
	sock, _ := SocketPath(dir, 999999)
	if _, err := Register(dir, Instance{PID: 999999, Root: "/r", Socket: sock}); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil || len(got) != 0 {
		t.Errorf("List = %+v, %v; want the dead instance swept", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, sidecarName(999999))); !os.IsNotExist(err) {
		t.Error("the dead instance's sidecar should be removed")
	}
}

func TestListSweepsRefusingSocket(t *testing.T) {
	dir := shortDir(t)
	sock, _ := SocketPath(dir, os.Getpid())
	if _, err := Register(dir, Instance{PID: os.Getpid(), Root: "/r", Socket: sock}); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil || len(got) != 0 {
		t.Errorf("List = %+v, %v; want an instance without a listening socket swept", got, err)
	}
}
