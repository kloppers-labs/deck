package control

import (
	"path/filepath"
	"regexp"
	"strconv"
)

var targetSuffix = regexp.MustCompile(`^(.+?):(\d+)(?::(\d+)|-(\d+))?$`)

// ParseTarget splits a trailing :line, :line:col or :line-end off arg.
func ParseTarget(arg string) (path string, line, col, endLine int) {
	m := targetSuffix.FindStringSubmatch(arg)
	if m == nil {
		return arg, 0, 0, 0
	}
	line, _ = strconv.Atoi(m[2])
	col, _ = strconv.Atoi(m[3])
	endLine, _ = strconv.Atoi(m[4])
	if endLine < line {
		endLine = 0
	}
	return m[1], line, col, endLine
}

// Dirs lists every directory a deck may have registered in, Dir's choice
// first, so a client whose environment differs from deck's still finds it.
func Dirs(getenv func(string) string, home string) []string {
	var out []string
	if d := getenv("XDG_RUNTIME_DIR"); d != "" {
		out = append(out, filepath.Join(d, "deck"))
	}
	if d := getenv("XDG_STATE_HOME"); d != "" {
		out = append(out, filepath.Join(d, "deck", "run"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, ".local", "state", "deck", "run"))
	}
	return out
}

// ListAll lists the live instances across dirs, each pid once.
func ListAll(dirs []string) []Instance {
	var out []Instance
	seen := map[int]bool{}
	for _, d := range dirs {
		insts, _ := List(d)
		for _, in := range insts {
			if !seen[in.PID] {
				seen[in.PID] = true
				out = append(out, in)
			}
		}
	}
	return out
}
