package files

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const csprojFixture = "\xef\xbb\xbf" + `<Project Sdk="Microsoft.NET.Sdk.Web">
  <PropertyGroup>
    <TargetFramework>net9.0</TargetFramework>
    <Nullable>enable</Nullable>
  </PropertyGroup>
  <PropertyGroup Condition="'$(Configuration)' == 'Release'">
    <OutputType>Exe</OutputType>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Serilog" Version="4.0.0" />
    <PackageReference Include="Dapper" />
    <ProjectReference Include="..\Core\Core.csproj" />
  </ItemGroup>
  <ItemGroup>
    <PackageReference Include="Polly" />
  </ItemGroup>
</Project>
`

func TestCsprojCard(t *testing.T) {
	p, err := ParseCsproj([]byte(csprojFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := Csproj{Sdk: "Microsoft.NET.Sdk.Web", TargetFramework: "net9.0", OutputType: "Exe", Packages: 3, ProjectRefs: 1}
	if p != want {
		t.Errorf("ParseCsproj = %+v, want %+v", p, want)
	}
	multi, err := ParseCsproj([]byte(`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFrameworks>net8.0;net9.0</TargetFrameworks></PropertyGroup></Project>`))
	if err != nil || multi.TargetFramework != "net8.0;net9.0" {
		t.Errorf("TargetFrameworks fallback = %+v, %v", multi, err)
	}
	if got, want := p.summary(), []string{"net9.0 · Exe", "Microsoft.NET.Sdk.Web", "3 packages · 1 project ref"}; !slices.Equal(got, want) {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if _, err := ParseCsproj([]byte("<Project")); err == nil {
		t.Error("malformed XML is an error")
	}
}

const slnFixture = `
Microsoft Visual Studio Solution File, Format Version 12.00
Project("{2150E333-8FDC-42A3-9474-1A3956D46DE8}") = "src", "src", "{11111111-1111-1111-1111-111111111111}"
EndProject
Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Deck.Api", "src\Deck.Api\Deck.Api.csproj", "{22222222-2222-2222-2222-222222222222}"
EndProject
Project("{9A19103F-16F7-4668-BE54-9A1E7A4F7556}") = "Deck.Core", "src\Deck.Core\Deck.Core.csproj", "{33333333-3333-3333-3333-333333333333}"
EndProject
Global
EndGlobal
`

func TestSlnProjects(t *testing.T) {
	got := ParseSln([]byte(slnFixture))
	want := []SlnProject{
		{Name: "Deck.Api", Path: "src/Deck.Api/Deck.Api.csproj"},
		{Name: "Deck.Core", Path: "src/Deck.Core/Deck.Core.csproj"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ParseSln = %+v, want %+v (solution folders skipped)", got, want)
	}
	slnx := `<Solution><Folder Name="/src/"><Project Path="src/A/A.csproj" /></Folder><Project Path="B/B.fsproj" /></Solution>`
	if got := ParseSlnx([]byte(slnx)); !slices.Equal(got, []SlnProject{{"A", "src/A/A.csproj"}, {"B", "B/B.fsproj"}}) {
		t.Errorf("ParseSlnx = %+v", got)
	}
}

func TestDirCardListsChildrenWithBadges(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "sub/x.go", "bin/out.dll", "a.go", "b.txt")
	if err := os.WriteFile(filepath.Join(dir, "Api.csproj"), []byte(csprojFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "All.sln"), []byte(slnFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	badges := Aggregate(map[string]Badge{
		filepath.Join(dir, "a.go"):     BadgeModified,
		filepath.Join(dir, "sub/x.go"): BadgeAdded,
	})
	card := readDirCard(dir, defaultHide(), badges, SortName, 100)
	plain, styled := card.lines()
	if len(plain) != len(styled) {
		t.Fatalf("plain and styled differ in length: %d vs %d", len(plain), len(styled))
	}
	text := strings.Join(plain, "\n")
	for _, want := range []string{
		filepath.Base(dir) + "/",
		"1 dir · 4 files · 1 hidden",
		"Project  Api.csproj",
		"TargetFramework  net9.0",
		"Packages         3",
		"Solution  All.sln · 2 projects",
		"Deck.Core  src/Deck.Core/Deck.Core.csproj",
		"A  sub/",
		"M  a.go",
		"   b.txt",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("card is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "bin/") {
		t.Errorf("hidden entries stay off the card:\n%s", text)
	}
	for i := range plain {
		if ansi.Strip(styled[i]) != plain[i] {
			t.Errorf("line %d: styled %q does not match plain %q", i, ansi.Strip(styled[i]), plain[i])
		}
	}

	capped := readDirCard(dir, defaultHide(), nil, SortName, 2)
	cp, _ := capped.lines()
	if last := cp[len(cp)-1]; !strings.Contains(last, "… and 3 more") {
		t.Errorf("a capped card ends with the remainder, got %q", last)
	}
}

func TestDirectorySelectionShowsCard(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 1, "sub/b.go": 1})
	m := started(t, dir, 120, 20)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "sub/") || !strings.Contains(v, "2 files") || !strings.Contains(v, "a.go") {
		t.Errorf("selecting a directory shows its card in the viewer:\n%s", v)
	}
	if _, out := press(t, m, "e"); len(openMsgs(out)) != 0 {
		t.Error("e on a directory must not launch the editor")
	}
}
