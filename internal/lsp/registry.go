package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type ServerSpec struct {
	Name            string
	Command         []string
	Extensions      []string
	RootMarkers     []string
	PrestartMarkers []string
	SolutionFlag    string
	Solution        string
	Enabled         bool
	InstallHint     string
	NeedsTypeScript bool
	Settings        map[string]any
}

type ServerOverride struct {
	Command     []string
	Extensions  []string
	RootMarkers []string
	Solution    string
	Enabled     *bool
}

type Config struct {
	Enabled        bool
	Prestart       bool
	RequestTimeout time.Duration
	Servers        map[string]ServerOverride
}

func DefaultConfig() Config {
	return Config{Enabled: true, Prestart: true, RequestTimeout: 8 * time.Second}
}

func DefaultSpecs() []ServerSpec {
	return []ServerSpec{
		{
			Name:            "csharp",
			Command:         []string{"csharp-ls", "--features", "metadata-uris"},
			Extensions:      []string{".cs", ".csx"},
			RootMarkers:     []string{"*.sln", "*.slnx", "*.csproj"},
			PrestartMarkers: []string{"*.sln", "*.slnx"},
			SolutionFlag:    "--solution",
			Enabled:         true,
			InstallHint:     "dotnet tool install --global csharp-ls",
		},
		{
			Name:        "go",
			Command:     []string{"gopls"},
			Extensions:  []string{".go"},
			RootMarkers: []string{"go.work", "go.mod"},
			Enabled:     true,
			InstallHint: "go install golang.org/x/tools/gopls@latest",
		},
		{
			Name:            "typescript",
			Command:         []string{"typescript-language-server", "--stdio"},
			Extensions:      []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
			RootMarkers:     []string{"tsconfig.json", "jsconfig.json", "package.json"},
			Enabled:         true,
			InstallHint:     "npm install --global typescript-language-server typescript",
			NeedsTypeScript: true,
		},
	}
}

