// Package launch starts deck in a new split, tab or window of the terminal or
// agent workspace the caller runs in.
package launch

type Kind int

const (
	Unknown Kind = iota
	Orca
	Cmux
	Paradev
	Tmux
	Zellij
	WezTerm
	Kitty
	Ghostty
	ITerm2
	AppleTerminal
	Alacritty
)

// Target is where the caller runs; Handle is the environment's own id for it.
type Target struct {
	Kind   Kind
	Handle string
}

// Detect checks agent workspaces first, then multiplexers, then terminals.
func Detect(getenv func(string) string, goos string) Target {
	if h := getenv("ORCA_TERMINAL_HANDLE"); h != "" {
		return Target{Kind: Orca, Handle: h}
	}
	if h := getenv("CMUX_SURFACE_ID"); h != "" {
		return Target{Kind: Cmux, Handle: h}
	}
	if h := getenv("SUPERSET_WORKSPACE_ID"); h != "" {
		return Target{Kind: Paradev, Handle: h}
	}
	if getenv("TMUX") != "" {
		return Target{Kind: Tmux}
	}
	if getenv("ZELLIJ_PANE_ID") != "" || getenv("ZELLIJ") != "" {
		return Target{Kind: Zellij}
	}
	if h := getenv("WEZTERM_PANE"); h != "" {
		return Target{Kind: WezTerm, Handle: h}
	}
	if getenv("KITTY_WINDOW_ID") != "" {
		return Target{Kind: Kitty}
	}
	if goos == "darwin" {
		switch getenv("TERM_PROGRAM") {
		case "ghostty":
			return Target{Kind: Ghostty}
		case "iTerm.app":
			return Target{Kind: ITerm2}
		case "Apple_Terminal":
			return Target{Kind: AppleTerminal}
		}
	}
	if getenv("ALACRITTY_SOCKET") != "" || getenv("ALACRITTY_WINDOW_ID") != "" {
		return Target{Kind: Alacritty}
	}
	return Target{Kind: Unknown}
}
