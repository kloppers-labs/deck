package files

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var errNoTrash = errors.New("no trash available (no ~/.Trash and no trash command); refusing to delete")

// CreatePath creates name under dir, with any missing parents. A trailing
// slash makes a directory; otherwise an empty file. Existing entries are refused.
func CreatePath(dir, name string) (string, error) {
	isDir := strings.HasSuffix(name, "/")
	clean := strings.TrimSpace(strings.TrimSuffix(name, "/"))
	if clean == "" || !filepath.IsLocal(clean) {
		return "", fmt.Errorf("invalid name %q", name)
	}
	p := filepath.Join(dir, clean)
	if _, err := os.Lstat(p); err == nil {
		return "", fmt.Errorf("%s already exists", clean)
	}
	if isDir {
		return p, os.MkdirAll(p, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	return p, f.Close()
}

func RenamePath(path, newName string) (string, error) {
	clean := strings.TrimSpace(newName)
	if clean == "" || !filepath.IsLocal(clean) || strings.ContainsRune(clean, filepath.Separator) {
		return "", fmt.Errorf("invalid name %q", newName)
	}
	dst := filepath.Join(filepath.Dir(path), clean)
	if dst == path {
		return path, nil
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("%s already exists", clean)
	}
	return dst, os.Rename(path, dst)
}

// renameCursor is the rune offset just before the extension, or the end for
// directories and names that are all extension.
func renameCursor(name string, isDir bool) int {
	n := len([]rune(name))
	if isDir {
		return n
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == "" {
		return n
	}
	return len([]rune(stem))
}

// Trash moves entries to the user's trash: into Dir when set, otherwise (or
// when that fails) through the Cmd binary. With neither it refuses.
type Trash struct {
	Dir string
	Cmd string
}

// DefaultTrash is ~/.Trash on macOS plus any `trash` command on PATH.
func DefaultTrash() Trash {
	var t Trash
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			if fi, err := os.Stat(filepath.Join(home, ".Trash")); err == nil && fi.IsDir() {
				t.Dir = filepath.Join(home, ".Trash")
			}
		}
	}
	if p, err := exec.LookPath("trash"); err == nil {
		t.Cmd = p
	}
	return t
}

func (t Trash) Move(path string) error {
	var dirErr error
	if t.Dir != "" {
		if dirErr = os.Rename(path, uniqueName(t.Dir, filepath.Base(path))); dirErr == nil {
			return nil
		}
	}
	if t.Cmd != "" {
		if out, err := exec.Command(t.Cmd, path).CombinedOutput(); err != nil {
			return fmt.Errorf("trash: %v %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if dirErr != nil {
		return dirErr
	}
	return errNoTrash
}

func uniqueName(dir, name string) string {
	p := filepath.Join(dir, name)
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		stem, ext = name, ""
	}
	for i := 2; ; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s %d%s", stem, i, ext))
		if _, err := os.Lstat(p); err != nil {
			return p
		}
	}
}

var trashFor = DefaultTrash

var revealInOS = func(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Run()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Run()
}

// shellCommand is the user's $SHELL started in the directory holding path (or
// path itself when it is a directory).
func shellCommand(path string) *exec.Cmd {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	c := exec.Command(sh)
	c.Dir = path
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		c.Dir = filepath.Dir(path)
	}
	return c
}
