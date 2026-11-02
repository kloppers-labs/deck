package editor

import (
	"os"
	"path/filepath"
	"strings"
)

const doctorName = ".deck-doctor"

// CheckEnv is what Check inspects; MicroPath is empty when micro isn't installed.
type CheckEnv struct {
	MicroPath    string
	MicroVersion string
	LookPath     func(string) (string, error)
	Missing      []MissingServer
}

// Check lists problems with the micro setup, one human-readable line each.
func Check(env CheckEnv) []string {
	if env.MicroPath == "" {
		return []string{"micro not found — brew install micro"}
	}
	var issues []string
	if env.MicroVersion != "" && !versionAtLeast(env.MicroVersion, minOmniMicro) {
		issues = append(issues, "micro "+env.MicroVersion+" is older than "+minOmniMicro+", fuzzy finder disabled")
	}
	if missing := missingTools(env.LookPath); len(missing) > 0 {
		issues = append(issues, strings.Join(missing, ", ")+" not found, micro fuzzy finder disabled")
	}
	for _, m := range env.Missing {
		line := "micro LSP: " + m.Binary + " not found"
		if m.Hint != "" {
			line += " — " + m.Hint
		}
		issues = append(issues, line)
	}
	return issues
}

// NotifyOnce reports whether these issues are new for this manifest digest, and records them.
func NotifyOnce(dir, digest string, issues []string) bool {
	key := hash([]byte(digest + "\x00" + strings.Join(issues, "\n")))
	p := filepath.Join(dir, doctorName)
	if prev, err := os.ReadFile(p); err == nil && string(prev) == key {
		return false
	}
	_ = writeFile(p, []byte(key))
	return true
}
