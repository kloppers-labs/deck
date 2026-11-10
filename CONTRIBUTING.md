# Contributing to deck

Thanks for helping make deck better. Bug reports, ideas and pull requests are all welcome.

## Reporting a bug

Open an [issue](https://github.com/kloppers-labs/deck/issues/new/choose) with your OS, terminal, `deck --version`, and the steps that trigger the problem. A screenshot helps a lot for anything visual.

## Development

You need Go (the version in `go.mod`) and `git`.

```sh
git clone https://github.com/kloppers-labs/deck.git
cd deck
go run .                 # launch deck on its own source
go test ./...            # run the tests
go test -race ./...      # what CI runs
```

`CLAUDE.md` describes the architecture in depth: how the root model routes messages, the frame contract every pane follows, and where each feature lives.

A few conventions:

- **Tests sit next to the code.** Panes are tested by calling `Update` with key and mouse messages, never by starting a real program.
- **Every pane must fit its frame.** `View()` returns exactly the rows and columns it was given. `TestFrameFitsTerminal` checks every tab at several sizes.
- **Nothing blocks the UI.** Git, file I/O, LSP and HTTP work runs in commands and comes back as routed messages.
- **Keep it formatted.** CI runs `gofmt`, `go vet` and `go test -race` on macOS and Linux.

## Pull requests

Keep each PR focused on one change, and describe what changed and why. Include a screenshot or GIF for UI changes ([vhs](https://github.com/charmbracelet/vhs) makes recording easy).

## Releasing

Maintainers push a `vX.Y.Z` tag. The release workflow runs GoReleaser, which publishes the archives, `.deb`/`.rpm`/`.apk` packages and checksums, and updates the Homebrew cask in [kloppers-labs/homebrew-tap](https://github.com/kloppers-labs/homebrew-tap).
