// Package editor launches the user's editor at a position and, for micro, ships
// deck's bundled runtime (plugins, settings, bindings) and reads back where it closed.
package editor

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/ui"
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

func Open(t Target) tea.Cmd { return func() tea.Msg { return OpenMsg{Target: t} } }

// Mode says whether deck hands the terminal to the editor or lets it run alongside.
type Mode int

const (
	Suspend Mode = iota
	Detach
)

// Opts carries what Launch needs beyond the editor string and target.
// ConfigDir is deck's micro runtime; empty means micro runs with the user's own config.
type Opts struct {
	ConfigDir string
	StateFile string
	Cwd       string
	Environ   []string
}

// Launch builds the command that opens t in the configured editor.
func Launch(editorCfg string, t Target, o Opts) (*exec.Cmd, Mode, error) {
	if t.Path == "" {
		return nil, Suspend, errors.New("no file to open")
	}
	var extra []string
	if IsMicro(editorCfg) && o.ConfigDir != "" && !slices.Contains(strings.Fields(editorCfg), "-colorscheme") {
		extra = []string{"-colorscheme", "dracula"}
	}
	name, args, mode := buildArgs(editorCfg, t, extra)
	c := exec.Command(name, args...)
	if IsMicro(editorCfg) {
		configureMicro(c, t, o)
	}
	return c, mode, c.Err
}

func environ(o Opts) []string {
	if o.Environ != nil {
		return slices.Clone(o.Environ)
	}
	return os.Environ()
}
