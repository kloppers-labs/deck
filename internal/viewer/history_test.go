package viewer

import "testing"

func loc(line int) Loc { return Loc{Path: "a.cs", Line: line} }

func TestHistoryBackForward(t *testing.T) {
	var h History
	h.Push(loc(1))
	h.Push(loc(2))
	if got, ok := h.Back(loc(3)); !ok || got != loc(2) {
		t.Fatalf("Back = %v %v, want line 2", got, ok)
	}
	if got, ok := h.Back(loc(2)); !ok || got != loc(1) {
		t.Fatalf("Back = %v %v, want line 1", got, ok)
	}
	if _, ok := h.Back(loc(1)); ok {
		t.Error("Back at the oldest entry should fail")
	}
	if got, ok := h.Forward(); !ok || got != loc(2) {
		t.Fatalf("Forward = %v %v, want line 2", got, ok)
	}
	if got, ok := h.Forward(); !ok || got != loc(3) {
		t.Fatalf("Forward = %v %v, want line 3 (where Back started)", got, ok)
	}
	if _, ok := h.Forward(); ok {
		t.Error("Forward at the newest entry should fail")
	}
}

func TestHistoryPushTruncatesForward(t *testing.T) {
	var h History
	h.Push(loc(1))
	h.Push(loc(2))
	h.Push(loc(3))
	h.Back(loc(4))
	h.Back(loc(3))
	h.Push(loc(9))
	if _, ok := h.Forward(); ok {
		t.Error("a push after going back drops the forward entries")
	}
	if got, ok := h.Back(loc(10)); !ok || got != loc(9) {
		t.Errorf("Back after push = %v %v, want line 9", got, ok)
	}
}

func TestHistoryDedupeAndMax(t *testing.T) {
	var h History
	h.Push(loc(1))
	h.Push(loc(1))
	if h.Len() != 1 {
		t.Errorf("consecutive duplicates kept: len = %d", h.Len())
	}
	for i := range 250 {
		h.Push(loc(i + 2))
	}
	if h.Len() != maxHistory {
		t.Errorf("len = %d, want %d", h.Len(), maxHistory)
	}
	if got, _ := h.Back(loc(999)); got != loc(251) {
		t.Errorf("newest entry = %v, want line 251", got)
	}
}
