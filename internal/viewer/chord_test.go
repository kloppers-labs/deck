package viewer

import "testing"

func feed(c *Chord, keys ...string) (string, bool) {
	var action string
	var consumed bool
	for _, k := range keys {
		action, consumed = c.Feed(k)
	}
	return action, consumed
}

func TestChordSequences(t *testing.T) {
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"g", "g"}, "gg"},
		{[]string{"g", "d"}, "gd"},
		{[]string{"g", "i"}, "gi"},
		{[]string{"g", "y"}, "gy"},
		{[]string{"g", "r"}, "gr"},
		{[]string{"z", "z"}, "zz"},
		{[]string{"z", "t"}, "zt"},
		{[]string{"z", "b"}, "zb"},
		{[]string{"]", "d"}, "]d"},
		{[]string{"[", "d"}, "[d"},
	}
	for _, c := range cases {
		var ch Chord
		got, consumed := feed(&ch, c.keys...)
		if got != c.want || !consumed {
			t.Errorf("%v = %q (consumed %v), want %q", c.keys, got, consumed, c.want)
		}
		if ch.Pending() {
			t.Errorf("%v left the chord pending", c.keys)
		}
	}
}

func TestChordPendingAndAbort(t *testing.T) {
	var ch Chord
	if a, consumed := ch.Feed("g"); a != "" || !consumed || !ch.Pending() || ch.Prefix() != "g" {
		t.Fatalf("g should start a pending chord, got %q %v pending=%v", a, consumed, ch.Pending())
	}
	if a, consumed := ch.Feed("x"); a != "" || !consumed {
		t.Errorf("a non-matching key aborts and is swallowed, got %q %v", a, consumed)
	}
	if ch.Pending() {
		t.Error("abort should clear the pending prefix")
	}
	if _, consumed := ch.Feed("j"); consumed {
		t.Error("a plain key with no chord pending is not consumed")
	}
	ch.Feed("z")
	ch.Feed("esc")
	if ch.Pending() {
		t.Error("esc aborts a pending chord")
	}
}
