package files

import (
	"path/filepath"
	"strings"
)

// HidePolicy decides which tree entries are shown. Ignored holds absolute
// gitignored paths; Keep is a revealed path that stays visible with its ancestors.
type HidePolicy struct {
	Names       map[string]bool
	ShowDot     bool
	ShowIgnored bool
	Ignored     map[string]bool
	Keep        string
}

func NewHidePolicy(names []string, showDot bool) HidePolicy {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return HidePolicy{Names: set, ShowDot: showDot}
}

func (h HidePolicy) Hidden(n *Node) bool {
	if h.Keep != "" && (n.Path == h.Keep || isUnder(h.Keep, n.Path)) {
		return false
	}
	if !h.ShowDot && strings.HasPrefix(n.Name, ".") {
		return true
	}
	if h.ShowIgnored {
		return false
	}
	return h.Names[n.Name] || h.Ignored[n.Path]
}

// IsIgnored reports whether path or one of its ancestors is gitignored.
func (h HidePolicy) IsIgnored(path string) bool {
	if len(h.Ignored) == 0 {
		return false
	}
	for p := path; ; {
		if h.Ignored[p] {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
