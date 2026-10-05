package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// redirectConfigHome points configHome at a temp dir for the duration of a test.
func redirectConfigHome(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "micro")
	prev := configHome
	configHome = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configHome = prev })
	return dir
}

func TestPrepareWritesColorscheme(t *testing.T) {
	dir := redirectConfigHome(t)
	got, err := Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("Prepare() = %q, want %q", got, dir)
	}
	cs := filepath.Join(dir, "colorschemes", "dracula.micro")
	if _, err := os.Stat(cs); err != nil {
		t.Errorf("colorscheme not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Errorf("settings.json not written: %v", err)
	}
}

func TestPrepareDoesNotClobberSettings(t *testing.T) {
	dir := redirectConfigHome(t)
	if _, err := Prepare(); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")
	custom := []byte(`{"colorscheme":"dracula","tabsize":2}`)
	if err := os.WriteFile(settings, custom, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Errorf("settings.json was overwritten: %s", got)
	}
}

func TestCommandMicroForcesTheme(t *testing.T) {
	redirectConfigHome(t)
	c := Command("micro", "/tmp/file.go")
	if !strings.Contains(strings.Join(c.Args, " "), "-colorscheme dracula") {
		t.Errorf("micro command missing colorscheme flag: %v", c.Args)
	}
	var sawConfigHome, sawTrueColor bool
	for _, e := range c.Env {
		if strings.HasPrefix(e, "MICRO_CONFIG_HOME=") {
			sawConfigHome = true
		}
		if e == "MICRO_TRUECOLOR=1" {
			sawTrueColor = true
		}
	}
	if !sawConfigHome || !sawTrueColor {
		t.Errorf("micro command missing env: config_home=%v truecolor=%v", sawConfigHome, sawTrueColor)
	}
}

func TestCommandPassesThroughArgs(t *testing.T) {
	redirectConfigHome(t)
	c := Command("vim -u NONE", "/tmp/file.go")
	want := []string{"vim", "-u", "NONE", "/tmp/file.go"}
	if len(c.Args) != len(want) {
		t.Fatalf("args = %v, want %v", c.Args, want)
	}
	for i, a := range want {
		if c.Args[i] != a {
			t.Errorf("arg %d = %q, want %q", i, c.Args[i], a)
		}
	}
}

func TestCommandMicroRespectsUserColorscheme(t *testing.T) {
	redirectConfigHome(t)
	c := Command("micro -colorscheme gruvbox", "/tmp/file.go")
	joined := strings.Join(c.Args, " ")
	if strings.Contains(joined, "dracula") {
		t.Errorf("user-supplied colorscheme should win, got %v", c.Args)
	}
	if !strings.Contains(joined, "-colorscheme gruvbox") {
		t.Errorf("user colorscheme flag lost: %v", c.Args)
	}
}

func TestCommandOtherEditorIsPlain(t *testing.T) {
	redirectConfigHome(t)
	c := Command("vim", "/tmp/file.go")
	if joined := strings.Join(c.Args, " "); strings.Contains(joined, "-colorscheme") {
		t.Errorf("non-micro editor should not get colorscheme flags: %v", c.Args)
	}
	for _, e := range c.Env {
		if strings.HasPrefix(e, "MICRO_CONFIG_HOME=") {
			t.Errorf("non-micro editor should not get MICRO_CONFIG_HOME: %v", c.Env)
		}
	}
}
