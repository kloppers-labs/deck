package files

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestFormatMeta(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 14, 0, 0, time.UTC)
	cases := []struct {
		name string
		meta Meta
		git  Badge
		want []string
	}{
		{
			"file",
			Meta{Name: "Program.cs", Size: 4300, ModTime: now.Add(-3 * time.Hour), Mode: 0o644, Owner: "gerhard", Lines: 132, Lang: "C#"},
			BadgeModified,
			[]string{"Program.cs", "4.2 KB · 132 lines", "3h ago (2026-11-02 09:14)", "-rw-r--r-- gerhard", "git: modified", "C#"},
		},
		{
			"binary symlink",
			Meta{Name: "tool", IsSymlink: true, Target: "/opt/tool", Size: 2 << 20, ModTime: now.Add(-40 * 24 * time.Hour), Mode: 0o755, Lines: -1, Binary: true},
			BadgeNone,
			[]string{"tool", "→ /opt/tool", "2 MB · binary", "1mo ago (2026-09-23 12:14)", "-rwxr-xr-x"},
		},
		{
			"directory",
			Meta{Name: "src", IsDir: true, Files: 12, Dirs: 3, Size: 1536, ModTime: now.Add(-5 * time.Minute), Mode: os.ModeDir | 0o755, Owner: "gerhard", Lines: -1},
			BadgeAdded,
			[]string{"src/", "12 files · 3 dirs · 1.5 KB", "5m ago (2026-11-02 12:09)", "drwxr-xr-x gerhard", "git: added"},
		},
		{
			"partial directory",
			Meta{Name: "node_modules", IsDir: true, Files: 19990, Dirs: 10, Size: 3 << 30, ModTime: now.Add(-30 * time.Second), Mode: os.ModeDir | 0o755, Lines: -1, Partial: true},
			BadgeNone,
			[]string{"node_modules/", "19990+ files · 10+ dirs · 3 GB+", "just now (2026-11-02 12:13)", "drwxr-xr-x", "partial: walk capped"},
		},
		{
			"csproj",
			Meta{Name: "Api.csproj", Size: 900, ModTime: now.Add(-2 * 24 * time.Hour), Mode: 0o644, Lines: 20, Lang: "XML",
				Extras: []string{"net9.0 · Exe", "Microsoft.NET.Sdk.Web", "5 packages · 2 project refs"}},
			BadgeNone,
			[]string{"Api.csproj", "900 B · 20 lines", "2d ago (2026-10-31 12:14)", "-rw-r--r--", "XML", "net9.0 · Exe", "Microsoft.NET.Sdk.Web", "5 packages · 2 project refs"},
		},
	}
	for _, c := range cases {
		got := stripAll(FormatMeta(c.meta, c.git, 40, now))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	for _, line := range FormatMeta(cases[0].meta, BadgeModified, 8, now) {
		if w := ansi.StringWidth(line); w > 8 {
			t.Errorf("line %q is %d wide, want <= 8", ansi.Strip(line), w)
		}
	}
	narrow := stripAll(FormatMeta(cases[2].meta, BadgeNone, 20, now))
	want := []string{"src/", "12 files", "3 dirs", "1.5 KB", "5m ago", "(2026-11-02 12:09)", "drwxr-xr-x gerhard"}
	if !slices.Equal(narrow, want) {
		t.Errorf("a narrow panel breaks groups onto their own lines:\n got %q\nwant %q", narrow, want)
	}
}

func TestStatPathFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(p, []byte("package a\n\nfunc A() {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := statPath(p, defaultStatLimits, nil)
	if m.Err != nil || m.Lines != 3 || m.Size != 22 || m.Lang != "Go" || m.Name != "a.go" {
		t.Errorf("meta = %+v", m)
	}
	bin := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(bin, []byte{0, 1, 2, 0, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if m := statPath(bin, defaultStatLimits, nil); !m.Binary || m.Lines != -1 {
		t.Errorf("binary meta = %+v", m)
	}
}

func TestStatCmdCapsDirWalk(t *testing.T) {
	dir := t.TempDir()
	for i := range 10 {
		touch(t, dir, fmt.Sprintf("sub/f%02d.txt", i))
	}
	full := statPath(dir, defaultStatLimits, nil)
	if full.Partial || full.Files != 10 || full.Dirs != 1 {
		t.Errorf("uncapped walk = %+v", full)
	}
	byCount := statPath(dir, statLimits{MaxEntries: 5, MaxDur: time.Minute, MaxCountBytes: 1 << 20}, nil)
	if !byCount.Partial || byCount.Files+byCount.Dirs > 5 {
		t.Errorf("walk capped at 5 entries = %+v", byCount)
	}
	byTime := statPath(dir, statLimits{MaxEntries: 100, MaxDur: time.Nanosecond, MaxCountBytes: 1 << 20}, nil)
	if !byTime.Partial {
		t.Errorf("walk capped by time = %+v", byTime)
	}
	msg, ok := StatCmd(dir, 7, nil)().(StatMsg)
	if !ok || msg.Seq != 7 || msg.Meta.Path != dir {
		t.Errorf("StatCmd = %#v", msg)
	}
	if msg.RouteTo() != ui.DestFiles {
		t.Errorf("StatMsg routes to %v, want DestFiles", msg.RouteTo())
	}
}

func TestMetaCacheKeyedByMtime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newMetaCache()
	if m := statPath(p, defaultStatLimits, c); m.Lines != 1 {
		t.Fatalf("lines = %d", m.Lines)
	}
	if err := os.WriteFile(p, []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if m := statPath(p, defaultStatLimits, c); m.Lines != 3 {
		t.Errorf("a changed mtime must miss the cache, lines = %d", m.Lines)
	}
}

func hoverAt(m Model, row int) tea.MouseMsg {
	r := m.layout().TreeInner()
	return tea.MouseMsg{X: r.X + 2, Y: r.Y + 1 + row, Action: tea.MouseActionMotion}
}

func dueMsgs(msgs []tea.Msg) []hoverDueMsg {
	var out []hoverDueMsg
	for _, msg := range msgs {
		if d, ok := msg.(hoverDueMsg); ok {
			out = append(out, d)
		}
	}
	return out
}

func TestHoverTooltipAfterDwell(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3, "b.go": 7})
	m := started(t, dir, 120, 30)
	m, cmd := m.Update(hoverAt(m, 1))
	if cmd == nil || m.hover.path != filepath.Join(dir, "b.go") {
		t.Fatalf("motion over a row should start the dwell timer, hover = %+v", m.hover)
	}
	if m.tooltip() != "" {
		t.Fatal("no tooltip before the dwell")
	}
	seq := m.hover.seq
	m, cmd = m.Update(hoverDueMsg{seq: seq})
	m, _ = settle(t, m, cmd)
	tip := ansi.Strip(m.tooltip())
	if !strings.Contains(tip, "b.go") || !strings.Contains(tip, "7 lines") {
		t.Fatalf("tooltip after the dwell = %q", tip)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "7 lines") {
		t.Errorf("the tooltip is composed over the view:\n%s", v)
	}

	m, _ = m.Update(hoverAt(m, 0))
	if m.tooltip() != "" || m.hover.seq == seq {
		t.Error("moving to another row hides the tooltip and invalidates the seq")
	}
	m, cmd = m.Update(hoverDueMsg{seq: seq})
	m, _ = settle(t, m, cmd)
	if m.tooltip() != "" {
		t.Error("a stale dwell must not show a tooltip")
	}

	m, _ = m.Update(tea.MouseMsg{X: m.layout().ViewerInner().X + 3, Y: 4, Action: tea.MouseActionMotion})
	if m.hover.path != "" {
		t.Error("leaving the tree clears the hover")
	}
}

func TestInfoPanelToggleI(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3, "b.go": 9})
	m := started(t, dir, 140, 30)
	if m.layout().ShowInfo {
		t.Fatal("the info panel is off by default")
	}
	m, _ = press(t, m, "i")
	if !m.layout().ShowInfo {
		t.Fatal("i opens the info panel")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "3 lines") {
		t.Errorf("the panel shows the selected file's metadata:\n%s", v)
	}
	m, _ = press(t, m, "j")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "9 lines") {
		t.Errorf("the panel follows the selection:\n%s", v)
	}
	m, _ = press(t, m, "enter", "i")
	if m.layout().ShowInfo {
		t.Error("i in the viewer closes the panel too")
	}
}
