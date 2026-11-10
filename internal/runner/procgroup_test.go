package runner

import (
	"fmt"
	"math/rand/v2"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/config"
)

func shTask(cmd string) config.Task { return config.Task{Name: "t", Cmd: cmd} }

func drain(t *testing.T, p *proc) []tea.Msg {
	t.Helper()
	var msgs []tea.Msg
	deadline := time.After(10 * time.Second)
	for {
		got := make(chan tea.Msg, 1)
		go func() { got <- p.next()() }()
		select {
		case m := <-got:
			if m == nil {
				return msgs
			}
			msgs = append(msgs, m)
		case <-deadline:
			t.Fatalf("proc never finished; got %d messages", len(msgs))
		}
	}
}

func outputText(msgs []tea.Msg) []string {
	var lines []string
	for _, m := range msgs {
		if o, ok := m.(OutputMsg); ok {
			for _, c := range o.lines {
				lines = append(lines, c.text)
			}
		}
	}
	return lines
}

func lastExit(t *testing.T, msgs []tea.Msg) ExitMsg {
	t.Helper()
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	ex, ok := msgs[len(msgs)-1].(ExitMsg)
	if !ok {
		t.Fatalf("last message = %T, want ExitMsg", msgs[len(msgs)-1])
	}
	return ex
}

func uniqueSleep() string {
	return fmt.Sprintf("30.%06d", rand.IntN(1_000_000))
}

func pgrepCount(t *testing.T, pattern string) int {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(out)))
}

func waitGone(t *testing.T, pattern string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for pgrepCount(t, pattern) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%q still running", pattern)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStartStreamsStdoutThenExit(t *testing.T) {
	msgs := drain(t, start(0, 1, shTask("echo hello"), ""))
	if got := outputText(msgs); !slices.Equal(got, []string{"hello"}) {
		t.Errorf("output = %q, want [hello]", got)
	}
	if ex := lastExit(t, msgs); ex.err != nil || ex.code != 0 {
		t.Errorf("exit = %+v, want clean", ex)
	}
}

func TestStartCapturesStderrAndExitCode(t *testing.T) {
	msgs := drain(t, start(2, 1, shTask("echo oops >&2; exit 3"), ""))
	if got := outputText(msgs); !slices.Equal(got, []string{"oops"}) {
		t.Errorf("output = %q, want stderr captured", got)
	}
	ex := lastExit(t, msgs)
	if ex.err == nil || ex.code != 3 || ex.idx != 2 {
		t.Errorf("exit = %+v, want idx 2 code 3", ex)
	}
}

func TestBatchedOutputMsgLines(t *testing.T) {
	msgs := drain(t, start(0, 1, shTask("i=0; while [ $i -lt 600 ]; do echo line$i; i=$((i+1)); done"), ""))
	outputs := 0
	for _, m := range msgs {
		if o, ok := m.(OutputMsg); ok {
			outputs++
			if len(o.lines) > maxBatch {
				t.Errorf("batch of %d lines exceeds %d", len(o.lines), maxBatch)
			}
		}
	}
	if got := len(outputText(msgs)); got != 600 {
		t.Fatalf("got %d lines, want 600", got)
	}
	if outputs >= 600 {
		t.Errorf("600 lines arrived in %d messages; output is not batched", outputs)
	}
}

func TestStartAppliesDirAndEnv(t *testing.T) {
	dir := t.TempDir()
	task := config.Task{Cmd: `pwd; echo "$DECK_X $FORCE_COLOR"`, Env: map[string]string{"DECK_X": "yes"}}
	got := outputText(drain(t, start(0, 1, task, dir)))
	if len(got) != 2 || !strings.HasSuffix(got[0], strings.TrimPrefix(dir, "/private")) || got[1] != "yes 1" {
		t.Errorf("output = %q, want the task dir and env applied", got)
	}
}

func TestColorEnvSet(t *testing.T) {
	env := taskEnv([]string{"PATH=/bin", "FORCE_COLOR=0"}, map[string]string{"A": "b"})
	for _, want := range []string{
		"PATH=/bin", "FORCE_COLOR=1", "CLICOLOR_FORCE=1",
		"DOTNET_SYSTEM_CONSOLE_ALLOW_ANSI_COLOR_REDIRECTION=1", "A=b",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("env %q missing %q", env, want)
		}
	}
	if slices.Contains(env, "FORCE_COLOR=0") {
		t.Error("the inherited FORCE_COLOR should be overridden")
	}
}

func TestTaskDir(t *testing.T) {
	cases := []struct{ root, dir, want string }{
		{"/r", "", "/r"},
		{"/r", "src/api", "/r/src/api"},
		{"/r", "/abs", "/abs"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := taskDir(c.root, c.dir); got != c.want {
			t.Errorf("taskDir(%q,%q) = %q, want %q", c.root, c.dir, got, c.want)
		}
	}
}

func TestStopKillsPipeline(t *testing.T) {
	marker := "sleep " + uniqueSleep()
	p := start(0, 1, shTask(marker+" | cat"), "")
	deadline := time.Now().Add(2 * time.Second)
	for pgrepCount(t, marker) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the pipeline never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	began := time.Now()
	p.stop()
	msgs := drain(t, p)
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("stop took %v, want < 5s", took)
	}
	if ex := lastExit(t, msgs); ex.err == nil {
		t.Errorf("a stopped task should report an error exit, got %+v", ex)
	}
	waitGone(t, marker)
}

func TestStopEscalatesToKill(t *testing.T) {
	old := killDelay
	killDelay = 200 * time.Millisecond
	t.Cleanup(func() { killDelay = old })
	marker := "sleep " + uniqueSleep()
	p := start(0, 1, shTask(`trap "" TERM; `+marker+` & trap "" TERM; wait`), "")
	time.Sleep(200 * time.Millisecond)
	p.stop()
	if !p.wait(5 * time.Second) {
		t.Fatal("the group should be killed after killDelay")
	}
	waitGone(t, marker)
}

func TestSplitChunks(t *testing.T) {
	cases := []struct {
		in   string
		want []chunk
	}{
		{"a\nb\n", []chunk{{text: "a"}, {text: "b"}}},
		{"a\r\nb", []chunk{{text: "a"}, {text: "b"}}},
		{"10%\r20%\r", []chunk{{text: "10%", cr: true}, {text: "20%", cr: true}}},
		{"x\r\ny\r", []chunk{{text: "x"}, {text: "y", cr: true}}},
	}
	for _, c := range cases {
		if got := readChunks(strings.NewReader(c.in)); !slices.Equal(got, c.want) {
			t.Errorf("chunks(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func readChunks(r interface{ Read([]byte) (int, error) }) []chunk {
	ch := make(chan chunk, 64)
	go func() {
		scanChunks(r, func(c chunk) { ch <- c })
		close(ch)
	}()
	var out []chunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}
