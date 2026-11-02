# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What deck is

A Bubble Tea TUI "command center" with four tabs — **Files**, **Git**, **Run**, **HTTP** — over the current working directory. `main.go` loads config, maps `mouse` to program options (`hover` → all-motion, `click` → cell-motion, always `WithReportFocus`) and boots the root model in `internal/app`; everything else lives under `internal/`.

Deck does not call any AI API directly. The only AI feature is the Git tab's `ctrl+g` commit-message helper (`internal/aicommit`), which shells out to the user's already-authenticated `claude`, `codex`, or `crush` CLI (`mods` is a deprecated alias for `crush`). Editing is delegated to `micro` (configurable) via `tea.ExecProcess`, which suspends the TUI for the duration of the child process.

## Common commands

```bash
go build ./...                       # build
go test ./...                        # run all tests
go test ./internal/git -run TestX    # single test
go test -race ./...                  # race detector
go run .                             # launch the TUI
```

## Configuration (`internal/config`)

`config.Load() (Config, []Warning)` reads `$XDG_CONFIG_HOME/deck/config.toml` (falling back to `~/.config/deck/config.toml`), then a project-local `.deck.toml` found by walking up from the cwd to the directory holding `.git` (or the cwd itself). Later files override scalars and **append** `tasks`. The whole schema (spec §12) is defined up front — `Files`, `Viewer`, `LSP{Enabled, Prestart, RequestTimeoutMS, Servers}`, `Git{ConfirmCommit}`, `Task{Name, Cmd, Dir, Env, Kind}`, plus `mouse`, `editor_return`, `micro_bundle`, `micro_lsp` — so later work only reads config, it never adds fields.

- Defaults live only in `Default()`. Files are decoded on top of it, so omitted booleans keep their defaults and an explicit `false` sticks; afterwards zero strings/numbers and nil slices/maps are filled from `Default()`.
- Nothing is fatal: a decode error, an unknown key (with a "did you mean" suggestion from the schema) or an invalid enum value (`mouse`, `editor_return`, `micro_lsp`, `files.icons`) becomes a `Warning`. `main` passes the warnings to `app.New(cfg, warnings)`, and `Init` shows them as one toast.
- `lsp.servers` is empty by default; built-in server definitions belong to the lsp package. `LSPServer.Enabled` is a `*bool` so an omitted `enabled` means on.
- `tasks` empty → `runner.detect()` infers tasks from marker files (`go.mod`, `package.json` scripts, `angular.json`, `*.sln`/`*.csproj`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `Makefile`/`justfile` targets).

## Architecture

### Root model owns the chrome (`internal/app`)

`Model` holds one value of each pane type and an `active tab`, plus the toast queue, help-modal state and mouse state. `app.go` is the model and routing, `mouse.go` the mouse translation, `help.go` the help modal.

**Routing contract (`ui.Routed`).** Every async or cross-tab message implements `RouteTo() ui.Dest` (`DestApp`, `DestFiles`, `DestGit`, `DestRun`, `DestHTTP`). `app.Update` delivers any `ui.Routed` message to its destination whichever tab is active — today `git.RefreshMsg`/`CommitMsgReady`, `runner.OutputMsg`/`ExitMsg`, `httpc.RespMsg` and `ui.ToastMsg` (→ app). A new async message needs only a `RouteTo` method, never a new `case` in `app.Update`. Other app-level cases: `files.OpenMsg` launches the editor; `git.GenCommitMsgRequest` runs `aicommit.Generate` (90 s timeout, cancellable through `Ctx`); `editorClosedMsg` triggers `git.Refresh` (and an error toast if the editor failed); `spinner.TickMsg` fans out to every pane with a spinner. Keys, mouse events and everything else go to the active pane after global handling.

**Frame contract.** `SetSize(w, h)` is a pane's **outer** box, borders included, and `View()` must return exactly `h` rows of at most `w` columns — panes end `View` with `ui.Fit`. The body is `h - 5` (tab bar 3, status 1, footer 1) and `app.View` fits every section again as a backstop. Below 60×16 the frame is a centred `terminal too small` notice. `layout_test.go` checks every tab (with and without help) at 60×16, 80×24, 120×40 and 200×50 using long-name fixtures.

**Capture rules.** A pane's `Capturing()` is true only while text is being typed or a modal is open (filter, commit box, focused HTTP field, branch picker); reading, scrolling and selecting never capture. While capturing, single-letter globals (`q`, `?`, `1`–`4`) are suppressed and the footer shows `esc normal` instead of `q`/`?`. Capture-proof keys are checked first: `ctrl+c`, `tab`/`shift+tab`, `alt+1`…`alt+4`, `F1`, `alt+m`.

**Mouse.** `app.mouse` drops motion events on the same cell as the last one, handles tab-bar clicks with the pure `tabHitTest`, ignores the status and footer rows, and forwards body events to the active pane with `Y` reduced by the tab-bar height; containers keep translating into child-local coordinates (`ui.Rect.Local`). `alt+m` toggles reporting at runtime (`tea.DisableMouse` / re-enable per `cfg.Mouse`) and the footer shows `mouse off`. `ui.ClickTracker` turns presses into click counts (double-click = same cell within 400 ms).

