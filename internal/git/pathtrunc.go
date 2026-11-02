package git

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// TruncatePath fits a path into w cells, shrinking the directory part from the
// middle first so the file name stays readable.
func TruncatePath(p string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(p) <= w {
		return p
	}
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return truncateMiddle(p, w)
	}
	dir, base := p[:i], p[i+1:]
	if ansi.StringWidth(base) > w-2 {
		return truncateMiddle(base, w)
	}
	return truncateMiddle(dir, w-ansi.StringWidth(base)-1) + "/" + base
}

func truncateMiddle(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw <= w {
		return s
	}
	if w <= 1 {
		return ansi.Truncate("…", max(w, 0), "")
	}
	head := (w - 1) / 2
	tail := w - 1 - head
	return ansi.Truncate(s, head, "") + "…" + ansi.TruncateLeft(s, sw-tail, "")
}
