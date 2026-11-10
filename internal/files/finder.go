package files

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/ui/picker"
)

const nameBonus = 1000

var maxIndexPaths = 200_000

// FinderIndexMsg carries a freshly built find-file index.
type FinderIndexMsg struct {
	Seq    int
	Root   string
	Items  []picker.Item
	Source string
	Capped bool
	Err    error
}

func (FinderIndexMsg) RouteTo() ui.Dest { return ui.DestFiles }

type lister func(ctx context.Context, dir string, limit int, name string, args ...string) ([]string, bool, error)

type indexEnv struct {
	lookPath func(string) (string, error)
	list     lister
}

var finderEnv = indexEnv{lookPath: exec.LookPath, list: listCommand}

type fileIndex struct {
	Paths  []string
	Source string
	Capped bool
	Err    error
}

// buildIndex lists files under root, relative to it: through fd, else git
// ls-files, else a walk honouring hide. At most limit paths are kept.
func buildIndex(ctx context.Context, env indexEnv, root string, hide HidePolicy, limit int) fileIndex {
	for _, fd := range []string{"fd", "fdfind"} {
		if _, err := env.lookPath(fd); err != nil {
			continue
		}
		args := []string{"--type", "f", "--hidden", "--print0", "--exclude", ".git", "--exclude", "bin", "--exclude", "obj"}
		for name := range hide.Names {
			args = append(args, "--exclude", name)
		}
		if paths, capped, err := env.list(ctx, root, limit, fd, args...); err == nil {
			return fileIndex{Paths: paths, Source: "fd", Capped: capped}
		}
		break
	}
	if _, err := env.lookPath("git"); err == nil {
		if paths, capped, err := env.list(ctx, root, limit, "git", "ls-files", "-co", "--exclude-standard", "-z"); err == nil {
			return fileIndex{Paths: withoutHidden(paths, hide), Source: "git", Capped: capped}
		}
	}
	paths, capped, err := walkIndex(ctx, root, hide, limit)
	return fileIndex{Paths: paths, Source: "walk", Capped: capped, Err: err}
}

func withoutHidden(paths []string, hide HidePolicy) []string {
	if len(hide.Names) == 0 {
		return paths
	}
	out := paths[:0]
	for _, p := range paths {
		if !hiddenSegment(p, hide) {
			out = append(out, p)
		}
	}
	return out
}

func hiddenSegment(rel string, hide HidePolicy) bool {
	for _, seg := range strings.Split(rel, "/") {
		if hide.Names[seg] {
			return true
		}
	}
	return false
}

var errCapped = errors.New("capped")

