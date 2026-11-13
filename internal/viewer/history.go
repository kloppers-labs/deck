package viewer

const maxHistory = 200

// Loc is a cursor location in a file, in 0-based lines and rune columns.
type Loc struct {
	Path      string
	Line, Col int
}

// History is a jump list. Back from the newest entry first records where the
// jump started, so Forward can return there.
type History struct {
	entries []Loc
	pos     int
}

func (h *History) Len() int { return len(h.entries) }

func (h *History) Push(l Loc) {
	h.entries = h.entries[:min(h.pos, len(h.entries))]
	if n := len(h.entries); n == 0 || h.entries[n-1] != l {
		h.entries = append(h.entries, l)
	}
	if over := len(h.entries) - maxHistory; over > 0 {
		h.entries = append([]Loc(nil), h.entries[over:]...)
	}
	h.pos = len(h.entries)
}

func (h *History) Back(cur Loc) (Loc, bool) {
	if len(h.entries) == 0 {
		return Loc{}, false
	}
	if h.pos >= len(h.entries) {
		if n := len(h.entries); n > 0 && h.entries[n-1] != cur {
			h.entries = append(h.entries, cur)
		}
		h.pos = len(h.entries) - 1
	}
	if h.pos <= 0 {
		return Loc{}, false
	}
	h.pos--
	return h.entries[h.pos], true
}

func (h *History) Forward() (Loc, bool) {
	if h.pos >= len(h.entries)-1 {
		return Loc{}, false
	}
	h.pos++
	return h.entries[h.pos], true
}

// Recent lists up to n distinct paths from the jump list, newest first.
func (h *History) Recent(n int) []string {
	var out []string
	seen := map[string]bool{}
	for i := len(h.entries) - 1; i >= 0 && len(out) < n; i-- {
		if p := h.entries[i].Path; p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
