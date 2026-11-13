package control

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed guide.md
var guide string

const skillHeader = `---
name: deck
description: Use when the user mentions deck, asks what they're looking at or have selected, says "show me", "walk me through", "open X in deck", or wants to review the branch's changes together.
---

`

// Guide is how agents should use deck, as markdown.
func Guide() string { return guide }

// InstallClaudeSkill writes the guide as a Claude Code skill under configDir,
// asking on in/out before replacing an existing file unless yes is set.
func InstallClaudeSkill(configDir string, yes bool, in io.Reader, out io.Writer) (string, error) {
	path := filepath.Join(configDir, "skills", "deck", "SKILL.md")
	if _, err := os.Stat(path); err == nil && !yes {
		fmt.Fprintf(out, "%s exists; overwrite? [y/N] ", path)
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return path, errors.New("cancelled")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}
	return path, os.WriteFile(path, []byte(skillHeader+guide), 0o644)
}
