# Suggested Commands

## Build & run
- `go build ./...` — full build.
- `go run .` — launch the TUI.

## Test
- `go test ./...` — all tests.
- `go test -race ./...` — with race detector.
- `go test ./internal/git -run TestX` — single test by name.
- Git pane integration tests (`internal/git/integration_test.go`) shell out to real `git` — requires `git` on `PATH`.

## Format / vet
- `gofmt -w .` or `go fmt ./...` — formatting.
- `go vet ./...` — vet.

## Darwin notes

Standard BSD userland — no GNU-specific flags. `sed -i ''` (empty backup arg required on macOS). `find` lacks `-printf`. None of this matters for the Go tooling itself, but matters if you shell out from tests.
