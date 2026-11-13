package app

import (
	"context"
	"errors"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/files"
	"github.com/kloppers-labs/deck/internal/git"
	"github.com/kloppers-labs/deck/internal/ui"
)

const (
	slowWatch          = 10 * time.Second
	slowStatus         = time.Second
	fingerprintTimeout = 10 * time.Second
	missingFile        = -1
)

var fingerprint = git.Fingerprint

type watchState struct {
	every    time.Duration
	path     string
	stamp    int64
	fp       git.TreePrint
	inFlight bool
	slow     bool
	treeOff  bool
}

type watchTickMsg struct{ n int }

type fileStampMsg struct {
	path  string
	stamp int64
}

type treePrintMsg struct {
	fp   git.TreePrint
	took time.Duration
	err  error
}

func (m Model) watchCmd(n int) tea.Cmd {
	if !m.cfg.Agent.Enabled || !m.cfg.Agent.Watch {
		return nil
	}
	every := m.watch.every
	if every <= 0 {
		every = time.Duration(m.cfg.Agent.WatchMS) * time.Millisecond
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return watchTickMsg{n: n} })
}

func (m Model) watchMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case watchTickMsg:
		return m.watchTick(msg)
	case fileStampMsg:
		return m.fileStamp(msg)
	case treePrintMsg:
		return m.treePrint(msg)
	}
	return m, nil
}

func (m Model) watchTick(msg watchTickMsg) (Model, tea.Cmd) {
	cmds := []tea.Cmd{m.watchCmd(msg.n + 1)}
	if path := m.files.ShownFile(); path != "" {
		cmds = append(cmds, statCmd(path))
	}
	if msg.n%2 == 0 && !m.watch.inFlight && !m.watch.treeOff {
		m.watch.inFlight = true
		cmds = append(cmds, fingerprintCmd(m.ed.cwd))
	}
	return m, tea.Batch(cmds...)
}

func statCmd(path string) tea.Cmd {
	return func() tea.Msg {
		fi, err := os.Stat(path)
		if err != nil {
			return fileStampMsg{path: path, stamp: missingFile}
		}
		return fileStampMsg{path: path, stamp: fi.ModTime().UnixNano() ^ fi.Size()}
	}
}

func fingerprintCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fingerprintTimeout)
		defer cancel()
		start := time.Now()
		fp, err := fingerprint(ctx, dir)
		return treePrintMsg{fp: fp, took: time.Since(start), err: err}
	}
}

func (m Model) fileStamp(msg fileStampMsg) (Model, tea.Cmd) {
	if msg.path != m.watch.path {
		m.watch.path, m.watch.stamp = msg.path, msg.stamp
		return m, nil
	}
	if msg.stamp == m.watch.stamp {
		return m, nil
	}
	m.watch.stamp = msg.stamp
	if msg.path != m.files.ShownFile() {
		return m, nil
	}
	return m, func() tea.Msg { return files.ReloadFileMsg{} }
}

func (m Model) treePrint(msg treePrintMsg) (Model, tea.Cmd) {
	m.watch.inFlight = false
	if errors.Is(msg.err, git.ErrNotRepo) {
		m.watch.treeOff = true
		return m, nil
	}
	var cmds []tea.Cmd
	if msg.took > slowStatus && !m.watch.slow {
		m.watch.slow, m.watch.every = true, slowWatch
		var toast tea.Cmd
		m, toast = m.addToast(ui.ToastMsg{Level: ui.LevelWarn, Text: "git status is slow here; checking every 10s"})
		cmds = append(cmds, toast)
	}
	if msg.err != nil || msg.fp == m.watch.fp {
		return m, tea.Batch(cmds...)
	}
	prev := m.watch.fp
	m.watch.fp = msg.fp
	if prev == (git.TreePrint{}) {
		return m, tea.Batch(cmds...)
	}
	cmds = append(cmds, git.Refresh)
	if msg.fp.Status != prev.Status {
		cmds = append(cmds, func() tea.Msg { return files.RefreshTreeMsg{} })
	}
	return m, tea.Batch(cmds...)
}
