package files

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/config"
	"deck/internal/editor"
	"deck/internal/ui"
	"deck/internal/viewer"
)

var pyInstall = []string{"npm", "install", "--global", "pyright"}

func missingPython() viewer.ServerStatus {
	return viewer.ServerStatus{
		Server: "python", Label: "Py", Language: "Python", State: viewer.ServerFailed, Percent: -1,
		Err: "pyright-langserver not found", Hint: strings.Join(pyInstall, " "), Install: pyInstall,
	}
}

func lookFor(found ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, f := range found {
			if f == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

func TestOfferFor(t *testing.T) {
	ready := missingPython()
	ready.State = viewer.ServerReady
	noInstall := missingPython()
	noInstall.Install = nil
	cases := []struct {
		name     string
		st       viewer.ServerStatus
		declined map[string]bool
		look     func(string) (string, error)
		want     bool
	}{
		{"missing with installer", missingPython(), nil, lookFor("npm"), true},
		{"installer not on PATH", missingPython(), nil, lookFor("brew"), false},
		{"declined this session", missingPython(), map[string]bool{"python": true}, lookFor("npm"), false},
		{"other server declined", missingPython(), map[string]bool{"go": true}, lookFor("npm"), true},
		{"server is running", ready, nil, lookFor("npm"), false},
		{"no install command", noInstall, nil, lookFor("npm"), false},
		{"no server", viewer.ServerStatus{Ext: ".txt"}, nil, lookFor("npm"), false},
	}
	for _, c := range cases {
		o, ok := offerFor(c.st, c.declined, c.look)
		if ok != c.want {
			t.Errorf("%s: offered=%v, want %v", c.name, ok, c.want)
		}
		if ok && (o.server != "python" || o.pkg != "pyright" || o.language != "Python") {
			t.Errorf("%s: offer = %+v", c.name, o)
		}
	}
}

func TestInstallPackage(t *testing.T) {
	for argv, want := range map[string]string{
		"npm install --global pyright":                               "pyright",
		"npm install --global typescript-language-server typescript": "typescript-language-server",
		"go install golang.org/x/tools/gopls@latest":                 "gopls",
		"dotnet tool install --global csharp-ls":                     "csharp-ls",
		"rustup component add rust-analyzer":                         "rust-analyzer",
		"brew install lua-language-server":                           "lua-language-server",
		"npm install --global vscode-langservers-extracted":          "vscode-langservers-extracted",
		"npm install -g @scope/thing@1.2.3":                          "thing",
	} {
		if got := installPackage(strings.Fields(argv)); got != want {
			t.Errorf("installPackage(%q) = %q, want %q", argv, got, want)
		}
	}
}

func TestOfferQuestion(t *testing.T) {
	o, _ := offerFor(missingPython(), nil, lookFor("npm"))
	want := "Python language server isn't installed. Install pyright now? (npm install --global pyright)"
	if got := o.question(); got != want {
		t.Errorf("question = %q, want %q", got, want)
	}
}

type installStub struct {
	*stubLSP
	out       string
	err       error
	installed []string
}

func (s *installStub) Install(_ context.Context, server string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installed = append(s.installed, server)
	if s.err == nil {
		s.status = viewer.ServerStatus{Server: server, Label: "Py", Language: "Python", State: viewer.ServerIdle, Percent: -1}
	}
	return s.out, s.err
}

func (s *installStub) Status(path string) viewer.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Ext = filepath.Ext(path)
	return st
}

func withLookPath(t *testing.T, f func(string) (string, error)) {
	t.Helper()
	old := installerLookPath
	installerLookPath = f
	t.Cleanup(func() { installerLookPath = old })
}

func pyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"app.py", "util.py"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("def main():\n    pass\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func pyStub() *installStub {
	s := &installStub{stubLSP: newStubLSP()}
	s.status = missingPython()
	return s
}

func startedWith(t *testing.T, dir string, cfg config.Config, nav LSP) Model {
	t.Helper()
	m := New(dir, cfg).WithLSP(nav)
	m.SetSize(120, 30)
	m, _ = settle(t, m, m.Init())
	return m
}

func offering(t *testing.T, nav LSP) Model {
	t.Helper()
	withLookPath(t, lookFor("npm"))
	m := startedWith(t, pyFixture(t), config.Default(), nav)
	if m.offer == nil {
		t.Fatal("opening a file whose server is missing should offer an install")
	}
	return m
}

func toastsIn(msgs []tea.Msg) []ui.ToastMsg {
	var out []ui.ToastMsg
	for _, msg := range msgs {
		if t, ok := msg.(ui.ToastMsg); ok {
			out = append(out, t)
		}
	}
	return out
}

func TestOfferShownWhenFileOpens(t *testing.T) {
	m := offering(t, pyStub())
	if !m.Capturing() {
		t.Error("the offer owns the keyboard")
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Python language server isn't installed.", "Install pyright now?", "(npm install --global pyright)", "y/n"} {
		if !strings.Contains(strings.Join(strings.Fields(view), " "), want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if b := m.HelpBindings(); len(b) == 0 || b[0].Help().Key != "y" {
		t.Errorf("help should describe the offer, got %+v", b)
	}
}

func TestOfferYesInstallsAndRestarts(t *testing.T) {
	nav := pyStub()
	nav.out = "added 1 package in 3s\n"
	m := offering(t, nav)
	symbolsBefore := nav.count("symbols")
	m, out := press(t, m, "y")
	if m.offer != nil || m.Capturing() {
		t.Error("y closes the offer")
	}
	if len(nav.installed) != 1 || nav.installed[0] != "python" {
		t.Fatalf("installed %v, want python", nav.installed)
	}
	var resync bool
	for _, msg := range out {
		if _, ok := msg.(editor.ResyncMsg); ok {
			resync = true
		}
	}
	if !resync {
		t.Error("a successful install should resync the micro bundle")
	}
	toasts := toastsIn(out)
	if len(toasts) != 2 || !strings.Contains(toasts[0].Text, "installing pyright") || !strings.Contains(toasts[1].Text, "installed pyright") {
		t.Errorf("toasts = %+v", toasts)
	}
	if nav.count("symbols") <= symbolsBefore {
		t.Error("the open file should be re-sent to the new server")
	}
	for _, st := range m.LSPStatus() {
		if st.Server == "python" && (st.State == viewer.ServerFailed || st.Title != "") {
			t.Errorf("stale status after install: %+v", st)
		}
	}
	nav.status = missingPython()
	m, _ = press(t, m, "j")
	if m.offer != nil {
		t.Error("a server that was installed is not offered again")
	}
}

func TestOfferShowsProgressWhileInstalling(t *testing.T) {
	m := offering(t, pyStub())
	m, cmd := m.Update(keyMsg("y"))
	if cmd == nil {
		t.Fatal("y should start the install")
	}
	var progress bool
	for _, st := range m.LSPStatus() {
		if st.Server == "python" && st.State == viewer.ServerStarting && st.Title == "installing pyright" {
			progress = true
		}
	}
	if !progress {
		t.Errorf("status bar should show the install, got %+v", m.LSPStatus())
	}
}

func TestOfferInstallFailureToastsOutput(t *testing.T) {
	nav := pyStub()
	nav.out = "npm WARN deprecated x\nnpm ERR! code EACCES\nnpm ERR! permission denied\n"
	nav.err = errors.New("exit status 243")
	m := offering(t, nav)
	m, out := press(t, m, "y")
	toasts := toastsIn(out)
	last := toasts[len(toasts)-1]
	if last.Level != ui.LevelError || last.Text != "pyright install failed: exit status 243: npm ERR! code EACCES · npm ERR! permission denied" {
		t.Errorf("failure toast = %+v", last)
	}
	failed := false
	for _, st := range m.LSPStatus() {
		failed = failed || (st.Server == "python" && st.State == viewer.ServerFailed)
	}
	if !failed {
		t.Error("the status bar should show the server failed again")
	}
	m, _ = press(t, m, "j")
	if m.offer != nil {
		t.Error("a failed install is not offered again this session")
	}
}

func TestOfferDeclined(t *testing.T) {
	for _, k := range []string{"n", "esc"} {
		nav := pyStub()
		m := offering(t, nav)
		m, out := press(t, m, k)
		if m.offer != nil || len(nav.installed) != 0 || len(out) != 0 {
			t.Errorf("%s: offer=%v installed=%v out=%v", k, m.offer, nav.installed, out)
		}
		m, _ = press(t, m, "j")
		if m.SelectedPath() == "" || filepath.Ext(m.viewer.Path()) != ".py" || m.offer != nil {
			t.Errorf("%s: a declined server is not offered again (open %q, offer %v)", k, m.viewer.Path(), m.offer)
		}
	}
}

func TestOfferOtherKeyDismissesForNow(t *testing.T) {
	nav := pyStub()
	m := offering(t, nav)
	m, _ = press(t, m, "x")
	if m.offer != nil || len(nav.installed) != 0 {
		t.Fatal("another key closes the offer without installing")
	}
	m, _ = press(t, m, "j")
	if m.offer == nil {
		t.Error("a dismissed (not declined) offer comes back on the next file")
	}
}

func TestNoOfferWithoutInstaller(t *testing.T) {
	withLookPath(t, lookFor())
	m := startedWith(t, pyFixture(t), config.Default(), pyStub())
	if m.offer != nil || m.Capturing() {
		t.Error("no offer when the installer tool isn't on PATH")
	}
}

func TestNoOfferWhenDisabled(t *testing.T) {
	withLookPath(t, lookFor("npm"))
	cfg := config.Default()
	cfg.LSP.OfferInstall = false
	if m := startedWith(t, pyFixture(t), cfg, pyStub()); m.offer != nil {
		t.Error("lsp.offer_install = false turns the offer off")
	}
}

func TestNoOfferWithoutInstallerSupport(t *testing.T) {
	withLookPath(t, lookFor("npm"))
	nav := newStubLSP()
	nav.status = missingPython()
	if m := startedWith(t, pyFixture(t), config.Default(), nav); m.offer != nil {
		t.Error("an LSP that cannot install never offers")
	}
}

func TestFailureEventOffersForOpenFile(t *testing.T) {
	withLookPath(t, lookFor("npm"))
	nav := pyStub()
	nav.status = viewer.ServerStatus{Server: "python", Label: "Py", State: viewer.ServerIdle, Percent: -1}
	m := startedWith(t, pyFixture(t), config.Default(), nav)
	if m.offer != nil {
		t.Fatal("no offer before the server is known to be missing")
	}
	nav.status = missingPython()
	m, _ = send(t, m, LSPEventMsg{Event: missingPython()})
	if m.offer == nil {
		t.Error("the missing-binary event should offer an install for the open file")
	}
}

func TestOfferFitsSmallFrame(t *testing.T) {
	m := offering(t, pyStub())
	for _, size := range [][2]int{{60, 11}, {80, 19}, {200, 45}} {
		m.SetSize(size[0], size[1])
		lines := strings.Split(m.View(), "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d rows", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: row %d is %d wide", size[0], size[1], i, w)
			}
		}
		if !strings.Contains(ansi.Strip(m.View()), "y/n") {
			t.Errorf("%dx%d: the prompt should stay visible", size[0], size[1])
		}
	}
}
