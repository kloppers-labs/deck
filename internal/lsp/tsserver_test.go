package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFindTSServerPrefersProjectInstall(t *testing.T) {
	tmp := t.TempDir()
	want := filepath.Join(tmp, "app", tsserverRel)
	touch(t, want)
	got, local := FindTSServer(filepath.Join(tmp, "app", "src", "components"), "")
	if got != want || !local {
		t.Errorf("FindTSServer = %q local=%v, want %q local", got, local, want)
	}
}

func TestFindTSServerBesideGlobalLanguageServer(t *testing.T) {
	tmp := t.TempDir()
	prefix := filepath.Join(tmp, "lib")
	cli := filepath.Join(prefix, "node_modules", "typescript-language-server", "lib", "cli.mjs")
	touch(t, cli)
	want := filepath.Join(prefix, tsserverRel)
	touch(t, want)
	bin := filepath.Join(tmp, "bin", "typescript-language-server")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cli, bin); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	got, local := FindTSServer(project, bin)
	wantReal, _ := filepath.EvalSymlinks(want)
	if got != wantReal || local {
		t.Errorf("FindTSServer = %q local=%v, want global %q", got, local, wantReal)
	}
}

func TestResolveTypeScriptWithoutInstallIsMissing(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin", "typescript-language-server")
	touch(t, bin)
	if got, _ := FindTSServer("", bin); got != "" {
		t.Skipf("machine has a global TypeScript at %s", got)
	}
	project := filepath.Join(tmp, "project")
	touch(t, filepath.Join(project, "package.json"))
	specs := []ServerSpec{{Name: "typescript", Command: []string{bin, "--stdio"}, Extensions: []string{".ts"}, RootMarkers: []string{"package.json"}, Enabled: true, NeedsTypeScript: true}}
	r := NewRegistry(specs, Env{IsExec: func(string) bool { return true }})
	_, err := r.Resolve(filepath.Join(project, "a.ts"))
	var missing *MissingBinaryError
	if !errors.As(err, &missing) || missing.Binary != "typescript" || missing.Hint != TSInstallHint {
		t.Errorf("Resolve error = %v, want missing typescript with hint", err)
	}
}

func TestResolveTypeScriptPassesGlobalFallback(t *testing.T) {
	tmp := t.TempDir()
	prefix := filepath.Join(tmp, "lib")
	bin := filepath.Join(prefix, "node_modules", "typescript-language-server", "lib", "cli.mjs")
	touch(t, bin)
	ts := filepath.Join(prefix, tsserverRel)
	touch(t, ts)
	project := filepath.Join(tmp, "project")
	touch(t, filepath.Join(project, "package.json"))
	specs := []ServerSpec{{Name: "typescript", Command: []string{bin, "--stdio"}, Extensions: []string{".ts"}, RootMarkers: []string{"package.json"}, Enabled: true, NeedsTypeScript: true}}
	r := NewRegistry(specs, Env{IsExec: func(string) bool { return true }})
	res, err := r.Resolve(filepath.Join(project, "a.ts"))
	if err != nil {
		t.Fatal(err)
	}
	opts, _ := res.InitOptions.(map[string]any)
	tsOpts, _ := opts["tsserver"].(map[string]any)
	wantReal, _ := filepath.EvalSymlinks(ts)
	if tsOpts["fallbackPath"] != wantReal {
		t.Errorf("InitOptions = %#v, want tsserver.fallbackPath %q", res.InitOptions, wantReal)
	}
}
