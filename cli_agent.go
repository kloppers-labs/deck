package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/presence"
)

const (
	exitError  = 1
	exitUsage  = 2
	exitNoDeck = 3

	callTimeout   = 5 * time.Second
	launchTimeout = 5 * time.Second
	launchPoll    = 100 * time.Millisecond
)

var agentCommands = []string{"open", "review", "diff", "where", "ls", "launch", "guide"}

func isAgentCommand(name string) bool { return slices.Contains(agentCommands, name) }

type agentEnv struct {
	Getenv func(string) string
	Getwd  func() (string, error)
	Home   string
	Launch func(dir string, mode string) error
	Now    func() time.Time
}

func defaultAgentEnv() agentEnv {
	home, _ := os.UserHomeDir()
	return agentEnv{Getenv: os.Getenv, Getwd: os.Getwd, Home: home, Now: time.Now, Launch: launchDeck}
}

var gitTop = func(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return dir
	}
	return strings.TrimSpace(string(out))
}

type agentFlags struct {
	pid         int
	from        string
	noLaunch    bool
	json        bool
	base        string
	uncommitted bool
	split, tab  bool
	window      bool
	install     string
	yes         bool
}

func parseAgentFlags(name string, args []string, env agentEnv, stderr io.Writer) (agentFlags, []string, error) {
	var f agentFlags
	fs := flag.NewFlagSet("deck "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.IntVar(&f.pid, "pid", 0, "talk to the deck with this pid")
	fs.StringVar(&f.from, "from", defaultFrom(env), "who is asking, shown in deck")
	fs.BoolVar(&f.noLaunch, "no-launch", false, "fail with exit 3 instead of starting deck")
	switch name {
	case "where":
		fs.BoolVar(&f.json, "json", false, "print the full position as JSON")
	case "review":
		fs.StringVar(&f.base, "base", "", "compare against this ref")
		fs.BoolVar(&f.uncommitted, "uncommitted", false, "only uncommitted changes")
	case "launch":
		fs.BoolVar(&f.split, "split", false, "open deck in a split")
		fs.BoolVar(&f.tab, "tab", false, "open deck in a new tab")
		fs.BoolVar(&f.window, "window", false, "open deck in a new window")
	case "guide":
		fs.StringVar(&f.install, "install", "", "install the guide for an agent (claude)")
		fs.BoolVar(&f.yes, "yes", false, "overwrite without asking")
	}
	if p := env.Getenv("DECK_PID"); p != "" {
		f.pid, _ = strconv.Atoi(p)
	}
	err := fs.Parse(flagsFirst(args))
	return f, fs.Args(), err
}

var valueFlags = map[string]bool{"pid": true, "from": true, "base": true, "install": true}

// flagsFirst moves flags ahead of positional arguments; Go's flag package stops at the first positional.
func flagsFirst(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			rest = append(rest, a)
			continue
		}
		flags = append(flags, a)
		if name := strings.TrimLeft(a, "-"); valueFlags[name] && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(append(flags, "--"), rest...)
}

func defaultFrom(env agentEnv) string {
	if env.Getenv("CLAUDECODE") == "1" {
		return "Claude"
	}
	return "agent"
}

func runAgent(name string, args []string, env agentEnv, stdout, stderr io.Writer) int {
	f, rest, err := parseAgentFlags(name, args, env, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return exitUsage
	}
	cwd, err := env.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "deck:", err)
		return exitError
	}
	switch name {
	case "ls":
		return listDecks(env, stdout)
	case "guide":
		return runGuide(f, env, os.Stdin, stdout, stderr)
	case "launch":
		return launchCommand(f, rest, cwd, env, stdout, stderr)
	}
	req := control.Request{V: control.Version, Cmd: name, From: f.from}
	target := cwd
	switch name {
	case "open", "diff":
		if len(rest) != 1 {
			fmt.Fprintf(stderr, "deck %s: want exactly one path[:line]\n", name)
			return exitUsage
		}
		path, line, col, end := control.ParseTarget(rest[0])
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		req.Path, req.Line, req.Col, req.EndLine = filepath.Clean(path), line, col, end
		target = req.Path
	case "review":
		req.Base, req.Uncommitted = f.base, f.uncommitted
	}
	if len(rest) > 0 && (name == "where" || name == "review") {
		fmt.Fprintf(stderr, "deck %s: unexpected argument %q\n", name, rest[0])
		return exitUsage
	}
	root := gitTop(cwd)
	if name == "where" {
		f.noLaunch = true
	}
	inst, code := findDeck(f, env, root, target, stderr)
	if code != 0 {
		return code
	}
	reply, err := control.Call(inst.Socket, req, callTimeout)
	if err != nil {
		fmt.Fprintln(stderr, "deck:", err)
		return exitError
	}
	if !reply.OK {
		fmt.Fprintln(stderr, "deck:", reply.Error)
		return exitError
	}
	if name == "where" {
		return printWhere(reply.Where, f.json, cwd, stdout)
	}
	fmt.Fprintln(stdout, successLine(name, reply.Message))
	return 0
}

