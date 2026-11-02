package git

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPathMiddleTruncate(t *testing.T) {
	long := "Business.Experience.Infrastructure/Migrations/20250101120000_InitialCreate.Designer.cs"
	cases := []struct {
		name string
		path string
		w    int
		want string
	}{
		{"fits", "src/main.go", 20, "src/main.go"},
		{"exact", "src/main.go", 11, "src/main.go"},
		{"shrinks the directory first", "very/long/directory/name/main.go", 20, "very/…y/name/main.go"},
		{"keeps the file name", long, 50, "Busi…ions/20250101120000_InitialCreate.Designer.cs"},
		{"middle of an over-long base", long, 20, "202501011…esigner.cs"},
		{"no directory", "a-really-long-file-name.txt", 12, "a-rea…me.txt"},
		{"one cell", "abc/def", 1, "…"},
		{"zero", "abc", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TruncatePath(c.path, c.w)
			if got != c.want {
				t.Errorf("TruncatePath(%q, %d) = %q, want %q", c.path, c.w, got, c.want)
			}
			if w := ansi.StringWidth(got); w > c.w {
				t.Errorf("TruncatePath(%q, %d) = %q is %d wide", c.path, c.w, got, w)
			}
		})
	}
}

func TestPathTruncateWide(t *testing.T) {
	got := TruncatePath("文件夹/目录/非常长的文件名字.go", 14)
	if w := ansi.StringWidth(got); w > 14 {
		t.Errorf("wide path %q is %d wide", got, w)
	}
}
