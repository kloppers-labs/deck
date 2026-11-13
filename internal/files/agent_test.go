package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshTreeKeepsViewer(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 5, "b.go": 5})
	outside := fixture(t, map[string]int{"lib.go": 50})
	m := started(t, dir, 120, 40)
	ext := filepath.Join(outside, "lib.go")
	m, cmd := m.AgentOpen(ext, 30, 0, 0)
	m, _ = settle(t, m, cmd)
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd = m.Update(RefreshTreeMsg{})
	m, _ = settle(t, m, cmd)
	if m.viewer.Path() != ext || m.ViewerLoc().Line != 29 {
		t.Errorf("a tree refresh moved the viewer to %s:%d, want %s:29", m.viewer.Path(), m.ViewerLoc().Line, ext)
	}
	found := false
	for _, n := range m.tree.Rows() {
		if n.Path == filepath.Join(dir, "new.go") {
			found = true
		}
	}
	if !found {
		t.Error("a tree refresh should list the new file")
	}
}
