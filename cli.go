package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

var version = ""

const usage = `deck: a terminal command center with Files, Git, Run and HTTP tabs.

Usage:
  deck [dir]       open deck in dir (default: the current directory)
  deck -- <dir>    open a directory named like a subcommand
  deck --version   print the version
  deck --help      show this help

Agents (talk to the deck running for this project, starting one if needed):
  deck open <path>[:line[:col]|:line-end]   show a file or directory
  deck review [--base ref] [--uncommitted]  show the branch's changes
  deck diff <path>[:line]                   show one file's change
  deck where [--json]                       print what the user is looking at
  deck ls                                   list running decks
  deck launch [--split|--tab|--window]      start deck next to this terminal
  deck guide [--install claude]             how agents should use deck
  Common flags: --pid N, --from NAME, --no-launch (exit 3 instead of starting deck)

Config: ~/.config/deck/config.toml, plus a project-local .deck.toml.
Press ? inside deck for every key binding.
`

func cli(args []string, stdout, stderr io.Writer) (code int, launch bool) {
	if len(args) == 0 {
		return 0, true
	}
	if isAgentCommand(args[0]) {
		return runAgent(args[0], args[1:], defaultAgentEnv(), stdout, stderr), false
	}
	if args[0] == "--" {
		if len(args) != 2 {
			fmt.Fprintf(stderr, "deck: -- takes exactly one directory\n\n%s", usage)
			return 2, false
		}
		return chdir(args[1], stderr)
	}
	switch args[0] {
	case "-v", "--version":
		fmt.Fprintln(stdout, "deck", versionString())
		return 0, false
	case "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0, false
	}
	if len(args) > 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(stderr, "deck: unexpected argument %q\n\n%s", args[0], usage)
		return 2, false
	}
	return chdir(args[0], stderr)
}

func chdir(dir string, stderr io.Writer) (int, bool) {
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(stderr, "deck:", err)
		return 1, false
	}
	return 0, true
}

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
