// Package git wraps the git CLI for the small set of operations deck needs:
// listing changes, staging, diffing, committing, and switching branches. Every
// call shells out to the user's installed git so behaviour matches their config.
package git

import (
	"os/exec"
	"strconv"
	"strings"
)

// Change is one entry from `git status --porcelain`. Staged/Unstaged hold the
// raw porcelain codes (e.g. "M", "A", "?"); a space means "no change in that
// column". Path is the working-tree path (the destination for renames).
type Change struct {
	Staged   string
	Unstaged string
	Path     string
}

// Staged reports whether the index column carries a staged change. "?" is the
// untracked marker, not a staged state, so it's excluded.
func (c Change) IsStaged() bool {
	return c.Staged != "" && c.Staged != " " && c.Staged != "?"
}

// Untracked reports whether git doesn't yet track the file.
func (c Change) IsUntracked() bool { return c.Staged == "?" && c.Unstaged == "?" }

// ParseStatus turns `git status --porcelain` output into a slice of Changes.
// Porcelain v1 format is "XY PATH", where X is the index state, Y the
// working-tree state, and renames appear as "ORIG -> DEST".
func ParseStatus(out string) []Change {
	var changes []Change
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		staged := string(line[0])
		unstaged := string(line[1])
		path := line[3:]
		if i := strings.Index(path, " -> "); i != -1 {
			path = path[i+len(" -> "):]
		}
		changes = append(changes, Change{Staged: staged, Unstaged: unstaged, Path: path})
	}
	return changes
}

func run(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo() bool {
	out, err := run("rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Status returns the parsed working-tree changes.
func Status() ([]Change, error) {
	out, err := run("status", "--porcelain")
	if err != nil {
		return nil, err
	}
	return ParseStatus(out), nil
}

// Stage adds a path to the index.
func Stage(path string) error {
	_, err := run("add", "--", path)
	return err
}

// Unstage removes a path from the index, keeping working-tree changes.
func Unstage(path string) error {
	_, err := run("reset", "-q", "HEAD", "--", path)
	return err
}

// Diff returns the unstaged diff for a path.
func Diff(path string) (string, error) {
	return run("diff", "--", path)
}

// StagedDiff returns the staged diff (everything in the index).
func StagedDiff() (string, error) {
	return run("diff", "--staged")
}

// Commit records the staged changes with the given message.
func Commit(message string) (string, error) {
	return run("commit", "-m", message)
}

// CurrentBranch returns the checked-out branch name.
func CurrentBranch() (string, error) {
	out, err := run("rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

// Branches lists local branch names, current branch first.
func Branches() ([]string, error) {
	out, err := run("branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var branches []string
	for _, b := range strings.Split(strings.TrimSpace(out), "\n") {
		if b = strings.TrimSpace(b); b != "" {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

// Checkout switches to an existing branch.
func Checkout(branch string) (string, error) {
	return run("checkout", branch)
}

// Log returns the n most recent commits, one decorated line each.
func Log(n int) (string, error) {
	return run("log", "--oneline", "--decorate", "-n", strconv.Itoa(n))
}
