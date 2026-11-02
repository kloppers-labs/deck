# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What deck is

A Bubble Tea TUI "command center" with four tabs — **Files**, **Git**, **Run**, **HTTP** — over the current working directory. `main.go` loads config, maps `mouse` to program options (`hover` → all-motion, `click` → cell-motion, always `WithReportFocus`) and boots the root model in `internal/app`. After `p.Run()` returns, `main` calls `Model.StopAll` on the final model as a backstop so no task outlives deck. Everything else lives under `internal/`.

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
- Configured `tasks` are always shown; tasks detected by the Run pane are merged after them (see `internal/runner`).

## Architecture

### Root model owns the chrome (`internal/app`)

`Model` holds one value of each pane type and an `active tab`, plus the toast queue, help-modal state and mouse state. `app.go` is the model and routing, `mouse.go` the mouse translation, `help.go` the help modal.

**Routing contract (`ui.Routed`).** Every async or cross-tab message implements `RouteTo() ui.Dest` (`DestApp`, `DestFiles`, `DestGit`, `DestRun`, `DestHTTP`). `app.Update` delivers any `ui.Routed` message to its destination whichever tab is active — today `git.RefreshMsg`/`CommitMsgReady`, `runner.OutputMsg`/`ExitMsg`/`DetectedMsg`, `httpc.RespMsg`/`LoadRequestMsg`, and the app-bound `ui.ToastMsg`, `ui.FocusTabMsg` (switch to the tab owning a `Dest`) and `editor.OpenMsg`. A new async message needs only a `RouteTo` method, never a new `case` in `app.Update`. Other app-level cases: `files.OpenMsg` launches the editor; `editor.OpenMsg{Target}` launches it at a position through `editor.CommandAt` (micro gets `+L:C`); `git.GenCommitMsgRequest` runs `aicommit.Generate` (90 s timeout, cancellable through `Ctx`); `editorClosedMsg` triggers `git.Refresh` (and an error toast if the editor failed); `spinner.TickMsg` fans out to every pane with a spinner. Keys, mouse events and everything else go to the active pane after global handling.

**Frame contract.** `SetSize(w, h)` is a pane's **outer** box, borders included, and `View()` must return exactly `h` rows of at most `w` columns — panes end `View` with `ui.Fit`. The body is `h - 5` (tab bar 3, status 1, footer 1) and `app.View` fits every section again as a backstop. Below 60×16 the frame is a centred `terminal too small` notice. `layout_test.go` checks every tab (with and without help) at 60×16, 80×24, 120×40 and 200×50 using long-name fixtures.

**Capture rules.** A pane's `Capturing()` is true only while text is being typed or a modal is open (filter, commit box, HTTP insert mode or `.http` sidebar, Run task filter or log search, branch picker); reading, scrolling and selecting never capture. While capturing, single-letter globals (`q`, `?`, `1`–`4`) are suppressed and the footer shows `esc normal` instead of `q`/`?`. Capture-proof keys are checked first: `ctrl+c`, `tab`/`shift+tab`, `alt+1`…`alt+4`, `F1`, `alt+m`.

**Mouse.** `app.mouse` drops motion events on the same cell as the last one, handles tab-bar clicks with the pure `tabHitTest`, ignores the status and footer rows, and forwards body events to the active pane with `Y` reduced by the tab-bar height; containers keep translating into child-local coordinates (`ui.Rect.Local`). `alt+m` toggles reporting at runtime (`tea.DisableMouse` / re-enable per `cfg.Mouse`) and the footer shows `mouse off`. `ui.ClickTracker` turns presses into click counts (double-click = same cell within 400 ms).

**Toasts and status bar.** `ui.ToastMsg{Level, Text, TTL}` (or `ui.Toast(level, text)`) from anywhere shows on the right of the status bar; the newest live toast wins and each expires through a `tea.Tick`. The pure `statusLine(left, right, w)` lays out project · branch · dirty · running on the left and the toast on the right, truncating the left side first.

