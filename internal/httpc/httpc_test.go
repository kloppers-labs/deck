package httpc

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	req, _, err := buildRequest("GET", "api.example.com/v1/cars", "", "")
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
	if _, _, err := buildRequest("GET", "   ", "", ""); err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestBuildRequestBodyAndHeaders(t *testing.T) {
	req, _, err := buildRequest("POST", "https://x.test/p", "Content-Type: application/json", `{"a":1}`)
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

func TestLocalhostDefaultsHTTP(t *testing.T) {
	cases := map[string]string{
		"localhost:5000/x":    "http",
		"127.0.0.1/x":         "http",
		"[::1]:5001/x":        "http",
		"LOCALHOST":           "http",
		"api.example.com/v1":  "https",
		"localhost.evil.com/": "https",
	}
	for raw, want := range cases {
		req, _, err := buildRequest("GET", raw, "", "")
		if err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		if req.URL.Scheme != want {
			t.Errorf("%s: scheme = %q, want %q", raw, req.URL.Scheme, want)
		}
	}
}

func TestJSONBodyAddsContentType(t *testing.T) {
	req, auto, err := buildRequest("POST", "localhost/x", "", `{"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Content-Type") != "application/json" || len(auto) != 1 || auto[0] != "Content-Type: application/json" {
		t.Errorf("Content-Type = %q, auto = %q", req.Header.Get("Content-Type"), auto)
	}
	req, auto, _ = buildRequest("POST", "localhost/x", "content-type: text/plain", `{"a":1}`)
	if req.Header.Get("Content-Type") != "text/plain" || len(auto) != 0 {
		t.Error("an explicit Content-Type wins")
	}
	if _, auto, _ = buildRequest("POST", "localhost/x", "", "not json"); len(auto) != 0 {
		t.Error("a non-JSON body gets no Content-Type")
	}
}

func TestLatencyIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("late body"))
	}))
	defer srv.Close()
	req, _, _ := buildRequest("GET", srv.URL, "", "")
	msg := doRequest(context.Background(), req, 1)().(RespMsg)
	if msg.err != nil || msg.body != "late body" {
		t.Fatalf("resp = %+v", msg)
	}
	if msg.latency < 150*time.Millisecond {
		t.Errorf("latency %v should include reading the body", msg.latency)
	}
}

func TestTruncatedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxRespBytes+10))
	}))
	defer srv.Close()
	req, _, _ := buildRequest("GET", srv.URL, "", "")
	msg := doRequest(context.Background(), req, 1)().(RespMsg)
	if !msg.truncated || len(msg.body) != maxRespBytes {
		t.Errorf("truncated = %v, len = %d", msg.truncated, len(msg.body))
	}
}

func TestFormatHeaders(t *testing.T) {
	got := formatHeaders(http.Header{"X-B": {"2"}, "Content-Type": {"text/plain"}, "X-A": {"1", "3"}})
	want := "Content-Type: text/plain\nX-A: 1\nX-A: 3\nX-B: 2"
	if ansi.Strip(got) != want {
		t.Errorf("formatHeaders = %q, want %q", ansi.Strip(got), want)
	}
}
