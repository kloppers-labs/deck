package runner

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/config"
)

// fakeStart replaces startProc with an inert fake, returning the recorded
// generations of each launch. Restored automatically when the test ends.
func fakeStart(t *testing.T) *[]int {
	t.Helper()
	var gens []int
	orig := startProc
	startProc = func(idx, gen int, command string) *proc {
		gens = append(gens, gen)
		return &proc{ch: make(chan tea.Msg, 8), cancel: func() {}}
	}
	t.Cleanup(func() { startProc = orig })
	return &gens
}

func testPane() Pane {
	p := NewPane([]config.Task{{Name: "dev", Cmd: "sleep 999"}}, "")
	p.SetSize(80, 24)
	return p
}

func TestStaleGenerationDropped(t *testing.T) {
	fakeStart(t)
	p := testPane()
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter}) // run → gen 1
	p, _ = p.Update(OutputMsg{idx: 0, gen: 99, text: "stale\n"})
	if got := p.jobs[0].out.String(); strings.Contains(got, "stale") {
		t.Errorf("stale-generation output was appended: %q", got)
	}
	p, _ = p.Update(ExitMsg{idx: 0, gen: 99})
	if p.jobs[0].state != stateRunning {
		t.Errorf("stale exit changed state to %v, want still running", p.jobs[0].state)
	}
}

func TestRestartQueuesAndRelaunches(t *testing.T) {
	gens := fakeStart(t)
	p := testPane()
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter}) // gen 1
	p, _ = p.Update(OutputMsg{idx: 0, gen: 1, text: "old run\n"})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	j := p.jobs[0]
	if !j.pendingRestart {
		t.Fatal("R while running should queue a restart")
	}
	// The cancelled process winds down and exits; the restart fires here.
	p, _ = p.Update(ExitMsg{idx: 0, gen: 1, err: errors.New("killed")})
	if j.state != stateRunning {
		t.Errorf("after queued restart state = %v, want running", j.state)
	}
	if j.gen != 2 {
		t.Errorf("gen = %d, want 2", j.gen)
	}
	if out := j.out.String(); strings.Contains(out, "old run") {
		t.Errorf("restart should clear the previous run's log, got %q", out)
	}
	if want := []int{1, 2}; len(*gens) != 2 || (*gens)[0] != want[0] || (*gens)[1] != want[1] {
		t.Errorf("startProc launches = %v, want %v", *gens, want)
	}
}

func TestExitRecordsDuration(t *testing.T) {
	fakeStart(t)
	p := testPane()
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p, _ = p.Update(ExitMsg{idx: 0, gen: 1})
	j := p.jobs[0]
	if j.state != stateDone {
		t.Errorf("state = %v, want done", j.state)
	}
	if j.finishedAt.IsZero() {
		t.Error("finishedAt should be set on exit")
	}
	if j.elapsed() == "" {
		t.Error("finished job should report a total duration")
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
