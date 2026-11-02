package httpc

import (
	"crypto/rand"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// fileRequest is one unresolved request from a .http file. Line is the
// 1-based request line, Start the first line of its block.
type fileRequest struct {
	Name    string
	Method  string
	URL     string
	Headers []string
	Body    string
	Line    int
	Start   int
}

type httpFile struct {
	Vars     map[string]string
	Requests []fileRequest
}

var (
	varDefRe  = regexp.MustCompile(`^@([A-Za-z_][\w.-]*)\s*=\s*(.*?)\s*$`)
	nameRe    = regexp.MustCompile(`^(?:#|//)\s*@name\s+(\S+)`)
	requestRe = regexp.MustCompile(`^([A-Z]+)\s+(\S+)(?:\s+HTTP/[\d.]+)?\s*$`)
	headerRe  = regexp.MustCompile(`^[\w-]+\s*:`)
)

// parseHTTPFile reads the REST Client / JetBrains .http format: "###",
// "# @name", "@var = value", request line, headers, blank line, body.
func parseHTTPFile(src string) httpFile {
	f := httpFile{Vars: map[string]string{}}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var cur *fileRequest
	var body []string
	inBody, start, blockName := false, 1, ""
	flush := func() {
		if cur != nil {
			for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
				body = body[:len(body)-1]
			}
			cur.Body = strings.Join(body, "\n")
			f.Requests = append(f.Requests, *cur)
		}
		cur, body, inBody = nil, nil, false
	}
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "###") {
			flush()
			start, blockName = i+2, strings.TrimSpace(strings.TrimLeft(line, "#"))
			continue
		}
		if inBody {
			body = append(body, raw)
			continue
		}
		if m := nameRe.FindStringSubmatch(line); m != nil {
			blockName = m[1]
			if cur != nil {
				cur.Name = blockName
			}
			continue
		}
		if line == "" {
			if cur != nil {
				inBody = true
			}
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if m := varDefRe.FindStringSubmatch(line); m != nil && cur == nil {
			f.Vars[m[1]] = m[2]
			continue
		}
		if cur == nil {
			cur = &fileRequest{Name: blockName, Method: "GET", Line: i + 1, Start: start}
			if m := requestRe.FindStringSubmatch(line); m != nil {
				cur.Method, cur.URL = m[1], m[2]
			} else {
				cur.URL = strings.Fields(line)[0]
			}
			continue
		}
		if strings.HasPrefix(line, "?") || strings.HasPrefix(line, "&") {
			cur.URL += line
			continue
		}
		if headerRe.MatchString(line) {
			cur.Headers = append(cur.Headers, line)
			continue
		}
		inBody = true
		body = append(body, raw)
	}
	flush()
	return f
}

// requestAt is the request whose block contains line; line <= 0 means the
// first request.
func (f httpFile) requestAt(line int) (fileRequest, bool) {
	if len(f.Requests) == 0 {
		return fileRequest{}, false
	}
	best := f.Requests[0]
	for _, r := range f.Requests {
		if r.Start <= line {
			best = r
		}
	}
	return best, true
}

const maxResolveDepth = 8

var placeholderRe = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// resolver expands {{name}} from file variables (over the environment) and
// the dynamic variables $guid, $timestamp, $dotenv KEY and $processEnv KEY.
type resolver struct {
	file   map[string]string
	env    map[string]string
	dotenv map[string]string
	now    func() time.Time
}

func (r resolver) resolve(s string) string { return r.expand(s, 0) }

func (r resolver) expand(s string, depth int) string {
	if depth >= maxResolveDepth {
		return s
	}
	return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholderRe.FindStringSubmatch(m)[1]
		if v, ok := r.dynamic(name); ok {
			return v
		}
		if v, ok := r.file[name]; ok {
			return r.expand(v, depth+1)
		}
		if v, ok := r.env[name]; ok {
			return r.expand(v, depth+1)
		}
		return m
	})
}

func (r resolver) dynamic(name string) (string, bool) {
	fields := strings.Fields(name)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "$") {
		return "", false
	}
	switch fields[0] {
	case "$guid", "$uuid", "$random.uuid":
		return newUUID(), true
	case "$timestamp":
		now := time.Now
		if r.now != nil {
			now = r.now
		}
		return strconv.FormatInt(now().Unix(), 10), true
	case "$dotenv":
		if len(fields) > 1 {
			return r.dotenv[fields[1]], true
		}
	case "$processEnv":
		if len(fields) > 1 {
			return os.Getenv(fields[1]), true
		}
	}
	return "", false
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseDotEnv reads KEY=VALUE lines, ignoring comments, "export" and quotes.
func parseDotEnv(src string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && slices.Contains([]byte{'"', '\''}, v[0]) && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}