// MergeSpecs applies non-empty override fields over the defaults; unknown names add new servers.
func MergeSpecs(defaults []ServerSpec, overrides map[string]ServerOverride) []ServerSpec {
	out := make([]ServerSpec, 0, len(defaults)+len(overrides))
	known := map[string]bool{}
	for _, d := range defaults {
		known[d.Name] = true
		if o, ok := overrides[d.Name]; ok {
			d = applyOverride(d, o)
		}
		out = append(out, d)
	}
	var added []string
	for name := range overrides {
		if !known[name] {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	for _, name := range added {
		out = append(out, applyOverride(ServerSpec{Name: name, Enabled: true}, overrides[name]))
	}
	return out
}

func applyOverride(s ServerSpec, o ServerOverride) ServerSpec {
	if len(o.Command) > 0 {
		s.Command = slices.Clone(o.Command)
	}
	if len(o.Extensions) > 0 {
		s.Extensions = slices.Clone(o.Extensions)
	}
	if len(o.RootMarkers) > 0 {
		s.RootMarkers = slices.Clone(o.RootMarkers)
	}
	if o.Solution != "" {
		s.Solution = o.Solution
	}
	if o.Enabled != nil {
		s.Enabled = *o.Enabled
	}
	return s
}

func LanguageID(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".cs", ".csx":
		return "csharp"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	}
	return strings.TrimPrefix(ext, ".")
}

type Env struct {
	LookPath func(string) (string, error)
	Home     string
	GOPATH   string
	IsExec   func(string) bool
}

func DefaultEnv() Env {
	home, _ := os.UserHomeDir()
	gopath, _, _ := strings.Cut(os.Getenv("GOPATH"), string(os.PathListSeparator))
	return Env{LookPath: exec.LookPath, Home: home, GOPATH: gopath, IsExec: isExecutable}
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

type NoServerError struct{ Ext string }

func (e *NoServerError) Error() string { return "no language server for " + e.Ext }

type MissingBinaryError struct {
	Server string
	Binary string
	Hint   string
}

func (e *MissingBinaryError) Error() string { return e.Binary + " not found" }

type Resolved struct {
	Spec        ServerSpec
	Root        string
	Binary      string
	Args        []string
	LanguageID  string
	InitOptions any
}

type Registry struct {
	specs []ServerSpec
	env   Env

	mu       sync.Mutex
	binaries map[string]binaryResult
}

type binaryResult struct {
	path string
	err  error
}

func NewRegistry(specs []ServerSpec, env Env) *Registry {
	if env.LookPath == nil {
		env.LookPath = exec.LookPath
	}
	if env.IsExec == nil {
		env.IsExec = isExecutable
	}
	return &Registry{specs: specs, env: env, binaries: map[string]binaryResult{}}
}

func (r *Registry) Specs() []ServerSpec { return slices.Clone(r.specs) }

func (r *Registry) SpecFor(path string) (ServerSpec, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, s := range r.specs {
		if s.Enabled && len(s.Command) > 0 && slices.Contains(s.Extensions, ext) {
			return s, true
		}
	}
	return ServerSpec{}, false
}

// LookupBinary searches PATH, then the usual per-user tool directories; results are cached.
func (r *Registry) LookupBinary(name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res, ok := r.binaries[name]; ok {
		return res.path, res.err
	}
	path, err := r.lookup(name)
	r.binaries[name] = binaryResult{path, err}
	return path, err
}

func (r *Registry) lookup(name string) (string, error) {
	if strings.ContainsRune(name, filepath.Separator) {
		if r.env.IsExec(name) {
			return name, nil
		}
		return "", exec.ErrNotFound
	}
	if p, err := r.env.LookPath(name); err == nil {
		return p, nil
	}
	var dirs []string
	if r.env.Home != "" {
		dirs = append(dirs, filepath.Join(r.env.Home, ".dotnet", "tools"), filepath.Join(r.env.Home, "go", "bin"))
	}
	if r.env.GOPATH != "" {
		dirs = append(dirs, filepath.Join(r.env.GOPATH, "bin"))
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	for _, d := range dirs {
		if p := filepath.Join(d, name); r.env.IsExec(p) {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// Boundary is the git top level above path, else $HOME, else the top directory below /.
func (r *Registry) Boundary(path string) string {
	return r.boundaryFrom(filepath.Dir(path))
}

func (r *Registry) boundaryFrom(dir string) string {
	last := dir
	for dir != "/" && dir != filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if dir == r.env.Home {
			return dir
		}
		last = dir
		dir = filepath.Dir(dir)
	}
	return last
}

func (r *Registry) Resolve(path string) (Resolved, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Resolved{}, err
	}
	spec, ok := r.SpecFor(abs)
	if !ok {
		return Resolved{}, &NoServerError{Ext: filepath.Ext(abs)}
	}
	res, err := r.resolveIn(spec, filepath.Dir(abs))
	res.LanguageID = LanguageID(abs)
	return res, err
}

func (r *Registry) resolveIn(spec ServerSpec, dir string) (Resolved, error) {
	res := Resolved{Spec: spec, Root: dir}
	bin, err := r.LookupBinary(spec.Command[0])
	if err != nil {
		return res, &MissingBinaryError{Server: spec.Name, Binary: filepath.Base(spec.Command[0]), Hint: spec.InstallHint}
	}
	res.Binary = bin
	var match string
	if spec.Solution != "" {
		match, _ = filepath.Abs(spec.Solution)
		res.Root = filepath.Dir(match)
	} else if root, m, ok := findRootFrom(dir, spec.RootMarkers, r.boundaryFrom(dir)); ok {
		res.Root, match = root, m
	}
	res.Args = slices.Clone(spec.Command[1:])
	if spec.SolutionFlag != "" && isSolution(match) {
		res.Args = append(res.Args, spec.SolutionFlag, match)
	}
	if spec.NeedsTypeScript {
		ts, local := FindTSServer(res.Root, bin)
		if ts == "" {
			return res, &MissingBinaryError{Server: spec.Name, Binary: "typescript", Hint: TSInstallHint}
		}
		if !local {
			res.InitOptions = map[string]any{"tsserver": map[string]any{"fallbackPath": ts}}
		}
	}
	return res, nil
}

func isSolution(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".sln" || ext == ".slnx"
}

// FindRoot walks each marker in priority order up from file to boundary; the first hit wins.
func FindRoot(file string, markers []string, boundary string) (root, match string, ok bool) {
	return findRootFrom(filepath.Dir(file), markers, boundary)
}

func findRootFrom(start string, markers []string, boundary string) (string, string, bool) {
	for _, marker := range markers {
		for dir := start; dir != "/" && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if hit := matchMarker(dir, marker); hit != "" {
				return dir, hit, true
			}
			if dir == boundary {
				break
			}
		}
	}
	return "", "", false
}

func matchMarker(dir, marker string) string {
	if !strings.ContainsAny(marker, "*?[") {
		p := filepath.Join(dir, marker)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var hits []string
	for _, e := range entries {
		if ok, _ := filepath.Match(marker, e.Name()); ok && !e.IsDir() {
			hits = append(hits, e.Name())
		}
	}
	if len(hits) == 0 {
		return ""
	}
	base := filepath.Base(dir)
	for _, h := range hits {
		if strings.TrimSuffix(h, filepath.Ext(h)) == base {
			return filepath.Join(dir, h)
		}
	}
	return filepath.Join(dir, hits[0])
}
