// Package ide lets Claude Code use deck as its editor: deck advertises itself
// with a lockfile and speaks Claude's MCP-over-WebSocket IDE protocol.
package ide

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const ideName = "deck"

type lockFile struct {
	PID              int      `json:"pid"`
	WorkspaceFolders []string `json:"workspaceFolders"`
	IDEName          string   `json:"ideName"`
	Transport        string   `json:"transport"`
	AuthToken        string   `json:"authToken"`
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeLock(dir string, port int, lf lockFile) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	data, err := json.Marshal(lf)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, strconv.Itoa(port)+".lock")
	tmp, err := os.CreateTemp(dir, ".deck-lock-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// sweep removes lockfiles left by decks that are no longer running; other IDEs' files are left alone.
func sweep(dir string) {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.lock"))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var lf lockFile
		if json.Unmarshal(data, &lf) != nil || lf.IDEName != ideName || lf.PID <= 0 {
			continue
		}
		if err := syscall.Kill(lf.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
			os.Remove(p)
		}
	}
}
