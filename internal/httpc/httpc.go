// Package httpc is deck's "HTTP" tab: a minimal REST client. The user edits a
// method, URL, headers, and body, fires the request via net/http, and sees the
// status, latency, and a syntax-highlighted response body. This file holds the
// pure, side-effect-free helpers (request building, header parsing, response
// highlighting) so they can be unit-tested without a Bubble Tea program.
package httpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"deck/internal/ui"
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

// buildRequest assembles an *http.Request from the pane's fields. A URL without
// a scheme is assumed https so the user can type "api.example.com/x".
func buildRequest(method, rawURL, headersText, body string) (*http.Request, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("URL is empty")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing host in %q", rawURL)
	}

	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, u.String(), rdr)
	if err != nil {
		return nil, err
	}
	hdrs, err := parseHeaders(headersText)
	if err != nil {
		return nil, err
	}
	for k, vs := range hdrs {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return req, nil
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
