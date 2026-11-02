package lsp

import (
	"os"
	"path/filepath"
)

const tsserverRel = "node_modules/typescript/lib/tsserver.js"

const TSInstallHint = "npm install --global typescript"

var globalNodeModules = []string{
	"/opt/homebrew/lib",
	"/usr/local/lib",
	"/opt/homebrew/opt/typescript/libexec/lib",
	"/usr/local/opt/typescript/libexec/lib",
}

// FindTSServer finds tsserver.js in a node_modules at or above root (local), else in a global install
// beside serverBin's real path or a known prefix. An empty root skips the local search.
func FindTSServer(root, serverBin string) (path string, local bool) {
	if root != "" {
		if p := walkUpFor(root, tsserverRel); p != "" {
			return p, true
		}
	}
	if serverBin != "" {
		if real, err := filepath.EvalSymlinks(serverBin); err == nil {
			if p := walkUpFor(filepath.Dir(real), tsserverRel); p != "" {
				return p, false
			}
		}
	}
	home, _ := os.UserHomeDir()
	dirs := globalNodeModules
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".npm-global", "lib"), filepath.Join(home, ".local", "lib"))
	}
	for _, d := range dirs {
		if p := filepath.Join(d, tsserverRel); fileExists(p) {
			return p, false
		}
	}
	return "", false
}

func walkUpFor(dir, rel string) string {
	for {
		if p := filepath.Join(dir, rel); fileExists(p) {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
