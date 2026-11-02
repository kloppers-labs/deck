package lsp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func rng(sl, sc, el, ec int) Range {
	return Range{Start: Position{sl, sc}, End: Position{el, ec}}
}

func TestDecodeLocations(t *testing.T) {
	loc := `{"uri":"file:///a.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":5}}}`
	link := `{"originSelectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},` +
		`"targetUri":"file:///b.go",` +
		`"targetRange":{"start":{"line":10,"character":0},"end":{"line":20,"character":1}},` +
		`"targetSelectionRange":{"start":{"line":11,"character":5},"end":{"line":11,"character":8}}}`
	want := Location{URI: "file:///a.go", Range: rng(1, 2, 1, 5)}
	wantLink := Location{URI: "file:///b.go", Range: rng(11, 5, 11, 8)}
	for _, tc := range []struct {
		name string
		raw  string
		want []Location
	}{
		{"empty", ``, nil},
		{"null", `null`, nil},
		{"empty array", `[]`, nil},
		{"single location", loc, []Location{want}},
		{"location array", `[` + loc + `,` + loc + `]`, []Location{want, want}},
		{"location links", `[` + link + `]`, []Location{wantLink}},
		{"single link", link, []Location{wantLink}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeLocations(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if _, err := decodeLocations(json.RawMessage(`42`)); err == nil {
		t.Fatal("want error for a number")
	}
}

func TestDecodeHover(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"null", `null`, ""},
		{"markup", `{"contents":{"kind":"markdown","value":"**x** int"}}`, "**x** int"},
		{"plaintext markup", `{"contents":{"kind":"plaintext","value":"x int"}}`, "x int"},
		{"marked string plain", `{"contents":"just text"}`, "just text"},
		{"marked string code", `{"contents":{"language":"go","value":"func F()"}}`, "```go\nfunc F()\n```"},
		{"marked string array", `{"contents":[{"language":"csharp","value":"string Name"},"The name."]}`,
			"```csharp\nstring Name\n```\n\nThe name."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeHover(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got.Markdown != tc.want {
				t.Fatalf("got %q, want %q", got.Markdown, tc.want)
			}
		})
	}
	got, err := decodeHover(json.RawMessage(`{"contents":"x","range":{"start":{"line":3,"character":1},"end":{"line":3,"character":4}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Range == nil || *got.Range != rng(3, 1, 3, 4) {
		t.Fatalf("range = %+v", got.Range)
	}
}

func symbolSummary(syms []Symbol) string {
	var b strings.Builder
	for _, s := range syms {
		b.WriteString(strings.Repeat("  ", s.Depth))
		b.WriteString(s.Name)
		b.WriteString("^")
		if s.Parent >= 0 {
			b.WriteString(syms[s.Parent].Name)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestDecodeSymbolsHierarchical(t *testing.T) {
	raw := `[
	 {"name":"Ns","kind":3,"range":{"start":{"line":0,"character":0},"end":{"line":30,"character":0}},
	  "selectionRange":{"start":{"line":0,"character":10},"end":{"line":0,"character":12}},
	  "children":[
	   {"name":"Foo","kind":5,"detail":"class","range":{"start":{"line":1,"character":0},"end":{"line":10,"character":0}},
	    "selectionRange":{"start":{"line":1,"character":6},"end":{"line":1,"character":9}},
	    "children":[{"name":"Bar","kind":6,"range":{"start":{"line":2,"character":0},"end":{"line":4,"character":0}},
	     "selectionRange":{"start":{"line":2,"character":4},"end":{"line":2,"character":7}}}]},
	   {"name":"Baz","kind":5,"range":{"start":{"line":12,"character":0},"end":{"line":20,"character":0}},
	    "selectionRange":{"start":{"line":12,"character":6},"end":{"line":12,"character":9}}}]},
	 {"name":"Top","kind":12,"range":{"start":{"line":31,"character":0},"end":{"line":32,"character":0}},
	  "selectionRange":{"start":{"line":31,"character":5},"end":{"line":31,"character":8}}}
	]`
	syms, err := decodeSymbols(json.RawMessage(raw), "file:///a.cs")
	if err != nil {
		t.Fatal(err)
	}
	want := "Ns^\n  Foo^Ns\n    Bar^Foo\n  Baz^Ns\nTop^\n"
	if got := symbolSummary(syms); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if syms[1].Detail != "class" || syms[1].Kind != SymbolClass || syms[1].SelectionRange != rng(1, 6, 1, 9) {
		t.Fatalf("Foo = %+v", syms[1])
	}
	if syms[2].URI != "file:///a.cs" {
		t.Fatalf("URI = %q", syms[2].URI)
	}
}

func TestDecodeSymbolsFlatRebuiltByContainment(t *testing.T) {
	sym := func(name string, kind int, r Range, container string) map[string]any {
		return map[string]any{"name": name, "kind": kind, "containerName": container,
			"location": map[string]any{"uri": "file:///a.go", "range": r}}
	}
	flat := []any{
		sym("Bar", 6, rng(2, 0, 4, 0), "Foo"),
		sym("Top", 12, rng(31, 0, 32, 0), ""),
		sym("Ns", 3, rng(0, 0, 30, 0), ""),
		sym("Baz", 5, rng(12, 0, 20, 0), "Ns"),
		sym("Foo", 5, rng(1, 0, 10, 0), "Ns"),
	}
	raw, _ := json.Marshal(flat)
	syms, err := decodeSymbols(raw, "file:///a.go")
	if err != nil {
		t.Fatal(err)
	}
	want := "Ns^\n  Foo^Ns\n    Bar^Foo\n  Baz^Ns\nTop^\n"
	if got := symbolSummary(syms); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if syms[2].Container != "Foo" || syms[2].URI != "file:///a.go" {
		t.Fatalf("Bar = %+v", syms[2])
	}
}

func TestDecodeSymbolsNull(t *testing.T) {
	for _, raw := range []string{``, `null`, `[]`} {
		syms, err := decodeSymbols(json.RawMessage(raw), "file:///a.go")
		if err != nil || len(syms) != 0 {
			t.Fatalf("%q: syms=%v err=%v", raw, syms, err)
		}
	}
}

func TestDecodeWorkspaceSymbols(t *testing.T) {
	raw := `[
	 {"name":"Foo","kind":5,"containerName":"Ns","location":{"uri":"file:///a.cs","range":{"start":{"line":3,"character":1},"end":{"line":9,"character":0}}}},
	 {"name":"Bar","kind":11,"location":{"uri":"file:///b.cs"}}
	]`
	syms, err := decodeWorkspaceSymbols(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 2 || syms[0].URI != "file:///a.cs" || syms[0].Range != rng(3, 1, 9, 0) || syms[0].Container != "Ns" ||
		syms[1].URI != "file:///b.cs" || syms[1].Kind != SymbolInterface || syms[1].Parent != -1 {
		t.Fatalf("got %+v", syms)
	}
}
