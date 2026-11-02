package git

import (
	"context"
	"errors"
	"slices"
	"strings"
)

func fetchArgs() []string { return []string{"fetch", "--prune"} }
func pullArgs() []string  { return []string{"pull", "--ff-only"} }

// pushArgs pushes to the upstream, or sets one on origin (else the first remote).
func pushArgs(branch, upstream string, remotes []string) ([]string, error) {
	if upstream != "" {
		return []string{"push"}, nil
	}
	if branch == "" || branch == "(detached)" {
		return nil, errors.New("push: not on a branch")
	}
	if len(remotes) == 0 {
		return nil, errors.New("push: no remote configured")
	}
	remote := remotes[0]
	if slices.Contains(remotes, "origin") {
		remote = "origin"
	}
	return []string{"push", "-u", remote, branch}, nil
}

func (r *Repo) remote(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	return r.git(ctx, args...)
}

func (r *Repo) Remotes(ctx context.Context) ([]string, error) {
	out, err := r.git(ctx, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func (r *Repo) Fetch(ctx context.Context) error {
	_, err := r.remote(ctx, fetchArgs()...)
	return err
}

func (r *Repo) Pull(ctx context.Context) error {
	_, err := r.remote(ctx, pullArgs()...)
	return err
}

// Push pushes the current branch, setting its upstream when it has none.
func (r *Repo) Push(ctx context.Context, branch, upstream string) error {
	var remotes []string
	if upstream == "" {
		var err error
		if remotes, err = r.Remotes(ctx); err != nil {
			return err
		}
	}
	args, err := pushArgs(branch, upstream, remotes)
	if err != nil {
		return err
	}
	_, err = r.remote(ctx, args...)
	return err
}
