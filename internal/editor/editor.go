// Package editor builds the command deck uses to launch the user's editor.
//
// For micro specifically, deck ships its own Dracula colorscheme and points
// micro at a private config directory via MICRO_CONFIG_HOME, so the editor
// matches deck's palette without touching the user's own ~/.config/micro. deck
// owns the contents of ~/.config/deck/micro; users should not edit it.
//
// Any other editor is launched plainly. Theming is best-effort: if the private
// config can't be written, the editor still launches without it.
package editor

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"deck/internal/ui"
)

// Target is where to open the editor. Line, Col and Top are 1-based and 0
// means unset; Col is a rune column in the raw line.
type Target struct {
	Path           string
	Line, Col, Top int
}

// OpenMsg asks the root model to open the editor at Target.
type OpenMsg struct{ Target Target }

func (OpenMsg) RouteTo() ui.Dest { return ui.DestApp }

//go:embed colorschemes/dracula.micro
var draculaMicro []byte

// settingsJSON seeds sensible micro defaults in deck's private config. The
// colorscheme is also forced via CLI flag, so this mainly carries the editing
// ergonomics.
const settingsJSON = `{
  "colorscheme": "dracula",
  "syntax": true,
  "softwrap": true,
  "ruler": true,
  "tabsize": 4
}
`

// configHome resolves deck's private micro config directory. It's a variable so
// tests can redirect it away from the real home directory.
var configHome = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "deck", "micro"), nil
}

// Command launches the configured editor (which may carry arguments) on t. micro
// also gets +LINE:COL and deck's colorscheme unless the user set one.
func Command(editor string, t Target) *exec.Cmd {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"micro"}
	}
	name, args := parts[0], parts[1:]
	if filepath.Base(name) != "micro" {
		return exec.Command(name, append(args, t.Path)...)
	}
	if pos := microPosition(t); pos != "" {
		args = append(args, pos)
	}
	dir, err := Prepare()
	if err != nil {
		return exec.Command(name, append(args, t.Path)...)
	}
	if !slices.Contains(args, "-colorscheme") {
		args = append(args, "-colorscheme", "dracula")
	}
	c := exec.Command(name, append(args, t.Path)...)
	c.Env = append(os.Environ(),
		"MICRO_CONFIG_HOME="+dir,
		"MICRO_TRUECOLOR=1",
	)
	return c
}

// Prepare writes deck's bundled micro colorscheme and default settings into the
// private config directory, returning its path. The colorscheme is rewritten on
// every call so theme updates reach users; settings.json is only seeded if
// absent.
func Prepare() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	csDir := filepath.Join(dir, "colorschemes")
	if err := os.MkdirAll(csDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(csDir, "dracula.micro"), draculaMicro, 0o644); err != nil {
		return "", err
	}
	settingsPath := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := os.WriteFile(settingsPath, []byte(settingsJSON), 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func microPosition(t Target) string {
	if t.Line <= 0 {
		return ""
	}
	pos := "+" + strconv.Itoa(t.Line)
	if t.Col > 0 {
		pos += ":" + strconv.Itoa(t.Col)
	}
	return pos
}
