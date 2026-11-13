package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/files"
	"github.com/kloppers-labs/deck/internal/git"
)

func tickAt(t *testing.T, m Model, n int) Model {
	t.Helper()
	return drive(t, m, watchTickMsg{n: n})
}

func touchLater(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func TestWatchReloadsChangedOpenFile(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	path := filepath.Join(dir, "a.go")
	m = openFile(t, m, path, 2)
	m = tickAt(t, m, 1)
	touchLater(t, path, "first\nsecond changed\nthird\n")
	m = tickAt(t, m, 1)
	if !strings.Contains(ansi.Strip(m.View()), "second changed") {
		t.Error("the open file should reload after it changed on disk")
	}
	if got := m.files.ViewerLoc().Line; got != 1 {
		t.Errorf("cursor line = %d, want it kept at 1", got)
	}
}

func TestOpenFileDeletedShowsNote(t *testing.T) {
	m, dir := agentFixture(t, config.Default())
	path := filepath.Join(dir, "a.go")
	m = openFile(t, m, path, 1)
	m = tickAt(t, m, 1)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m = tickAt(t, m, 1)
	if !strings.Contains(m.View(), "file deleted") {
		t.Error("a deleted open file should say so")
	}
}

func withFingerprints(t *testing.T, fps ...git.TreePrint) {
	t.Helper()
	prev := fingerprint
	i := 0
	fingerprint = func(context.Context, string) (git.TreePrint, error) {
		fp := fps[min(i, len(fps)-1)]
		i++
		return fp, nil
	}
	t.Cleanup(func() { fingerprint = prev })
}

func refreshes(m Model, msg treePrintMsg) (Model, bool) {
	next, cmd := m.Update(msg)
	got := false
	for _, out := range runQuick(cmd) {
		switch out.(type) {
		case git.RefreshMsg:
			got = true
		case files.ReloadMsg:
			panic("the watcher must not send files.ReloadMsg: it re-opens the tree selection over the viewer")
		}
	}
	return next.(Model), got
}

func TestWatchStatusChangeRefreshesTreeContentOnlyDoesNot(t *testing.T) {
	m, _ := agentFixture(t, config.Default())
	m.watch.fp = git.TreePrint{Status: "s1", Content: "c1"}
	next, cmd := m.Update(treePrintMsg{fp: git.TreePrint{Status: "s1", Content: "c2"}})
	m = next.(Model)
	var tree, refresh bool
	for _, out := range runQuick(cmd) {
		switch out.(type) {
		case files.RefreshTreeMsg:
			tree = true
		case git.RefreshMsg:
			refresh = true
		}
	}
	if !refresh || tree {
		t.Errorf("a content-only change: git refresh %v, tree refresh %v; want git only", refresh, tree)
	}
	_, cmd = m.Update(treePrintMsg{fp: git.TreePrint{Status: "s2", Content: "c2"}})
	tree = false
	for _, out := range runQuick(cmd) {
		if _, ok := out.(files.RefreshTreeMsg); ok {
			tree = true
		}
	}
	if !tree {
		t.Error("a status change should refresh the tree")
	}
}

func TestWatchOffWhenAgentDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Enabled = false
	m, _ := agentFixture(t, cfg)
	if m.watchCmd(1) != nil {
		t.Error("agent.enabled = false should turn polling off too")
	}
}

func TestWatchFingerprintChangeRefreshes(t *testing.T) {
	withFingerprints(t, git.TreePrint{Status: "a"}, git.TreePrint{Status: "b"})
	m, _ := agentFixture(t, config.Default())
	m = tickAt(t, m, 2)
	if m.watch.fp.Status != "a" {
		t.Fatalf("baseline fingerprint = %q, want a", m.watch.fp)
	}
	next, cmd := m.Update(watchTickMsg{n: 4})
	m = next.(Model)
	var got bool
	for _, out := range runQuick(cmd) {
		if tp, ok := out.(treePrintMsg); ok {
			m, got = refreshes(m, tp)
		}
	}
	if !got {
		t.Error("a changed fingerprint should refresh git")
	}
}

func TestWatchNoChangeNoRefresh(t *testing.T) {
	m, _ := agentFixture(t, config.Default())
	m.watch.fp = git.TreePrint{Status: "same", Content: "same"}
	if _, got := refreshes(m, treePrintMsg{fp: git.TreePrint{Status: "same", Content: "same"}}); got {
		t.Error("an unchanged fingerprint should not refresh")
	}
}

func TestWatchSkipsWhileInFlight(t *testing.T) {
	withFingerprints(t, git.TreePrint{Status: "a"})
	m, _ := agentFixture(t, config.Default())
	m.watch.inFlight = true
	_, cmd := m.Update(watchTickMsg{n: 2})
	for _, out := range runQuick(cmd) {
		if _, ok := out.(treePrintMsg); ok {
			t.Error("a second fingerprint started while one was in flight")
		}
	}
}

func TestWatchBackoffOnSlowStatus(t *testing.T) {
	m, _ := agentFixture(t, config.Default())
	m.watch.fp = git.TreePrint{Status: "a"}
	m = drive(t, m, treePrintMsg{fp: git.TreePrint{Status: "a"}, took: 2 * time.Second})
	if m.watch.every != slowWatch {
		t.Errorf("interval = %v, want %v", m.watch.every, slowWatch)
	}
	m = drive(t, m, treePrintMsg{fp: git.TreePrint{Status: "a"}, took: 2 * time.Second})
	if n := strings.Count(m.View(), "git status is slow"); n != 1 {
		t.Errorf("slow-status toast shown %d times, want once", n)
	}
	var count int
	for _, ts := range m.toasts {
		if strings.Contains(ts.msg.Text, "git status is slow") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("slow-status toasts queued = %d, want 1", count)
	}
}

func TestWatchDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Watch = false
	m, _ := agentFixture(t, cfg)
	if m.watchCmd(1) != nil {
		t.Error("agent.watch = false should not poll")
	}
	if m, _ := agentFixture(t, config.Default()); m.watchCmd(1) == nil {
		t.Error("polling is on by default")
	}
}
