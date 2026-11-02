package lsp

import (
	"strings"
	"sync"
	"testing"
)

func TestRingKeepsTail(t *testing.T) {
	r := NewRing(8)
	if _, err := r.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if got := r.String(); got != "hello" {
		t.Fatalf("got %q", got)
	}
	_, _ = r.Write([]byte(" world"))
	if got := r.String(); got != "lo world" {
		t.Fatalf("got %q, want %q", got, "lo world")
	}
	n, _ := r.Write([]byte("0123456789abc"))
	if n != 13 {
		t.Fatalf("Write returned %d, want full length", n)
	}
	if got := r.String(); got != "56789abc" {
		t.Fatalf("got %q, want 56789abc", got)
	}
}

func TestRingConcurrentWrites(t *testing.T) {
	r := NewRing(1024)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_, _ = r.Write([]byte("line\n"))
			}
		}()
	}
	wg.Wait()
	got := r.String()
	if len(got) != 1024 || strings.Trim(got, "line\n") != "" {
		t.Fatalf("len=%d", len(got))
	}
}

func TestEventsDropLogWhenFull(t *testing.T) {
	q := newEventQueue(4)
	for i := range 4 {
		q.push(LogEvent{Message: string(rune('a' + i))})
	}
	q.push(StatusEvent{Server: "go", State: StateReady})
	q.push(LogEvent{Message: "dropped"})
	q.push(DiagnosticsEvent{URI: "file:///a.go"})

	var got []string
	for {
		ev, ok := q.pop()
		if !ok {
			break
		}
		switch e := ev.(type) {
		case LogEvent:
			got = append(got, "log:"+e.Message)
		case StatusEvent:
			got = append(got, "status:"+e.Server)
		case DiagnosticsEvent:
			got = append(got, "diag")
		}
	}
	want := "log:c log:d status:go diag"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}

	for i := range 5 {
		q.push(StatusEvent{Server: string(rune('a' + i))})
	}
	if q.len() != 4 {
		t.Fatalf("len = %d, want 4", q.len())
	}
	if ev, _ := q.pop(); ev.(StatusEvent).Server != "b" {
		t.Fatalf("oldest kept = %+v, want b", ev)
	}
}
