package editor

import (
	"testing"
)

func TestTruecolorOnlyWhenColorterm(t *testing.T) {
	cases := []struct {
		colorterm string
		want      bool
	}{
		{"truecolor", true},
		{"24bit", true},
		{"", false},
		{"8bit", false},
	}
	for _, c := range cases {
		env := []string{"TERM=xterm-256color"}
		if c.colorterm != "" {
			env = append(env, "COLORTERM="+c.colorterm)
		}
		cmd, _, _ := Launch("micro", Target{Path: "/tmp/a.go"}, Opts{ConfigDir: "/cfg", Environ: env})
		v, ok := envValue(cmd.Env, "MICRO_TRUECOLOR")
		if got := ok && v == "1"; got != c.want {
			t.Errorf("COLORTERM=%q: MICRO_TRUECOLOR set = %v, want %v", c.colorterm, got, c.want)
		}
	}
}

func TestStateEnvSet(t *testing.T) {
	cmd, _, _ := Launch("micro", Target{Path: "/tmp/a.go", Line: 40, Col: 2, Top: 31}, Opts{
		ConfigDir: "/cfg",
		StateFile: "/tmp/deck-state",
		Environ:   []string{"DECK_EDITOR_TOP=999"},
	})
	if v, _ := envValue(cmd.Env, "DECK_EDITOR_STATE"); v != "/tmp/deck-state" {
		t.Errorf("DECK_EDITOR_STATE = %q, want /tmp/deck-state", v)
	}
	if v, _ := envValue(cmd.Env, "DECK_EDITOR_TOP"); v != "31" {
		t.Errorf("DECK_EDITOR_TOP = %q, want 31", v)
	}

	cmd, _, _ = Launch("micro", Target{Path: "/tmp/a.go"}, Opts{ConfigDir: "/cfg", Environ: []string{"DECK_EDITOR_STATE=/stale"}})
	if v, ok := envValue(cmd.Env, "DECK_EDITOR_STATE"); ok && v != "" {
		t.Errorf("an inherited DECK_EDITOR_STATE must not leak through, got %q", v)
	}
}

func TestParseMicroVersion(t *testing.T) {
	cases := map[string]string{
		"Version: 2.0.15\nCommit hash: 6a62575b\n": "2.0.15",
		"Version: 2.0.14-dev.12\n":                 "2.0.14-dev.12",
		"garbage":                                  "",
	}
	for in, want := range cases {
		if got := ParseMicroVersion(in); got != want {
			t.Errorf("ParseMicroVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"2.0.14", true},
		{"2.0.15", true},
		{"2.1.0", true},
		{"3.0", true},
		{"2.0.13", false},
		{"2.0.14-dev.3", true},
		{"1.4.1", false},
		{"", false},
	}
	for _, c := range cases {
		if got := versionAtLeast(c.v, "2.0.14"); got != c.want {
			t.Errorf("versionAtLeast(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestIsMicro(t *testing.T) {
	for editor, want := range map[string]bool{
		"":                        true,
		"micro":                   true,
		"/opt/homebrew/bin/micro": true,
		"micro -softwrap true":    true,
		"vim":                     false,
		"code -w":                 false,
	} {
		if got := IsMicro(editor); got != want {
			t.Errorf("IsMicro(%q) = %v, want %v", editor, got, want)
		}
	}
}
