package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// TreePrint fingerprints the working tree: Status changes when HEAD or any
// path's status changes, Content also when a changed file is edited again.
type TreePrint struct{ Status, Content string }

func Fingerprint(ctx context.Context, dir string) (TreePrint, error) {
	r, err := OpenRepo(ctx, dir)
	if err != nil {
		return TreePrint{}, err
	}
	out, err := r.git(ctx, "--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return TreePrint{}, err
	}
	status := sha256.Sum256([]byte(out))
	h := sha256.New()
	for _, e := range ParseStatusV2(out).Entries {
		if fi, err := os.Lstat(r.Abs(e.Path)); err == nil {
			fmt.Fprintf(h, "%s\x00%d\x00%d\x00", e.Path, fi.ModTime().UnixNano(), fi.Size())
		}
	}
	return TreePrint{Status: hex.EncodeToString(status[:]), Content: hex.EncodeToString(h.Sum(nil))}, nil
}
