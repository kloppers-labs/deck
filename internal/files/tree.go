package files

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/ui"
)

var bigDirEntries = 5000

// Node is one tree entry. A symlink to a directory counts as a directory; a
// broken link is Broken with its Target. Children load on the first expand.
type Node struct {
	Path, Name                         string
	IsDir, IsSymlink, Expanded, Loaded bool
	Broken                             bool
	Target                             string
	Size                               int64
	ModTime                            time.Time
	Children                           []*Node
	Depth                              int

	loading bool
	err     error
}

func (n *Node) child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

type dirLoadedMsg struct {
	Path  string
	Nodes []*Node
	Err   error
}

func (dirLoadedMsg) RouteTo() ui.Dest { return ui.DestFiles }

// Tree is the sidebar model: a lazily loaded directory tree flattened into the
// visible rows, with the selection tracked by path so it survives reloads.
type Tree struct {
	launch  string
	root    *Node
	rows    []*Node
	sel     int
	selPath string
	top     int
	height  int
	hide    HidePolicy
	sort    SortMode
}

// NewTree lists root. A root over the size cap loads through the returned command.
func NewTree(root string, hide HidePolicy, mode SortMode) (Tree, tea.Cmd) {
	t := Tree{launch: root, hide: hide, sort: mode}
	t.root = &Node{Path: root, Name: filepath.Base(root), IsDir: true, Expanded: true}
	cmd := t.load(t.root)
	t.refresh()
	return t, cmd
}

func newNode(dir string, de fs.DirEntry) *Node {
	p := filepath.Join(dir, de.Name())
	n := &Node{Path: p, Name: de.Name(), IsDir: de.IsDir()}
	if info, err := de.Info(); err == nil {
		n.Size, n.ModTime = info.Size(), info.ModTime()
	}
	if de.Type()&fs.ModeSymlink != 0 {
		n.IsSymlink = true
		if fi, err := os.Stat(p); err == nil {
			n.IsDir, n.Size, n.ModTime = fi.IsDir(), fi.Size(), fi.ModTime()
		} else {
			n.Broken = true
			n.Target, _ = os.Readlink(p)
		}
	}
	return n
}

// readChildren lists dir sorted by mode. With limit > 0 it gives up (big=true)
// as soon as the directory has more than limit entries.
func readChildren(dir string, limit int, mode SortMode) (nodes []*Node, big bool, err error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	n := -1
	if limit > 0 {
		n = limit + 1
	}
	des, err := f.ReadDir(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	if limit > 0 && len(des) > limit {
		return nil, true, nil
	}
	nodes = make([]*Node, 0, len(des))
	for _, de := range des {
		nodes = append(nodes, newNode(dir, de))
	}
	sortNodes(nodes, mode)
	return nodes, false, nil
}

func loadDirCmd(path string, mode SortMode) tea.Cmd {
	return func() tea.Msg {
		nodes, _, err := readChildren(path, 0, mode)
		return dirLoadedMsg{Path: path, Nodes: nodes, Err: err}
	}
}

func (t *Tree) load(n *Node) tea.Cmd {
	nodes, big, err := readChildren(n.Path, bigDirEntries, t.sort)
	if big {
		n.loading = true
		return loadDirCmd(n.Path, t.sort)
	}
	n.Children, n.err, n.Loaded = nodes, err, true
	return nil
}

func (t *Tree) loadSync(n *Node) {
	if n.Loaded {
		return
	}
	nodes, _, err := readChildren(n.Path, 0, t.sort)
	n.Children, n.err, n.Loaded, n.loading = nodes, err, true, false
}

// Loaded applies an async directory load, unless the directory was loaded in
// place meanwhile.
func (t *Tree) Loaded(msg dirLoadedMsg) {
	n := t.find(msg.Path)
	if n == nil || !n.loading {
		return
	}
	n.loading = false
	n.Children, n.err, n.Loaded = msg.Nodes, msg.Err, true
	sortNodes(n.Children, t.sort)
	t.refresh()
}

// Flatten lists the visible nodes under root in display order, setting each
// node's Depth. Collapsed directories and hidden entries are skipped.
func Flatten(root *Node, hide HidePolicy) []*Node {
	rows := make([]*Node, 0, len(root.Children))
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		for _, c := range n.Children {
			if hide.Hidden(c) {
				continue
			}
			c.Depth = depth
			rows = append(rows, c)
			if c.IsDir && c.Expanded {
				walk(c, depth+1)
			}
		}
	}
	walk(root, 0)
	return rows
}

func (t *Tree) refresh() {
	t.rows = Flatten(t.root, t.hide)
	if i := t.indexOf(t.selPath); i >= 0 {
		t.sel = i
	}
	t.sel = min(max(t.sel, 0), max(len(t.rows)-1, 0))
	t.selPath = ""
	if len(t.rows) > 0 {
		t.selPath = t.rows[t.sel].Path
	}
	t.ensureVisible()
}

