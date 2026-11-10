// Package httpc is deck's "HTTP" tab: a REST client with normal and insert
// modes, a scrollable response with headers, and requests from .http files.
package httpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/kloppers-labs/deck/internal/ui"
)

// methods are the HTTP verbs offered by the method picker.
var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// parseHeaders turns "Key: Value" lines into an http.Header. Blank lines are
// skipped; a non-blank line without a colon is a user error worth surfacing.
func parseHeaders(text string) (http.Header, error) {
	h := http.Header{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("header line %d: expected \"Key: Value\", got %q", i+1, line)
		}
		k = strings.TrimSpace(k)
		if k == "" {
			return nil, fmt.Errorf("header line %d: empty header name", i+1)
		}
		h.Add(k, strings.TrimSpace(v))
	}
	return h, nil
}

// buildRequest defaults the scheme to http:// for loopback hosts (https://
// otherwise) and adds a JSON Content-Type when missing, listing it in auto.
func buildRequest(method, rawURL, headersText, body string) (req *http.Request, auto []string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil, errors.New("URL is empty")
	}
	if !strings.Contains(rawURL, "://") {
		scheme := "https://"
		if isLoopback(rawURL) {
			scheme = "http://"
		}
		rawURL = scheme + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if u.Host == "" {
		return nil, nil, fmt.Errorf("missing host in %q", rawURL)
	}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err = http.NewRequest(method, u.String(), rdr)
	if err != nil {
		return nil, nil, err
	}
	hdrs, err := parseHeaders(headersText)
	if err != nil {
		return nil, nil, err
	}
	for k, vs := range hdrs {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	trimmed := strings.TrimSpace(body)
	if req.Header.Get("Content-Type") == "" && trimmed != "" && json.Valid([]byte(trimmed)) {
		req.Header.Set("Content-Type", "application/json")
		auto = append(auto, "Content-Type: application/json")
	}
	return req, auto, nil
}

// isLoopback reports whether a scheme-less URL's host is localhost, 127.0.0.1
// or ::1.
func isLoopback(raw string) bool {
	host, _, _ := strings.Cut(raw, "/")
	if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end > 0 {
			host = host[1:end]
		}
	} else if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// formatHeaders renders response headers sorted by name, one value per line.
func formatHeaders(h http.Header) string {
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(h)) {
		for _, v := range h[k] {
			lines = append(lines, ui.Active.Render(k)+": "+v)
		}
	}
	return strings.Join(lines, "\n")
}

// prettyJSON indents JSON for readability, returning the input unchanged if it
// doesn't parse (so non-JSON or truncated bodies still display).
func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// highlightBody syntax-colours a response body for terminal display, picking a
// lexer from the Content-Type (pretty-printing JSON) and falling back to content
// analysis. Empty bodies render as empty.
func highlightBody(contentType, body, theme string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	if ui.IsBinary([]byte(body)) {
		return ui.Muted.Render("⬡ binary response — preview not available")
	}
	ct := strings.ToLower(contentType)
	var lexer chroma.Lexer
	switch {
	case strings.Contains(ct, "json"):
		body = prettyJSON(body)
		lexer = lexers.Get("json")
	case strings.Contains(ct, "html"):
		lexer = lexers.Get("html")
	case strings.Contains(ct, "xml"):
		lexer = lexers.Get("xml")
	}
	if lexer == nil {
		lexer = lexers.Analyse(body)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return body
	}
	it, err := lexer.Tokenise(nil, body)
	if err != nil {
		return body
	}
	style := styles.Get(theme)
	if style == nil {
		style = styles.Fallback
	}
	var buf strings.Builder
	if err := formatter.Format(&buf, style, it); err != nil {
		return body
	}
	return buf.String()
}
