package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type Commit struct{ Hash, Subject string }

// ReviewFile is one changed path; Added and Deleted are -1 for binary files.
type ReviewFile struct {
	Path, OldPath  string
	Status         byte
	Added, Deleted int
	Binary         bool
	Untracked      bool
}

// Review is what the branch changed against Base: committed and uncommitted
// work against MergeBase, or only uncommitted work against HEAD.
type Review struct {
	Base, MergeBase string
	Uncommitted     bool
	Fallback        string
	Commits         []Commit
	Files           []ReviewFile
	against         string
}

func (r *Repo) refExists(ctx context.Context, ref string) bool {
	_, err := r.git(ctx, "rev-parse", "--verify", "-q", ref+"^{commit}")
	return err == nil
}

// ResolveBase picks the review base: explicit, configured, origin's default
// branch, origin/main, origin/master, main, master; "" when none exists.
func (r *Repo) ResolveBase(ctx context.Context, explicit, configured string) (string, error) {
	if explicit != "" {
		if !r.refExists(ctx, explicit) {
			return "", fmt.Errorf("unknown base %q", explicit)
		}
		return explicit, nil
	}
	candidates := []string{configured}
	if head, err := r.git(ctx, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil {
		candidates = append(candidates, strings.TrimSpace(head))
	}
	candidates = append(candidates, "origin/main", "origin/master", "main", "master")
	for _, c := range candidates {
		if c != "" && r.refExists(ctx, c) {
			return c, nil
		}
	}
	return "", nil
}

func (r *Repo) LoadReview(ctx context.Context, base string, uncommitted bool) (Review, error) {
	rv := Review{Base: base, Uncommitted: uncommitted}
	head := ""
	if out, err := r.git(ctx, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
		head = strings.TrimSpace(out)
	}
	if base == "" {
		rv.Uncommitted = true
		rv.Fallback = "no base branch found; showing uncommitted changes"
	} else if head != "" {
		mb, err := r.git(ctx, "merge-base", base, "HEAD")
		if err != nil {
			return Review{}, fmt.Errorf("merge-base %s: %w", base, err)
		}
		rv.MergeBase = strings.TrimSpace(mb)
		commits, err := r.commitsSince(ctx, rv.MergeBase)
		if err != nil {
			return Review{}, err
		}
		rv.Commits = commits
		if len(commits) == 0 && !rv.Uncommitted {
			rv.Uncommitted = true
			rv.Fallback = "no commits ahead of " + base + "; showing uncommitted changes"
		}
	}
	switch {
	case head == "":
		rv.against = emptyTree
	case rv.Uncommitted:
		rv.against = "HEAD"
	default:
		rv.against = rv.MergeBase
	}
	files, err := r.reviewFiles(ctx, rv.against)
	if err != nil {
		return Review{}, err
	}
	rv.Files = files
	return rv, nil
}

func (r *Repo) commitsSince(ctx context.Context, mb string) ([]Commit, error) {
	out, err := r.git(ctx, "log", "--format=%H%x00%s", mb+"..HEAD")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if hash, subject, ok := strings.Cut(line, "\x00"); ok {
			commits = append(commits, Commit{Hash: hash, Subject: subject})
		}
	}
	return commits, nil
}

func (r *Repo) reviewFiles(ctx context.Context, against string) ([]ReviewFile, error) {
	names, err := r.git(ctx, "diff", "--no-color", "--no-ext-diff", "--name-status", "-z", "-M", against)
	if err != nil {
		return nil, err
	}
	stats, err := r.git(ctx, "diff", "--no-color", "--no-ext-diff", "--numstat", "-z", "-M", against)
	if err != nil {
		return nil, err
	}
	files := parseNameStatusZ(names)
	counts := parseNumstatZ(stats)
	for i := range files {
		if c, ok := counts[files[i].Path]; ok {
			files[i].Added, files[i].Deleted = c[0], c[1]
			files[i].Binary = c[0] < 0
		}
	}
	untracked, err := r.git(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(untracked, "\x00") {
		if p == "" {
			continue
		}
		f := ReviewFile{Path: p, Status: 'A', Untracked: true}
		f.Added, f.Binary = countLines(r.Abs(p))
		if f.Binary {
			f.Added, f.Deleted = -1, -1
		}
		files = append(files, f)
	}
	return files, nil
}

func countLines(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return 0, true
	}
	n := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n, false
}

func parseNameStatusZ(out string) []ReviewFile {
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var files []ReviewFile
	for i := 0; i < len(fields); i++ {
		code := fields[i]
		if code == "" || i+1 >= len(fields) {
			continue
		}
		f := ReviewFile{Status: code[0]}
		if code[0] == 'R' || code[0] == 'C' {
			if i+2 >= len(fields) {
				break
			}
			f.OldPath, f.Path = fields[i+1], fields[i+2]
			i += 2
		} else {
			f.Path = fields[i+1]
			i++
		}
		files = append(files, f)
	}
	return files
}

func parseNumstatZ(out string) map[string][2]int {
	counts := map[string][2]int{}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) {
			path = fields[i+2]
			i += 2
		}
		added, errA := strconv.Atoi(parts[0])
		deleted, errD := strconv.Atoi(parts[1])
		if errA != nil || errD != nil {
			added, deleted = -1, -1
		}
		counts[path] = [2]int{added, deleted}
	}
	return counts
}

func (r *Repo) ReviewDiff(ctx context.Context, rv Review, f ReviewFile) (string, error) {
	base := []string{"diff", "--no-color", "--no-ext-diff"}
	if f.Untracked {
		out, err := r.git(ctx, append(base, "--no-index", "--", os.DevNull, f.Path)...)
		var ee *ExecError
		if errors.As(err, &ee) && ee.Stderr == "" {
			return out, nil
		}
		return out, err
	}
	against := rv.against
	if against == "" {
		against = "HEAD"
	}
	args := append(base, "-M", against, "--", f.Path)
	if f.OldPath != "" {
		args = append(args, f.OldPath)
	}
	return r.git(ctx, args...)
}
