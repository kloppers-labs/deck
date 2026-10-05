# Tech Stack

- **Language**: Go 1.26.3 (per `go.mod`). Single module `module deck`.
- **TUI framework**: `github.com/charmbracelet/bubbletea` v1.3.10, `bubbles` v1.0.0, `lipgloss` v1.1.1-prerelease, `glamour` v1.0.0 (markdown rendering).
- **Syntax highlighting**: `github.com/alecthomas/chroma/v2` v2.26.1.
- **Config**: `github.com/BurntSushi/toml` v1.6.0.
- **No HTTP/AI SDK deps** — uses stdlib `net/http` for the HTTP pane and `os/exec` for AI backends.

## External runtime dependencies

- `git` on `PATH` (used by `internal/git` and required for its integration tests).
- An AI CLI on `PATH`: `claude`, `codex`, or `mods` (selected via config `ai_backend`).
- An editor binary (default `micro`) on `PATH` for the Files pane "open" action.

## Config defaults (`internal/config/config.go:Default`)

`ai_backend="claude"`, `ai_model="sonnet"`, `editor="micro"`, `tasks=nil`. User config at `~/.config/deck/config.toml`. Empty `tasks` triggers `runner.detect()` based on marker files (`go.mod`, `package.json`, `angular.json`, `*.sln`/`*.csproj`, `pyproject.toml`).
