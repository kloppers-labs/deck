package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

func TestInvalidateRoutesToFiles(t *testing.T) {
	if got := (InvalidateMsg{}).RouteTo(); got != ui.DestFiles {
		t.Errorf("InvalidateMsg.RouteTo = %v, want DestFiles", got)
	}
}

func TestInvalidateReloadsSavedViewerFile(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30})
	m := started(t, dir, 90, 30)
	m, _ = press(t, m, "enter", "j", "j", "j")
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(strings.Repeat("edited in micro\n", 30)), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Update(InvalidateMsg{Saved: []string{path}})
	if cmd == nil {
		t.Fatal("a saved file shown in the viewer should be reloaded")
	}
	m, _ = settle(t, m, cmd)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "edited in micro") {
		t.Errorf("viewer should show the saved content:\n%s", v)
	}
	if line := m.viewer.Loc().Line; line != 3 {
		t.Errorf("reload should keep the cursor on line 3, got %d", line)
	}
}

func TestInvalidateIgnoresOtherFiles(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3, "b.go": 3})
	m := started(t, dir, 90, 30)
	if _, cmd := m.Update(InvalidateMsg{Saved: []string{filepath.Join(dir, "b.go")}}); cmd != nil {
		t.Error("saving a file that isn't shown needs no reload")
	}
}
