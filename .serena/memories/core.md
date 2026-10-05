# Project: deck

Bubble Tea TUI "command center" over CWD with five tabs: Files, Git, AI, Run, HTTP.

## Source map

- `main.go` — boots `internal/app` root model.
- `internal/app/app.go` — root `Model`, tab enum, global key routing, cross-pane message routing.
- `internal/files/` — directory listing + Chroma/glamour preview. Emits `OpenMsg`, `SendToAIMsg`, `SendTestsMsg`.
- `internal/git/git.go` — pure `git` CLI wrapper; `internal/git/pane.go` — UI.
- `internal/ai/ai.go` — CLI adapter for `claude`/`codex`/`mods` (pure, testable, `BuildArgs`); `internal/ai/pane.go` — UI.
- `internal/runner/` — Run tab; `sh -c` per task, merged stdout+stderr stream.
- `internal/httpc/httpc.go` — pure REST helpers; `pane.go` — UI.
- `internal/ui/` — shared lipgloss styles (`styles.go`) and global keymap (`keys.go`).
- `internal/config/config.go` — TOML loader at `~/.config/deck/config.toml`.

## Invariants

- Deck never calls AI APIs directly — always shells out to user's authenticated CLIs.
- Editing is delegated to external editor (default `micro`) via `tea.ExecProcess` (suspends TUI).
- Panes are **value types**, not pointers — root model copies them in/out of `Update`. Pane `Update` returns `(Self, tea.Cmd)`.
- Each pane exposes: `New…`, `SetSize`, `Update`, `View`, usually `Capturing() bool`.
- `m.capturing()` gates global single-letter shortcuts so typing into focused text inputs works. `ctrl+c` and tab nav always win.
- Async streaming messages (`ai.ChunkMsg`/`DoneMsg`, `runner.OutputMsg`/`ExitMsg`, `httpc.RespMsg`) are routed by **type, not by active tab** — lets background work keep flowing while user switches tabs.

## Adding a new tab

Touch all of: `tab` enum, `tabNames`, `Model` struct, `New`, `resize`, `capturing`, `routeToPane`, `paneView`, `globalKey` number switch, footer hint, and a global-route case for any async msgs the pane emits.

## Streaming pattern (ai + runner)

Goroutine produces onto buffered `chan tea.Msg`; `.Next()`/`.next()` returns a `tea.Cmd` that blocks on the channel. After each chunk msg, pane returns `stream.Next()` again — single in-flight read until channel closes. Cancellation uses `exec.CommandContext`.

## Further memories

- `mem:tech_stack` — Go version, key deps.
- `mem:suggested_commands` — build/test/run commands.
- `mem:conventions` — Go style, testing approach, pure-helper split.
- `mem:task_completion` — what to run before declaring done.
