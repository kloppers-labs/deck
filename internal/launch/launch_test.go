package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestDetectOrder(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		goos string
		want Target
	}{
		{"orca beats tmux", []string{"ORCA_TERMINAL_HANDLE", "term_1", "TMUX", "/tmp/t"}, "darwin", Target{Kind: Orca, Handle: "term_1"}},
		{"cmux beats ghostty", []string{"CMUX_SURFACE_ID", "s1", "TERM_PROGRAM", "ghostty"}, "darwin", Target{Kind: Cmux, Handle: "s1"}},
		{"paradev", []string{"SUPERSET_WORKSPACE_ID", "w1", "TMUX", "/tmp/t"}, "linux", Target{Kind: Paradev, Handle: "w1"}},
		{"tmux beats wezterm", []string{"TMUX", "/tmp/t", "WEZTERM_PANE", "3"}, "linux", Target{Kind: Tmux}},
		{"zellij pane id", []string{"ZELLIJ_PANE_ID", "2"}, "linux", Target{Kind: Zellij}},
		{"zellij var", []string{"ZELLIJ", "0"}, "linux", Target{Kind: Zellij}},
		{"wezterm", []string{"WEZTERM_PANE", "3", "TERM_PROGRAM", "WezTerm"}, "darwin", Target{Kind: WezTerm, Handle: "3"}},
		{"kitty", []string{"KITTY_WINDOW_ID", "1"}, "linux", Target{Kind: Kitty}},
		{"ghostty on mac", []string{"TERM_PROGRAM", "ghostty"}, "darwin", Target{Kind: Ghostty}},
		{"ghostty on linux has no route", []string{"TERM_PROGRAM", "ghostty"}, "linux", Target{Kind: Unknown}},
		{"iterm", []string{"TERM_PROGRAM", "iTerm.app"}, "darwin", Target{Kind: ITerm2}},
		{"apple terminal", []string{"TERM_PROGRAM", "Apple_Terminal"}, "darwin", Target{Kind: AppleTerminal}},
		{"alacritty", []string{"ALACRITTY_SOCKET", "/tmp/a.sock"}, "linux", Target{Kind: Alacritty}},
		{"warp", []string{"TERM_PROGRAM", "WarpTerminal"}, "darwin", Target{Kind: Unknown}},
		{"nothing", nil, "linux", Target{Kind: Unknown}},
	}
	for _, tc := range cases {
		if got := Detect(env(tc.env...), tc.goos); got != tc.want {
			t.Errorf("%s: Detect = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestCommandArgv(t *testing.T) {
	cd := "cd '/p' && exec '/bin/deck'"
	cases := []struct {
		t    Target
		mode Mode
		want []string
	}{
		{Target{Kind: Orca, Handle: "H"}, Split, []string{"orca", "terminal", "split", "--terminal", "H", "--direction", "vertical", "--command", cd, "--json"}},
		{Target{Kind: Orca, Handle: "H"}, Tab, []string{"orca", "terminal", "create", "--worktree", "active", "--command", cd, "--json"}},
		{Target{Kind: Orca, Handle: "H"}, Window, []string{"orca", "terminal", "create", "--worktree", "active", "--command", cd, "--json"}},
		{Target{Kind: Cmux}, Split, []string{"cmux", "new-split", "right", "--command", cd}},
		{Target{Kind: Cmux}, Tab, []string{"cmux", "new-split", "right", "--command", cd}},
		{Target{Kind: Paradev, Handle: "W"}, Split, []string{"superset", "panes", "new-tab", "--workspace", "W", "--command", cd}},
		{Target{Kind: Tmux}, Split, []string{"tmux", "split-window", "-h", "-c", "/p", "/bin/deck"}},
		{Target{Kind: Tmux}, Tab, []string{"tmux", "new-window", "-c", "/p", "-n", "deck", "/bin/deck"}},
		{Target{Kind: Tmux}, Window, []string{"tmux", "new-window", "-c", "/p", "-n", "deck", "/bin/deck"}},
		{Target{Kind: Zellij}, Split, []string{"zellij", "action", "new-pane", "-d", "right", "--cwd", "/p", "--", "/bin/deck"}},
		{Target{Kind: Zellij}, Tab, []string{"zellij", "action", "new-tab", "--cwd", "/p", "-n", "deck", "--", "/bin/deck"}},
		{Target{Kind: WezTerm}, Split, []string{"wezterm", "cli", "split-pane", "--right", "--percent", "45", "--cwd", "/p", "--", "/bin/deck"}},
		{Target{Kind: WezTerm}, Tab, []string{"wezterm", "cli", "spawn", "--cwd", "/p", "--", "/bin/deck"}},
		{Target{Kind: WezTerm}, Window, []string{"wezterm", "cli", "spawn", "--new-window", "--cwd", "/p", "--", "/bin/deck"}},
		{Target{Kind: Kitty}, Split, []string{"kitten", "@", "launch", "--type=window", "--location=vsplit", "--cwd=/p", "/bin/deck"}},
		{Target{Kind: Kitty}, Tab, []string{"kitten", "@", "launch", "--type=tab", "--cwd=/p", "/bin/deck"}},
		{Target{Kind: Kitty}, Window, []string{"kitten", "@", "launch", "--type=os-window", "--cwd=/p", "/bin/deck"}},
		{Target{Kind: Alacritty}, Split, []string{"alacritty", "msg", "create-window", "--working-directory", "/p", "-e", "/bin/deck"}},
	}
	for _, tc := range cases {
		got, err := Command(tc.t, tc.mode, "/p", "/bin/deck")
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Command(%v, %s) = %q, %v\nwant %q", tc.t.Kind, tc.mode, got, err, tc.want)
		}
	}
}

func TestCommandQuotesDir(t *testing.T) {
	got, err := Command(Target{Kind: Orca, Handle: "H"}, Split, "/my proj/it's", "/bin/deck")
	if err != nil {
		t.Fatal(err)
	}
	want := `cd '/my proj/it'\''s' && exec '/bin/deck'`
	if got[8] != want {
		t.Errorf("command = %q, want %q", got[8], want)
	}
	dir := filepath.Join(t.TempDir(), "my proj", "it's")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, _ = Command(Target{Kind: Orca, Handle: "H"}, Split, dir, "/bin/deck")
	out, err := exec.Command("sh", "-c", strings.Replace(got[8], "exec '/bin/deck'", "pwd -P", 1)).Output()
	real, _ := filepath.EvalSymlinks(dir)
	if err != nil || strings.TrimSpace(string(out)) != real {
		t.Errorf("the quoted cd went to %q (%v), want %q", out, err, real)
	}
}

func scriptOf(t *testing.T, kind Kind, mode Mode, dir string) string {
	t.Helper()
	argv, err := Command(Target{Kind: kind}, mode, dir, "/bin/deck")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 3 || argv[0] != "osascript" || argv[1] != "-e" {
		t.Fatalf("argv = %q, want osascript -e <script>", argv)
	}
	return argv[2]
}

func TestGhosttyScript(t *testing.T) {
	s := scriptOf(t, Ghostty, Split, "/p")
	for _, want := range []string{`tell application "Ghostty"`, "new surface configuration", `initial working directory of cfg to "/p"`, `command of cfg to "/bin/deck"`, "split", "direction right"} {
		if !strings.Contains(s, want) {
			t.Errorf("ghostty split script lacks %q:\n%s", want, s)
		}
	}
	if s := scriptOf(t, Ghostty, Tab, "/p"); !strings.Contains(s, "new tab in front window with configuration cfg") {
		t.Errorf("ghostty tab script:\n%s", s)
	}
	if s := scriptOf(t, Ghostty, Split, `/a "b"`); !strings.Contains(s, `"/a \"b\""`) {
		t.Errorf("quotes in the dir should be escaped:\n%s", s)
	}
}

func TestITermScript(t *testing.T) {
	s := scriptOf(t, ITerm2, Split, "/p")
	for _, want := range []string{`tell application "iTerm2"`, "split vertically with default profile command", `cd '/p' && exec '/bin/deck'`} {
		if !strings.Contains(s, want) {
			t.Errorf("iterm script lacks %q:\n%s", want, s)
		}
	}
	if s := scriptOf(t, AppleTerminal, Split, "/p"); !strings.Contains(s, `tell application "Terminal" to do script`) {
		t.Errorf("terminal script:\n%s", s)
	}
}

func TestUnknownNoRoute(t *testing.T) {
	if _, err := Command(Target{Kind: Unknown}, Split, "/p", "/bin/deck"); !errors.Is(err, ErrNoRoute) {
		t.Errorf("err = %v, want ErrNoRoute", err)
	}
}

func TestHintKitty(t *testing.T) {
	if got := Hint(Target{Kind: Unknown}, "/p"); got != "deck isn't running here — start it with: cd /p && deck" {
		t.Errorf("Hint = %q", got)
	}
	if got := Hint(Target{Kind: Kitty}, "/p"); !strings.Contains(got, "allow_remote_control") {
		t.Errorf("kitty hint = %q", got)
	}
}

func TestStartUsesSeam(t *testing.T) {
	var got []string
	prev := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.CommandContext(ctx, "sh", "-c", "echo boom >&2; exit 1")
	}
	t.Cleanup(func() { execCommand = prev })
	err := Start(context.Background(), []string{"tmux", "split-window"})
	if !reflect.DeepEqual(got, []string{"tmux", "split-window"}) {
		t.Errorf("ran %q", got)
	}
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the command's stderr", err)
	}
}
