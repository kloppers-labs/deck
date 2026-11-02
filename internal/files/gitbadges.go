package files

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/git"
	"deck/internal/ui"
)

// Badge is a file's git state, ordered so a larger value is a stronger signal.
type Badge int

const (
	BadgeNone Badge = iota
	BadgeRenamed
	BadgeUntracked
	BadgeAdded
	BadgeModified
	BadgeDeleted
	BadgeConflict
)

const badgeTimeout = 10 * time.Second

var badgeLetters = [...]string{"", "R", "?", "A", "M", "D", "U"}

var badgeNames = [...]string{"", "renamed", "untracked", "added", "modified", "deleted", "conflict"}

func (b Badge) Letter() string { return badgeLetters[b] }

func (b Badge) String() string { return badgeNames[b] }

func badgeStyle(b Badge) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch b {
	case BadgeModified:
		return s.Foreground(ui.ColorWarn)
	case BadgeAdded, BadgeUntracked:
		return s.Foreground(ui.ColorString)
	case BadgeDeleted:
		return s.Foreground(ui.ColorError)
	case BadgeConflict:
		return s.Foreground(ui.ColorError).Bold(true)
	case BadgeRenamed:
		return s.Foreground(ui.ColorInfo)
	}
	return s
}

func (b Badge) Render() string {
	if b == BadgeNone {
		return ""
	}
	return badgeStyle(b).Render(b.Letter())
}

func rank(b Badge) int {
	if b == BadgeAdded {
		return int(BadgeUntracked)
	}
	return int(b)
}

func strongest(a, b Badge) Badge {
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// Badges is one status snapshot keyed by absolute path: each changed file's
// badge and the gitignored paths.
type Badges struct {
	Status  map[string]Badge
	Ignored map[string]bool
}

// BadgeSource loads Badges for the repository containing root.
type BadgeSource func(ctx context.Context, root string) (Badges, error)

// BadgesMsg carries a BadgeSource result; Seq drops stale loads.
type BadgesMsg struct {
	Seq    int
	Badges Badges
	Err    error
}

// RefreshBadgesMsg asks the Files tab to reload its git badges.
type RefreshBadgesMsg struct{}

func (BadgesMsg) RouteTo() ui.Dest        { return ui.DestFiles }
func (RefreshBadgesMsg) RouteTo() ui.Dest { return ui.DestFiles }

func RefreshBadges() tea.Msg { return RefreshBadgesMsg{} }

func BadgeOf(e git.Entry) Badge {
	switch {
	case e.Conflict():
		return BadgeConflict
	case e.Untracked():
		return BadgeUntracked
	case e.Ignored():
		return BadgeNone
	case e.Deleted():
		return BadgeDeleted
	case e.Kind == '2' && e.Y == '.':
		return BadgeRenamed
	case e.X == 'A' && e.Y == '.':
		return BadgeAdded
	}
	return BadgeModified
}

// BadgesFromEntries keys a porcelain status by absolute path under top.
func BadgesFromEntries(top string, entries []git.Entry) Badges {
	b := Badges{Status: map[string]Badge{}, Ignored: map[string]bool{}}
	for _, e := range entries {
		p := filepath.Join(top, filepath.FromSlash(strings.TrimSuffix(e.Path, "/")))
		if e.Ignored() {
			b.Ignored[p] = true
			continue
		}
		if badge := BadgeOf(e); badge != BadgeNone {
			b.Status[p] = badge
		}
	}
	return b
}

// Aggregate adds every ancestor directory of each changed path, carrying the
// strongest badge beneath it (conflict > deleted > modified > added/untracked > renamed).
func Aggregate(changes map[string]Badge) map[string]Badge {
	out := make(map[string]Badge, len(changes)*2)
	for p, b := range changes {
		out[p] = strongest(out[p], b)
		for dir := filepath.Dir(p); ; dir = filepath.Dir(dir) {
			cur, seen := out[dir]
			if seen && strongest(cur, b) == cur {
				break
			}
			out[dir] = strongest(cur, b)
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return out
}

// LogicalTop maps a repository top level reported by git (symlinks resolved)
// onto the spelling of the launch directory, so badge paths match tree paths.
func LogicalTop(launch, top string) string {
	if top == launch || isUnder(launch, top) || isUnder(top, launch) {
		return top
	}
	real, err := filepath.EvalSymlinks(launch)
	if err != nil {
		return top
	}
	rel, err := filepath.Rel(real, top)
	if err != nil {
		return top
	}
	return filepath.Join(launch, rel)
}

func badgesCmd(src BadgeSource, root string, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), badgeTimeout)
		defer cancel()
		b, err := src(ctx, root)
		return BadgesMsg{Seq: seq, Badges: b, Err: err}
	}
}
