package files

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/ui"
)

func TestBadgeOfEntry(t *testing.T) {
	cases := []struct {
		e    git.Entry
		want Badge
	}{
		{git.Entry{Kind: '1', X: '.', Y: 'M'}, BadgeModified},
		{git.Entry{Kind: '1', X: 'M', Y: '.'}, BadgeModified},
		{git.Entry{Kind: '1', X: 'A', Y: '.'}, BadgeAdded},
		{git.Entry{Kind: '1', X: '.', Y: 'D'}, BadgeDeleted},
		{git.Entry{Kind: '1', X: 'D', Y: '.'}, BadgeDeleted},
		{git.Entry{Kind: '2', X: 'R', Y: '.'}, BadgeRenamed},
		{git.Entry{Kind: '2', X: 'R', Y: 'M'}, BadgeModified},
		{git.Entry{Kind: 'u', X: 'U', Y: 'U'}, BadgeConflict},
		{git.Entry{Kind: '?', X: '?', Y: '?'}, BadgeUntracked},
		{git.Entry{Kind: '!', X: '!', Y: '!'}, BadgeNone},
	}
	for _, c := range cases {
		if got := BadgeOf(c.e); got != c.want {
			t.Errorf("BadgeOf(%c %c%c) = %v, want %v", c.e.Kind, c.e.X, c.e.Y, got, c.want)
		}
	}
}

func TestBadgesFromEntries(t *testing.T) {
	top := "/repo"
	b := BadgesFromEntries(top, []git.Entry{
		{Kind: '1', X: '.', Y: 'M', Path: "src/a.go"},
		{Kind: '!', X: '!', Y: '!', Path: "bin/"},
		{Kind: '!', X: '!', Y: '!', Path: "debug.log"},
	})
	if b.Status[filepath.FromSlash("/repo/src/a.go")] != BadgeModified || len(b.Status) != 1 {
		t.Errorf("status = %v", b.Status)
	}
	if !b.Ignored[filepath.FromSlash("/repo/bin")] || !b.Ignored[filepath.FromSlash("/repo/debug.log")] {
		t.Errorf("ignored = %v; directory entries lose their trailing slash", b.Ignored)
	}
}

func TestAggregateStrongestChild(t *testing.T) {
	p := func(s string) string { return filepath.FromSlash(s) }
	agg := Aggregate(map[string]Badge{
		p("/r/src/api/a.cs"):  BadgeRenamed,
		p("/r/src/api/b.cs"):  BadgeModified,
		p("/r/src/core/c.cs"): BadgeUntracked,
		p("/r/src/core/d.cs"): BadgeDeleted,
		p("/r/docs/x.md"):     BadgeAdded,
		p("/r/docs/y.md"):     BadgeRenamed,
		p("/r/merge/z.go"):    BadgeConflict,
		p("/r/merge/w.go"):    BadgeDeleted,
	})
	want := map[string]Badge{
		p("/r/src/api"):      BadgeModified,
		p("/r/src/core"):     BadgeDeleted,
		p("/r/src"):          BadgeDeleted,
		p("/r/docs"):         BadgeAdded,
		p("/r/merge"):        BadgeConflict,
		p("/r"):              BadgeConflict,
		p("/r/src/api/a.cs"): BadgeRenamed,
	}
	for dir, b := range want {
		if agg[dir] != b {
			t.Errorf("Aggregate[%s] = %v, want %v", dir, agg[dir], b)
		}
	}
	order := []Badge{BadgeRenamed, BadgeUntracked, BadgeModified, BadgeDeleted, BadgeConflict}
	for i := 1; i < len(order); i++ {
		if strongest(order[i-1], order[i]) != order[i] {
			t.Errorf("%v should beat %v", order[i], order[i-1])
		}
	}
	if strongest(BadgeAdded, BadgeUntracked) == BadgeRenamed {
		t.Error("added and untracked rank together above renamed")
	}
}

