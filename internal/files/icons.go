package files

import (
	"path/filepath"
	"strings"
)

var nerdByExt = map[string]string{
	".go": "", ".cs": "\U000f031b", ".csproj": "\U000f0610", ".sln": "\U000f0610", ".slnx": "\U000f0610",
	".ts": "", ".tsx": "", ".js": "", ".jsx": "", ".json": "",
	".md": "", ".py": "", ".rs": "", ".rb": "", ".html": "",
	".css": "", ".scss": "", ".yml": "", ".yaml": "", ".toml": "",
	".xml": "\U000f05c0", ".sh": "", ".sql": "", ".http": "\U000f059f", ".lua": "",
}

const (
	nerdFile      = ""
	nerdDir       = ""
	nerdDirOpen   = ""
	nerdSymlink   = ""
	unicodeLinked = "↪"
)

// icon is the two-cell prefix before a tree entry's name: an expand marker for
// directories and, in nerd mode, a file-type glyph.
func icon(n *Node, mode string) string {
	switch mode {
	case "nerd":
		switch {
		case n.IsDir && n.Expanded:
			return nerdDirOpen + " "
		case n.IsDir:
			return nerdDir + " "
		case n.IsSymlink:
			return nerdSymlink + " "
		}
		if g, ok := nerdByExt[strings.ToLower(filepath.Ext(n.Name))]; ok {
			return g + " "
		}
		return nerdFile + " "
	case "none":
		switch {
		case n.IsDir && n.Expanded:
			return "- "
		case n.IsDir:
			return "+ "
		}
		return "  "
	}
	switch {
	case n.IsDir && n.Expanded:
		return "▾ "
	case n.IsDir:
		return "▸ "
	case n.IsSymlink:
		return unicodeLinked + " "
	}
	return "  "
}
