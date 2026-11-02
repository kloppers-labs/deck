package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const versionProbeTimeout = 3 * time.Second

// ConfigHome is deck's private micro config directory; the user's ~/.config/micro is never touched.
func ConfigHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "deck", "micro")
}

var microEnvKeys = []string{"MICRO_CONFIG_HOME", "MICRO_TRUECOLOR", "DECK_EDITOR_STATE", "DECK_EDITOR_TOP"}

func configureMicro(c *exec.Cmd, t Target, o Opts) {
	c.Dir = ProjectRoot(t.Path, o.Cwd)
	base := environ(o)
	colorterm, _ := lookupEnv(base, "COLORTERM")
	inheritedTruecolor, hasTruecolor := lookupEnv(base, "MICRO_TRUECOLOR")
	env := withoutKeys(base, microEnvKeys)
	if o.ConfigDir != "" {
		env = append(env, "MICRO_CONFIG_HOME="+o.ConfigDir)
	}
	switch {
	case colorterm == "truecolor" || colorterm == "24bit":
		env = append(env, "MICRO_TRUECOLOR=1")
	case hasTruecolor:
		env = append(env, "MICRO_TRUECOLOR="+inheritedTruecolor)
	}
	if o.StateFile != "" {
		env = append(env, "DECK_EDITOR_STATE="+o.StateFile)
		if t.Top > 0 {
			env = append(env, "DECK_EDITOR_TOP="+strconv.Itoa(t.Top))
		}
	}
	c.Env = env
}

func lookupEnv(env []string, key string) (string, bool) {
	var val string
	var found bool
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			val, found = v, true
		}
	}
	return val, found
}

func withoutKeys(env, keys []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		drop := false
		for _, key := range keys {
			if k == key {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}

// MicroVersion runs `micro -version` and returns the version, or "" if it can't tell.
func MicroVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-version").Output()
	if err != nil {
		return ""
	}
	return ParseMicroVersion(string(out))
}

// ParseMicroVersion extracts the version from `micro -version` output.
func ParseMicroVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func versionAtLeast(v, min string) bool {
	if v == "" {
		return false
	}
	have, want := versionParts(v), versionParts(min)
	for i := range max(len(have), len(want)) {
		h, w := part(have, i), part(want, i)
		if h != w {
			return h > w
		}
	}
	return true
}

func versionParts(v string) []int {
	core, _, _ := strings.Cut(v, "-")
	var out []int
	for _, f := range strings.Split(core, ".") {
		n, err := strconv.Atoi(f)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func part(p []int, i int) int {
	if i < len(p) {
		return p[i]
	}
	return 0
}
