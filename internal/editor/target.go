package editor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
)

// Target is a place to open in the editor. Line, Col and Top are 1-based; 0
// means unset.
type Target struct {
	Path           string
	Line, Col, Top int
}

// OpenMsg asks the root model to open Target in the editor.
type OpenMsg struct{ Target }

func (OpenMsg) RouteTo() ui.Dest { return ui.DestApp }

func Open(t Target) tea.Cmd { return func() tea.Msg { return OpenMsg{t} } }

// CommandAt is Command with the cursor placed at t's line and column, for
// editors that support it (micro: "+L:C").
func CommandAt(editor string, t Target) *exec.Cmd {
	c := Command(editor, t.Path)
	if t.Line <= 0 || filepath.Base(c.Args[0]) != "micro" {
		return c
	}
	pos := fmt.Sprintf("+%d:%d", t.Line, max(t.Col, 1))
	c.Args = slices.Insert(c.Args, len(c.Args)-1, pos)
	return c
}
