package lsp

import (
	"net/url"
	"path/filepath"
	"strings"
)

const fileScheme = "file://"

// PathToURI turns an absolute path into a file URI; virtual URIs pass through unchanged.
func PathToURI(path string) string {
	if IsVirtual(path) || strings.HasPrefix(path, fileScheme) {
		return path
	}
	path = filepath.ToSlash(path)
	var b strings.Builder
	b.WriteString(fileScheme)
	if !strings.HasPrefix(path, "/") {
		b.WriteByte('/')
	}
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(path); i++ {
		c := path[i]
		if keepInPath(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

func keepInPath(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		c == '-' || c == '.' || c == '_' || c == '~' || c == '/'
}

// URIToPath returns the local path of a file URI; any other URI comes back unchanged with ok=false.
func URIToPath(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, fileScheme)
	if !ok {
		return uri, false
	}
	if i := strings.IndexByte(rest, '/'); i > 0 {
		rest = rest[i:]
	}
	if p, err := url.PathUnescape(rest); err == nil {
		rest = p
	}
	return filepath.FromSlash(rest), true
}

// IsVirtual reports a URI with a non-file scheme, such as csharp:/metadata/...
func IsVirtual(s string) bool {
	i := strings.IndexByte(s, ':')
	if i < 2 {
		return false
	}
	for _, c := range s[:i] {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '+' || c == '-' || c == '.') {
			return false
		}
	}
	return s[:i] != "file"
}
