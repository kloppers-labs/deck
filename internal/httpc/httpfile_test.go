package httpc

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

const aspnetSample = `@WebApi_HostAddress = http://localhost:5062

GET {{WebApi_HostAddress}}/weatherforecast/
Accept: application/json

###
`

func TestParseASPNETTemplate(t *testing.T) {
	f := parseHTTPFile(aspnetSample)
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(f.Requests))
	}
	r := f.Requests[0]
	if r.Method != "GET" || r.URL != "{{WebApi_HostAddress}}/weatherforecast/" || r.Line != 3 {
		t.Errorf("request = %+v", r)
	}
	if !slices.Equal(r.Headers, []string{"Accept: application/json"}) || r.Body != "" {
		t.Errorf("headers %q body %q", r.Headers, r.Body)
	}
	res := resolver{file: f.Vars}
	if got := res.resolve(r.URL); got != "http://localhost:5062/weatherforecast/" {
		t.Errorf("resolved URL = %q", got)
	}
}

func TestParseSeparatorsNamesCommentsAndBodies(t *testing.T) {
	src := `# a file comment
@base = https://api.test
@token = abc

### List cars
// @name listCars
GET {{base}}/cars?page=1 HTTP/1.1
Authorization: Bearer {{token}}

###
# @name createCar
POST {{base}}/cars
Content-Type: application/json

{
  "make": "VW",

  "model": "Polo"
}


### Bare URL
{{base}}/health
`
	f := parseHTTPFile(src)
	if len(f.Requests) != 3 {
		t.Fatalf("requests = %+v, want 3", f.Requests)
	}
	list, create, health := f.Requests[0], f.Requests[1], f.Requests[2]
	if list.Name != "listCars" || list.Method != "GET" || list.URL != "{{base}}/cars?page=1" {
		t.Errorf("list = %+v", list)
	}
	if !slices.Equal(list.Headers, []string{"Authorization: Bearer {{token}}"}) {
		t.Errorf("list headers = %q", list.Headers)
	}
	if create.Name != "createCar" || create.Method != "POST" || create.Line != 12 {
		t.Errorf("create = %+v", create)
	}
	wantBody := "{\n  \"make\": \"VW\",\n\n  \"model\": \"Polo\"\n}"
	if create.Body != wantBody {
		t.Errorf("body = %q, want %q", create.Body, wantBody)
	}
	if health.Method != "GET" || health.URL != "{{base}}/health" || health.Name != "Bare URL" {
		t.Errorf("health = %+v", health)
	}
	if f.Vars["base"] != "https://api.test" || f.Vars["token"] != "abc" {
		t.Errorf("vars = %v", f.Vars)
	}
}

func TestRequestAt(t *testing.T) {
	f := parseHTTPFile("GET http://a/1\n\n###\nGET http://a/2\n\n###\nGET http://a/3\n")
	cases := map[int]string{0: "http://a/1", 1: "http://a/1", 4: "http://a/2", 5: "http://a/2", 7: "http://a/3", 99: "http://a/3"}
	for line, want := range cases {
		if r, ok := f.requestAt(line); !ok || r.URL != want {
			t.Errorf("requestAt(%d) = %q, want %q", line, r.URL, want)
		}
	}
}

func TestResolveDynamicVariables(t *testing.T) {
	r := resolver{
		file:   map[string]string{"host": "{{scheme}}://h", "scheme": "http"},
		env:    map[string]string{"scheme": "https", "key": "env-key"},
		dotenv: map[string]string{"SECRET": "s3"},
		now:    func() time.Time { return time.Unix(1700000000, 0) },
	}
	cases := map[string]string{
		"{{host}}/x":              "http://h/x",
		"{{key}}":                 "env-key",
		"{{ $timestamp }}":        "1700000000",
		"{{$dotenv SECRET}}":      "s3",
		"{{$dotenv MISSING}}":     "",
		"{{unknown}}":             "{{unknown}}",
		"{{$processEnv DECK_NO}}": "",
	}
	for in, want := range cases {
		if got := r.resolve(in); got != want {
			t.Errorf("resolve(%q) = %q, want %q", in, got, want)
		}
	}
	guid := r.resolve("{{$guid}}")
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(guid) {
		t.Errorf("$guid = %q, want a v4 UUID", guid)
	}
	if r.resolve("{{$guid}}") == guid {
		t.Error("$guid should differ per use")
	}
}

func TestResolveSelfReferenceTerminates(t *testing.T) {
	r := resolver{file: map[string]string{"a": "{{a}}x"}}
	if got := r.resolve("{{a}}"); !strings.HasSuffix(got, "x") {
		t.Errorf("resolve = %q", got)
	}
}

func TestParseDotEnv(t *testing.T) {
	got := parseDotEnv("# c\nA=1\nexport B = \"two words\"\nC='q'\nbad line\n")
	if got["A"] != "1" || got["B"] != "two words" || got["C"] != "q" || len(got) != 3 {
		t.Errorf("dotenv = %v", got)
	}
}
