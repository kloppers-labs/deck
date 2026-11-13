package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fingerprintOf(t *testing.T, dir string) string {
	t.Helper()
	fp, err := Fingerprint(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return fp.Status + "|" + fp.Content
}

func TestFingerprintSplitsStatusFromContent(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "one\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "a.txt", "two\n")
	before, _ := Fingerprint(context.Background(), dir)
	writeFile(t, dir, "a.txt", "three, longer\n")
	future := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(dir, "a.txt"), future, future)
	after, _ := Fingerprint(context.Background(), dir)
	if after.Status != before.Status || after.Content == before.Content {
		t.Errorf("re-editing a modified file: status %v→%v, content changed %v; want status equal, content different",
			before.Status, after.Status, after.Content != before.Content)
	}
}

func TestFingerprintChangesOnEdit(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "one\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	clean := fingerprintOf(t, dir)
	if fingerprintOf(t, dir) != clean {
		t.Fatal("an unchanged tree should fingerprint the same")
	}
	writeFile(t, dir, "a.txt", "two\n")
	edited := fingerprintOf(t, dir)
	if edited == clean {
		t.Error("modifying a file should change the fingerprint")
	}
	writeFile(t, dir, "a.txt", "three, longer\n")
	future := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(dir, "a.txt"), future, future)
	if fingerprintOf(t, dir) == edited {
		t.Error("editing an already-modified file should change the fingerprint")
	}
	writeFile(t, dir, "new.txt", "x\n")
	withNew := fingerprintOf(t, dir)
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "two")
	if fingerprintOf(t, dir) == withNew {
		t.Error("a commit should change the fingerprint")
	}
}

func TestFingerprintNotRepo(t *testing.T) {
	requireGit(t)
	if _, err := Fingerprint(context.Background(), t.TempDir()); !errors.Is(err, ErrNotRepo) {
		t.Errorf("err = %v, want ErrNotRepo", err)
	}
}
