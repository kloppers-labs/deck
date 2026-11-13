package launch

import (
	"errors"
	"strings"
)

type Mode string

const (
	Split  Mode = "split"
	Tab    Mode = "tab"
	Window Mode = "window"
)

var ErrNoRoute = errors.New("no way to open a terminal here")

// Command builds the argv that opens deckBin in dir next to the caller.
// Environments without the requested mode fall back to the nearest one they have.
func Command(t Target, mode Mode, dir, deckBin string) ([]string, error) {
	inDir := "cd " + shQuote(dir) + " && exec " + shQuote(deckBin)
	switch t.Kind {
	case Orca:
		if mode == Split {
			return []string{"orca", "terminal", "split", "--terminal", t.Handle, "--direction", "vertical", "--command", inDir, "--json"}, nil
		}
		return []string{"orca", "terminal", "create", "--worktree", "active", "--command", inDir, "--json"}, nil
	case Cmux:
		return []string{"cmux", "new-split", "right", "--command", inDir}, nil
	case Paradev:
		return []string{"superset", "panes", "new-tab", "--workspace", t.Handle, "--command", inDir}, nil
	case Tmux:
		if mode == Split {
			return []string{"tmux", "split-window", "-h", "-c", dir, deckBin}, nil
		}
		return []string{"tmux", "new-window", "-c", dir, "-n", "deck", deckBin}, nil
	case Zellij:
		if mode == Split {
			return []string{"zellij", "action", "new-pane", "-d", "right", "--cwd", dir, "--", deckBin}, nil
		}
		return []string{"zellij", "action", "new-tab", "--cwd", dir, "-n", "deck", "--", deckBin}, nil
	case WezTerm:
		switch mode {
		case Split:
			return []string{"wezterm", "cli", "split-pane", "--right", "--percent", "45", "--cwd", dir, "--", deckBin}, nil
		case Window:
			return []string{"wezterm", "cli", "spawn", "--new-window", "--cwd", dir, "--", deckBin}, nil
		}
		return []string{"wezterm", "cli", "spawn", "--cwd", dir, "--", deckBin}, nil
	case Kitty:
		kind := map[Mode]string{Split: "--type=window", Tab: "--type=tab", Window: "--type=os-window"}[mode]
		argv := []string{"kitten", "@", "launch", kind}
		if mode == Split {
			argv = append(argv, "--location=vsplit")
		}
		return append(argv, "--cwd="+dir, deckBin), nil
	case Ghostty:
		return osascript(ghosttyScript(mode, dir, deckBin)), nil
	case ITerm2:
		return osascript(itermScript(mode, inDir)), nil
	case AppleTerminal:
		return osascript(`tell application "Terminal" to do script ` + asQuote(inDir)), nil
	case Alacritty:
		return []string{"alacritty", "msg", "create-window", "--working-directory", dir, "-e", deckBin}, nil
	}
	return nil, ErrNoRoute
}

func osascript(script string) []string { return []string{"osascript", "-e", script} }

func ghosttyScript(mode Mode, dir, deckBin string) string {
	action := "split (focused terminal of selected tab of front window) direction right with configuration cfg"
	switch mode {
	case Tab:
		action = "new tab in front window with configuration cfg"
	case Window:
		action = "new window with configuration cfg"
	}
	return strings.Join([]string{
		`tell application "Ghostty"`,
		"\tset cfg to new surface configuration",
		"\tset initial working directory of cfg to " + asQuote(dir),
		"\tset command of cfg to " + asQuote(deckBin),
		"\t" + action,
		"end tell",
	}, "\n")
}

func itermScript(mode Mode, inDir string) string {
	cmd := asQuote(`/bin/sh -c "` + strings.ReplaceAll(inDir, `"`, `\"`) + `"`)
	switch mode {
	case Tab:
		return "tell application \"iTerm2\"\n\ttell current window to create tab with default profile command " + cmd + "\nend tell"
	case Window:
		return "tell application \"iTerm2\"\n\tcreate window with default profile command " + cmd + "\nend tell"
	}
	return "tell application \"iTerm2\"\n\ttell current session of current window\n\t\tsplit vertically with default profile command " + cmd + "\n\tend tell\nend tell"
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func asQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Hint tells the user how to start deck themselves.
func Hint(t Target, dir string) string {
	h := "deck isn't running here — start it with: cd " + dir + " && deck"
	if t.Kind == Kitty {
		h += " (kitty: set allow_remote_control to let agents open deck)"
	}
	return h
}
