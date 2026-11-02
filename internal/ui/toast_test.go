package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestToastRoutesToApp(t *testing.T) {
	var r Routed = ToastMsg{}
	if r.RouteTo() != DestApp {
		t.Errorf("ToastMsg routes to %v, want DestApp", r.RouteTo())
	}
}

func TestToastCmdCarriesLevelAndText(t *testing.T) {
	msg, ok := Toast(LevelError, "boom")().(ToastMsg)
	if !ok || msg.Level != LevelError || msg.Text != "boom" {
		t.Errorf("Toast cmd produced %#v", msg)
	}
}

func TestToastLifetimeDefaults(t *testing.T) {
	if got := (ToastMsg{TTL: time.Second}).Lifetime(); got != time.Second {
		t.Errorf("explicit TTL = %v, want 1s", got)
	}
	info := ToastMsg{Level: LevelInfo}.Lifetime()
	errs := ToastMsg{Level: LevelError}.Lifetime()
	if info <= 0 || errs <= info {
		t.Errorf("default lifetimes info=%v error=%v, want errors to linger longer", info, errs)
	}
}

func TestToastRenderMarksLevel(t *testing.T) {
	if got := ansi.Strip(ToastMsg{Level: LevelError, Text: "x"}.Render()); !strings.Contains(got, "✗") {
		t.Errorf("error toast %q should carry ✗", got)
	}
	if got := ansi.Strip(ToastMsg{Level: LevelWarn, Text: "x"}.Render()); !strings.Contains(got, "!") {
		t.Errorf("warning toast %q should carry !", got)
	}
}
