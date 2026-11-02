package main

import (
	"syscall"
	"testing"
	"time"
)

type fakeProgram struct{ quit chan struct{} }

func (f fakeProgram) Quit() { close(f.quit) }

func TestHangupQuitsGracefully(t *testing.T) {
	p := fakeProgram{quit: make(chan struct{})}
	stop := quitOnHangup(p)
	defer stop()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.quit:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGHUP should ask the program to quit so tasks and servers are stopped")
	}
}
