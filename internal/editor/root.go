package editor

import (
	"path/filepath"
	"strings"
)

var rootTiers = [][]string{
	{"*.sln", "*.slnx"},
	{"go.mod", "tsconfig.json", "package.json"},
	{".git"},
}

// ProjectRoot is the directory micro starts in for path: the nearest solution,
// then module/package root, then repository, searching no higher than cwd.
func ProjectRoot(path, cwd string) string {
	dir := filepath.Dir(path)
	if cwd == "" || !within(dir, cwd) {
		return dir
	}
	for _, tier := range rootTiers {
		for d := dir; ; d = filepath.Dir(d) {
			if hasAny(d, tier) {
				return d
			}
			if d == cwd || d == filepath.Dir(d) {
				break
			}
		}
	}
	return cwd
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hasAny(dir string, markers []string) bool {
	for _, m := range markers {
		if matches, _ := filepath.Glob(filepath.Join(globEscape(dir), m)); len(matches) > 0 {
			return true
		}
	}
	return false
}

func globEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`).Replace(s)
}
