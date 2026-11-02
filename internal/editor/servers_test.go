package editor

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"deck/internal/config"
	"deck/internal/lsp"
)

var update = flag.Bool("update", false, "rewrite golden files")

func fakeLookup(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
}

func goldenZDeck(t *testing.T) ([]byte, []MissingServer) {
	t.Helper()
	return RenderZDeck(lsp.DefaultSpecs(), fakeLookup(map[string]string{
		"csharp-ls": "/home/dev/.dotnet/tools/csharp-ls",
		"gopls":     "/home/dev/go/bin/gopls",
	}))
}

func TestZDeckGolden(t *testing.T) {
	got, missing := goldenZDeck(t)
	golden := filepath.Join("testdata", "z_deck.golden.lua")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("z_deck.lua differs from %s (run go test -update):\n%s", golden, got)
	}
	if len(missing) != 1 || missing[0].Binary != "typescript-language-server" || missing[0].Hint == "" {
		t.Errorf("missing = %+v, want only typescript-language-server with an install hint", missing)
	}
}

func TestZDeckLuacParses(t *testing.T) {
	luac, err := exec.LookPath("luac")
	if err != nil {
		t.Skip("luac not on PATH")
	}
	got, _ := goldenZDeck(t)
	path := filepath.Join(t.TempDir(), "z_deck.lua")
	if err := os.WriteFile(path, got, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(luac, "-p", path).CombinedOutput(); err != nil {
		t.Errorf("luac -p: %v\n%s", err, out)
	}
}

func TestZDeckQuotesAwkwardPaths(t *testing.T) {
	specs := []lsp.ServerSpec{{Name: "go", Command: []string{"gopls", "-remote=auto"}, Extensions: []string{".go"}, Enabled: true}}
	got, _ := RenderZDeck(specs, fakeLookup(map[string]string{"gopls": "/Users/me/My \"Tools\"\\bin/gopls"}))
	if !slices.Contains(strings.Split(string(got), "\n"), `        cmd = "/Users/me/My \"Tools\"\\bin/gopls",`) {
		t.Errorf("paths should be quoted as Lua strings:\n%s", got)
	}
}

func TestZDeckSkipsDisabledServers(t *testing.T) {
	specs := lsp.DefaultSpecs()
	for i := range specs {
		specs[i].Enabled = specs[i].Name != "go"
	}
	_, missing := RenderZDeck(specs, fakeLookup(nil))
	for _, m := range missing {
		if m.Server == "go" {
			t.Error("a disabled server is neither rendered nor reported missing")
		}
	}
}

func TestSpecsFromConfig(t *testing.T) {
	off := false
	specs := SpecsFromConfig(config.LSP{Servers: map[string]config.LSPServer{
		"csharp": {Command: []string{"/opt/csharp-ls"}, Solution: "App.sln"},
		"go":     {Enabled: &off},
	}})
	byName := map[string]lsp.ServerSpec{}
	for _, s := range specs {
		byName[s.Name] = s
	}
	if cs := byName["csharp"]; cs.Command[0] != "/opt/csharp-ls" || cs.Solution != "App.sln" {
		t.Errorf("csharp override not applied: %+v", cs)
	}
	if byName["go"].Enabled {
		t.Error("go should be disabled by config")
	}
	if !byName["typescript"].Enabled {
		t.Error("untouched servers keep their defaults")
	}
}