func TestBadgeColours(t *testing.T) {
	cases := map[Badge]struct {
		letter string
		colour any
	}{
		BadgeModified:  {"M", ui.ColorWarn},
		BadgeAdded:     {"A", ui.ColorString},
		BadgeUntracked: {"?", ui.ColorString},
		BadgeDeleted:   {"D", ui.ColorError},
		BadgeConflict:  {"U", ui.ColorError},
		BadgeRenamed:   {"R", ui.ColorInfo},
	}
	for b, want := range cases {
		if b.Letter() != want.letter {
			t.Errorf("%v letter = %q, want %q", b, b.Letter(), want.letter)
		}
		if got := badgeStyle(b).GetForeground(); got != want.colour {
			t.Errorf("%v colour = %v, want %v", b, got, want.colour)
		}
	}
	if BadgeNone.Letter() != "" || BadgeNone.Render() != "" {
		t.Error("no badge renders nothing")
	}
}

func TestBadgesRefreshOnRoutedGitMsg(t *testing.T) {
	dir := fixture(t, map[string]int{"src/a.go": 1, "z.go": 1})
	m := started(t, dir, 120, 30)
	m, _ = m.Update(git.StatusLoadedMsg{
		Seq:  1,
		Repo: &git.Repo{Top: dir},
		Snapshot: git.Snapshot{Entries: []git.Entry{
			{Kind: '1', X: '.', Y: 'M', Path: "src/a.go"},
		}},
	})
	if got := m.badgeFor(filepath.Join(dir, "src")); got != BadgeModified {
		t.Errorf("src badge = %v, want M after the routed git status", got)
	}
	m, _ = m.Update(git.StatusLoadedMsg{Seq: 2, Repo: &git.Repo{Top: dir}})
	if got := m.badgeFor(filepath.Join(dir, "src")); got != BadgeNone {
		t.Errorf("a clean status clears the badge, got %v", got)
	}
	m, _ = m.Update(git.StatusLoadedMsg{Seq: 3, Repo: &git.Repo{Top: dir}, Snapshot: git.Snapshot{Entries: []git.Entry{
		{Kind: '?', X: '?', Y: '?', Path: "z.go"},
	}}})
	m, _ = m.Update(git.StatusLoadedMsg{Seq: 4, Err: errors.New("boom")})
	if got := m.badgeFor(filepath.Join(dir, "z.go")); got != BadgeUntracked {
		t.Errorf("a failed status keeps the last badges, got %v", got)
	}
}

func TestBadgeSourceRefreshTriggers(t *testing.T) {
	dir := fixture(t, map[string]int{"src/a.go": 1, "z.go": 1})
	calls := 0
	src := func(_ context.Context, root string) (Badges, error) {
		calls++
		if root != dir {
			t.Errorf("source asked for %s, want %s", root, dir)
		}
		return Badges{
			Status:  map[string]Badge{filepath.Join(dir, "z.go"): BadgeAdded},
			Ignored: map[string]bool{filepath.Join(dir, "src"): true},
		}, nil
	}
	m := New(dir, config.Default()).WithBadges(src)
	m.SetSize(120, 30)
	m, _ = settle(t, m, m.Init())
	if calls != 1 {
		t.Fatalf("Init should load badges once, calls = %d", calls)
	}
	if m.badgeFor(filepath.Join(dir, "z.go")) != BadgeAdded {
		t.Error("badges from the source should show")
	}
	if got := rowNames(m.tree.Rows()); len(got) != 1 || got[0] != "z.go" {
		t.Errorf("the ignored set from the same call hides src: %v", got)
	}
	triggers := []struct {
		name string
		msg  tea.Msg
	}{
		{"R", keyMsg("R")},
		{"RefreshBadges", RefreshBadges()},
		{"ReloadCurrent", ReloadCurrent()},
		{"tea.FocusMsg", tea.FocusMsg{}},
	}
	for _, tr := range triggers {
		before := calls
		next, cmd := m.Update(tr.msg)
		m, _ = settle(t, next, cmd)
		if calls != before+1 {
			t.Errorf("%s did not refresh the badges (calls %d → %d)", tr.name, before, calls)
		}
	}
}
