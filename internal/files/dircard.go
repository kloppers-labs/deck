package files

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/viewer"
)

const (
	cardEntries   = 200
	cardNameWidth = 40
	labelWidth    = 17
	slnFolderType = "2150E333-8FDC-42A3-9474-1A3956D46DE8"
)

// Csproj is the summary of an SDK-style project file shown on project cards.
type Csproj struct {
	Sdk, TargetFramework, OutputType string
	Packages, ProjectRefs            int
}

type xmlInclude struct {
	Include string `xml:"Include,attr"`
}

type xmlProject struct {
	Sdk            string `xml:"Sdk,attr"`
	PropertyGroups []struct {
		TargetFramework  string `xml:"TargetFramework"`
		TargetFrameworks string `xml:"TargetFrameworks"`
		OutputType       string `xml:"OutputType"`
	} `xml:"PropertyGroup"`
	ItemGroups []struct {
		Packages []xmlInclude `xml:"PackageReference"`
		Projects []xmlInclude `xml:"ProjectReference"`
	} `xml:"ItemGroup"`
}

func ParseCsproj(data []byte) (Csproj, error) {
	var x xmlProject
	if err := xml.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &x); err != nil {
		return Csproj{}, err
	}
	p := Csproj{Sdk: x.Sdk}
	for _, g := range x.PropertyGroups {
		if p.TargetFramework == "" {
			p.TargetFramework = cmpOr(g.TargetFramework, g.TargetFrameworks)
		}
		if p.OutputType == "" {
			p.OutputType = g.OutputType
		}
	}
	for _, g := range x.ItemGroups {
		p.Packages += len(g.Packages)
		p.ProjectRefs += len(g.Projects)
	}
	return p, nil
}

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func (p Csproj) summary() []string {
	var out []string
	if head := strings.Join(nonEmpty(p.TargetFramework, p.OutputType), " · "); head != "" {
		out = append(out, head)
	}
	if p.Sdk != "" {
		out = append(out, p.Sdk)
	}
	return append(out, plural(p.Packages, "package")+" · "+plural(p.ProjectRefs, "project ref"))
}

func nonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// SlnProject is one project listed by a solution file, Path using forward slashes.
type SlnProject struct{ Name, Path string }

var slnProjectLine = regexp.MustCompile(`(?m)^Project\("\{([^}]+)\}"\)\s*=\s*"([^"]+)"\s*,\s*"([^"]+)"`)

