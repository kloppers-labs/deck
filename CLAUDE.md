# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What deck is

A Bubble Tea TUI "command center" with four tabs — **Files**, **Git**, **Run**, **HTTP** — over the current working directory. `main.go` boots the root model in `internal/app`; everything else lives under `internal/`.

Deck does not call any AI API directly. The only AI feature is the Git tab's `ctrl+g` commit-message helper (`internal/aicommit`), which shells out to the user's already-authenticated `claude`, `codex`, or `crush` CLI (`mods` is a deprecated alias for `crush`). Editing is delegated to `micro` (configurable) via `tea.ExecProcess`, which suspends the TUI for the duration of the child process.

## Common commands

```bash
go build ./...                       # build
go test ./...                        # run all tests
go test ./internal/git -run TestX    # single test
go test -race ./...                  # race detector
go run .                             # launch the TUI
```

User config lives at `~/.config/deck/config.toml` (see `internal/config/config.go`). Defaults: `ai_backend="claude"`, `ai_model="sonnet"` (both used only by the commit helper), `editor="micro"` (the editor string may carry args; the file path is appended last). `tasks` is a list of `{name, cmd}` for the Run tab; if empty, `runner.detect()` infers tasks from marker files (`go.mod`, `package.json` scripts, `angular.json`, `*.sln`/`*.csproj`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `Makefile`/`justfile` targets).

## Architecture

### Root model owns the tabs (`internal/app/app.go`)

`Model` holds one value of each pane type and an `active tab`. `Update` does three jobs:

1. **Global keys** (`globalKey`): tab/shift-tab cycle panes; `1`–`4` jump to a tab; `q`/`ctrl+c` quit. These are gated by `m.capturing()` — if the active pane reports it owns the keyboard (text input focused), global single-letter shortcuts are suppressed so typing works. `ctrl+c` and tab navigation always win.
2. **Cross-pane messages** routed by type: `files.OpenMsg` launches the editor; `git.GenCommitMsgRequest` runs `aicommit.Generate` against the staged diff (90 s timeout, cancellable through the request's `Ctx`) and replies with `CommitMsgReady{Text, Err}`; `editorClosedMsg` triggers `git.Refresh` so file changes show up.
3. **Async output is routed by type, not by `active`**: `git.RefreshMsg`/`CommitMsgReady`, `runner.OutputMsg`/`ExitMsg`, and `httpc.RespMsg` are dispatched to their owning pane regardless of which tab is focused, and `spinner.TickMsg` fans out to every pane with a spinner. This is what lets a running task keep streaming, and a git refresh land, while the user is on another tab. Anything else falls through to `routeToPane`, which only updates the active pane. (This type switch is temporary; the planned `ui.Routed` contract replaces it.)

When adding a new tab: add to the `tab` enum, `tabNames`, the `Model` struct, `New`, `resize`, `capturing`, `routeToPane`, `paneView`, and the global `globalKey` number switch — plus a footer hint string and a global-route case for any async messages it emits.

### Each pane is its own package

Panes are value types (so the root model can copy them in/out of `Update` cheaply). Each exports `New…`, `SetSize`, `Update(msg) (Self, tea.Cmd)`, `View`, and usually a `Capturing() bool`.

- `internal/files` — directory listing + Chroma-highlighted preview (markdown via glamour). Emits `OpenMsg`.
- `internal/git` — wraps the `git` CLI (`git.go`) and renders a change list + diff (`pane.go`). Staging, committing, branch switching, and `ctrl+g` to request an AI commit message: the pane shows a spinner while generating, `esc` cancels, and a failure keeps the typed text and reports the error in the status line. `NewPane(theme, commitAI)` hides the `ctrl+g` hint when the backend CLI is not on `PATH`.
- `internal/aicommit` — no UI. `BuildArgs` maps a backend to its CLI invocation; `Generate(ctx, backend, model, diff)` pipes the diff (capped at 60 KB on a line boundary) to the CLI on **stdin**, never argv, captures stderr into the error, and strips Markdown fences; `Available` checks `PATH`. Tests swap the package-level `execCommand`/`lookPath` seams for a helper process.
- `internal/runner` — Run tab. Each task spawns under `sh -c`, merges stdout+stderr through an `io.Pipe`, and streams `OutputMsg`s tagged with the task index, ending in `ExitMsg`. Tasks are cancellable via stored `context.CancelFunc`.
- `internal/httpc` — minimal REST client. `httpc.go` holds pure helpers (request building, header parsing, Chroma highlighting) so it's unit-testable without a `tea.Program`; `pane.go` is the UI.
- `internal/ui` — shared lipgloss styles and the global keymap (`keys.go`).
- `internal/config` — TOML loader with field-level defaults.
- `internal/lsp` — in-house LSP client, standard library only (no Bubble Tea, no other `deck/internal` import; `TestImportsStandardLibraryOnly` enforces it). `conn.go` is JSON-RPC with Content-Length framing; `registry.go` holds `DefaultSpecs()` (csharp-ls, gopls, typescript-language-server), `MergeSpecs` for config overrides, `FindRoot` (markers in priority order, walking up to the git top level / `$HOME`, never `/`) and `LookupBinary` (`PATH`, then `~/.dotnet/tools`, `~/go/bin`, `$GOPATH/bin`, Homebrew). `Manager.ClientFor(ctx, path)` lazily starts one `Client` per (server, root), and routes virtual URIs (csharp-ls `csharp:/…` metadata, read through `Client.Metadata`) to an already-running client; `Prestart(ctx, cwd)` eagerly starts only servers with `PrestartMarkers` (csharp-ls when a `.sln`/`.slnx` is found). A missing binary fails once and is never retried; a crashed server restarts once. Clients track Starting → Indexing → Ready from `$/progress` (or 2 s without progress), keep a 30-document didOpen LRU, and resync on mtime change or `Reload`. `Events()` carries `StatusEvent`/`DiagnosticsEvent`/`LogEvent`/`ShowMessageEvent` through a 256-entry queue that drops logs first. `Shutdown(ctx)` sends shutdown/exit, then kills the process group (servers start with `Setpgid`). Columns stay in the server's negotiated encoding; convert with `RuneToUnits`/`UnitsToRune`/`ByteToUnits`/`UnitsToByte`. Tests drive `lsptest`, an in-process fake server over `net.Pipe`, through the injectable `Starter`; `integration_test.go` runs real gopls when installed.

### Streaming pattern (used by `runner`)

Goroutine produces messages onto a buffered `chan tea.Msg`; a method like `.Next()` returns a `tea.Cmd` that blocks on the channel. After handling each `OutputMsg`, the pane returns `proc.next()` again, so Bubble Tea keeps a single in-flight read pending until the channel closes. `cancel()` ends the goroutine cleanly via `exec.CommandContext`.

### Testing notes

Tests sit next to the code they cover. The `aicommit`, `httpc`, and `git` packages have substantial unit coverage of their pure helpers (`BuildArgs`/`capDiff`/`stripFences`, `parseHeaders`/`buildRequest`/`cycleMethod`, `ParseStatus`). `internal/git/integration_test.go` exercises the real `git` CLI against a temp repo — needs `git` on `PATH`. Run `go test -race ./internal/lsp/...` after touching `internal/lsp`. Prefer extending those pure helpers and testing them directly over driving the Bubble Tea `Program`.