func (t *Tree) indexOf(path string) int {
	if path == "" {
		return -1
	}
	for i, n := range t.rows {
		if n.Path == path {
			return i
		}
	}
	return -1
}

func (t *Tree) find(path string) *Node {
	if path == t.root.Path {
		return t.root
	}
	if !isUnder(path, t.root.Path) {
		return nil
	}
	rel, _ := filepath.Rel(t.root.Path, path)
	n := t.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if n = n.child(part); n == nil {
			return nil
		}
	}
	return n
}

func (t Tree) Root() *Node      { return t.root }
func (t Tree) Rows() []*Node    { return t.rows }
func (t Tree) Launch() string   { return t.launch }
func (t Tree) Sort() SortMode   { return t.sort }
func (t Tree) Hide() HidePolicy { return t.hide }

func (t Tree) Selected() *Node {
	if t.sel < 0 || t.sel >= len(t.rows) {
		return nil
	}
	return t.rows[t.sel]
}

// Visible returns the rows inside the scroll window and the index of the first.
func (t Tree) Visible() ([]*Node, int) {
	end := min(t.top+t.height, len(t.rows))
	if t.top >= end {
		return nil, t.top
	}
	return t.rows[t.top:end], t.top
}

func (t *Tree) SetHeight(h int) {
	t.height = max(h, 0)
	t.ensureVisible()
}

func (t *Tree) ensureVisible() {
	if t.height <= 0 {
		return
	}
	if t.sel < t.top {
		t.top = t.sel
	}
	if t.sel >= t.top+t.height {
		t.top = t.sel - t.height + 1
	}
	t.clampTop()
}

func (t *Tree) clampTop() {
	t.top = min(max(t.top, 0), max(len(t.rows)-t.height, 0))
}

func (t *Tree) Select(i int) {
	if len(t.rows) == 0 {
		return
	}
	t.sel = min(max(i, 0), len(t.rows)-1)
	t.selPath = t.rows[t.sel].Path
	t.ensureVisible()
}

func (t *Tree) Move(d int) { t.Select(t.sel + d) }

// ScrollBy moves the window without moving the selection.
func (t *Tree) ScrollBy(d int) {
	t.top += d
	t.clampTop()
}

func (t *Tree) Expand(n *Node) tea.Cmd {
	if n == nil || !n.IsDir {
		return nil
	}
	n.Expanded = true
	var cmd tea.Cmd
	if !n.Loaded && !n.loading {
		cmd = t.load(n)
	}
	t.refresh()
	return cmd
}

func (t *Tree) Collapse(n *Node) {
	if n == nil || !n.IsDir {
		return
	}
	n.Expanded = false
	if isUnder(t.selPath, n.Path) {
		t.selPath = n.Path
	}
	t.refresh()
}

func (t *Tree) Toggle(n *Node) tea.Cmd {
	if n != nil && n.Expanded {
		t.Collapse(n)
		return nil
	}
	return t.Expand(n)
}

// SelectParent moves the selection to the directory holding the selected node.
func (t *Tree) SelectParent() {
	n := t.Selected()
	if n == nil {
		return
	}
	if i := t.indexOf(filepath.Dir(n.Path)); i >= 0 {
		t.Select(i)
	}
}

// Reveal shows path, expanding (and loading) its ancestors and rerooting when
// it lies outside the root, then selects it even if the hide policy would not.
func (t *Tree) Reveal(path string) {
	if path != t.root.Path && !isUnder(path, t.root.Path) {
		if isUnder(path, t.launch) {
			t.setRoot(t.launch, t.root)
		} else {
			t.setRoot(filepath.Dir(path), nil)
		}
	}
	if path == t.root.Path {
		return
	}
	rel, _ := filepath.Rel(t.root.Path, path)
	parts := strings.Split(rel, string(filepath.Separator))
	n := t.root
	for _, part := range parts[:len(parts)-1] {
		if n = t.freshChild(n, part); n == nil {
			break
		}
		n.Expanded = true
	}
	if n != nil {
		t.freshChild(n, parts[len(parts)-1])
	}
	t.hide.Keep = path
	t.selPath = path
	t.refresh()
}

// freshChild loads dir and returns its child name, re-reading dir once when the
// child appeared after the last listing.
func (t *Tree) freshChild(dir *Node, name string) *Node {
	t.loadSync(dir)
	if c := dir.child(name); c != nil {
		return c
	}
	t.reloadNode(dir)
	return dir.child(name)
}

// setRoot makes path the root, reusing a loaded node for it and grafting keep
// (the old root) back in so its expansion survives.
func (t *Tree) setRoot(path string, keep *Node) {
	t.hide.Keep = ""
	if n := t.find(path); n != nil {
		n.Expanded = true
		t.loadSync(n)
		t.root = n
		return
	}
	root := &Node{Path: path, Name: filepath.Base(path), IsDir: true, Expanded: true}
	t.loadSync(root)
	if keep != nil && isUnder(keep.Path, path) {
		graft(t, root, keep)
	}
	t.root = root
}

