package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/glamour"

	"deck/internal/ui"
)

// maxPreviewBytes caps how much of a file we read for the preview pane, so
// opening a huge file in the navigator stays cheap.
const maxPreviewBytes = 128 * 1024

// isMarkdown reports whether a path should render through glamour rather than a
// syntax highlighter.
func isMarkdown(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

// highlight syntax-colours source for terminal display, detecting the language
// from the filename and falling back to content analysis or plain text. theme
// is a chroma style name; an unknown name falls back to chroma's default.
func highlight(filename, source, theme string) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(source)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	style := styles.Get(theme)
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return source
	}
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return source
	}
	var buf strings.Builder
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return source
	}
	return buf.String()
}

// Preview reads a file and returns a display-ready, coloured rendering:
// markdown via glamour, everything else via syntax highlighting. theme is the
// chroma/glamour style name. On any read error it returns the error text so the
// pane has something to show.
func Preview(path string, width int, theme string) string {
	data, err := readCapped(path)
	if err != nil {
		return "cannot preview: " + err.Error()
	}
	if ui.IsBinary(data) {
		return ui.Muted.Render("⬡ binary file — preview not available")
	}
	source := string(data)
	if isMarkdown(path) {
		return renderMarkdown(source, width, theme)
	}
	return highlight(path, source, theme)
}

// mermaidFence matches a ```mermaid fenced block, capturing the diagram body.
var mermaidFence = regexp.MustCompile("(?s)```mermaid[ \t]*\n(.*?)\n```")

// mermaidBin resolves the optional mermaid-ascii binary once. It's a variable
// so tests can stub the lookup.
var mermaidBin = sync.OnceValues(func() (string, error) {
	return exec.LookPath("mermaid-ascii")
})

// expandMermaid replaces ```mermaid blocks with their mermaid-ascii rendering,
// re-wrapped in a plain code fence so glamour shows the art verbatim instead of
// trying to format the diagram source. When mermaid-ascii isn't installed, or a
// diagram fails to render, the original fence is left untouched.
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
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(m[1])
		out, err := cmd.Output()
		if err != nil {
			return block
		}
		return "```\n" + strings.TrimRight(string(out), "\n") + "\n```"
	})
}

// renderMarkdown renders markdown through glamour. glamour ships far fewer
// styles than chroma, so when theme isn't one of its built-ins we retry with
// "dracula"; if even that fails we return the raw source.
func renderMarkdown(source string, width int, theme string) string {
	source = expandMermaid(source)
	w := width
	if w <= 0 {
		w = 80
	}
	for _, style := range []string{theme, "dracula"} {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			glamour.WithWordWrap(w),
			glamour.WithEmoji(),
		)
		if err != nil {
			continue
		}
		if out, rerr := r.Render(source); rerr == nil {
			return out
		}
	}
	return source
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, maxPreviewBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}
