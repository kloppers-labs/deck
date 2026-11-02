package lsp

import "testing"

func TestPathURIRoundTrip(t *testing.T) {
	for path, uri := range map[string]string{
		"/tmp/a.go":                   "file:///tmp/a.go",
		"/Users/me/My Project/Foo.cs": "file:///Users/me/My%20Project/Foo.cs",
		"/src/C#/Program.cs":          "file:///src/C%23/Program.cs",
		"/home/ü/日本/x.ts":             "file:///home/%C3%BC/%E6%97%A5%E6%9C%AC/x.ts",
		"/odd/100%/a?b.go":            "file:///odd/100%25/a%3Fb.go",
		"/Users/me/proj/src/[id].tsx": "file:///Users/me/proj/src/%5Bid%5D.tsx",
	} {
		if got := PathToURI(path); got != uri {
			t.Errorf("PathToURI(%q) = %q, want %q", path, got, uri)
		}
		back, ok := URIToPath(uri)
		if !ok || back != path {
			t.Errorf("URIToPath(%q) = %q, %v, want %q", uri, back, ok, path)
		}
	}
}

func TestURIToPathAcceptsUnescapedAndHost(t *testing.T) {
	for uri, want := range map[string]string{
		"file:///Users/me/My Project/a.go": "/Users/me/My Project/a.go",
		"file://localhost/tmp/a.go":        "/tmp/a.go",
		"file:///c%3A/x.cs":                "/c:/x.cs",
	} {
		if got, ok := URIToPath(uri); !ok || got != want {
			t.Errorf("URIToPath(%q) = %q, %v, want %q", uri, got, ok, want)
		}
	}
}

func TestMetadataURIPassthrough(t *testing.T) {
	const meta = "csharp:/metadata/projects/Api/assemblies/System.Runtime/symbols/System.String.cs"
	if got := PathToURI(meta); got != meta {
		t.Fatalf("PathToURI(meta) = %q", got)
	}
	got, ok := URIToPath(meta)
	if ok || got != meta {
		t.Fatalf("URIToPath(meta) = %q, %v; want passthrough with ok=false", got, ok)
	}
	if !IsVirtual(meta) || IsVirtual("file:///a.cs") || IsVirtual("/a.cs") {
		t.Fatal("IsVirtual misclassified")
	}
}
