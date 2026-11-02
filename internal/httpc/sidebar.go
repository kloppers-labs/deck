package httpc

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/ui"
)

const (
	maxDiscoverDepth = 4
	maxHTTPFiles     = 100
	maxHTTPFileBytes = 1 << 20
	sidebarMaxWidth  = 72
)

var skipDirs = map[string]bool{"bin": true, "obj": true, "node_modules": true, "TestResults": true}

// LoadRequestMsg asks the HTTP tab to load the request at Line (1-based; 0 for
// the first) of the .http file at Path, and to take focus.
type LoadRequestMsg struct {
	Path string
	Line int
}

func LoadRequest(path string, line int) tea.Cmd {
	return func() tea.Msg { return LoadRequestMsg{Path: path, Line: line} }
}

type requestLoadedMsg struct {
	req    fileRequest
	vars   map[string]string
	dotenv map[string]string
	envs   envSet
	err    error
}

type httpFilesMsg struct {
	items []sidebarItem
	envs  envSet
}

func (LoadRequestMsg) RouteTo() ui.Dest   { return ui.DestHTTP }
func (requestLoadedMsg) RouteTo() ui.Dest { return ui.DestHTTP }
func (httpFilesMsg) RouteTo() ui.Dest     { return ui.DestHTTP }

type sidebarItem struct {
	file string
	path string
	req  fileRequest
}

func (it sidebarItem) label() string {
	what := it.req.Method + " " + it.req.URL
	if it.req.Name != "" {
		what = it.req.Name + ui.Muted.Render("  "+what)
	}
	return ui.Muted.Render(it.file+" › ") + what
}

func readHTTPFile(path string) (httpFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return httpFile{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxHTTPFileBytes))
	if err != nil {
		return httpFile{}, err
	}
	return parseHTTPFile(string(data)), nil
}

func workDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// discoverCmd lists the requests in .http files under root ("" for the
// working directory), bounded in depth and file count.
func discoverCmd(root string) tea.Cmd {
	return func() tea.Msg {
		if root == "" {
			root = workDir()
		}
		var items []sidebarItem
		files := 0
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if files >= maxHTTPFiles {
				return filepath.SkipAll
			}
			rel, _ := filepath.Rel(root, path)
			if d.IsDir() {
				if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") ||
					strings.Count(rel, string(filepath.Separator)) >= maxDiscoverDepth) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".http") {
				return nil
			}
			files++
			f, err := readHTTPFile(path)
			if err != nil {
				return nil
			}
			for _, r := range f.Requests {
				items = append(items, sidebarItem{file: filepath.ToSlash(rel), path: path, req: r})
			}
			return nil
		})
		return httpFilesMsg{items: items, envs: loadEnvs(root, root)}
	}
}

// loadCmd reads one request from a .http file along with the file's
// variables, the nearest environments and the .env beside it.
func loadCmd(path string, line int) tea.Cmd {
	return func() tea.Msg {
		f, err := readHTTPFile(path)
		if err != nil {
			return requestLoadedMsg{err: err}
		}
		req, ok := f.requestAt(line)
		if !ok {
			return requestLoadedMsg{err: os.ErrNotExist}
		}
		dir := filepath.Dir(path)
		root := workDir()
		if !strings.HasPrefix(dir, root) {
			root = dir
		}
		dotenv := map[string]string{}
		if data, err := os.ReadFile(filepath.Join(dir, ".env")); err == nil {
			dotenv = parseDotEnv(string(data))
		}
		return requestLoadedMsg{req: req, vars: f.Vars, dotenv: dotenv, envs: loadEnvs(dir, root)}
	}
}

type sidebar struct {
	open   bool
	loaded bool
	items  []sidebarItem
	cursor int
}

func (s *sidebar) move(d int) {
	s.cursor = min(max(s.cursor+d, 0), max(len(s.items)-1, 0))
}

func (s sidebar) selected() (sidebarItem, bool) {
	if s.cursor < 0 || s.cursor >= len(s.items) {
		return sidebarItem{}, false
	}
	return s.items[s.cursor], true
}

func (s sidebar) view(w, h int) string {
	innerW, rows := max(w-4, 1), max(h-3, 1)
	head := ui.Active.Render(".http requests") + ui.Muted.Render("  enter load · esc close")
	var lines []string
	switch {
	case !s.loaded:
		lines = append(lines, ui.Muted.Render("searching…"))
	case len(s.items) == 0:
		lines = append(lines, ui.Muted.Render("no .http files found"))
	}
	top := min(max(s.cursor-rows+1, 0), max(len(s.items)-rows, 0))
	for i := top; i < len(s.items) && len(lines) < rows; i++ {
		row := "  " + s.items[i].label()
		if i == s.cursor {
			row = ui.Active.Render("› ") + s.items[i].label()
		}
		lines = append(lines, ui.FitWidth(row, innerW, "…"))
	}
	content := ui.FitWidth(head, innerW, "…") + "\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorAccent).
		Padding(0, 1).
		Render(ui.Fit(content, innerW, max(h-2, 1)))
}
