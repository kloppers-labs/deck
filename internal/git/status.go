package git

import (
	"context"
	"strconv"
	"strings"
)

// Entry is one porcelain v2 record with root-relative paths; Kind is the
// record type ('1', '2', 'u', '?', '!') and X/Y the index/work-tree codes.
type Entry struct {
	Kind byte
	X, Y byte
	Path string
	Orig string
}

func (e Entry) tracked() bool    { return e.Kind == '1' || e.Kind == '2' }
func (e Entry) Staged() bool     { return e.tracked() && e.X != '.' }
func (e Entry) Unstaged() bool   { return e.tracked() && e.Y != '.' }
func (e Entry) Untracked() bool  { return e.Kind == '?' }
func (e Entry) Conflict() bool   { return e.Kind == 'u' }
func (e Entry) Ignored() bool    { return e.Kind == '!' }
func (e Entry) Deleted() bool    { return e.tracked() && (e.Y == 'D' || (e.X == 'D' && e.Y == '.')) }
func (e Entry) worktreeOn() bool { return e.Unstaged() || e.Untracked() || e.Conflict() }

// Snapshot is one `git status --porcelain=v2 --branch -z` result.
type Snapshot struct {
	OID      string
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
	Initial  bool
	Detached bool
	Entries  []Entry
}

// ParseStatusV2 parses NUL-separated porcelain v2 output, skipping malformed records.
func ParseStatusV2(out string) Snapshot {
	var s Snapshot
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 2 || rec[1] != ' ' {
			continue
		}
		switch rec[0] {
		case '#':
			s.header(rec[2:])
		case '1':
			if f := strings.SplitN(rec, " ", 9); len(f) == 9 && len(f[1]) == 2 {
				s.Entries = append(s.Entries, Entry{Kind: '1', X: f[1][0], Y: f[1][1], Path: f[8]})
			}
		case '2':
			f := strings.SplitN(rec, " ", 10)
			if len(f) != 10 || len(f[1]) != 2 || i+1 >= len(recs) {
				continue
			}
			i++
			s.Entries = append(s.Entries, Entry{Kind: '2', X: f[1][0], Y: f[1][1], Path: f[9], Orig: recs[i]})
		case 'u':
			if f := strings.SplitN(rec, " ", 11); len(f) == 11 && len(f[1]) == 2 {
				s.Entries = append(s.Entries, Entry{Kind: 'u', X: f[1][0], Y: f[1][1], Path: f[10]})
			}
		case '?', '!':
			s.Entries = append(s.Entries, Entry{Kind: rec[0], X: rec[0], Y: rec[0], Path: rec[2:]})
		}
	}
	return s
}

func (s *Snapshot) header(h string) {
	key, val, _ := strings.Cut(h, " ")
	switch key {
	case "branch.oid":
		if val == "(initial)" {
			s.Initial = true
		} else {
			s.OID = val
		}
	case "branch.head":
		s.Branch = val
		s.Detached = val == "(detached)"
	case "branch.upstream":
		s.Upstream = val
	case "branch.ab":
		a, b, _ := strings.Cut(val, " ")
		s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
		s.Behind, _ = strconv.Atoi(strings.TrimPrefix(b, "-"))
	}
}

// StatusSnapshot replaces IsRepo, CurrentBranch and Status with one call.
func (r *Repo) StatusSnapshot(ctx context.Context, includeIgnored bool) (Snapshot, error) {
	args := []string{"--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all"}
	if includeIgnored {
		args = append(args, "--ignored=matching")
	}
	out, err := r.git(ctx, args...)
	if err != nil {
		return Snapshot{}, err
	}
	return ParseStatusV2(out), nil
}
