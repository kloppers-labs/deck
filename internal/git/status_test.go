package git

import (
	"reflect"
	"strings"
	"testing"
)

func z(records ...string) string { return strings.Join(records, "\x00") + "\x00" }

func TestParseStatusV2(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Snapshot
	}{
		{
			name: "ordinary entries",
			in: z("1 .M N... 100644 100644 100644 aaa bbb internal/ai/ai.go",
				"1 A. N... 000000 100644 100644 000 bbb internal/git/git.go",
				"1 MM N... 100644 100644 100644 aaa bbb main.go"),
			want: Snapshot{Entries: []Entry{
				{Kind: '1', X: '.', Y: 'M', Path: "internal/ai/ai.go"},
				{Kind: '1', X: 'A', Y: '.', Path: "internal/git/git.go"},
				{Kind: '1', X: 'M', Y: 'M', Path: "main.go"},
			}},
		},
		{
			name: "spaces and unicode",
			in:   z("1 .M N... 100644 100644 100644 aaa bbb src/My File.cs", "? naïve ünïcode/über file.txt"),
			want: Snapshot{Entries: []Entry{
				{Kind: '1', X: '.', Y: 'M', Path: "src/My File.cs"},
				{Kind: '?', X: '?', Y: '?', Path: "naïve ünïcode/über file.txt"},
			}},
		},
		{
			name: "rename containing an arrow",
			in:   z("2 R. N... 100644 100644 100644 aaa bbb R100 new -> name.txt", "old -> name.txt"),
			want: Snapshot{Entries: []Entry{
				{Kind: '2', X: 'R', Y: '.', Path: "new -> name.txt", Orig: "old -> name.txt"},
			}},
		},
		{
			name: "unmerged",
			in:   z("u UU N... 100644 100644 100644 100644 h1 h2 h3 conflicted file.go"),
			want: Snapshot{Entries: []Entry{{Kind: 'u', X: 'U', Y: 'U', Path: "conflicted file.go"}}},
		},
		{
			name: "untracked and ignored",
			in:   z("? notes.txt", "! bin/"),
			want: Snapshot{Entries: []Entry{
				{Kind: '?', X: '?', Y: '?', Path: "notes.txt"},
				{Kind: '!', X: '!', Y: '!', Path: "bin/"},
			}},
		},
		{
			name: "branch headers with ahead and behind",
			in:   z("# branch.oid 1234abcd", "# branch.head feat/supercharge", "# branch.upstream origin/feat/supercharge", "# branch.ab +2 -1"),
			want: Snapshot{OID: "1234abcd", Branch: "feat/supercharge", Upstream: "origin/feat/supercharge", Ahead: 2, Behind: 1},
		},
		{
			name: "no upstream, initial commit",
			in:   z("# branch.oid (initial)", "# branch.head main"),
			want: Snapshot{Branch: "main", Initial: true},
		},
		{
			name: "detached",
			in:   z("# branch.oid 1234", "# branch.head (detached)"),
			want: Snapshot{OID: "1234", Branch: "(detached)", Detached: true},
		},
		{
			name: "garbage is skipped",
			in:   z("1 short", "x what", ""),
			want: Snapshot{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseStatusV2(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseStatusV2:\n got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestEntryPredicates(t *testing.T) {
	cases := []struct {
		e                                     Entry
		staged, unstaged, untracked, conflict bool
	}{
		{Entry{Kind: '1', X: 'M', Y: 'M'}, true, true, false, false},
		{Entry{Kind: '1', X: '.', Y: 'D'}, false, true, false, false},
		{Entry{Kind: '2', X: 'R', Y: '.'}, true, false, false, false},
		{Entry{Kind: '?', X: '?', Y: '?'}, false, false, true, false},
		{Entry{Kind: 'u', X: 'U', Y: 'U'}, false, false, false, true},
		{Entry{Kind: '!', X: '!', Y: '!'}, false, false, false, false},
	}
	for _, c := range cases {
		if c.e.Staged() != c.staged || c.e.Unstaged() != c.unstaged || c.e.Untracked() != c.untracked || c.e.Conflict() != c.conflict {
			t.Errorf("%+v: staged=%v unstaged=%v untracked=%v conflict=%v", c.e,
				c.e.Staged(), c.e.Unstaged(), c.e.Untracked(), c.e.Conflict())
		}
	}
}
