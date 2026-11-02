package editor

import (
	"slices"
	"testing"
)

func TestProfiles(t *testing.T) {
	const p = "/src/app/main.go"
	at := Target{Path: p, Line: 12, Col: 5, Top: 3}
	cases := []struct {
		name   string
		editor string
		target Target
		want   []string
		mode   Mode
	}{
		{"micro", "micro", at, []string{"micro", "+12:5", p}, Suspend},
		{"micro line only", "micro", Target{Path: p, Line: 7}, []string{"micro", "+7", p}, Suspend},
		{"micro keeps user args", "micro -softwrap false", at, []string{"micro", "-softwrap", "false", "+12:5", p}, Suspend},
		{"vim", "vim", at, []string{"vim", "+call cursor(12,5)", p}, Suspend},
		{"nvim by full path", "/opt/homebrew/bin/nvim", at, []string{"/opt/homebrew/bin/nvim", "+call cursor(12,5)", p}, Suspend},
		{"vim line only", "vim", Target{Path: p, Line: 7}, []string{"vim", "+7", p}, Suspend},
		{"helix", "hx", at, []string{"hx", p + ":12:5"}, Suspend},
		{"zed", "zed", at, []string{"zed", p + ":12:5"}, Detach},
		{"subl waits", "subl -w", at, []string{"subl", "-w", p + ":12:5"}, Suspend},
		{"code detaches", "code", at, []string{"code", "-g", p + ":12:5"}, Detach},
		{"code -w suspends", "code -w", at, []string{"code", "-w", "-g", p + ":12:5"}, Suspend},
		{"code --wait suspends", "code --wait", at, []string{"code", "--wait", "-g", p + ":12:5"}, Suspend},
		{"cursor is code family", "cursor", at, []string{"cursor", "-g", p + ":12:5"}, Detach},
		{"nano", "nano", at, []string{"nano", "+12,5", p}, Suspend},
		{"emacs", "emacs -nw", at, []string{"emacs", "-nw", "+12:5", p}, Suspend},
		{"kak", "kak", at, []string{"kak", "+12:5", p}, Suspend},
		{"rider", "rider", at, []string{"rider", "--line", "12", "--column", "5", p}, Detach},
		{"idea", "idea", at, []string{"idea", "--line", "12", "--column", "5", p}, Detach},
		{"template", "hx {file}:{line}:{col}", at, []string{"hx", p + ":12:5"}, Suspend},
		{"template without position", "myedit --goto {file} {line} {col}", Target{Path: p}, []string{"myedit", "--goto", p, "1", "1"}, Suspend},
		{"unknown editor gets the path", "ed", at, []string{"ed", p}, Suspend},
		{"empty editor is micro", "", at, []string{"micro", "+12:5", p}, Suspend},
		{"no line: micro", "micro", Target{Path: p}, []string{"micro", p}, Suspend},
		{"no line: vim", "vim", Target{Path: p}, []string{"vim", p}, Suspend},
		{"no line: code", "code", Target{Path: p}, []string{"code", p}, Detach},
		{"no line: rider", "rider", Target{Path: p}, []string{"rider", p}, Detach},
		{"no line: hx", "hx", Target{Path: p}, []string{"hx", p}, Suspend},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, args, mode := Args(c.editor, c.target)
			got := append([]string{name}, args...)
			if !slices.Equal(got, c.want) {
				t.Errorf("args = %q, want %q", got, c.want)
			}
			if mode != c.mode {
				t.Errorf("mode = %v, want %v", mode, c.mode)
			}
		})
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"code -w":              "VS Code",
		"/usr/local/bin/code":  "VS Code",
		"cursor":               "Cursor",
		"rider":                "Rider",
		"zed":                  "Zed",
		"micro -softwrap true": "micro",
	}
	for editor, want := range cases {
		if got := DisplayName(editor); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", editor, got, want)
		}
	}
}
