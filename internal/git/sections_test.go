package git

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestItemsGroupBySection(t *testing.T) {
	entries := []Entry{
		{Kind: '?', X: '?', Y: '?', Path: "new.txt"},
		{Kind: '1', X: 'M', Y: 'M', Path: "both.go"},
		{Kind: 'u', X: 'U', Y: 'U', Path: "conflict.go"},
		{Kind: '1', X: '.', Y: 'M', Path: "work.go"},
		{Kind: '!', X: '!', Y: '!', Path: "bin/"},
	}
	var got []string
	for _, it := range Items(entries) {
		got = append(got, it.Section.String()+":"+it.Entry.Path)
	}
	want := "Staged:both.go Unstaged:both.go Unstaged:work.go Untracked:new.txt Conflicts:conflict.go"
	if strings.Join(got, " ") != want {
		t.Errorf("Items = %v, want %s", got, want)
	}
}

func TestItemCode(t *testing.T) {
	mm := Entry{Kind: '1', X: 'M', Y: 'D', Path: "x"}
	cases := []struct {
		it   Item
		want string
	}{
		{Item{SectionStaged, mm}, "M"},
		{Item{SectionUnstaged, mm}, "D"},
		{Item{SectionUntracked, Entry{Kind: '?'}}, "?"},
		{Item{SectionConflicts, Entry{Kind: 'u', X: 'U', Y: 'D'}}, "UD"},
	}
	for _, c := range cases {
		if got := c.it.Code(); got != c.want {
			t.Errorf("%+v Code = %q, want %q", c.it, got, c.want)
		}
	}
}

func TestHeaderAheadBehind(t *testing.T) {
	s := Snapshot{Branch: "feat/supercharge", Upstream: "origin/feat/supercharge", Ahead: 2, Entries: []Entry{
		{Kind: '1', X: 'M', Y: 'M', Path: "a"},
		{Kind: '1', X: 'A', Y: '.', Path: "b"},
		{Kind: '1', X: '.', Y: 'M', Path: "c"},
	}}
	want := "⎇ feat/supercharge → origin/feat/supercharge ↑2 ↓0 · 2 staged · 2 unstaged"
	if got := ansi.Strip(headerLine(s)); got != want {
		t.Errorf("headerLine = %q\nwant         %q", got, want)
	}
	noUp := ansi.Strip(headerLine(Snapshot{Branch: "main", Entries: []Entry{{Kind: '?', Path: "n"}, {Kind: 'u', Path: "c"}}}))
	if noUp != "⎇ main · 1 untracked · 1 conflict" {
		t.Errorf("headerLine without upstream = %q", noUp)
	}
	if got := ansi.Strip(headerLine(Snapshot{Branch: "main"})); got != "⎇ main · clean" {
		t.Errorf("clean headerLine = %q", got)
	}
}
