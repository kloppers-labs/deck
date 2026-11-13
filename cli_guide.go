package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/kloppers-labs/deck/internal/control"
)

func runGuide(f agentFlags, env agentEnv, in io.Reader, stdout, stderr io.Writer) int {
	switch f.install {
	case "":
		fmt.Fprint(stdout, control.Guide())
		return 0
	case "claude":
	default:
		fmt.Fprintf(stderr, "deck guide: --install supports: claude (got %q)\n", f.install)
		return exitUsage
	}
	dir := env.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(env.Home, ".claude")
	}
	path, err := control.InstallClaudeSkill(dir, f.yes, in, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "deck guide:", err)
		return exitError
	}
	fmt.Fprintln(stdout, "installed the deck skill for Claude Code at "+path)
	return 0
}
