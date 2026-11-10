package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/config"
)

func longNameFixture(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	long := strings.Repeat("a-very-long-file-name-", 3)[:60]
	deep := filepath.Join(dir, strings.Repeat("nested-directory-", 4)[:60])
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		name := fmt.Sprintf("%s-%02d.go", long, i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := exec.LookPath("git"); err == nil {
		_ = exec.Command("git", "init", "-q", dir).Run()
	}
	t.Chdir(dir)
	cfg := config.Default()
	for i := range 30 {
		cfg.Tasks = append(cfg.Tasks, config.Task{
			Name: fmt.Sprintf("%s-task-%02d", long, i),
			Cmd:  "echo " + long,
		})
	}
	return cfg
}

func TestFrameFitsTerminal(t *testing.T) {
	cfg := longNameFixture(t)
	sizes := [][2]int{{60, 16}, {80, 24}, {120, 40}, {200, 50}}
	for _, size := range sizes {
		w, h := size[0], size[1]
		for i, name := range tabNames {
			for _, help := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%dx%d/help=%v", name, w, h, help), func(t *testing.T) {
					m := update(t, New(cfg, nil, nil), tea.WindowSizeMsg{Width: w, Height: h},
						keyMsg(fmt.Sprintf("alt+%d", i+1)))
					if m.active != tab(i) {
						t.Fatalf("active = %d, want %d", m.active, i)
					}
					if help {
						m = update(t, m, keyMsg("f1"))
					}
					view := m.View()
					if got := lipgloss.Height(view); got != h {
						t.Errorf("frame height = %d, want %d", got, h)
					}
					if got := lipgloss.Width(view); got > w {
						t.Errorf("frame width = %d, want <= %d", got, w)
					}
				})
			}
		}
	}
}

func TestTooSmall(t *testing.T) {
	m := sized(t, 50, 12)
	view := m.View()
	if !strings.Contains(view, "terminal too small") {
		t.Errorf("frame at 50x12 should explain it is too small:\n%s", view)
	}
	if got := lipgloss.Height(view); got != 12 {
		t.Errorf("too-small frame height = %d, want 12", got)
	}
	if got := lipgloss.Width(view); got > 50 {
		t.Errorf("too-small frame width = %d, want <= 50", got)
	}
}
