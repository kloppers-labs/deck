package viewer

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	tea "github.com/charmbracelet/bubbletea"
)

type colorProfile int

const (
	profile256 colorProfile = iota
	profileTrue
)

var detectProfile = func() colorProfile {
	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return profileTrue
	}
	return profile256
}

type paletteKey struct {
	theme string
	prof  colorProfile
}

type palette struct {
	mu    sync.Mutex
	style *chroma.Style
	prof  colorProfile
	sgr   map[chroma.TokenType]string
}

var palettes sync.Map

func paletteFor(theme string, prof colorProfile) *palette {
	k := paletteKey{theme, prof}
	if p, ok := palettes.Load(k); ok {
		return p.(*palette)
	}
	p, _ := palettes.LoadOrStore(k, &palette{style: styles.Get(theme), prof: prof, sgr: map[chroma.TokenType]string{}})
	return p.(*palette)
}

func (p *palette) code(t chroma.TokenType) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.sgr[t]; ok {
		return s
	}
	s := entrySGR(p.style.Get(t), p.prof)
	p.sgr[t] = s
	return s
}

func entrySGR(e chroma.StyleEntry, prof colorProfile) string {
	var parts []string
	if e.Colour.IsSet() {
		if prof == profileTrue {
			parts = append(parts, "38;2;"+strconv.Itoa(int(e.Colour.Red()))+";"+
				strconv.Itoa(int(e.Colour.Green()))+";"+strconv.Itoa(int(e.Colour.Blue())))
		} else {
			parts = append(parts, "38;5;"+strconv.Itoa(nearest256(e.Colour.Red(), e.Colour.Green(), e.Colour.Blue())))
		}
	}
	if e.Bold == chroma.Yes {
		parts = append(parts, "1")
	}
	if e.Italic == chroma.Yes {
		parts = append(parts, "3")
	}
	if e.Underline == chroma.Yes {
		parts = append(parts, "4")
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

func nearestLevel(v int) int {
	best := 0
	for i, l := range cubeLevels {
		if abs(v-l) < abs(v-cubeLevels[best]) {
			best = i
		}
	}
	return best
}

func nearest256(r8, g8, b8 uint8) int {
	r, g, b := int(r8), int(g8), int(b8)
	ri, gi, bi := nearestLevel(r), nearestLevel(g), nearestLevel(b)
	cube := 16 + 36*ri + 6*gi + bi
	cubeDist := sq(r-cubeLevels[ri]) + sq(g-cubeLevels[gi]) + sq(b-cubeLevels[bi])
	gray := min(max((r+g+b)/3-8, 0)/10, 23)
	gv := 8 + 10*gray
	grayDist := sq(r-gv) + sq(g-gv) + sq(b-gv)
	if grayDist < cubeDist {
		return 232 + gray
	}
	return cube
}

func abs(v int) int { return max(v, -v) }
func sq(v int) int  { return v * v }

// Highlight tokenises raw as one text so multi-line constructs stay correct, and
// returns one styled line per raw line, tab-expanded, sanitised and ending in a reset.
func Highlight(path string, raw []string, theme string, tabWidth int) []string {
	out := make([]string, 0, len(raw))
	text := strings.Join(raw, "\n")
	pal := paletteFor(theme, detectProfile())
	lexer := chroma.Coalesce(LexerFor(path, text[:min(len(text), 4096)]))
	it, err := lexer.Tokenise(nil, text)
	if err != nil {
		for _, l := range raw {
			out = append(out, ExpandTabs(sanitizeLine(l), tabWidth)+sgrReset)
		}
		return out
	}
	var b strings.Builder
	col, cur := 0, ""
	finish := func() {
		i := len(out)
		if i < len(raw) && col > maxStyledCols {
			out = append(out, ExpandTabs(sanitizeLine(raw[i]), tabWidth)+sgrReset)
		} else {
			out = append(out, b.String()+sgrReset)
		}
		b.Reset()
		col, cur = 0, ""
	}
	for tok := it(); tok != chroma.EOF; tok = it() {
		sgr := pal.code(tok.Type)
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				finish()
			}
			if part == "" {
				continue
			}
			if sgr != cur {
				if cur != "" {
					b.WriteString(sgrReset)
				}
				b.WriteString(sgr)
				cur = sgr
			}
			var s string
			s, col = expandFrom(sanitizeLine(part), col, tabWidth)
			b.WriteString(s)
		}
	}
	if b.Len() > 0 || len(out) < len(raw) {
		finish()
	}
	for len(out) < len(raw) {
		out = append(out, sgrReset)
	}
	return out[:len(raw)]
}

func (m Model) highlightCmd(d *Document, gen, from, to int) tea.Cmd {
	latest, theme, path := m.latest, m.opts.Theme, d.Path
	raw := d.Raw[from:to]
	tw := d.TabWidth
	return func() tea.Msg {
		if latest != nil && int(latest.Load()) != gen {
			return DocHighlightedMsg{Doc: d, Gen: gen, From: from, Skipped: true}
		}
		return DocHighlightedMsg{Doc: d, Gen: gen, From: from, Lines: Highlight(path, raw, theme, tw)}
	}
}
