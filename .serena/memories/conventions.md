# Conventions

## Package layout

- One pane = one package under `internal/`. Pane packages typically split into `<name>.go` (pure logic, externally testable) and `pane.go` (Bubble Tea UI). Tests live next to the code they cover.
- Shared lipgloss styles and global keymap go in `internal/ui`. Don't define styles in panes — use or extend `internal/ui/styles.go`.
- `internal/config` is the only TOML touchpoint. Add new options as struct fields with `toml:"..."` tags and apply defaults in `Default()` (per-field defaulting in `Load` after decode).

## Bubble Tea idioms used here

- **Pane = value type**, not pointer. `Update` signature is `func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd)`. The root model copies the pane in, calls `Update`, and stores the returned value back.
- Cross-pane communication is via typed messages, not direct method calls. New cross-pane behavior: define a message type in the emitting pane, handle it in `internal/app/app.go:Update` (the routing switch), then forward to the target pane.
- Async work (subprocess stdout, HTTP responses) is structured as a goroutine feeding a buffered `chan tea.Msg`, with a `Next()`-style method returning a `tea.Cmd` that blocks on the channel. The pane re-issues `Next()` after each received message so Bubble Tea always has one read pending until the channel closes. Cancel via `exec.CommandContext` or stored `context.CancelFunc`.

## Testability discipline

- Pure helpers (`ai.BuildArgs`, `httpc.parseHeaders`/`buildRequest`/`cycleMethod`, `git.ParseStatus`) are deliberately separated from the `tea.Program` so they can be unit-tested without driving the TUI. Prefer extending these and testing them directly over writing program-level tests.

## External process integration

- Editor launches via `tea.ExecProcess` — TUI is suspended for the duration; on close, the app emits an `editorClosedMsg` that triggers `git.Refresh` so file changes show up.
- AI backends are invoked through `os/exec` with args built by `ai.BuildArgs`. The TUI never sees AI API keys; backends use their own auth.

## Style

- Standard Go: `gofmt`, lower-case package names, exported identifiers PascalCase, errors returned not panicked. No custom linters configured.
