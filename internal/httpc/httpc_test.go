package httpc

import (
	"io"
	"strings"
	"testing"
)

func TestParseHeaders(t *testing.T) {
	h, err := parseHeaders("Content-Type: application/json\n\nX-Token:  abc123  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := h.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := h.Get("X-Token"); got != "abc123" {
		t.Errorf("X-Token = %q, want trimmed abc123", got)
	}
}

func TestParseHeadersRejectsMissingColon(t *testing.T) {
	if _, err := parseHeaders("Authorization Bearer xyz"); err == nil {
		t.Error("expected error for a header line without a colon")
	}
}

func TestBuildRequestDefaultsScheme(t *testing.T) {
	req, err := buildRequest("GET", "api.example.com/v1/cars", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.URL.Scheme != "https" {
		t.Errorf("scheme = %q, want https", req.URL.Scheme)
	}
	if req.URL.Host != "api.example.com" || req.URL.Path != "/v1/cars" {
		t.Errorf("host/path = %q %q, want api.example.com /v1/cars", req.URL.Host, req.URL.Path)
	}
}

func TestBuildRequestEmptyURL(t *testing.T) {
	if _, err := buildRequest("GET", "   ", "", ""); err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestBuildRequestBodyAndHeaders(t *testing.T) {
	req, err := buildRequest("POST", "https://x.test/p", "Content-Type: application/json", `{"a":1}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header = %q, want application/json", got)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != `{"a":1}` {
		t.Errorf("body = %q, want {\"a\":1}", string(body))
	}
}

func TestPrettyJSON(t *testing.T) {
	out := prettyJSON(`{"a":1,"b":2}`)
	if !strings.Contains(out, "\n") || !strings.Contains(out, "  ") {
		t.Errorf("expected indented JSON, got %q", out)
	}
	// Invalid JSON passes through untouched.
	if got := prettyJSON("not json"); got != "not json" {
		t.Errorf("invalid JSON should pass through, got %q", got)
	}
}

func TestHighlightBodyEmpty(t *testing.T) {
	if got := highlightBody("application/json", "   ", "dracula"); got != "" {
		t.Errorf("blank body should highlight to empty, got %q", got)
	}
}

func TestHighlightBodyJSON(t *testing.T) {
	out := highlightBody("application/json", `{"name":"deck"}`, "dracula")
	if out == "" || !strings.Contains(out, "deck") {
		t.Errorf("highlighted JSON should retain content, got %q", out)
	}
}
