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
  deck --version   print the version
  deck --help      show this help

Config: ~/.config/deck/config.toml, plus a project-local .deck.toml.
Press ? inside deck for every key binding.
`

func cli(args []string, stdout, stderr io.Writer) (code int, launch bool) {
	if len(args) == 0 {
		return 0, true
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
	if err := os.Chdir(args[0]); err != nil {
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
