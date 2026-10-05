# Task Completion Checklist

Before declaring a coding task done, run:

1. `gofmt -w .` (or `go fmt ./...`) — formatting.
2. `go vet ./...` — vet.
3. `go build ./...` — compile everything.
4. `go test ./...` — full test suite. Use `-race` if touching goroutine/channel code (panes with streaming, runner, ai).

No linter, type checker, or formatter beyond the standard Go toolchain is configured for this repo.

Special cases:
- Changes touching `internal/git` — the integration tests shell out to real `git`; ensure `git` is available.
- Changes touching `internal/runner` or `internal/ai` streaming — run with `-race` because those packages spawn goroutines that produce onto channels.
- UI/TUI behavior cannot be auto-verified — if you change pane interactions, exercise the change with `go run .` and confirm visually.