// ParseSln lists the projects of a classic .sln, skipping solution folders.
func ParseSln(data []byte) []SlnProject {
	var out []SlnProject
	for _, m := range slnProjectLine.FindAllSubmatch(data, -1) {
		if strings.EqualFold(string(m[1]), slnFolderType) {
			continue
		}
		out = append(out, SlnProject{Name: string(m[2]), Path: strings.ReplaceAll(string(m[3]), `\`, "/")})
	}
	return out
}

// ParseSlnx lists the projects of an XML .slnx solution, folders included.
func ParseSlnx(data []byte) []SlnProject {
	var out []SlnProject
	dec := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Project" {
			continue
		}
		for _, a := range se.Attr {
			if a.Name.Local == "Path" {
				p := strings.ReplaceAll(a.Value, `\`, "/")
				out = append(out, SlnProject{Name: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), Path: p})
			}
		}
	}
}

type cardEntry struct {
	Name  string
	IsDir bool
	Size  int64
	Badge Badge
}

type projectCard struct {
	File string
	Csproj
}

type solutionCard struct {
	File     string
	Projects []SlnProject
}

type dirCard struct {
	Name         string
	Entries      []cardEntry
	Dirs, Files  int
	Hidden, More int
	Projects     []projectCard
	Solutions    []solutionCard
	Err          error
}

// readDirCard gathers what a directory card shows: up to limit visible
// children with their badges, plus any project and solution files in it.
func readDirCard(path string, hide HidePolicy, badges map[string]Badge, mode SortMode, limit int) dirCard {
	c := dirCard{Name: filepath.Base(path)}
	nodes, _, err := readChildren(path, 0, mode)
	if err != nil {
		c.Err = err
		return c
	}
	for _, n := range nodes {
		if hide.Hidden(n) {
			c.Hidden++
			continue
		}
		if n.IsDir {
			c.Dirs++
		} else {
			c.Files++
			c.addProject(n)
		}
		if len(c.Entries) >= limit {
			c.More++
			continue
		}
		c.Entries = append(c.Entries, cardEntry{Name: n.Name, IsDir: n.IsDir, Size: n.Size, Badge: badges[n.Path]})
	}
	return c
}

func (c *dirCard) addProject(n *Node) {
	ext := strings.ToLower(filepath.Ext(n.Name))
	switch ext {
	case ".csproj", ".fsproj", ".vbproj", ".sln", ".slnx":
	default:
		return
	}
	data, err := os.ReadFile(n.Path)
	if err != nil {
		return
	}
	switch ext {
	case ".sln":
		c.Solutions = append(c.Solutions, solutionCard{File: n.Name, Projects: ParseSln(data)})
	case ".slnx":
		c.Solutions = append(c.Solutions, solutionCard{File: n.Name, Projects: ParseSlnx(data)})
	default:
		if p, err := ParseCsproj(data); err == nil {
			c.Projects = append(c.Projects, projectCard{File: n.Name, Csproj: p})
		}
	}
}

type seg struct {
	style lipgloss.Style
	text  string
}

type cardWriter struct{ plain, styled []string }

func (w *cardWriter) add(segs ...seg) {
	var p, s strings.Builder
	for _, g := range segs {
		p.WriteString(g.text)
		s.WriteString(g.style.Render(g.text))
	}
	w.plain = append(w.plain, p.String())
	w.styled = append(w.styled, s.String())
}

var (
	plainStyle = lipgloss.NewStyle()
	cardTitle  = lipgloss.NewStyle().Bold(true).Foreground(ui.ColorAccent)
)

func (c dirCard) lines() (plain, styled []string) {
	var w cardWriter
	w.add(seg{cardTitle, c.Name + "/"})
	if c.Err != nil {
		w.add(seg{ui.Err, loadError(c.Err)})
		return w.plain, w.styled
	}
	counts := plural(c.Dirs, "dir") + " · " + plural(c.Files, "file")
	if c.Hidden > 0 {
		counts += fmt.Sprintf(" · %d hidden", c.Hidden)
	}
	w.add(seg{ui.Muted, counts})
	for _, p := range c.Projects {
		w.add()
		w.add(seg{cardTitle, "Project  " + p.File})
		for _, kv := range [][2]string{
			{"Sdk", p.Sdk}, {"TargetFramework", p.TargetFramework}, {"OutputType", p.OutputType},
			{"Packages", fmt.Sprint(p.Packages)}, {"Project refs", fmt.Sprint(p.ProjectRefs)},
		} {
			if kv[1] != "" {
				w.add(seg{ui.Muted, fmt.Sprintf("  %-*s", labelWidth, kv[0])}, seg{plainStyle, kv[1]})
			}
		}
	}
	for _, s := range c.Solutions {
		w.add()
		w.add(seg{cardTitle, "Solution  " + s.File + " · " + plural(len(s.Projects), "project")})
		nameW := 0
		for _, p := range s.Projects {
			nameW = max(nameW, ansi.StringWidth(p.Name))
		}
		for _, p := range s.Projects {
			w.add(seg{plainStyle, fmt.Sprintf("  %-*s", nameW+2, p.Name)}, seg{ui.Muted, p.Path})
		}
	}
	if len(c.Entries) > 0 {
		w.add()
	}
	nameW := 0
	for _, e := range c.Entries {
		nameW = max(nameW, min(ansi.StringWidth(e.Name)+1, cardNameWidth))
	}
	for _, e := range c.Entries {
		w.add(entrySegs(e, nameW)...)
	}
	if c.More > 0 {
		w.add(seg{ui.Muted, fmt.Sprintf("… and %d more", c.More)})
	}
	return w.plain, w.styled
}

func entrySegs(e cardEntry, nameW int) []seg {
	letter := " "
	if e.Badge != BadgeNone {
		letter = e.Badge.Letter()
	}
	badge := seg{badgeStyle(e.Badge), letter + "  "}
	name := ui.FitWidth(e.Name, cardNameWidth-1, "…")
	if e.IsDir {
		return []seg{badge, {rowDir, name + "/"}}
	}
	name += strings.Repeat(" ", max(nameW-ansi.StringWidth(name), 0))
	return []seg{badge, {plainStyle, name}, {ui.Muted, "  " + humanBytes(e.Size)}}
}

// dirCardDoc builds a directory card as a viewer document, off the update loop.
func dirCardDoc(path string, hide HidePolicy, badges map[string]Badge, mode SortMode) func() (*viewer.Document, error) {
	return func() (*viewer.Document, error) {
		plain, styled := readDirCard(path, hide, badges, mode, cardEntries).lines()
		return viewer.NewVirtualDocument(path, plain, styled), nil
	}
}
