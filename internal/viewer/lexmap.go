package viewer

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

var extLexers = map[string]string{
	".props": "XML", ".targets": "XML", ".resx": "XML", ".xaml": "XML", ".slnx": "XML",
	".nuspec": "XML", ".config": "XML", ".csproj": "XML", ".fsproj": "XML", ".vbproj": "XML",
	".razor": "HTML", ".cshtml": "HTML",
	".sln": "INI", ".editorconfig": "INI",
	".http": "plaintext", ".rest": "plaintext",
}

// LexerFor picks the chroma lexer for path: the .NET-aware extension map, then
// chroma's filename match, then content analysis for extensionless files.
func LexerFor(path, content string) chroma.Lexer {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	if name, ok := extLexers[ext]; ok {
		if l := lexers.Get(name); l != nil {
			return l
		}
	}
	if l := lexers.Match(base); l != nil {
		return l
	}
	if ext == "" {
		if l := lexers.Analyse(content); l != nil {
			return l
		}
	}
	return lexers.Fallback
}

func isMarkdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}
