package files

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/ui"
	"deck/internal/viewer"
)

// Meta is what the hover tooltip and the info panel show for one path. Lines
// is -1 when not counted; a directory's Size is the total of the files walked.
type Meta struct {
	Path, Name               string
	IsDir, IsSymlink, Broken bool
	Target                   string
	Size                     int64
	ModTime                  time.Time
	Mode                     fs.FileMode
	Owner                    string
	Lines                    int
	Binary                   bool
	Files, Dirs              int
	Partial                  bool
	Lang                     string
	Extras                   []string
	Err                      error
}

// StatMsg carries a StatCmd result; Seq ties it to the hover or info request.
type StatMsg struct {
	Seq  int
	Meta Meta
}

func (StatMsg) RouteTo() ui.Dest { return ui.DestFiles }

type statLimits struct {
	MaxEntries    int
	MaxDur        time.Duration
	MaxCountBytes int64
}

var defaultStatLimits = statLimits{MaxEntries: 20_000, MaxDur: 300 * time.Millisecond, MaxCountBytes: 10 << 20}

type metaCache struct {
	mu sync.Mutex
	m  map[string]Meta
}

func newMetaCache() *metaCache { return &metaCache{m: map[string]Meta{}} }

func (c *metaCache) get(path string, fi fs.FileInfo) (Meta, bool) {
	if c == nil {
		return Meta{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.m[path]
	if !ok || !m.ModTime.Equal(fi.ModTime()) || (!m.IsDir && m.Size != fi.Size()) {
		return Meta{}, false
	}
	return m, true
}

func (c *metaCache) put(m Meta) {
	if c == nil || m.Err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) > 2048 {
		clear(c.m)
	}
	c.m[m.Path] = m
}

// StatCmd gathers path's Meta off the update loop.
func StatCmd(path string, seq int, cache *metaCache) tea.Cmd {
	return func() tea.Msg { return StatMsg{Seq: seq, Meta: statPath(path, defaultStatLimits, cache)} }
}

func statPath(path string, lim statLimits, cache *metaCache) Meta {
	m := Meta{Path: path, Name: filepath.Base(path), Lines: -1}
	li, err := os.Lstat(path)
	if err != nil {
		m.Err = err
		return m
	}
	fi := li
	if li.Mode()&fs.ModeSymlink != 0 {
		m.IsSymlink = true
		m.Target, _ = os.Readlink(path)
		if st, err := os.Stat(path); err == nil {
			fi = st
		} else {
			m.Broken = true
		}
	}
	if cached, ok := cache.get(path, fi); ok {
		return cached
	}
	m.IsDir, m.Size, m.ModTime, m.Mode, m.Owner = fi.IsDir(), fi.Size(), fi.ModTime(), fi.Mode(), ownerOf(fi)
	switch {
	case m.Broken:
	case m.IsDir:
		walkDir(&m, lim)
	default:
		countLines(&m, lim.MaxCountBytes)
		if lang := viewer.LexerFor(path, "").Config().Name; lang != "plaintext" && lang != "fallback" {
			m.Lang = lang
		}
		m.Extras = projectExtras(path)
	}
	cache.put(m)
	return m
}

func walkDir(m *Meta, lim statLimits) {
	start := time.Now()
	m.Size = 0
	n := 0
	_ = filepath.WalkDir(m.Path, func(p string, d fs.DirEntry, err error) error {
		if p == m.Path {
			return nil
		}
		if n >= lim.MaxEntries || time.Since(start) > lim.MaxDur {
			m.Partial = true
			return fs.SkipAll
		}
		n++
		if d.IsDir() {
			m.Dirs++
			return nil
		}
		m.Files++
		if info, err := d.Info(); err == nil {
			m.Size += info.Size()
		}
		return nil
	})
}

func countLines(m *Meta, limit int64) {
	if m.Size > limit {
		return
	}
	f, err := os.Open(m.Path)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return
	}
	if ui.IsBinary(data) {
		m.Binary = true
		return
	}
	m.Lines = bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		m.Lines++
	}
}

func projectExtras(path string) []string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".csproj" && ext != ".fsproj" && ext != ".vbproj" && ext != ".sln" && ext != ".slnx" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	switch ext {
	case ".sln":
		return []string{plural(len(ParseSln(data)), "project")}
	case ".slnx":
		return []string{plural(len(ParseSlnx(data)), "project")}
	}
	p, err := ParseCsproj(data)
	if err != nil {
		return nil
	}
	return p.summary()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d %cB", int64(v), "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGTPE"[exp])
}

func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d/(30*24*time.Hour)))
	}
	return fmt.Sprintf("%dy ago", int(d/(365*24*time.Hour)))
}

var metaName = lipgloss.NewStyle().Bold(true)

// FormatMeta lays out m as lines at most w cells wide; git is the path's badge,
// aggregated for a directory.
func FormatMeta(m Meta, git Badge, w int, now time.Time) []string {
	name := m.Name
	if m.IsDir {
		name += "/"
	}
	lines := []string{metaName.Render(name)}
	if m.IsSymlink {
		lines = append(lines, ui.Muted.Render("→ "+m.Target))
	}
	if m.Err != nil {
		lines = append(lines, ui.Err.Render(loadError(m.Err)))
		return fitLines(lines, w)
	}
	switch {
	case m.Broken:
		lines = append(lines, ui.Err.Render("broken link"))
	case m.IsDir:
		more := ""
		if m.Partial {
			more = "+"
		}
		lines = append(lines, flow(" · ", w, fmt.Sprintf("%d%s files", m.Files, more), fmt.Sprintf("%d%s dirs", m.Dirs, more), humanBytes(m.Size)+more)...)
	case m.Binary:
		lines = append(lines, flow(" · ", w, humanBytes(m.Size), "binary")...)
	case m.Lines >= 0:
		lines = append(lines, flow(" · ", w, humanBytes(m.Size), plural(m.Lines, "line"))...)
	default:
		lines = append(lines, humanBytes(m.Size))
	}
	if !m.ModTime.IsZero() {
		lines = append(lines, flow(" ", w, ago(m.ModTime, now), "("+m.ModTime.Format("2006-01-02 15:04")+")")...)
	}
	for _, l := range flow(" ", w, m.Mode.String(), m.Owner) {
		lines = append(lines, ui.Muted.Render(l))
	}
	if git != BadgeNone {
		lines = append(lines, badgeStyle(git).Render("git: "+git.String()))
	}
	if m.Lang != "" {
		lines = append(lines, m.Lang)
	}
	lines = append(lines, m.Extras...)
	if m.Partial {
		lines = append(lines, ui.Muted.Render("partial: walk capped"))
	}
	return fitLines(lines, w)
}

// flow joins parts on one line when they fit in w, else gives each its own line.
func flow(sep string, w int, parts ...string) []string {
	parts = nonEmpty(parts...)
	if joined := strings.Join(parts, sep); ansi.StringWidth(joined) <= w {
		return []string{joined}
	}
	return parts
}

func fitLines(lines []string, w int) []string {
	for i, l := range lines {
		lines[i] = ui.FitWidth(l, w, "…")
	}
	return lines
}

func loadError(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
