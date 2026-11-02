package editor

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"deck/internal/config"
	"deck/internal/lsp"
)

// MissingServer is an enabled language server whose binary isn't installed.
type MissingServer struct {
	Server string
	Binary string
	Hint   string
}

type microServer struct {
	Name         string
	Cmd          string
	Args         []string
	Filetypes    []string
	TSServer     string
	RequireLocal string
}

var microFiletypes = map[string][]string{
	"csharp":     {"csharp"},
	"go":         {"go", "gomod"},
	"typescript": {"typescript", "javascript"},
	"python":     {"python"},
	"rust":       {"rust"},
	"bash":       {"shell"},
	"lua":        {"lua"},
	"yaml":       {"yaml"},
	"json":       {"json"},
}

var zDeckTemplate = template.Must(template.New("z_deck").Funcs(template.FuncMap{
	"lua":     luaString,
	"luaList": luaList,
}).ParseFS(runtimeFS, "runtime/plug/mlsp/z_deck.lua.tmpl")).Lookup("z_deck.lua.tmpl")

// SpecsFromConfig merges the user's [lsp.servers] over deck's built-in servers.
func SpecsFromConfig(c config.LSP) []lsp.ServerSpec {
	overrides := map[string]lsp.ServerOverride{}
	for name, s := range c.Servers {
		overrides[name] = lsp.ServerOverride{
			Command:     s.Command,
			RootMarkers: s.RootMarkers,
			Solution:    s.Solution,
			Enabled:     s.Enabled,
		}
	}
	return lsp.MergeSpecs(lsp.DefaultSpecs(), overrides)
}

// RenderZDeck renders mlsp's z_deck.lua for the installed servers among specs,
// and lists the enabled ones that are missing.
func RenderZDeck(specs []lsp.ServerSpec, lookup func(string) (string, error)) ([]byte, []MissingServer) {
	var servers []microServer
	var missing []MissingServer
	for _, s := range specs {
		if !s.Enabled || len(s.Command) == 0 {
			continue
		}
		bin, err := lookup(s.Command[0])
		if err != nil {
			missing = append(missing, MissingServer{Server: s.Name, Binary: filepath.Base(s.Command[0]), Hint: s.InstallHint()})
			continue
		}
		args := append([]string(nil), s.Command[1:]...)
		if s.Solution != "" && s.SolutionFlag != "" {
			sln, err := filepath.Abs(s.Solution)
			if err == nil {
				args = append(args, s.SolutionFlag, sln)
			}
		}
		fts, ok := microFiletypes[s.Name]
		if !ok {
			fts = []string{s.Name}
		}
		ms := microServer{Name: s.Name, Cmd: bin, Args: args, Filetypes: fts}
		if s.NeedsTypeScript {
			if ts, _ := lsp.FindTSServer("", bin); ts != "" {
				ms.TSServer = ts
			} else {
				ms.RequireLocal = "node_modules/typescript/lib/tsserver.js"
				missing = append(missing, MissingServer{Server: s.Name, Binary: "typescript", Hint: lsp.TSInstallHint})
			}
		}
		servers = append(servers, ms)
	}
	var buf bytes.Buffer
	if err := zDeckTemplate.Execute(&buf, servers); err != nil {
		panic(err)
	}
	return buf.Bytes(), missing
}

func luaString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\%03d`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func luaList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = luaString(s)
	}
	return strings.Join(quoted, ", ")
}