**Toasts and status bar.** `ui.ToastMsg{Level, Text, TTL}` (or `ui.Toast(level, text)`) from anywhere shows on the right of the status bar; the newest live toast wins and each expires through a `tea.Tick`. The pure `statusLine(left, right, w)` lays out project · branch · dirty · running on the left and the toast on the right, truncating the left side first.

**Help.** `?`/`F1` opens a centred modal composited with `ui.Overlay` over the dimmed body (`ui.Dimmed`), grouped Global / active tab / Mouse and built from the same `key.Binding` values the panes export. It is modal (only `ctrl+c` escapes it), scrolls with `j`/`k`/wheel, filters with `/`, and closes with `esc`/`?`/`F1`/`q`.

When adding a new tab: add to the `tab` enum, `tabNames`, `tabDests`, the `Model` struct, `New`, `resize`, `capturing`, `routeTo`, `paneView`, `paneBindings`, the `globalKey` switches and `ui.Keys` (`N` and `alt+N`), plus a `ui.Dest` for it.

### Each pane is its own package

Panes are value types (so the root model can copy them in/out of `Update` cheaply). Each exports `New…`, `SetSize`, `Update(msg) (Self, tea.Cmd)`, `View`, and usually a `Capturing() bool`.

- `internal/files` — directory listing + Chroma-highlighted preview (markdown via glamour). Emits `OpenMsg`. The header shows the directory relative to the launch root, left-truncated. Mouse: wheel scrolls whichever column it is over (the preview by 3 lines), a click selects a row (`ListTop` is the first row's local Y) or focuses the preview, a double-click opens. `SelectedPath()` reports the highlighted entry.
- `internal/git` — wraps the `git` CLI (`git.go`) and renders a change list + diff (`pane.go`). Staging, committing, branch switching, and `ctrl+g` to request an AI commit message: the pane shows a spinner while generating, `esc` cancels, and a failure keeps the typed text, reports the error in the status line and raises an error toast. The change list scrolls to keep the cursor visible. `NewPane(theme, commitAI)` hides the `ctrl+g` hint when the backend CLI is not on `PATH`.
- `internal/aicommit` — no UI. `BuildArgs` maps a backend to its CLI invocation; `Generate(ctx, backend, model, diff)` pipes the diff (capped at 60 KB on a line boundary) to the CLI on **stdin**, never argv, captures stderr into the error, and strips Markdown fences; `Available` checks `PATH`. Tests swap the package-level `execCommand`/`lookPath` seams for a helper process.
- `internal/runner` — Run tab. Each task spawns under `sh -c`, merges stdout+stderr through an `io.Pipe`, and streams `OutputMsg`s tagged with the task index, ending in `ExitMsg`. Tasks are cancellable via stored `context.CancelFunc`.
- `internal/httpc` — minimal REST client. `httpc.go` holds pure helpers (request building, header parsing, Chroma highlighting) so it's unit-testable without a `tea.Program`; `pane.go` is the UI. The response viewport takes the rows left after the request editors (minimum 1).
- `internal/ui` — shared lipgloss styles and the global keymap (`keys.go`), plus the chrome primitives: `route.go` (`Dest`, `Routed`), `fit.go` (`Fit`, `FitWidth`), `overlay.go` (ANSI-safe `Overlay`, `Dimmed`), `toast.go` (`ToastMsg`, `Toast`), `rect.go` (`Rect`), `click.go` (`ClickTracker`). All width math goes through `charmbracelet/x/ansi`, so escape sequences are never split.
- `internal/config` — see Configuration above.

### Streaming pattern (used by `runner`)

Goroutine produces messages onto a buffered `chan tea.Msg`; a method like `.Next()` returns a `tea.Cmd` that blocks on the channel. After handling each `OutputMsg`, the pane returns `proc.next()` again, so Bubble Tea keeps a single in-flight read pending until the channel closes. `cancel()` ends the goroutine cleanly via `exec.CommandContext`.

### Testing notes

Tests sit next to the code they cover. Panes are driven by calling `Update` with `tea.KeyMsg`/`tea.MouseMsg` and running returned commands synchronously; never a `tea.Program`. Cross-package checks use small exported accessors (`git.Pane.Reloads`, `files.Model.SelectedPath`) because `export_test.go` seams are invisible to other packages. The invariant tests are `TestFrameFitsTerminal` (`internal/app/layout_test.go`) and `TestAsyncRoutedWhileOtherTabActive` / `TestRunnerOutputRoutedWhileOtherTabActive` (`internal/app/app_test.go`, the latter runs a real `sh` task). The `aicommit`, `httpc`, `git` and `config` packages have substantial unit coverage of their pure helpers. `internal/git/integration_test.go` exercises the real `git` CLI against a temp repo — needs `git` on `PATH`.
