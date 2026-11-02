package runner

import "testing"

func TestMatchProblem(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Problem
		ok   bool
	}{
		{"dotnet", "src/Api/Foo.cs(12,5): error CS0103: The name 'x' does not exist in the current context [/r/src/Api/Api.csproj]",
			Problem{Path: "src/Api/Foo.cs", Line: 12, Col: 5, Severity: "error", Code: "CS0103", Msg: "The name 'x' does not exist in the current context"}, true},
		{"dotnet warning with range and node prefix", "  3>/abs/Bar.cs(7,9,7,20): warning CS8618: Non-nullable property",
			Problem{Path: "/abs/Bar.cs", Line: 7, Col: 9, Severity: "warning", Code: "CS8618", Msg: "Non-nullable property"}, true},
		{"msbuild coloured", "\x1b[31msrc/A.cs(1,2): error CS1002: ; expected\x1b[0m",
			Problem{Path: "src/A.cs", Line: 1, Col: 2, Severity: "error", Code: "CS1002", Msg: "; expected"}, true},
		{"go", "internal/x/y.go:12:5: undefined: foo",
			Problem{Path: "internal/x/y.go", Line: 12, Col: 5, Severity: "error", Msg: "undefined: foo"}, true},
		{"go test", "    y_test.go:40: got 1, want 2",
			Problem{Path: "y_test.go", Line: 40, Severity: "error", Msg: "got 1, want 2"}, true},
		{"tsc", "src/app.ts(3,4): error TS2322: Type 'string' is not assignable",
			Problem{Path: "src/app.ts", Line: 3, Col: 4, Severity: "error", Code: "TS2322", Msg: "Type 'string' is not assignable"}, true},
		{"tsc pretty", "src/app.tsx:3:4 - error TS2322: Bad",
			Problem{Path: "src/app.tsx", Line: 3, Col: 4, Severity: "error", Code: "TS2322", Msg: "Bad"}, true},
		{"plain", "Build succeeded.", Problem{}, false},
		{"go panic frame", "\t/r/x.go:123 +0x1d", Problem{}, false},
		{"url", "listening on http://localhost:5000", Problem{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := matchProblem(c.line)
			if ok != c.ok || got != c.want {
				t.Errorf("matchProblem(%q) = %+v, %v; want %+v, %v", c.line, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestMatchDotnet(t *testing.T) {
	p, ok := matchProblem("src/Api/Foo.cs(12,5): error CS0103: The name 'bar' does not exist")
	if !ok || p.Path != "src/Api/Foo.cs" || p.Line != 12 || p.Col != 5 || p.Code != "CS0103" {
		t.Errorf("got %+v, %v", p, ok)
	}
}

func TestMatchGo(t *testing.T) {
	p, ok := matchProblem("main.go:12:5: expected ';'")
	if !ok || p.Path != "main.go" || p.Line != 12 || p.Col != 5 {
		t.Errorf("got %+v, %v", p, ok)
	}
}

func TestMatchTsc(t *testing.T) {
	p, ok := matchProblem("file.ts(3,4): error TS1005: ';' expected.")
	if !ok || p.Path != "file.ts" || p.Line != 3 || p.Col != 4 || p.Code != "TS1005" {
		t.Errorf("got %+v, %v", p, ok)
	}
}

func TestProblemKey(t *testing.T) {
	a, _ := matchProblem("src/A.cs(1,2): error CS1002: ; expected [/r/A.csproj]")
	b, _ := matchProblem("  1>src/A.cs(1,2): error CS1002: ; expected [/r/A.csproj]")
	if a.key() != b.key() {
		t.Error("the msbuild summary repeat should dedupe with the original")
	}
}
