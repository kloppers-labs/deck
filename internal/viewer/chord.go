package viewer

var chordPrefixes = map[string]bool{"g": true, "z": true, "]": true, "[": true}

var chordActions = map[string]bool{
	"gg": true, "gd": true, "gi": true, "gy": true, "gr": true,
	"zz": true, "zt": true, "zb": true,
	"]d": true, "[d": true,
}

// Chord is the prefix state machine for two-key commands. It has no timers: a
// prefix stays pending until the next key completes or aborts it.
type Chord struct{ prefix string }

func (c Chord) Pending() bool  { return c.prefix != "" }
func (c Chord) Prefix() string { return c.prefix }

// Feed returns the completed action, if any, and whether the key belonged to
// the chord. A key that does not complete a pending chord aborts it.
func (c *Chord) Feed(k string) (string, bool) {
	if c.prefix == "" {
		if chordPrefixes[k] {
			c.prefix = k
			return "", true
		}
		return "", false
	}
	seq := c.prefix + k
	c.prefix = ""
	if chordActions[seq] {
		return seq, true
	}
	return "", true
}
