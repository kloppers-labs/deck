package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentDefaults(t *testing.T) {
	d := Default()
	want := Agent{Enabled: true, Launch: "split", Watch: true, WatchMS: 1000, Claude: true}
	if d.Agent != want {
		t.Errorf("Agent = %+v, want %+v", d.Agent, want)
	}
	if d.Git.ReviewBase != "" {
		t.Errorf("ReviewBase = %q, want empty", d.Git.ReviewBase)
	}
}

func TestAgentLaunchEnumWarns(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "[agent]\nlaunch = \"pane\"\n")
	c, ws := load(path)
	if c.Agent.Launch != "split" {
		t.Errorf("Launch = %q, want split", c.Agent.Launch)
	}
	if len(ws) != 1 || !strings.Contains(ws[0].Message, "agent.launch") {
		t.Errorf("warnings = %q, want one about agent.launch", warningsText(ws))
	}
}

func TestWatchMSClamp(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "[agent]\nwatch_ms = 100\n")
	c, ws := load(path)
	if c.Agent.WatchMS != 250 {
		t.Errorf("WatchMS = %d, want 250", c.Agent.WatchMS)
	}
	if len(ws) != 1 || !strings.Contains(ws[0].Message, "agent.watch_ms") {
		t.Errorf("warnings = %q, want one about agent.watch_ms", warningsText(ws))
	}

	path = writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "[agent]\nwatch_ms = 0\n")
	c, ws = load(path)
	if c.Agent.WatchMS != 1000 || len(ws) != 0 {
		t.Errorf("WatchMS = %d, warnings %q; want the default and no warning", c.Agent.WatchMS, warningsText(ws))
	}
}

func TestAgentFalseSticks(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "[agent]\nclaude = false\nwatch = false\n")
	c, _ := load(path)
	if c.Agent.Claude || c.Agent.Watch {
		t.Errorf("Agent = %+v, want claude and watch off", c.Agent)
	}
	if !c.Agent.Enabled {
		t.Error("Enabled should keep its default")
	}
}

func TestUnknownAgentKeySuggests(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "[agent]\nlanuch = \"tab\"\n")
	_, ws := load(path)
	if text := warningsText(ws); !strings.Contains(text, `did you mean "agent.launch"`) {
		t.Errorf("warnings %q should suggest agent.launch", text)
	}
}

func TestReviewBaseDecodes(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "[git]\nreview_base = \"origin/develop\"\n")
	c, ws := load(path)
	if c.Git.ReviewBase != "origin/develop" || len(ws) != 0 {
		t.Errorf("ReviewBase = %q, warnings %q", c.Git.ReviewBase, warningsText(ws))
	}
}