**Quit and re-run (`quit.go`).** `q`/`ctrl+c` quit at once when no task runs; otherwise a modal asks `N tasks running (a, b): quit and stop them? [y/N]`. `y` runs `run.StopAllCmd` (signal now, wait up to 5 s off the UI goroutine, then `tea.QuitMsg`), any other key dismisses it, and a second `ctrl+c` force-quits (main's backstop still stops the tasks). `ctrl+r` (gated by capture) re-runs the last Run task from any tab without switching. The Run tab badge shows the running count and a red `✗N` for failed tasks.

**Help.** `?`/`F1` opens a centred modal composited with `ui.Overlay` over the dimmed body (`ui.Dimmed`), grouped Global / active tab / Mouse and built from the same `key.Binding` values the panes export. It is modal (only `ctrl+c` escapes it), scrolls with `j`/`k`/wheel, filters with `/`, and closes with `esc`/`?`/`F1`/`q`.

When adding a new tab: add to the `tab` enum, `tabNames`, `tabDests`, the `Model` struct, `New`, `resize`, `capturing`, `routeTo`, `paneView`, `paneBindings`, the `globalKey` switches and `ui.Keys` (`N` and `alt+N`), plus a `ui.Dest` for it.

### Each pane is its own package

Panes are value types (so the root model can copy them in/out of `Update` cheaply). Each exports `New…`, `SetSize`, `Update(msg) (Self, tea.Cmd)`, `View`, and usually a `Capturing() bool`.

- `internal/files` — directory listing + Chroma-highlighted preview (markdown via glamour). Emits `OpenMsg`; `x` on a `.http` file emits `httpc.LoadRequestMsg`. The header shows the directory relative to the launch root, left-truncated. Mouse: wheel scrolls whichever column it is over (the preview by 3 lines), a click selects a row (`ListTop` is the first row's local Y) or focuses the preview, a double-click opens. `SelectedPath()` reports the highlighted entry.
- `internal/git` — wraps the `git` CLI (`git.go`) and renders a change list + diff (`pane.go`). Staging, committing, branch switching, and `ctrl+g` to request an AI commit message: the pane shows a spinner while generating, `esc` cancels, and a failure keeps the typed text, reports the error in the status line and raises an error toast. The change list scrolls to keep the cursor visible. `NewPane(theme, commitAI)` hides the `ctrl+g` hint when the backend CLI is not on `PATH`.
- `internal/aicommit` — no UI. `BuildArgs` maps a backend to its CLI invocation; `Generate(ctx, backend, model, diff)` pipes the diff (capped at 60 KB on a line boundary) to the CLI on **stdin**, never argv, captures stderr into the error, and strips Markdown fences; `Available` checks `PATH`. Tests swap the package-level `execCommand`/`lookPath` seams for a helper process.
- `internal/runner` — Run tab.
  - `procgroup.go`: each task runs under `sh -c` in its own process group (`Setpgid`), in `Task.Dir` (relative to the root) with `Task.Env` plus `FORCE_COLOR`/`CLICOLOR_FORCE`/`DOTNET_SYSTEM_CONSOLE_ALLOW_ANSI_COLOR_REDIRECTION`. Stopping cancels the context: `cmd.Cancel` sends SIGTERM to the group, a reaper sends SIGKILL to whatever is left after `killDelay` (3 s), and `proc.done` closes only once the group is gone. Output is split on `\n`, `\r\n` and bare `\r` into `chunk`s; a reader that would block drops lines once stopped so a stop never deadlocks.
  - `logbuf.go`: capped line buffer with stable absolute line numbers; a `\r`-terminated chunk is overwritten by the next one. `logview.go`: per-task scroll/follow (`↓ N new`), smart-case search, wrap, and problems; it renders only the visible rows.
  - `problems.go`: pure matchers for msbuild/dotnet and tsc (`path(l,c): error CODE: msg`), tsc `--pretty` and Go (`file.go:l:c:`). Paths resolve against the task dir; `enter` on a problem emits `editor.OpenMsg`.
  - `runner.go`/`dotnet.go`: `Init` detects tasks in the background (`DetectedMsg`): Go, npm/pnpm/yarn/bun scripts (from the lockfile), Angular, .NET (walks 3 levels skipping `bin`/`obj`/`node_modules`/`.git`; build/test the root `.sln`/`.slnx`, run/watch each Web or Exe project), Python, Cargo, Gemfile, Makefile/justfile. Capped sources report `+N more`. Detected tasks whose name or command is already configured are dropped; the rest show as `auto`.
  - `pane.go`: list ↔ log focus (`l`/click into the log, `esc`/`h` back), `StopAll(timeout)`/`StopAllCmd`, `Rerun`, `RunningNames`, `FailedCount`; a failed exit raises an error toast and the header shows `exit N`.
- `internal/httpc` — REST client. `httpc.go` holds pure helpers: `buildRequest` (loopback hosts default to `http://`, a JSON body gets an auto `Content-Type` shown greyed), header parsing/formatting and Chroma highlighting. `pane.go` has normal mode (not capturing: `j`/`k`/`g`/`G`/wheel scroll the response, `[`/`]` Body/Headers, `i`/`enter`/click to edit) and insert mode (`esc` back); `ctrl+s` sends, `esc` cancels an in-flight request (requests carry a `seq`, so a late reply is ignored), latency includes reading the body, and bodies over 1 MiB show `truncated at 1 MiB`. `httpfile.go` parses `.http` files (`###`, `# @name`, `@var = value`, `{{var}}`, `{{$guid}}`, `{{$timestamp}}`, `{{$dotenv KEY}}`, `{{$processEnv KEY}}`); variables resolve at send time, file variables over the environment. `env.go` loads the nearest `http-client.env.json` (+ `.private`, `$shared`); `ctrl+e` cycles environments. `sidebar.go`: `ctrl+o` lists requests from `**/*.http` (depth 4, 100 files), and `LoadRequestMsg` loads a request and focuses the tab.
- `internal/ui` — shared lipgloss styles and the global keymap (`keys.go`), plus the chrome primitives: `route.go` (`Dest`, `Routed`), `focus.go` (`FocusTabMsg`), `fit.go` (`Fit`, `FitWidth`), `overlay.go` (ANSI-safe `Overlay`, `Dimmed`), `toast.go` (`ToastMsg`, `Toast`), `rect.go` (`Rect`), `click.go` (`ClickTracker`). All width math goes through `charmbracelet/x/ansi`, so escape sequences are never split.
- `internal/config` — see Configuration above.
- `internal/lsp` — in-house LSP client, standard library only (no Bubble Tea, no other `deck/internal` import; `TestImportsStandardLibraryOnly` enforces it). `conn.go` is JSON-RPC with Content-Length framing; `registry.go` holds `DefaultSpecs()` (csharp-ls, gopls, typescript-language-server), `MergeSpecs` for config overrides, `FindRoot` (markers in priority order, walking up to the git top level / `$HOME`, never `/`) and `LookupBinary` (`PATH`, then `~/.dotnet/tools`, `~/go/bin`, `$GOPATH/bin`, Homebrew). `Manager.ClientFor(ctx, path)` lazily starts one `Client` per (server, root), and routes virtual URIs (csharp-ls `csharp:/…` metadata, read through `Client.Metadata`) to an already-running client; `Prestart(ctx, cwd)` eagerly starts only servers with `PrestartMarkers` (csharp-ls when a `.sln`/`.slnx` is found). A missing binary fails once and is never retried; a crashed server restarts once. Clients track Starting → Indexing → Ready from `$/progress` (or 2 s without progress), keep a 30-document didOpen LRU, and resync on mtime change or `Reload`. `Events()` carries `StatusEvent`/`DiagnosticsEvent`/`LogEvent`/`ShowMessageEvent` through a 256-entry queue that drops logs first. `Shutdown(ctx)` sends shutdown/exit, then kills the process group (servers start with `Setpgid`). Columns stay in the server's negotiated encoding; convert with `RuneToUnits`/`UnitsToRune`/`ByteToUnits`/`UnitsToByte`. Tests drive `lsptest`, an in-process fake server over `net.Pipe`, through the injectable `Starter`; `integration_test.go` runs real gopls when installed.

### Streaming pattern (used by `runner`)

The process goroutine pushes `chunk`s onto a buffered channel and the exit onto a second one. `proc.next()` returns a `tea.Cmd` that blocks for the first chunk, then drains up to 256 lines or 16 ms into one `OutputMsg`; once the line channel closes it returns the `ExitMsg`, then nil. After each `OutputMsg` the pane returns `proc.next()` again, so Bubble Tea keeps a single read pending.

### Testing notes

Tests sit next to the code they cover. Panes are driven by calling `Update` with `tea.KeyMsg`/`tea.MouseMsg` and running returned commands synchronously; never a `tea.Program`. Cross-package checks use small exported accessors (`git.Pane.Reloads`, `files.Model.SelectedPath`) because `export_test.go` seams are invisible to other packages. The invariant tests are `TestFrameFitsTerminal` (`internal/app/layout_test.go`) and `TestAsyncRoutedWhileOtherTabActive` / `TestRunnerOutputRoutedWhileOtherTabActive` (`internal/app/app_test.go`, the latter runs a real `sh` task). The `aicommit`, `httpc`, `git` and `config` packages have substantial unit coverage of their pure helpers. `internal/git/integration_test.go` exercises the real `git` CLI against a temp repo — needs `git` on `PATH`. Run `go test -race ./internal/lsp/...` after touching `internal/lsp`, and `go test -race ./internal/runner` after touching the runner; its process tests run real `sh`/`sleep` pipelines with unique `sleep 30.NNNNNN` markers and check `pgrep` afterwards.
