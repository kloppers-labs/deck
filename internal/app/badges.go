package app

import (
	"context"

	"deck/internal/files"
	"deck/internal/git"
)

// gitBadges is the Files tab's BadgeSource: one porcelain v2 status call that
// also lists ignored paths.
func gitBadges(ctx context.Context, root string) (files.Badges, error) {
	repo, err := git.OpenRepo(ctx, root)
	if err != nil {
		return files.Badges{}, err
	}
	snap, err := repo.StatusSnapshot(ctx, true)
	if err != nil {
		return files.Badges{}, err
	}
	return files.BadgesFromEntries(files.LogicalTop(root, repo.Top), snap.Entries), nil
}
