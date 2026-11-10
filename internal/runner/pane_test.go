package runner

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
)

func fakeStart(t *testing.T) *[]int {
	t.Helper()
	var gens []int
	orig := startProc
	startProc = func(idx, gen int, task config.Task, root string) *proc {
		gens = append(gens, gen)
		return &proc{idx: idx, gen: gen, lines: make(chan chunk, 8), exit: make(chan ExitMsg, 1), cancel: func() {}}
	}
	t.Cleanup(func() { startProc = orig })
	return &gens
}

func kp(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(p Pane, keys ...string) Pane {
	for _, k := range keys {
		p, _ = p.Update(kp(k))
	}
	return p
}

func out(gen int, lines ...string) OutputMsg {
	cs := make([]chunk, len(lines))
	for i, l := range lines {
		cs[i] = chunk{text: l}
	}
	return OutputMsg{idx: 0, gen: gen, lines: cs}
}

func plain(p Pane) string { return ansi.Strip(p.View()) }

func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, runCmds(c)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

func testPane() Pane {
	p := NewPane([]config.Task{{Name: "dev", Cmd: "sleep 999"}}, "/proj")
	p.SetSize(100, 24)
	return p
}

func TestMessagesRouteToRun(t *testing.T) {
	for _, msg := range []ui.Routed{OutputMsg{}, ExitMsg{}, DetectedMsg{}} {
		if msg.RouteTo() != ui.DestRun {
			t.Errorf("%T routes to %v, want DestRun", msg, msg.RouteTo())
		}
	}
}

func TestViewIsExactlyTheOuterBox(t *testing.T) {
	fakeStart(t)
	long := strings.Repeat("a-long-task-name-", 6)
	var tasks []config.Task
	for range 40 {
		tasks = append(tasks, config.Task{Name: long, Cmd: "echo " + long})
	}
	for _, size := range [][2]int{{60, 11}, {80, 19}, {200, 45}} {
		for _, wrap := range []bool{false, true} {
			p := NewPane(tasks, "")
			p.SetSize(size[0], size[1])
			p, _ = p.Update(DetectedMsg{more: 3})
			p, _ = p.Update(kp("enter"))
			p, _ = p.Update(out(1, strings.Repeat("y", 400), "\tsrc/A.cs(1,2): error CS1: x"))
			if wrap {
				p = press(p, "l", "w")
			}
			v := p.View()
			if got := lipgloss.Height(v); got != size[1] {
				t.Errorf("%v wrap=%v: height = %d, want %d", size, wrap, got, size[1])
			}
			if got := lipgloss.Width(v); got > size[0] {
				t.Errorf("%v wrap=%v: width = %d, want <= %d", size, wrap, got, size[0])
			}
		}
	}
}

func TestStaleGenerationDropped(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	p, _ = p.Update(OutputMsg{idx: 0, gen: 99, lines: []chunk{{text: "stale"}}})
	if strings.Contains(plain(p), "stale") {
		t.Error("stale-generation output was appended")
	}
	p, _ = p.Update(ExitMsg{idx: 0, gen: 99})
	if p.jobs[0].state != stateRunning {
		t.Errorf("stale exit changed state to %v, want still running", p.jobs[0].state)
	}
}

func TestRestartQueuesAndRelaunches(t *testing.T) {
	gens := fakeStart(t)
	p := press(testPane(), "enter")
	p, _ = p.Update(out(1, "old run"))
	p = press(p, "R")
	j := p.jobs[0]
	if !j.pendingRestart {
		t.Fatal("R while running should queue a restart")
	}
	p, _ = p.Update(ExitMsg{idx: 0, gen: 1, err: errors.New("killed"), code: -1})
	if j.state != stateRunning || j.gen != 2 {
		t.Errorf("after queued restart state = %v gen = %d, want running gen 2", j.state, j.gen)
	}
	if strings.Contains(plain(p), "old run") {
		t.Error("restart should clear the previous run's log")
	}
	if len(*gens) != 2 {
		t.Errorf("startProc launches = %v, want 2", *gens)
	}
}

func TestRestartAfterStopFires(t *testing.T) {
	marker := "sleep " + uniqueSleep()
	p := NewPane([]config.Task{{Name: "s", Cmd: marker}}, "")
	p.SetSize(100, 24)
	p = press(p, "enter")
	first := p.jobs[0].proc
	p = press(p, "R")
	var exit tea.Msg
	for exit == nil {
		msg := first.next()()
		if _, ok := msg.(ExitMsg); ok {
			exit = msg
		}
	}
	p, _ = p.Update(exit)
	j := p.jobs[0]
	if j.state != stateRunning || j.gen != 2 || j.proc == first {
		t.Fatalf("after the stopped run exited: state %v gen %d, want a fresh running gen 2", j.state, j.gen)
	}
	if !p.StopAll(5 * time.Second) {
		t.Error("StopAll timed out")
	}
	waitGone(t, marker)
}

func TestStopAllWaits(t *testing.T) {
	a, b := "sleep "+uniqueSleep(), "sleep "+uniqueSleep()
	p := NewPane([]config.Task{{Name: "a", Cmd: a + " | cat"}, {Name: "b", Cmd: b}}, "")
	p.SetSize(100, 24)
	p, _ = p.runJob(p.jobs[0])
	p, _ = p.runJob(p.jobs[1])
	time.Sleep(150 * time.Millisecond)
	began := time.Now()
	if !p.StopAll(5 * time.Second) {
		t.Fatal("StopAll reported a timeout")
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("StopAll took %v", took)
	}
	if pgrepCount(t, a) != 0 || pgrepCount(t, b) != 0 {
		t.Error("StopAll returned while task processes were still alive")
	}
	if names := p.RunningNames(); len(names) != 2 {
		t.Errorf("RunningNames = %v until the exits are processed", names)
	}
}

func TestExitRecordsDuration(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	p, _ = p.Update(ExitMsg{idx: 0, gen: 1})
	j := p.jobs[0]
	if j.state != stateDone || j.finishedAt.IsZero() || j.elapsed() == "" {
		t.Errorf("state = %v finished = %v elapsed = %q", j.state, j.finishedAt, j.elapsed())
	}
}

func TestFailedExitBadgeAndToast(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	p, cmd := p.Update(ExitMsg{idx: 0, gen: 1, err: errors.New("exit status 2"), code: 2})
	if p.FailedCount() != 1 {
		t.Errorf("FailedCount = %d, want 1", p.FailedCount())
	}
	var toast *ui.ToastMsg
	for _, m := range runCmds(cmd) {
		if tm, ok := m.(ui.ToastMsg); ok {
			toast = &tm
		}
	}
	if toast == nil || toast.Level != ui.LevelError || !strings.Contains(toast.Text, "dev") {
		t.Errorf("toast = %+v, want an error toast naming the task", toast)
	}
	p = press(p, "enter")
	if p.FailedCount() != 0 {
		t.Error("re-running clears the failure")
	}
}

func TestStoppedExitNoToast(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter", "x")
	_, cmd := p.Update(ExitMsg{idx: 0, gen: 1, err: errors.New("signal: terminated"), code: -1})
	for _, m := range runCmds(cmd) {
		if _, ok := m.(ui.ToastMsg); ok {
			t.Error("a task the user stopped should not raise a failure toast")
		}
	}
}

func TestHeaderShowsExitCode(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	p, _ = p.Update(ExitMsg{idx: 0, gen: 1, err: errors.New("exit status 3"), code: 3})
	if !strings.Contains(plain(p), "exit 3") {
		t.Errorf("header should show the exit code:\n%s", plain(p))
	}
}

func TestElapsedRefreshesOnTick(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	p.jobs[0].startedAt = time.Now().Add(-7 * time.Second)
	p, cmd := p.Update(p.spinner.Tick())
	if cmd == nil {
		t.Error("the spinner should keep ticking while a task runs")
	}
	if !strings.Contains(plain(p), "↑ 7s") {
		t.Errorf("header should show the live elapsed time:\n%s", plain(p))
	}
}

func TestRerunLast(t *testing.T) {
	gens := fakeStart(t)
	p := NewPane([]config.Task{{Name: "a", Cmd: "a"}, {Name: "b", Cmd: "b"}}, "")
	p.SetSize(100, 24)
	if _, cmd := p.Rerun(); cmd == nil {
		t.Error("re-run with nothing run yet should explain itself with a toast")
	}
	p = press(p, "j", "enter")
	p, _ = p.Update(ExitMsg{idx: 1, gen: 1})
	p = press(p, "k")
	p, _ = p.Rerun()
	if p.jobs[1].state != stateRunning || len(*gens) != 2 {
		t.Errorf("Rerun should relaunch the last task (b), states %v %v", p.jobs[0].state, p.jobs[1].state)
	}
}

func problemPane(t *testing.T) Pane {
	t.Helper()
	fakeStart(t)
	p := press(testPane(), "enter")
	p, _ = p.Update(out(1,
		"Build started",
		"src/Api/Foo.cs(12,5): error CS0103: The name 'x' does not exist",
		"noise",
		"src/Api/Bar.cs(3,1): warning CS8618: Non-nullable",
	))
	return press(p, "l")
}

func TestEJumpsNextProblem(t *testing.T) {
	p := press(problemPane(t), "e")
	if got, _ := p.jobs[0].log.selectedProblem(); got.Line != 12 {
		t.Errorf("e selected %+v, want the first problem", got)
	}
	p = press(p, "e")
	if got, _ := p.jobs[0].log.selectedProblem(); got.Line != 3 {
		t.Errorf("e again selected %+v, want the second problem", got)
	}
	p = press(p, "E")
	if got, _ := p.jobs[0].log.selectedProblem(); got.Line != 12 {
		t.Errorf("E selected %+v, want the first problem again", got)
	}
	if !strings.Contains(plain(p), "1 error 1 warning") {
		t.Errorf("footer should count problems:\n%s", plain(p))
	}
}

func TestEnterOpensEditorAtProblem(t *testing.T) {
	p := press(problemPane(t), "e")
	_, cmd := p.Update(kp("enter"))
	if cmd == nil {
		t.Fatal("enter on a problem should open the editor")
	}
	msg, ok := cmd().(editor.OpenMsg)
	want := editor.Target{Path: filepath.Join("/proj", "src/Api/Foo.cs"), Line: 12, Col: 5}
	if !ok || msg.Target != want {
		t.Errorf("enter emitted %+v, want OpenMsg %+v", msg, want)
	}
}

func TestProblemPathUsesTaskDir(t *testing.T) {
	fakeStart(t)
	p := NewPane([]config.Task{{Name: "api", Cmd: "dotnet build", Dir: "src"}}, "/proj")
	p.SetSize(100, 24)
	p = press(p, "enter")
	p, _ = p.Update(out(1, "Api/Foo.cs(1,1): error CS1: x"))
	p = press(p, "l", "e")
	if got, _ := p.jobs[0].log.selectedProblem(); got.Path != "/proj/src/Api/Foo.cs" {
		t.Errorf("path = %q, want it resolved against the task dir", got.Path)
	}
}

func TestLogSearchCounts(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	p, _ = p.Update(out(1, "foo one", "bar", "FOO two", "baz", "foo three"))
	p = press(p, "l", "/")
	if !p.Capturing() {
		t.Fatal("the log search prompt should capture")
	}
	p = press(p, "f", "o", "o", "enter")
	if p.Capturing() {
		t.Error("enter should close the prompt")
	}
	if !strings.Contains(plain(p), "/foo 1/3") {
		t.Errorf("footer should show 1/3:\n%s", plain(p))
	}
	p = press(p, "n")
	if !strings.Contains(plain(p), "/foo 2/3") {
		t.Errorf("n should advance to 2/3:\n%s", plain(p))
	}
	p = press(p, "N", "N")
	if !strings.Contains(plain(p), "/foo 3/3") {
		t.Errorf("N should wrap backwards to 3/3:\n%s", plain(p))
	}
	p = press(p, "esc")
	if strings.Contains(plain(p), "/foo") {
		t.Error("esc should clear the search")
	}
}

func TestWrapToggle(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	tail := "TAIL-END"
	p, _ = p.Update(out(1, strings.Repeat("x", 150)+tail))
	p = press(p, "l")
	if strings.Contains(plain(p), tail) {
		t.Fatal("without wrap the long line is clipped")
	}
	p = press(p, "w")
	if !strings.Contains(plain(p), tail) {
		t.Errorf("w should wrap the long line:\n%s", plain(p))
	}
}

func TestFollowIndicator(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "enter")
	for i := range 60 {
		p, _ = p.Update(out(1, fmt.Sprintf("line %d", i)))
	}
	if !strings.Contains(plain(p), "line 59") {
		t.Fatal("the log should follow new output")
	}
	p = press(p, "l", "k", "k")
	for i := range 5 {
		p, _ = p.Update(out(1, fmt.Sprintf("more %d", i)))
	}
	v := plain(p)
	if !strings.Contains(v, "↓ 5 new") || strings.Contains(v, "more 4") {
		t.Errorf("scrolled up: want a 5-new indicator and no jump:\n%s", v)
	}
	p = press(p, "F")
	if v := plain(p); strings.Contains(v, "new") || !strings.Contains(v, "more 4") {
		t.Errorf("F should resume following:\n%s", v)
	}
	p = press(p, "g")
	if !strings.Contains(plain(p), "line 0") {
		t.Error("g should go to the top")
	}
	p = press(p, "G")
	if !strings.Contains(plain(p), "more 4") {
		t.Error("G should go to the bottom")
	}
}

