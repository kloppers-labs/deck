// Package ui holds deck's shared lipgloss theme and key bindings so every pane
// renders with one consistent look. The chrome adapts to the terminal
// background: official Dracula (https://draculatheme.com) on dark terminals —
// matching the micro colorscheme deck ships and the chroma "dracula" style —
// with darkened counterparts on light terminals so text stays readable. Code
// and diff highlighting still follow the configured chroma/glamour theme.
package ui

import "github.com/charmbracelet/lipgloss"

// Palette — Dark values are official Dracula, matching
// ~/.config/micro/colorschemes/deck.micro.
var (
	ColorFg     = lipgloss.AdaptiveColor{Dark: "#F8F8F2", Light: "#24292F"} // foreground
	ColorBg     = lipgloss.AdaptiveColor{Dark: "#282A36", Light: "#FFFFFF"} // background
	ColorAccent = lipgloss.AdaptiveColor{Dark: "#BD93F9", Light: "#6F42C1"} // purple
	ColorString = lipgloss.AdaptiveColor{Dark: "#50FA7B", Light: "#1A7F37"} // green
	ColorMuted  = lipgloss.AdaptiveColor{Dark: "#6272A4", Light: "#57606A"} // comment blue-grey
	ColorError  = lipgloss.AdaptiveColor{Dark: "#FF5555", Light: "#CF222E"} // red
	ColorWarn   = lipgloss.AdaptiveColor{Dark: "#FFB86C", Light: "#9A6700"} // orange
	ColorInfo   = lipgloss.AdaptiveColor{Dark: "#8BE9FD", Light: "#0969DA"} // cyan
	ColorDim    = lipgloss.AdaptiveColor{Dark: "#44475A", Light: "#D0D7DE"} // current-line grey
)

var (
	// Title is the bar at the top of each pane.
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorBg).
		Background(ColorAccent).
		Padding(0, 1)

	// Active marks the currently focused selection.
	Active = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	// Muted is for secondary text (paths, hints, counts).
	Muted = lipgloss.NewStyle().Foreground(ColorMuted)

	// Dim draws an inactive backdrop, e.g. the body behind the help modal.
	Dim = lipgloss.NewStyle().Foreground(ColorDim)

	// Err styles error output.
	Err = lipgloss.NewStyle().Foreground(ColorError)

	// Pane wraps a bordered content region.
	Pane = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorMuted).
		Padding(0, 1)

	// PaneActive is Pane with an accent border for the focused tab.
	PaneActive = Pane.BorderForeground(ColorAccent)

	// Help is the key-hint footer.
	Help = lipgloss.NewStyle().Foreground(ColorMuted)

	// TabActive is the focused tab in the top tab bar.
	TabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent).
			Border(tabBorder(true)).
			BorderForeground(ColorAccent).
			Padding(0, 1)

	// TabInactive is an unfocused tab in the top tab bar.
	TabInactive = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Border(tabBorder(false)).
			BorderForeground(ColorMuted).
			Padding(0, 1)

	// StatusBar is the persistent one-line bar above the help footer.
	StatusBar = lipgloss.NewStyle().
			Foreground(ColorFg).
			Background(lipgloss.AdaptiveColor{Dark: "#44475A", Light: "#EAEEF2"}).
			Padding(0, 1)
)

// tabBorder is a rounded border whose bottom edge connects the tab to the rule
// beneath the bar: the active tab opens into the content ("┘ └"), inactive tabs
// sit on the rule ("┴┴┴").
func tabBorder(active bool) lipgloss.Border {
	b := lipgloss.RoundedBorder()
	if active {
		b.BottomLeft, b.Bottom, b.BottomRight = "┘", " ", "└"
	} else {
		b.BottomLeft, b.Bottom, b.BottomRight = "┴", "─", "┴"
	}
	return b
}
