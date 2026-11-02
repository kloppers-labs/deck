package httpc

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseEnvJSON(t *testing.T) {
	envs, err := parseEnvJSON([]byte(`{
		"$shared": {"version": "v1"},
		"dev": {"host": "http://localhost:5000", "port": 5000, "debug": true, "nested": {"x": 1}},
		"prod": {"host": "https://api.example.com"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if envs["dev"]["port"] != "5000" || envs["dev"]["debug"] != "true" {
		t.Errorf("dev = %v, want scalars stringified", envs["dev"])
	}
	if _, ok := envs["dev"]["nested"]; ok {
		t.Error("nested objects are not variables")
	}
}

func TestLoadEnvsMergesPrivateAndShared(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src", "Api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(filepath.Join(root, "http-client.env.json"), `{"$shared":{"v":"1"},"dev":{"host":"h-dev","token":"public"},"prod":{"host":"h-prod"}}`)
	writeJSON(filepath.Join(root, "http-client.private.env.json"), `{"dev":{"token":"secret"}}`)
	envs := loadEnvs(dir, root)
	if !slices.Equal(envs.names, []string{"dev", "prod"}) {
		t.Fatalf("names = %v, want [dev prod]", envs.names)
	}
	dev := envs.vars("dev")
	if dev["host"] != "h-dev" || dev["token"] != "secret" || dev["v"] != "1" {
		t.Errorf("dev = %v, want private over public and $shared included", dev)
	}
	if got := envs.vars(""); got["v"] != "1" || got["host"] != "" {
		t.Errorf("no env = %v, want only $shared", got)
	}
}

func TestLoadEnvsNearestWins(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "http-client.env.json"), []byte(`{"root":{}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"near":{}}`), 0o644)
	if got := loadEnvs(dir, root).names; !slices.Equal(got, []string{"near"}) {
		t.Errorf("names = %v, want the nearest env file", got)
	}
}

func TestNextEnvCycles(t *testing.T) {
	e := envSet{names: []string{"dev", "prod"}}
	seq := []string{e.next("")}
	for range 3 {
		seq = append(seq, e.next(seq[len(seq)-1]))
	}
	if want := []string{"dev", "prod", "", "dev"}; !slices.Equal(seq, want) {
		t.Errorf("cycle = %q, want %q", seq, want)
	}
}