func TestEscReturnsFocusToList(t *testing.T) {
	fakeStart(t)
	p := press(testPane(), "l")
	if p.focus != focusLog {
		t.Fatal("l should focus the log")
	}
	if p = press(p, "esc"); p.focus != focusList {
		t.Error("esc should return to the task list")
	}
}

func TestMouseClickFocusesRegions(t *testing.T) {
	fakeStart(t)
	p := testPane()
	p, _ = p.Update(tea.MouseMsg{X: 80, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if p.focus != focusLog {
		t.Error("clicking the log should focus it")
	}
	p, _ = p.Update(tea.MouseMsg{X: 2, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if p.focus != focusList {
		t.Error("clicking the list should focus it")
	}
}

func TestClickSelectsTaskRow(t *testing.T) {
	fakeStart(t)
	p := NewPane([]config.Task{{Name: "alpha", Cmd: "a"}, {Name: "bravo", Cmd: "b"}, {Name: "charlie", Cmd: "c"}}, "")
	p.SetSize(100, 24)
	row := -1
	for i, line := range strings.Split(plain(p), "\n") {
		if strings.Contains(line, "charlie") {
			row = i
		}
	}
	p, _ = p.Update(tea.MouseMsg{X: 3, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if j := p.selected(); j == nil || j.name != "charlie" {
		t.Errorf("clicking the charlie row selected %+v", j)
	}
}

func TestDetectedMerged(t *testing.T) {
	p := NewPane([]config.Task{{Name: "api", Cmd: "go run ./cmd/api"}}, "")
	p.SetSize(100, 24)
	p, _ = p.Update(DetectedMsg{tasks: []config.Task{
		{Name: "go test", Cmd: "go test ./...", Kind: "test"},
		{Name: "api", Cmd: "other"},
	}, more: 2})
	if len(p.jobs) != 2 || p.jobs[1].name != "go test" || !p.jobs[1].auto || p.jobs[0].auto {
		t.Fatalf("jobs = %+v, want api then auto go test", p.jobs)
	}
	v := plain(p)
	if !strings.Contains(v, "auto") || !strings.Contains(v, "+2 more") {
		t.Errorf("view should mark auto tasks and the hidden count:\n%s", v)
	}
}

func TestInitDetectsAsync(t *testing.T) {
	p := NewPane(nil, t.TempDir())
	cmd := p.Init()
	if cmd == nil {
		t.Fatal("Init should detect tasks in the background")
	}
	if _, ok := cmd().(DetectedMsg); !ok {
		t.Error("detection should report a DetectedMsg")
	}
}

func TestCapturingWhileFiltering(t *testing.T) {
	p := testPane()
	if p.Capturing() {
		t.Fatal("Run pane should not capture before the filter is active")
	}
	if p = press(p, "/"); !p.Capturing() {
		t.Error("'/' should start the filter and capture the keyboard")
	}
}

func TestFmtDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{12 * time.Second, "12s"},
		{12*time.Minute + 34*time.Second, "12m34s"},
		{time.Hour + 2*time.Minute, "1h02m"},
	}
	for _, c := range cases {
		if got := fmtDuration(c.d); got != c.want {
			t.Errorf("fmtDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
