package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/launch"
)

const startTimeout = 10 * time.Second

func launchDeck(dir, mode string) error {
	if mode == "" {
		cfg, _ := config.Load()
		mode = cfg.Agent.Launch
	}
	t := launch.Detect(os.Getenv, runtime.GOOS)
	if mode == "off" {
		return errors.New(`starting deck automatically is off (agent.launch = "off"); ` + launch.Hint(t, dir))
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argv, err := launch.Command(t, launch.Mode(mode), dir, exe)
	if errors.Is(err, launch.ErrNoRoute) {
		return errors.New(launch.Hint(t, dir))
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	if err := launch.Start(ctx, argv); err != nil {
		return fmt.Errorf("%v\n%s", err, launch.Hint(t, dir))
	}
	return nil
}

func launchCommand(f agentFlags, rest []string, cwd string, env agentEnv, stdout, stderr io.Writer) int {
	dir := gitTop(cwd)
	if len(rest) > 0 {
		dir = rest[0]
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		dir = filepath.Clean(dir)
	}
	mode := ""
	switch {
	case f.split:
		mode = "split"
	case f.tab:
		mode = "tab"
	case f.window:
		mode = "window"
	}
	dirs := control.Dirs(env.Getenv, env.Home)
	if inst, ok := control.Pick(control.ListAll(dirs), 0, dir, dir); ok && inst.Root == dir {
		fmt.Fprintf(stdout, "deck is already running for %s (pid %d)\n", dir, inst.PID)
		return 0
	}
	if env.Launch == nil {
		fmt.Fprintln(stderr, "deck: cannot start deck from here")
		return exitNoDeck
	}
	if err := env.Launch(dir, mode); err != nil {
		fmt.Fprintln(stderr, "deck:", err)
		return exitNoDeck
	}
	if _, ok := waitForDeck(env, dirs, dir, dir); !ok {
		fmt.Fprintln(stderr, "deck: started deck but it did not come up within", launchTimeout)
		return exitNoDeck
	}
	fmt.Fprintln(stdout, "deck launch: started deck for "+dir)
	return 0
}

func waitForDeck(env agentEnv, dirs []string, root, target string) (control.Instance, bool) {
	deadline := env.Now().Add(launchTimeout)
	for {
		if inst, ok := control.Pick(control.ListAll(dirs), 0, root, target); ok {
			return inst, true
		}
		if !env.Now().Before(deadline) {
			return control.Instance{}, false
		}
		time.Sleep(launchPoll)
	}
}
