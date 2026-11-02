package editor

import (
	"slices"
	"strings"
	"testing"

	"deck/internal/ui"
)

func envValue(env []string, key string) (string, bool) {
	var val string
	var found bool
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			val, found = v, true
		}
	}
	return val, found
}

func TestLaunchMicroAddsColorschemeWithBundle(t *testing.T) {
	c, mode, _ := Launch("micro", Target{Path: "/tmp/file.go", Line: 2}, Opts{ConfigDir: "/cfg/micro"})
	if mode != Suspend {
		t.Errorf("micro mode = %v, want Suspend", mode)
	}
	want := []string{"micro", "-colorscheme", "dracula", "+2", "/tmp/file.go"}
	if !slices.Equal(c.Args, want) {
		t.Errorf("args = %q, want %q", c.Args, want)
	}
	if v, _ := envValue(c.Env, "MICRO_CONFIG_HOME"); v != "/cfg/micro" {
		t.Errorf("MICRO_CONFIG_HOME = %q, want /cfg/micro", v)
	}
}

func TestLaunchMicroRespectsUserColorscheme(t *testing.T) {
	c, _, _ := Launch("micro -colorscheme gruvbox", Target{Path: "/tmp/file.go"}, Opts{ConfigDir: "/cfg/micro"})
	joined := strings.Join(c.Args, " ")
	if strings.Contains(joined, "dracula") || !strings.Contains(joined, "-colorscheme gruvbox") {
		t.Errorf("user-supplied colorscheme should win, got %v", c.Args)
	}
}

func TestLaunchMicroWithoutBundleIsPlain(t *testing.T) {
	c, _, _ := Launch("micro", Target{Path: "/tmp/file.go", Line: 4, Col: 2}, Opts{Environ: []string{"TERM=xterm"}})
	if want := []string{"micro", "+4:2", "/tmp/file.go"}; !slices.Equal(c.Args, want) {
		t.Errorf("args = %q, want %q", c.Args, want)
	}
	if _, ok := envValue(c.Env, "MICRO_CONFIG_HOME"); ok {
		t.Error("without the bundle micro should use the user's own config")
	}
}

func TestLaunchOtherEditorIsPlain(t *testing.T) {
	c, _, _ := Launch("vim -u NONE", Target{Path: "/tmp/file.go", Line: 12, Col: 5}, Opts{ConfigDir: "/cfg/micro", StateFile: "/tmp/s"})
	if want := []string{"vim", "-u", "NONE", "+call cursor(12,5)", "/tmp/file.go"}; !slices.Equal(c.Args, want) {
		t.Errorf("args = %q, want %q", c.Args, want)
	}
	if c.Env != nil {
		t.Errorf("non-micro editors inherit the environment unchanged, got %v", c.Env)
	}
}

func TestLaunchNeedsAPath(t *testing.T) {
	if _, _, err := Launch("micro", Target{}, Opts{}); err == nil {
		t.Error("Launch without a path should fail")
	}
}

func TestOpenMsgRoutesToApp(t *testing.T) {
	if got := (OpenMsg{}).RouteTo(); got != ui.DestApp {
		t.Errorf("OpenMsg.RouteTo = %v, want DestApp", got)
	}
}
