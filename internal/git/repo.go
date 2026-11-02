package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotRepo reports that the directory is not inside a git work tree.
var ErrNotRepo = errors.New("not a git repository")

// Repo is a resolved work tree. Every call runs as `git -C Top`, so paths are
// always relative to the repository root whatever the process cwd is.
type Repo struct {
	Top    string
	GitDir string
}

// OpenRepo resolves the work tree containing cwd ("" means the process cwd).
func OpenRepo(ctx context.Context, cwd string) (*Repo, error) {
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = wd
	}
	out, _, err := runGit(ctx, cwd, "rev-parse", "--show-toplevel", "--absolute-git-dir")
	if err != nil {
		var ee *ExecError
		if errors.As(err, &ee) && strings.Contains(ee.Stderr, "not a git repository") {
			return nil, ErrNotRepo
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 || lines[0] == "" {
		return nil, ErrNotRepo
	}
	return &Repo{Top: lines[0], GitDir: lines[1]}, nil
}

func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	out, _, err := runGit(ctx, r.Top, args...)
	return out, err
}

// Abs returns the absolute path of a root-relative path.
func (r *Repo) Abs(path string) string { return filepath.Join(r.Top, filepath.FromSlash(path)) }

// IndexStamp changes whenever the index file is rewritten.
func (r *Repo) IndexStamp() int64 {
	fi, err := os.Stat(filepath.Join(r.GitDir, "index"))
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano() ^ fi.Size()
}

func (r *Repo) Stage(ctx context.Context, paths ...string) error {
	_, err := r.git(ctx, append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

// Unstage drops an entry from the index; on an unborn branch there is no HEAD to reset to.
func (r *Repo) Unstage(ctx context.Context, initial bool, e Entry) error {
	paths := []string{e.Path}
	if e.Orig != "" {
		paths = append(paths, e.Orig)
	}
	if initial {
		_, err := r.git(ctx, append([]string{"rm", "--cached", "-q", "-r", "--"}, paths...)...)
		return err
	}
	_, err := r.git(ctx, append([]string{"reset", "-q", "--"}, paths...)...)
	return err
}

func (r *Repo) StageAll(ctx context.Context) error {
	_, err := r.git(ctx, "add", "-A")
	return err
}

func (r *Repo) UnstageAll(ctx context.Context, initial bool) error {
	if initial {
		_, err := r.git(ctx, "rm", "--cached", "-q", "-r", "--", ".")
		return err
	}
	_, err := r.git(ctx, "reset", "-q")
	return err
}

// Discard throws away the changes an item shows in its section.
func (r *Repo) Discard(ctx context.Context, initial bool, it Item) error {
	e := it.Entry
	switch it.Section {
	case SectionUntracked:
		_, err := r.git(ctx, "clean", "-f", "-q", "--", e.Path)
		return err
	case SectionUnstaged:
		_, err := r.git(ctx, "restore", "--worktree", "--", e.Path)
		return err
	case SectionStaged:
		if initial || e.X == 'A' {
			_, err := r.git(ctx, "rm", "--cached", "-q", "--", e.Path)
			return err
		}
		if e.Orig != "" {
			if _, err := r.git(ctx, "restore", "--staged", "--worktree", "--source=HEAD", "--", e.Orig); err != nil {
				return err
			}
			_, err := r.git(ctx, "rm", "--cached", "-q", "--", e.Path)
			return err
		}
		_, err := r.git(ctx, "restore", "--staged", "--worktree", "--source=HEAD", "--", e.Path)
		return err
	}
	return errors.New("resolve conflicts before discarding")
}

// Diff returns the diff an item shows in its section.
func (r *Repo) Diff(ctx context.Context, it Item) (string, error) {
	e := it.Entry
	base := []string{"diff", "--no-color", "--no-ext-diff"}
	switch it.Section {
	case SectionStaged:
		args := append(base, "--cached", "-M", "--", e.Path)
		if e.Orig != "" {
			args = append(args, e.Orig)
		}
		return r.git(ctx, args...)
	case SectionUntracked:
		out, err := r.git(ctx, append(base, "--no-index", "--", os.DevNull, e.Path)...)
		var ee *ExecError
		if errors.As(err, &ee) && ee.Stderr == "" {
			return out, nil
		}
		return out, err
	}
	return r.git(ctx, append(base, "--", e.Path)...)
}

func (r *Repo) StagedDiff(ctx context.Context) (string, error) {
	return r.git(ctx, "diff", "--no-color", "--no-ext-diff", "--cached")
}

func commitArgs(amend bool) []string {
	if amend {
		return []string{"commit", "--amend", "-F", "-"}
	}
	return []string{"commit", "-F", "-"}
}

// Commit records the index with message, passed on stdin so bodies survive intact.
func (r *Repo) Commit(ctx context.Context, message string, amend bool) (string, error) {
	out, _, err := runGitInput(ctx, r.Top, message, commitArgs(amend)...)
	return out, err
}

func (r *Repo) HeadMessage(ctx context.Context) (string, error) {
	return r.git(ctx, "log", "-1", "--format=%B")
}

func (r *Repo) Branches(ctx context.Context) ([]string, error) {
	out, err := r.git(ctx, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, b := range strings.Split(out, "\n") {
		if b = strings.TrimSpace(b); b != "" {
			names = append(names, b)
		}
	}
	return names, nil
}

func switchArgs(name string) []string       { return []string{"switch", name} }
func createBranchArgs(name string) []string { return []string{"switch", "-c", name} }
func deleteBranchArgs(name string) []string { return []string{"branch", "-d", name} }

func (r *Repo) Switch(ctx context.Context, name string) error {
	_, err := r.git(ctx, switchArgs(name)...)
	return err
}

func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	_, err := r.git(ctx, createBranchArgs(name)...)
	return err
}

func (r *Repo) DeleteBranch(ctx context.Context, name string) error {
	_, err := r.git(ctx, deleteBranchArgs(name)...)
	return err
}
