package viewer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Navigator is the viewer's view of a language server. Positions are 0-based
// lines and rune columns; a nil Navigator means there is no LSP.
type Navigator interface {
	Hover(ctx context.Context, path string, at Loc) (Hover, error)
	Definition(ctx context.Context, path string, at Loc) ([]Location, error)
	TypeDefinition(ctx context.Context, path string, at Loc) ([]Location, error)
	Implementation(ctx context.Context, path string, at Loc) ([]Location, error)
	References(ctx context.Context, path string, at Loc) ([]Location, error)
	DocumentSymbols(ctx context.Context, path string) ([]Symbol, error)
	WorkspaceSymbols(ctx context.Context, path, query string) ([]Symbol, error)
	Document(ctx context.Context, uri string) (VirtualDoc, error)
	Status(path string) ServerStatus
}

// Range is a half-open span of document positions.
type Range struct{ Start, End Pos }

func (r Range) contains(p Pos) bool { return !p.before(r.Start) && !r.End.before(p) }

type Hover struct {
	Markdown string
	Range    *Range
}

// Location is a navigation target. Path is a file path, or a URI such as
// csharp:/metadata/... for a virtual document; Text is the trimmed target line.
type Location struct {
	Path  string
	Range Range
	Text  string
}

type SymbolKind int

const (
	SymbolFile SymbolKind = iota + 1
	SymbolModule
	SymbolNamespace
	SymbolPackage
	SymbolClass
	SymbolMethod
	SymbolProperty
	SymbolField
	SymbolConstructor
	SymbolEnum
	SymbolInterface
	SymbolFunction
	SymbolVariable
	SymbolConstant
	SymbolString
	SymbolNumber
	SymbolBoolean
	SymbolArray
	SymbolObject
	SymbolKey
	SymbolNull
	SymbolEnumMember
	SymbolStruct
	SymbolEvent
	SymbolOperator
	SymbolTypeParameter
	SymbolHeading SymbolKind = 100
)

// Symbol is a document or workspace symbol, flattened in DFS order; Parent
// indexes the same slice and is -1 at the top level.
type Symbol struct {
	Name, Detail, Container string
	Kind                    SymbolKind
	Path                    string
	Range, Selection        Range
	Depth, Parent           int
}

// VirtualDoc is a read-only document served by the language server.
type VirtualDoc struct{ Title, Text string }

type ServerState int

const (
	ServerNone ServerState = iota
	ServerIdle
	ServerStarting
	ServerIndexing
	ServerReady
	ServerExited
	ServerFailed
)

// ServerStatus describes the language server for a file. Label is the short
// language name ("C#"), Project the root's base name and Percent -1 when unknown.
type ServerStatus struct {
	Server, Label, Language, Ext, Project string
	State                                 ServerState
	Title, Message                        string
	Percent                               int
	Since                                 time.Time
	Err, Hint                             string
	Install                               []string
}

// Usable reports whether requests can be sent; an idle server starts on the first one.
func (s ServerStatus) Usable() bool { return s.State != ServerNone && s.State != ServerFailed }

func (s ServerStatus) Loading() bool {
	return s.State == ServerIdle || s.State == ServerStarting || s.State == ServerIndexing
}

func (s ServerStatus) NoServerText() string {
	t := "no language server for " + s.Ext
	if s.Err != "" {
		t += " — " + s.Err
	}
	return t
}

func (s ServerStatus) LoadingText(now time.Time) string {
	what := s.Project
	if what == "" {
		what = "the workspace"
	}
	secs := 0
	if !s.Since.IsZero() {
		secs = int(now.Sub(s.Since).Seconds())
	}
	return fmt.Sprintf("%s: still loading %s (%ds) — results may be incomplete", s.Label, what, secs)
}

// IsVirtual reports a URI with a non-file scheme, such as csharp:/metadata/...
func IsVirtual(path string) bool {
	i := strings.IndexByte(path, ':')
	if i < 2 {
		return false
	}
	for _, c := range path[:i] {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '+' || c == '-' || c == '.') {
			return false
		}
	}
	return path[:i] != "file"
}

func extOf(path string) string {
	if e := filepath.Ext(path); e != "" {
		return e
	}
	return filepath.Base(path)
}
