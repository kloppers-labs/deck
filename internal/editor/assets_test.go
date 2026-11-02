package editor

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var allTools = fakeLookup(map[string]string{"fzf": "/bin/fzf", "rg": "/bin/rg", "bat": "/bin/bat"})

func syncOpts() SyncOptions {
	return SyncOptions{LSP: true, LookPath: allTools, ZDeck: []byte("-- z_deck\n")}
}

func mustSync(t *testing.T, dir, version string, o SyncOptions) SyncResult {
	t.Helper()
	res, err := Sync(dir, version, o)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return res
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSyncWritesAllFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	res := mustSync(t, dir, "2.0.15", syncOpts())
	if !res.Features.LSP || !res.Features.Omni {
		t.Errorf("features = %+v, want LSP and Omni", res.Features)
	}
	for _, f := range []string{
		"colorschemes/dracula.micro",
		"plug/mlsp/main.lua",
		"plug/mlsp/json.lua",
		"plug/mlsp/config.lua",
		"plug/mlsp/LICENSE",
		"plug/mlsp/z_deck.lua",
		"plug/filemanager/filemanager.lua",
		"plug/MicroOmni/MicroOmni.lua",
		"plug/editorconfig/editorconfig.lua",
		"plug/deck/deck.lua",
		"plug/deck/repo.json",
		"settings.json",
		"bindings.json",
	} {
		if !exists(filepath.Join(dir, f)) {
			t.Errorf("%s was not written", f)
		}
	}
	if exists(filepath.Join(dir, "plug", "mlsp", "z_deck.lua.tmpl")) {
		t.Error("the template itself should not be installed")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "plug", "mlsp", "z_deck.lua")); string(got) != "-- z_deck\n" {
		t.Errorf("z_deck.lua = %q, want the rendered content", got)
	}
	if got := readJSON(t, filepath.Join(dir, "bindings.json")); got["Ctrl-p"] != "command:OmniGotoFile" {
		t.Errorf("bindings = %v", got)
	}
}

func mtimes(t *testing.T, dir string) map[string]time.Time {
	t.Helper()
	out := map[string]time.Time{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[p] = info.ModTime()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSyncNoRewriteWhenManifestSame(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	first := mustSync(t, dir, "2.0.15", syncOpts())
	old := time.Now().Add(-time.Hour)
	for p := range mtimes(t, dir) {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	second := mustSync(t, dir, "2.0.15", syncOpts())
	for p, m := range mtimes(t, dir) {
		if !m.Equal(old) {
			t.Errorf("%s was rewritten although nothing changed", p)
		}
	}
	if len(second.Written) != 0 || second.Digest != first.Digest {
		t.Errorf("second sync wrote %v, digest %q vs %q", second.Written, second.Digest, first.Digest)
	}

	o := syncOpts()
	o.ZDeck = []byte("-- changed\n")
	third := mustSync(t, dir, "2.0.15", o)
	if len(third.Written) != 1 || third.Written[0] != "plug/mlsp/z_deck.lua" || third.Digest == first.Digest {
		t.Errorf("only z_deck.lua should be rewritten, got %v", third.Written)
	}
}

func TestSyncRestoresDeletedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	mustSync(t, dir, "2.0.15", syncOpts())
	target := filepath.Join(dir, "plug", "deck", "deck.lua")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	mustSync(t, dir, "2.0.15", syncOpts())
	if !exists(target) {
		t.Error("a deleted bundled file should be written again")
	}
}

func TestSyncKeepsForeignDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	mine := filepath.Join(dir, "plug", "mine", "mine.lua")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("-- mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "plug", "MicroOmni", "notes.txt")
	mustSync(t, dir, "2.0.15", syncOpts())
	if err := os.WriteFile(extra, []byte("user file"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSync(t, dir, "2.0.13", syncOpts())
	if !exists(mine) {
		t.Error("a plugin deck didn't write must survive")
	}
	if !exists(extra) {
		t.Error("a file deck didn't write must survive even inside a bundled plugin directory")
	}
	if exists(filepath.Join(dir, "plug", "MicroOmni", "MicroOmni.lua")) {
		t.Error("deck's own MicroOmni files should be removed once MicroOmni is skipped")
	}
}

func TestSyncSkipsOmniOnOldMicroOrMissingTools(t *testing.T) {
	cases := []struct {
		name    string
		version string
		tools   map[string]string
	}{
		{"old micro", "2.0.13", map[string]string{"fzf": "x", "rg": "x", "bat": "x"}},
		{"unknown micro", "", map[string]string{"fzf": "x", "rg": "x", "bat": "x"}},
		{"no fzf", "2.0.15", map[string]string{"rg": "x", "bat": "x"}},
		{"no rg", "2.0.15", map[string]string{"fzf": "x", "bat": "x"}},
		{"no bat", "2.0.15", map[string]string{"fzf": "x", "rg": "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "micro")
			o := syncOpts()
			o.LookPath = fakeLookup(c.tools)
			res := mustSync(t, dir, c.version, o)
			if res.Features.Omni || exists(filepath.Join(dir, "plug", "MicroOmni")) {
				t.Error("MicroOmni should be skipped")
			}
			if got := readJSON(t, filepath.Join(dir, "bindings.json")); got["Ctrl-p"] != nil {
				t.Errorf("Ctrl-p should not be bound without MicroOmni: %v", got)
			}
			if !exists(filepath.Join(dir, "plug", "filemanager", "filemanager.lua")) {
				t.Error("other plugins are still installed")
			}
		})
	}
}

func TestSyncWithoutLSPSkipsMLSP(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	o := syncOpts()
	o.LSP = false
	res := mustSync(t, dir, "2.0.15", o)
	if res.Features.LSP || exists(filepath.Join(dir, "plug", "mlsp")) {
		t.Error("micro_lsp off should leave mlsp out")
	}
}

func TestSyncReportsUnparseableSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\n  // comment\n  \"softwrap\": true\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := mustSync(t, dir, "2.0.15", syncOpts())
	if len(res.Warnings) != 1 {
		t.Errorf("warnings = %q, want one about settings.json", res.Warnings)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(got) != body {
		t.Error("settings.json with comments must be left alone")
	}
}