func graft(t *Tree, root, keep *Node) {
	rel, _ := filepath.Rel(root.Path, keep.Path)
	parts := strings.Split(rel, string(filepath.Separator))
	n := root
	for _, part := range parts[:len(parts)-1] {
		if n = t.freshChild(n, part); n == nil {
			return
		}
		n.Expanded = true
	}
	t.loadSync(n)
	last := parts[len(parts)-1]
	for i, c := range n.Children {
		if c.Name == last {
			keep.Name = last
			n.Children[i] = keep
			return
		}
	}
}

// RerootUp makes the root's parent the root and selects the old root.
func (t *Tree) RerootUp() {
	parent := filepath.Dir(t.root.Path)
	if parent == t.root.Path {
		return
	}
	old := t.root
	t.setRoot(parent, old)
	t.selPath = old.Path
	t.refresh()
}

// RerootAt makes n (or the directory holding n) the root.
func (t *Tree) RerootAt(n *Node) {
	if n == nil {
		return
	}
	path := n.Path
	if !n.IsDir {
		path = filepath.Dir(path)
	}
	t.setRoot(path, t.root)
	t.refresh()
}

// RerootLaunch returns to the directory deck was started in.
func (t *Tree) RerootLaunch() {
	if t.root.Path == t.launch {
		return
	}
	t.setRoot(t.launch, t.root)
	t.refresh()
}

func (t *Tree) SetSort(mode SortMode) {
	t.sort = mode
	var walk func(n *Node)
	walk = func(n *Node) {
		sortNodes(n.Children, mode)
		for _, c := range n.Children {
			if c.Loaded {
				walk(c)
			}
		}
	}
	walk(t.root)
	t.refresh()
}

func (t *Tree) ToggleDotfiles() {
	t.hide.ShowDot = !t.hide.ShowDot
	t.refresh()
}

func (t *Tree) ToggleIgnored() {
	t.hide.ShowIgnored = !t.hide.ShowIgnored
	t.refresh()
}

func (t *Tree) SetIgnored(ignored map[string]bool) {
	t.hide.Ignored = ignored
	t.refresh()
}

// Reload re-reads every expanded directory in place, keeping node identity,
// expansion and the selection; collapsed directories reload on their next expand.
func (t *Tree) Reload() {
	t.reloadNode(t.root)
	t.refresh()
}

func (t *Tree) reloadNode(n *Node) {
	if !n.Loaded {
		return
	}
	fresh, _, err := readChildren(n.Path, 0, t.sort)
	n.err = err
	if err != nil {
		n.Children = nil
		return
	}
	old := make(map[string]*Node, len(n.Children))
	for _, c := range n.Children {
		old[c.Name] = c
	}
	for i, f := range fresh {
		o, ok := old[f.Name]
		if !ok || o.IsDir != f.IsDir {
			continue
		}
		o.Size, o.ModTime, o.IsSymlink, o.Broken, o.Target = f.Size, f.ModTime, f.IsSymlink, f.Broken, f.Target
		fresh[i] = o
		if o.Expanded {
			t.reloadNode(o)
		} else {
			o.Loaded, o.Children = false, nil
		}
	}
	n.Children = fresh
}

type rowStyle struct {
	width    int
	icons    string
	badge    Badge
	selected bool
	focused  bool
	ignored  bool
}

var (
	rowSelFocused = lipgloss.NewStyle().Bold(true).Foreground(ui.ColorFg).Background(ui.ColorDim)
	rowDir        = lipgloss.NewStyle().Foreground(ui.ColorInfo)
)

// renderRow draws one tree row exactly st.width cells wide, the name truncated
// with "…" so the git badge stays in the last column.
func renderRow(n *Node, st rowStyle) string {
	if st.width <= 0 {
		return ""
	}
	name := n.Name
	if n.Broken {
		name += " → " + n.Target
	}
	if n.loading {
		name += " …"
	}
	text := strings.Repeat("  ", n.Depth) + icon(n, st.icons) + name
	avail := st.width
	if st.badge != BadgeNone && st.width > 2 {
		avail -= 2
	}
	text = ansi.Truncate(text, avail, "…")
	text += strings.Repeat(" ", max(avail-ansi.StringWidth(text), 0))
	var style lipgloss.Style
	switch {
	case st.selected && st.focused:
		style = rowSelFocused
	case st.selected:
		style = ui.Active
	case n.Broken:
		style = ui.Err
	case st.ignored:
		style = ui.Dim
	case n.IsDir:
		style = rowDir
	default:
		style = lipgloss.NewStyle()
	}
	row := style.Render(text)
	if avail < st.width {
		row += " " + st.badge.Render()
	}
	return row
}
