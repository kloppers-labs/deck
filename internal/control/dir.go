// Package control lets other processes drive a running deck: each instance
// listens on a Unix socket and advertises itself with a sidecar file.
package control

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const maxSocketPath = 103

// Dir is where instances keep their sockets. It never depends on TMPDIR, which
// an agent's sandbox may point elsewhere.
func Dir(getenv func(string) string, home string) (string, error) {
	if d := getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "deck"), nil
	}
	if d := getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "deck", "run"), nil
	}
	if home == "" {
		return "", errors.New("no home directory for deck's control socket")
	}
	return filepath.Join(home, ".local", "state", "deck", "run"), nil
}

func SocketPath(dir string, pid int) (string, error) {
	p := filepath.Join(dir, strconv.Itoa(pid)+".sock")
	if len(p) > maxSocketPath {
		return "", fmt.Errorf("control socket path is too long (%d bytes): %s", len(p), p)
	}
	return p, nil
}

func sidecarName(pid int) string { return strconv.Itoa(pid) + ".json" }

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}
