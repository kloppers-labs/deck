package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kloppers-labs/deck/internal/files"
)

func TestFilesTabFocusRefreshesBadges(t *testing.T) {
	m := update(t, sized(t, 120, 30), keyMsg("2"))
	next, cmd := m.Update(keyMsg("1"))
	if next.(Model).active != tabFiles {
		t.Fatal("1 should switch to Files")
	}
	var refreshed bool
	for _, msg := range collect(cmd) {
		if _, ok := msg.(files.RefreshBadgesMsg); ok {
			refreshed = true
		}
	}
	if !refreshed {
		t.Error("focusing the Files tab should refresh its git badges")
	}
}

func TestGitBadgesAdapter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	write(".gitignore", "bin/\n")
	write("bin/out.dll", "x")
	write("src/a.go", "package a\n")

	b, err := gitBadges(context.Background(), filepath.Join(dir, "src"))
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Status[filepath.Join(dir, "src", "a.go")]; got != files.BadgeUntracked {
		t.Errorf("src/a.go = %v, want untracked (paths keep the caller's spelling): %v", got, b.Status)
	}
	if !b.Ignored[filepath.Join(dir, "bin")] {
		t.Errorf("bin/ should be ignored: %v", b.Ignored)
	}
}
