package ai

import (
	"errors"
	"strings"
	"testing"
)

// TestPaneUpdateSurvivesValueCopies guards against the strings.Builder-copied-
// by-value panic: Bubble Tea calls the value-receiver Update repeatedly,
// copying the Pane each time, so accumulated output must use a copy-safe type.
// Reassigning p on every step mirrors the real event loop.
func TestPaneUpdateSurvivesValueCopies(t *testing.T) {
	p := NewPane("claude", "sonnet")
	p, _ = p.Update(ChunkMsg{Text: "hello "})
	p, _ = p.Update(ChunkMsg{Text: "world"})
	p, _ = p.Update(DoneMsg{Err: errors.New("boom"), Stderr: "stderr detail"})

	if !strings.Contains(p.content, "hello world") {
		t.Errorf("chunks should accumulate, got %q", p.content)
	}
	if !strings.Contains(p.content, "boom") {
		t.Errorf("error should append to content, got %q", p.content)
	}
	if p.busy {
		t.Error("pane should not be busy after DoneMsg")
	}
}
