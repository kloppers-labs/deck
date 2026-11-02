package files

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/httpc"
)

func TestXOnHTTPFileLoadsRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Api.http")
	if err := os.WriteFile(path, []byte("GET http://localhost:5000/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(dir, "dracula")
	m.SetSize(100, 30)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd == nil {
		t.Fatal("x on a .http file should send it to the HTTP tab")
	}
	if msg, ok := cmd().(httpc.LoadRequestMsg); !ok || msg.Path != path {
		t.Errorf("x emitted %+v, want LoadRequestMsg for %s", msg, path)
	}
}

func TestXOnOtherFileDoesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(dir, "dracula")
	m.SetSize(100, 30)
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}); cmd != nil {
		if _, ok := cmd().(httpc.LoadRequestMsg); ok {
			t.Error("x on a non-.http file should not load a request")
		}
	}
}
