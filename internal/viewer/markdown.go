package viewer

import (
	"crypto/sha256"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

var mermaidFence = regexp.MustCompile("(?s)```mermaid[ \t]*\n(.*?)\n```")

var mermaidBin = sync.OnceValues(func() (string, error) {
	return exec.LookPath("mermaid-ascii")
})

var (
	mermaidMu    sync.Mutex
	mermaidCache = map[[32]byte]string{}
)

// expandMermaid swaps ```mermaid blocks for their mermaid-ascii rendering in a
// plain fence, leaving a block untouched when the tool is missing or fails.
func expandMermaid(source string) string {
	if !strings.Contains(source, "```mermaid") {
		return source
	}
	bin, err := mermaidBin()
	if err != nil {
		return source
	}
	return mermaidFence.ReplaceAllStringFunc(source, func(block string) string {
		m := mermaidFence.FindStringSubmatch(block)
		if len(m) < 2 {
			return block
		}
		key := sha256.Sum256([]byte(bin + "\x00" + m[1]))
		mermaidMu.Lock()
		cached, ok := mermaidCache[key]
		mermaidMu.Unlock()
		if ok {
			return cached
		}
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(m[1])
		out, err := cmd.Output()
		if err != nil {
			return block
		}
		rendered := "```\n" + strings.TrimRight(string(out), "\n") + "\n```"
		mermaidMu.Lock()
		mermaidCache[key] = rendered
		mermaidMu.Unlock()
		return rendered
	})
}

type mdKey struct {
	style string
	width int
}

type mdRenderer struct {
	mu sync.Mutex
	r  *glamour.TermRenderer
}

var (
	mdMu        sync.Mutex
	mdRenderers = map[mdKey]*mdRenderer{}
)

func rendererFor(style string, width int) *mdRenderer {
	k := mdKey{style, width}
	mdMu.Lock()
	defer mdMu.Unlock()
	if r, ok := mdRenderers[k]; ok {
		return r
	}
	tr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		mdRenderers[k] = nil
		return nil
	}
	r := &mdRenderer{r: tr}
	mdRenderers[k] = r
	return r
}

// renderMarkdown renders through glamour, retrying with "dracula" when theme is
// not a glamour style, and returns the source when both fail.
func renderMarkdown(source string, width int, theme string) string {
	source = expandMermaid(source)
	if width <= 0 {
		width = 80
	}
	for _, style := range []string{theme, "dracula"} {
		r := rendererFor(style, width)
		if r == nil {
			continue
		}
		r.mu.Lock()
		out, err := r.r.Render(source)
		r.mu.Unlock()
		if err == nil {
			return out
		}
	}
	return source
}

func (m Model) markdownCmd(d *Document, gen, width int) tea.Cmd {
	theme := m.opts.Theme
	source := strings.Join(d.Raw, "\n")
	return func() tea.Msg {
		out := strings.TrimRight(renderMarkdown(source, width, theme), "\n")
		lines := strings.Split(out, "\n")
		for i, l := range lines {
			lines[i] = sanitizeStyled(l)
		}
		return MarkdownRenderedMsg{Doc: d, Gen: gen, Width: width, Lines: lines}
	}
}

// sanitizeStyled keeps SGR sequences and printable text, and drops every other
// escape or control byte.
func sanitizeStyled(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b {
			n := escapeLen(s[i:])
			if seq := s[i : i+n]; n > 2 && seq[1] == '[' && seq[n-1] == 'm' {
				b.WriteString(seq)
			}
			i += n
			continue
		}
		if c == '\t' {
			b.WriteString("    ")
			i++
			continue
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}