func walkIndex(ctx context.Context, root string, hide HidePolicy, limit int) ([]string, bool, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p == root {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || hide.Names[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if hide.Names[d.Name()] {
			return nil
		}
		if limit > 0 && len(out) >= limit {
			return errCapped
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if errors.Is(err, errCapped) {
		return out, true, nil
	}
	return out, false, err
}

// listCommand runs name in dir and splits its NUL-separated output, stopping
// once limit entries have been read.
func listCommand(ctx context.Context, dir string, limit int, name string, args ...string) ([]string, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sc.Split(splitNUL)
	var out []string
	capped := false
	for sc.Scan() {
		if limit > 0 && len(out) >= limit {
			capped = true
			break
		}
		if p := strings.TrimPrefix(sc.Text(), "./"); p != "" {
			out = append(out, p)
		}
	}
	if capped {
		cancel()
		_ = cmd.Wait()
		return out, true, nil
	}
	if err := cmd.Wait(); err != nil {
		return nil, false, err
	}
	return out, false, nil
}

func splitNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func finderIndexCmd(root string, hide HidePolicy, seq int) tea.Cmd {
	env := finderEnv
	return func() tea.Msg {
		idx := buildIndex(context.Background(), env, root, hide, maxIndexPaths)
		items := make([]picker.Item, len(idx.Paths))
		for i, rel := range idx.Paths {
			items[i] = picker.Item{Title: rel, Path: filepath.Join(root, filepath.FromSlash(rel))}
		}
		return FinderIndexMsg{Seq: seq, Root: root, Items: items, Source: idx.Source, Capped: idx.Capped, Err: idx.Err}
	}
}

var lineSuffix = regexp.MustCompile(`^(.+?):(\d+)(?::(\d+))?$`)

// splitLineSuffix splits "file.cs:120" or "file.cs:120:7" into the query and a
// 1-based line and column (0 when absent).
func splitLineSuffix(q string) (string, int, int) {
	sm := lineSuffix.FindStringSubmatch(q)
	if sm == nil {
		return q, 0, 0
	}
	line, _ := strconv.Atoi(sm[2])
	col, _ := strconv.Atoi(sm[3])
	return sm[1], line, col
}

type baseSource []picker.Item

func (s baseSource) String(i int) string { return filepath.Base(s[i].Title) }
func (s baseSource) Len() int            { return len(s) }

type pathSource []picker.Item

func (s pathSource) String(i int) string { return s[i].Title }
func (s pathSource) Len() int            { return len(s) }

// matchFiles ranks relative paths for the finder: a fuzzy match inside the
// file name beats one that needs the directories. A :line suffix is ignored.
func matchFiles(query string, items []picker.Item) []picker.Match {
	q, _, _ := splitLineSuffix(query)
	named := make([]bool, len(items))
	var out []picker.Match
	for _, f := range fuzzy.FindFromNoSort(q, baseSource(items)) {
		title := items[f.Index].Title
		offset := utf8.RuneCountInString(title) - utf8.RuneCountInString(f.Str)
		pos := picker.RunePositions(f.Str, f.MatchedIndexes)
		for i := range pos {
			pos[i] += offset
		}
		named[f.Index] = true
		out = append(out, picker.Match{Index: f.Index, Score: f.Score + nameBonus, Positions: pos})
	}
	for _, f := range fuzzy.FindFromNoSort(q, pathSource(items)) {
		if !named[f.Index] {
			out = append(out, picker.Match{Index: f.Index, Score: f.Score, Positions: picker.RunePositions(f.Str, f.MatchedIndexes)})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		la, lb := len(items[out[a].Index].Title), len(items[out[b].Index].Title)
		if la != lb {
			return la < lb
		}
		return out[a].Index < out[b].Index
	})
	return out
}

type finderState struct {
	items    []picker.Item
	seq      int
	ready    bool
	building bool
	stale    bool
	source   string
	capped   bool
}

func (f finderState) status() string {
	switch {
	case !f.ready:
		return "indexing…"
	case f.capped:
		return f.source + " · capped at " + strconv.Itoa(maxIndexPaths)
	}
	return f.source
}

func (m *Model) openFinder() tea.Cmd {
	m.openPicker(picker.Options{
		ID: pickFind, Dest: ui.DestFiles, Title: "Find file",
		Placeholder: "name, path or file:line", Match: matchFiles,
	})
	var cmds []tea.Cmd
	if m.finder.ready {
		cmds = append(cmds, m.pick.SetItems(m.finder.items))
	}
	if !m.finder.ready || m.finder.stale {
		cmds = append(cmds, m.indexFiles())
	}
	m.pick.SetStatus(m.finder.status())
	return tea.Batch(cmds...)
}

func (m *Model) indexFiles() tea.Cmd {
	if m.finder.building {
		return nil
	}
	m.finder.building = true
	m.finder.seq++
	return finderIndexCmd(m.tree.Launch(), m.tree.Hide(), m.finder.seq)
}

func (m Model) finderIndexed(msg FinderIndexMsg) (Model, tea.Cmd) {
	if msg.Seq != m.finder.seq {
		return m, nil
	}
	m.finder = finderState{items: msg.Items, seq: msg.Seq, ready: true, source: msg.Source, capped: msg.Capped}
	var cmds []tea.Cmd
	if m.pick.Active() && m.pick.ID() == pickFind {
		cmds = append(cmds, m.pick.SetItems(msg.Items))
		m.pick.SetStatus(m.finder.status())
	}
	if msg.Err != nil {
		cmds = append(cmds, ui.Toast(ui.LevelWarn, "find: "+msg.Err.Error()))
	}
	return m, tea.Batch(cmds...)
}
