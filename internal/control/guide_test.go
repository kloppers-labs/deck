package control

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuideMentionsCommands(t *testing.T) {
	g := Guide()
	for _, want := range []string{"deck where", "deck open", "deck diff", "deck review", "--no-launch", "exit code 3", "/ide"} {
		if !strings.Contains(g, want) {
			t.Errorf("guide lacks %q", want)
		}
	}
}

func TestInstallWritesSkill(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	path, err := InstallClaudeSkill(dir, false, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "skills", "deck", "SKILL.md") {
		t.Errorf("path = %q", path)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "---\nname: deck\ndescription: ") || !strings.Contains(string(data), Guide()) {
		t.Errorf("skill file = %q", data)
	}
}

func TestInstallAsksBeforeOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills", "deck", "SKILL.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("mine"), 0o644)
	var out bytes.Buffer
	_, err := InstallClaudeSkill(dir, false, strings.NewReader("n\n"), &out)
	if err == nil || err.Error() != "cancelled" {
		t.Errorf("err = %v, want cancelled", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Error("declining should keep the existing file")
	}
	if !strings.Contains(out.String(), "overwrite") {
		t.Errorf("prompt = %q, want it to ask about overwriting", out.String())
	}
}

func TestInstallYesOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills", "deck", "SKILL.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("mine"), 0o644)
	if _, err := InstallClaudeSkill(dir, true, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) == "mine" {
		t.Error("--yes should overwrite")
	}
}
