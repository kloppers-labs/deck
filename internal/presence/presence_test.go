package presence

import (
	"sync"
	"testing"
)

func TestPublishBumpsSeqOnlyOnChange(t *testing.T) {
	s := New()
	snap := Snapshot{Tab: "files", File: "/r/a.go", Line: 1, Col: 1}
	if !s.Publish(snap) {
		t.Fatal("first publish should change the store")
	}
	if s.Publish(snap) {
		t.Error("publishing the same snapshot should not change the store")
	}
	if got := s.Get().Seq; got != 1 {
		t.Errorf("Seq = %d, want 1", got)
	}
	snap.Line = 2
	snap.Seq = 99
	s.Publish(snap)
	if got := s.Get(); got.Seq != 2 || got.Line != 2 {
		t.Errorf("after a change: %+v, want Seq 2 and Line 2", got)
	}
}

func TestSubscribeCoalesces(t *testing.T) {
	s := New()
	ch, stop := s.Subscribe()
	defer stop()
	for i := 1; i <= 5; i++ {
		s.Publish(Snapshot{Line: i})
	}
	select {
	case seq := <-ch:
		if seq != 5 {
			t.Errorf("seq = %d, want the latest 5", seq)
		}
	default:
		t.Fatal("a subscriber should be notified")
	}
	select {
	case seq := <-ch:
		t.Errorf("unexpected second notification %d", seq)
	default:
	}
}

func TestUnsubscribeStops(t *testing.T) {
	s := New()
	ch, stop := s.Subscribe()
	stop()
	s.Publish(Snapshot{Line: 1})
	select {
	case seq, ok := <-ch:
		if ok {
			t.Errorf("unsubscribed channel got %d", seq)
		}
	default:
	}
}

func TestGetIsDeepCopy(t *testing.T) {
	s := New()
	s.Publish(Snapshot{
		Recent: []string{"/r/a.go"},
		Sel:    &Range{StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 3},
		Git:    &GitSpot{Mode: "review"},
	})
	got := s.Get()
	got.Recent[0] = "x"
	got.Sel.EndLine = 9
	got.Git.Mode = "log"
	again := s.Get()
	if again.Recent[0] != "/r/a.go" || again.Sel.EndLine != 2 || again.Git.Mode != "review" {
		t.Errorf("store changed through a returned snapshot: %+v", again)
	}
}

func TestDiagnosticsByPath(t *testing.T) {
	s := New()
	d := []Diagnostic{{Line: 3, Col: 1, EndLine: 3, EndCol: 4, Severity: "error", Message: "boom", Source: "gopls"}}
	s.SetDiagnostics("/r/a.go", d)
	s.SetDiagnostics("/r/b.go", nil)
	if got := s.Diagnostics("/r/a.go"); len(got) != 1 || len(got["/r/a.go"]) != 1 || got["/r/a.go"][0].Message != "boom" {
		t.Errorf("Diagnostics(a) = %+v", got)
	}
	if got := s.Diagnostics(""); len(got) != 1 {
		t.Errorf("Diagnostics(all) = %+v, want only files with diagnostics", got)
	}
}

func TestConcurrentPublishGet(t *testing.T) {
	s := New()
	ch, stop := s.Subscribe()
	defer stop()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := range 100 {
				s.Publish(Snapshot{Line: i*100 + j, Recent: []string{"a"}})
			}
		}()
		go func() {
			defer wg.Done()
			for range 100 {
				_ = s.Get()
				s.SetDiagnostics("/a", []Diagnostic{{Line: 1}})
				_ = s.Diagnostics("")
			}
		}()
	}
	go func() {
		for range ch {
		}
	}()
	wg.Wait()
}
