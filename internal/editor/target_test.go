package editor

import (
	"slices"
	"testing"

	"deck/internal/ui"
)

func TestOpenMsgRoutesToApp(t *testing.T) {
	if got := (OpenMsg{}).RouteTo(); got != ui.DestApp {
		t.Errorf("OpenMsg routes to %v, want DestApp", got)
	}
	if msg, ok := Open(Target{Path: "/x"})().(OpenMsg); !ok || msg.Path != "/x" {
		t.Errorf("Open() = %+v, want an OpenMsg for /x", msg)
	}
}

func TestCommandAtMicroAddsPosition(t *testing.T) {
	redirectConfigHome(t)
	c := CommandAt("micro", Target{Path: "/p/Foo.cs", Line: 12, Col: 5})
	n := len(c.Args)
	if c.Args[n-1] != "/p/Foo.cs" || c.Args[n-2] != "+12:5" {
		t.Errorf("args = %q, want ... +12:5 /p/Foo.cs", c.Args)
	}
}

func TestCommandAtWithoutLineIsPlain(t *testing.T) {
	redirectConfigHome(t)
	c := CommandAt("micro", Target{Path: "/p/Foo.cs"})
	if slices.ContainsFunc(c.Args, func(a string) bool { return len(a) > 0 && a[0] == '+' }) {
		t.Errorf("args = %q, want no position", c.Args)
	}
}

func TestCommandAtOtherEditorIgnoresPosition(t *testing.T) {
	c := CommandAt("code -w", Target{Path: "/p/a.go", Line: 3, Col: 1})
	if !slices.Equal(c.Args, []string{"code", "-w", "/p/a.go"}) {
		t.Errorf("args = %q", c.Args)
	}
}
