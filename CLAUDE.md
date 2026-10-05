# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What deck is

A Bubble Tea TUI "command center" with five tabs — **Files**, **Git**, **AI**, **Run**, **HTTP** — over the current working directory. `main.go` boots the root model in `internal/app`; everything else lives under `internal/`.

Deck does not call any AI API directly. The AI pane shells out to the user's already-authenticated `claude`, `codex`, or `mods` CLIs (`internal/ai/ai.go:BuildArgs`). Editing is delegated to `micro` (configurable) via `tea.ExecProcess`, which suspends the TUI for the duration of the child process.

## Common commands

```bash
go build ./...                       # build
go test ./...                        # run all tests
go test ./internal/git -run TestX    # single test
go test -race ./...                  # race detector
go run .                             # launch the TUI
```

User config lives at `~/.config/deck/config.toml` (see `internal/config/config.go`). Defaults: `ai_backend="claude"`, `ai_model="sonnet"`, `editor="micro"` (the editor string may carry args; the file path is appended last). `tasks` is a list of `{name, cmd}` for the Run tab; if empty, `runner.detect()` infers tasks from marker files (`go.mod`, `package.json` scripts, `angular.json`, `*.sln`/`*.csproj`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `Makefile`/`justfile` targets).

## Architecture

### Root model owns the tabs (`internal/app/app.go`)

`Model` holds one value of each pane type and an `active tab`. `Update` does three jobs:

1. **Global keys** (`globalKey`): tab/shift-tab cycle panes; `1`–`5` jump to a tab; `q`/`ctrl+c` quit. These are gated by `m.capturing()` — if the active pane reports it owns the keyboard (text input focused), global single-letter shortcuts are suppressed so typing works. `ctrl+c` and tab navigation always win.
2. **Cross-pane messages** routed by type: `files.OpenMsg` launches the editor; `files.SendToAIMsg`/`SendTestsMsg` switch to the AI tab and seed a prompt; `git.GenCommitMsgRequest` runs a one-shot AI call against the staged diff and replies with `CommitMsgReady`; `editorClosedMsg` triggers `git.Refresh` so file changes show up.
3. **Async pane output is routed by type, not by `active`**: `ai.ChunkMsg`/`DoneMsg`, `runner.OutputMsg`/`ExitMsg`, and `httpc.RespMsg` are dispatched to their owning pane regardless of which tab is focused. This is what lets a running task or in-flight AI request keep streaming while the user is on another tab. Anything else falls through to `routeToPane`, which only updates the active pane.

When adding a new tab: add to the `tab` enum, `tabNames`, the `Model` struct, `New`, `resize`, `capturing`, `routeToPane`, `paneView`, and the global `globalKey` number switch — plus a footer hint string and a global-route case for any async messages it emits.

### Each pane is its own package

Panes are value types (so the root model can copy them in/out of `Update` cheaply). Each exports `New…`, `SetSize`, `Update(msg) (Self, tea.Cmd)`, `View`, and usually a `Capturing() bool`.

- `internal/files` — directory listing + Chroma-highlighted preview (markdown via glamour). Emits `OpenMsg` / `SendToAIMsg` / `SendTestsMsg`.
- `internal/git` — wraps the `git` CLI (`git.go`) and renders a change list + diff (`pane.go`). Staging, committing, branch switching, and `ctrl+g` to request an AI commit message.
- `internal/ai` — `ai.go` is the CLI adapter (pure, testable); `pane.go` is the Bubble Tea UI. `Start()` runs a backend in a goroutine, pipes stdout line-by-line into a buffered `chan tea.Msg`, and surfaces `ChunkMsg`/`DoneMsg`. `Run()` is the one-shot variant used by commit-message generation.
- `internal/runner` — Run tab. Each task spawns under `sh -c`, merges stdout+stderr through an `io.Pipe`, and streams `OutputMsg`s tagged with the task index, ending in `ExitMsg`. Tasks are cancellable via stored `context.CancelFunc`.
- `internal/httpc` — minimal REST client. `httpc.go` holds pure helpers (request building, header parsing, Chroma highlighting) so it's unit-testable without a `tea.Program`; `pane.go` is the UI.
- `internal/ui` — shared lipgloss styles and the global keymap (`keys.go`).
- `internal/config` — TOML loader with field-level defaults.

### Streaming pattern (used by `ai` and `runner`)

Goroutine produces messages onto a buffered `chan tea.Msg`; a method like `.Next()` returns a `tea.Cmd` that blocks on the channel. After handling each `ChunkMsg`/`OutputMsg`, the pane returns `stream.Next()` (or `proc.next()`) again, so Bubble Tea keeps a single in-flight read pending until the channel closes. `cancel()` ends the goroutine cleanly via `exec.CommandContext`.

### Testing notes

Tests sit next to the code they cover. The `ai`, `httpc`, and `git` packages have substantial unit coverage of their pure helpers (`BuildArgs`, `parseHeaders`/`buildRequest`/`cycleMethod`, `ParseStatus`). `internal/git/integration_test.go` exercises the real `git` CLI against a temp repo — needs `git` on `PATH`. Prefer extending those pure helpers and testing them directly over driving the Bubble Tea `Program`.