func successLine(name, msg string) string {
	if msg == control.HeldMessage {
		return "held: the user is typing in deck; it shows as a hint they can accept"
	}
	return "deck " + name + ": " + msg
}

func findDeck(f agentFlags, env agentEnv, root, target string, stderr io.Writer) (control.Instance, int) {
	dirs := control.Dirs(env.Getenv, env.Home)
	if inst, ok := control.Pick(control.ListAll(dirs), f.pid, root, target); ok {
		return inst, 0
	}
	if f.noLaunch || f.pid > 0 || env.Launch == nil {
		fmt.Fprintln(stderr, "deck: no deck running for "+root)
		return control.Instance{}, exitNoDeck
	}
	if err := env.Launch(root, ""); err != nil {
		fmt.Fprintln(stderr, "deck:", err)
		return control.Instance{}, exitNoDeck
	}
	if inst, ok := waitForDeck(env, dirs, root, target); ok {
		return inst, 0
	}
	fmt.Fprintln(stderr, "deck: started deck but it did not come up within", launchTimeout)
	return control.Instance{}, exitNoDeck
}

func printWhere(snap *presence.Snapshot, asJSON bool, cwd string, stdout io.Writer) int {
	if snap == nil {
		snap = &presence.Snapshot{}
	}
	if asJSON {
		data, _ := json.MarshalIndent(snap, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprint(stdout, formatWhere(*snap, cwd))
	return 0
}

func displayPath(path, cwd string) string {
	if rel, err := filepath.Rel(cwd, path); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		return filepath.ToSlash(rel)
	}
	return path
}

func formatWhere(s presence.Snapshot, cwd string) string {
	var b strings.Builder
	switch {
	case s.Git != nil && s.File != "":
		fmt.Fprintf(&b, "git %s  %s:%d", s.Git.Mode, displayPath(s.File, cwd), s.Line)
		if s.Git.Side != "" {
			fmt.Fprintf(&b, " (%s)", s.Git.Side)
		}
		if s.Git.Base != "" {
			fmt.Fprintf(&b, "  vs %s", s.Git.Base)
		}
	case s.Git != nil:
		fmt.Fprintf(&b, "git %s", s.Git.Mode)
	case s.File != "" && s.Section != "" && s.Sel != nil:
		fmt.Fprintf(&b, "%s  %s  section %q %d-%d", s.Tab, displayPath(s.File, cwd), s.Section, s.Sel.StartLine, s.Sel.EndLine)
	case s.File != "":
		fmt.Fprintf(&b, "%s  %s:%d:%d", s.Tab, displayPath(s.File, cwd), s.Line, s.Col)
		if s.Sel != nil {
			fmt.Fprintf(&b, "  selection %d-%d", s.Sel.StartLine, s.Sel.EndLine)
		}
	case s.Dir != "":
		fmt.Fprintf(&b, "%s  %s/", s.Tab, displayPath(s.Dir, cwd))
	default:
		b.WriteString(s.Tab)
	}
	b.WriteByte('\n')
	if s.Git != nil && s.Sel != nil && s.Sel.EndLine > s.Sel.StartLine {
		fmt.Fprintf(&b, "selection %d-%d\n", s.Sel.StartLine, s.Sel.EndLine)
	}
	if s.SelText != "" {
		b.WriteString(s.SelText)
		if !strings.HasSuffix(s.SelText, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func listDecks(env agentEnv, stdout io.Writer) int {
	insts := control.ListAll(control.Dirs(env.Getenv, env.Home))
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tROOT\tFOCUSED")
	for _, in := range insts {
		focused := "-"
		if !in.Focused.IsZero() {
			focused = in.Focused.Format("15:04:05")
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", in.PID, in.Root, focused)
	}
	w.Flush()
	return 0
}
