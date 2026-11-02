# deck supercharge — design spec

Date: 2026-11-02 · Branch: `feat/supercharge` · Status: proposed

Companion plan: `docs/superpowers/plans/2026-11-02-deck-supercharge-plan.md`

---

## 1. Intent

deck should become the best terminal tool for exploring and working in a codebase, first of all a .NET/C# solution. The user spends most of their time *reading* code, moving between folders and files, and jumping into micro for short edits. The work splits into three parts:

1. **Remove what isn't used.** The AI tab duplicates the user's existing AI TUIs, so it goes. The only AI feature kept is the one-shot commit-message helper on the Git tab (`ctrl+g`).
2. **Layer 1: a smart code browser in the Files tab.** It adds:
   - fast scrolling;
   - a cursor;
   - LSP hover, go-to-definition, references and an outline;
   - jump history;
   - in-file search;
   - full mouse support;
   - a collapsible file tree that stays put while you read;
   - metadata when the mouse hovers over a file.
3. **Layer 2: micro, supercharged.** micro opens at the exact line and column you were reading, with bundled LSP, tree and fuzzy plugins plus good keybindings. When it closes, deck returns to the spot where you were.

Running through all three is a **full audit** of the existing tabs: Files, Git, Run, HTTP, the chrome, config and tests. It confirmed a set of real bugs, listed in §11, and this spec fixes them.

### 1.1 Claims verified against the code at HEAD (`b42ddd4`)

The audit made a number of claims. These were checked against the source before relying on them:

- **Mouse is never enabled.** `main.go:18` passes only `tea.WithAltScreen()`.
- **The Files preview highlights synchronously and resets scroll.** `refreshPreview` runs inside `Update` and `SetSize` and calls `GotoTop`.
  - Every non-key message goes to the list (`files.go:191-194`).
  - The `f` key both focuses the preview and is the viewport's page-down.
  - Preview focus makes `Capturing()` true, so `q`, `?` and `1`–`5` stop working.
- **`readCapped` truncates silently.** It does one `f.Read` into a 128 KiB buffer with no truncation notice (`preview.go:143-155`).
- **`git.run` mixes stderr into stdout.** It uses `CombinedOutput`. `Status` uses porcelain v1 without `-z`, and every path is relative to the current directory, not the repo root.
- **Git pane keys leak into the diff viewport.** Every key falls through to `p.diff.Update(msg)` (`pane.go:264-267`), so `space` and `b` also page the diff.
  - `IsStaged()` is true for `MM` files.
  - `NewPane` runs `reload()`, which makes synchronous git calls while deck starts up.
- **`refreshMsg` is unexported and is only delivered to the active pane.** `editorClosedMsg` returns `git.Refresh` (`app.go:161`) while the Files tab is active, so the Git pane never sees it.
- **`generateCommitMsg` wipes the commit box on failure.** It returns `CommitMsgReady{Text:""}` on any error, which wipes the box (`pane.go:222`). `ai.Run` passes the whole diff in argv.
- **Stopping a Run task can leave processes behind.** The runner uses `exec.CommandContext(ctx, "sh", "-c", ...)` with an `io.Pipe` and no process group, so `stop` only kills `sh`.
- **The HTTP tab always captures input.** `Capturing()` always returns true (`httpc/pane.go:129`), and the response viewport never receives a key.
- **Config errors are ignored.** `config.loadFrom` discards the `toml.DecodeFile` error.
- **`paneSlack = 2` doesn't fit every pane.** Panes count their borders inconsistently.
- **Two library limits:**
  - chroma v2.26.1 has **no** Razor lexer. It does have XML, INI, HTML and HTTP.
  - bubbletea v1.3.10 has `WithMouseAllMotion`, `WithReportFocus`, `MouseActionMotion` and `MouseButtonBackward`/`Forward`.
- **`.gitignore` ignores any directory named `micro/`.** Embedded runtime assets must **not** live in a directory called `micro`.
- **The tools exist on this machine:**
  - language servers: `~/.dotnet/tools/csharp-ls`, `~/go/bin/gopls`, `/opt/homebrew/bin/typescript-language-server`;
  - search tools: `rg` and `fd`;
  - micro 2.0.15.
- **Build and tests pass:** `go build ./... && go test ./...` gives 77 tests passing in 10 packages.

---

## 2. Goals

- **G1. Instant reading.**
  - Scrolling a 5,000-line `.cs` file with the wheel or with `ctrl+d` never drops input.
  - `View()` takes under 1 ms for a 200-row viewport over a 50k-line document.
  - Moving through the tree never blocks while a file is highlighted.
- **G2. IDE-grade navigation in C#.** The following all work with csharp-ls:
  - hover;
  - go-to-definition, including into metadata/decompiled sources;
  - find references;
  - implementation;
  - document outline;
  - workspace symbols;
  - back and forward.
- **G3. You never lose your place.** These keep the tree selection, the viewer position and the history:
  - switching folders or files;
  - opening micro and closing it again;
  - changing tabs;
  - resizing.
- **G4. Mouse-first and keyboard-complete.** Every action has a key. Mouse users can click, double-click, scroll, ctrl/alt-click, hover and use the back/forward buttons.
- **G5. micro feels like part of deck.** It opens at the same line, column and scroll position. It has working LSP features, a tree and a fuzzy finder, and returns to deck where you left off.
- **G6. Every existing tab is correct.** That means:
  - the frame always fits the terminal;
  - git works from a subdirectory and with odd paths;
  - stopping a task kills its whole process tree;
  - the HTTP response can be scrolled.

## 3. Non-goals

- deck does not become an editor. All editing stays in micro or another configured editor. The viewer is read-only.
- Writing to files through LSP (rename, code actions, formatting) is out of scope. micro does those through mlsp.
- deck does not call any AI API. The commit helper still shells out to `claude`, `codex` or `crush`.
- No Windows support work. deck targets macOS and Linux, and process-group handling is POSIX-only.
- No debugger or test explorer.

## 4. Anti-goals (things this work must not do)

- **Don't break existing tabs.**
  - Each milestone leaves `go build ./... && go test ./...` green.
  - Git, Run and HTTP keep their current core keys unless §8 changes them explicitly.
- **Don't depend on a language server being installed.**
  - With no server, or a server that crashes, the viewer still highlights, scrolls, searches and shows an outline (markdown headings).
  - LSP actions report once in the footer, e.g. `C#: csharp-ls not found — dotnet tool install -g csharp-ls`, and are never retried in a loop.
- **Keep startup instant.** None of the following may run before the first frame:
  - git calls;
  - language server spawns;
  - micro runtime syncs;
  - directory walks beyond the cwd listing;
  - finder indexing.

  All of them run as `tea.Cmd`s after `Init`.
- **Don't add heavy dependencies.**
  - No LSP library, `bubblezone` or `fsnotify` in this scope.
  - The only allowed additions are promoting `sahilm/fuzzy` and `charmbracelet/x/ansi` from indirect to direct dependencies.
- **Don't touch the user's own `~/.config/micro`.** deck owns only `~/.config/deck/micro`. Merges into its `settings.json` and `bindings.json` add missing keys and never overwrite a value.
- **No narrative code comments.** The user's rule is zero comments by default, and at most one line for a constraint the code can't express.

---

## 5. Architecture

### 5.1 Package map (after the work)

```
main.go                      flags-free entry; builds lsp.Manager, app.New(cfg, warnings, mgr); defer mgr.Shutdown + run.StopAll
internal/app                 root model: tabs, global keys, routing, mouse translation, toasts, status bar, help overlay
internal/ui                  palette/styles, keys, sanitize, + rect.go (Rect, hit-tests), overlay.go (ANSI-safe compositing),
                             route.go (Routed interface + Dest), toast.go (ToastMsg), fit.go (Fit(view,w,h))
internal/ui/picker           shared fuzzy overlay (finder, grep, refs, outline, workspace symbols, LSP log)
internal/files               Files tab container: tree sidebar + viewer + info panel; finder/grep/file-ops wiring
internal/viewer              read-only code viewer component (document, highlight, geometry, search, chords, history, LSP UI)
internal/lsp                 in-house LSP client: conn, protocol, decode, position, uri, registry, client, manager, lsptest/
internal/aicommit            one-shot commit-message generation (moved out of internal/ai)
internal/git                 git CLI (porcelain v2 -z, repo-root aware, async) + Git tab pane
internal/runner              Run tab (process groups, StopAll, .NET detection, problem matchers)
internal/httpc               HTTP tab (normal/insert modes, scrollable response, .http files)
internal/editor              editor launch profiles, Target{Path,Line,Col,Top}, micro runtime bundle, bridge state
internal/config              TOML (global + project-local .deck.toml), warnings
```

`internal/ai` is deleted.

### 5.2 Routing contract (new)

Today, every new async message needs its own `case` in `app.Update`, which makes `app.go` a merge hotspot. This spec replaces that with a declared destination:

```go
package ui

type Dest int

const (
    DestApp Dest = iota
    DestFiles
    DestGit
    DestRun
    DestHTTP
)

type Routed interface{ RouteTo() Dest }
```

- Every async or cross-tab message implements `RouteTo()`. Examples:
  - `git.RefreshMsg`, `git.StatusLoadedMsg`, `git.CommitMsgReady`
  - `runner.OutputMsg`, `runner.ExitMsg`
  - `httpc.RespMsg`
  - `viewer.DocLoadedMsg`
  - `files.StatMsg`
  - LSP event wrappers
- `app.Update` delivers any `ui.Routed` message to its destination pane, whatever tab is active.
- `spinner.TickMsg` keeps its fan-out.
- `tea.KeyMsg` and `tea.MouseMsg` go to the active pane after global handling.
- Everything else goes to the active pane, as today.

### 5.3 Frame contract (new)

- `SetSize(w, h)` gives the **outer** box. `View()` must return **exactly `h` rows** and **at most `w` columns**, borders included.
- `ui.Fit(s, w, h)` pads or clips a rendered block to that size, and panes call it as their last step.
- `app.View` also applies `Fit` to the body as a backstop.
- `paneSlack` is removed. The body height is `h - tabBar(3) - status(1) - footer(1)`.
- Below **60×16**, the frame shows a centred `terminal too small (WxH, need 60x16)` instead.
- A table-driven test checks every tab at 60×16, 80×24, 120×40 and 200×50, using long-name fixtures. It asserts `lipgloss.Height(frame) == h` and `lipgloss.Width(frame) <= w`.

### 5.4 Focus and the capturing gate

The gate decides when single-letter global keys (`q`, `?`, `1`–`4`) are disabled.

- **Rule:** a pane reports `Capturing() == true` only when **text is being typed** or **a modal is open**. That covers:
  - text inputs: filter, search prompt, goto prompt, rename/new-file prompt, commit box, focused HTTP field;
  - an open picker or overlay;
  - a pending key chord such as `g…`, `]…` or `z…`.

  Reading, scrolling and selecting never capture.
- **Capture-proof keys** are checked before the gate:

  | Key | Action |
  |---|---|
  | `ctrl+c` | quit |
  | `tab` / `shift+tab` | cycle tabs |
  | `alt+1`…`alt+4` | jump to a tab |
  | `F1` | help |
  | `alt+m` | toggle mouse |

- **Footer:** while capturing, it drops `q` and `?` and shows `esc normal` instead.

### 5.5 Mouse

- **Program options:** `main.go` adds `tea.WithMouseAllMotion()` (needed for hover) and `tea.WithReportFocus()`.
- **Config:** `mouse = "hover" | "click" | "off"`, default `"hover"`.
  - `"click"` uses `WithMouseCellMotion`.
  - `alt+m` toggles the mouse at runtime with `tea.DisableMouse` / `tea.EnableMouseAllMotion`, so the user can select text natively.
- **What `app` does with each event:**
  1. Drops motion events whose (X, Y) matches the last motion event.
  2. If `Y < tabBarHeight`, hit-tests the tab bar (pure `tabHitTest(x, widths)`). A press switches tabs.
  3. Otherwise subtracts `tabBarHeight` from Y and forwards the event to the active pane.
  4. Status bar and footer rows are ignored.
- **Coordinate translation is layered.** Each container subtracts its child's origin, and children work in local coordinates. `ui.Rect{X,Y,W,H}` provides `Contains` and `Local`.
- **Click gestures:**
  - **Double-click:** two presses within 400 ms on the same cell. Detected by a pure `clickTracker`.
  - **Go-to-definition click:** `viewer.goto_click = ["ctrl", "double", "right"]`, all enabled by default. macOS Terminal turns ctrl+click into a right-click, and cmd+click is taken by the terminal.
- **Help note:** native text selection needs shift+drag (Ghostty, kitty, WezTerm) or option+drag (iTerm2) while mouse reporting is on. `y`/`Y` copy over OSC52.

### 5.6 Toasts and the status bar

- **Toasts:** `ui.ToastMsg{Level, Text, TTL}` has destination `DestApp`. It shows on the right side of the status bar and expires on a tick.
- **Sources:**
  - config warnings;
  - editor exit errors (`editorClosedMsg.err` is ignored today);
  - commit-helper failures;
  - task failures;
  - "copied";
  - LSP `showMessage`.
- **Status bar layout:**
  - **Left:** project name (repo-root basename), `⎇ branch ↑a ↓b`, dirty count, `▶ n running`.
  - **Right:** LSP badges (`C# ⠋ Loading Foo.sln 42% (18s)` / `C# ●` / `C# ✗`), then the toast.
  - The AI backend/model segment is removed.

### 5.7 Help overlay

`?`/`F1` opens a centred modal composed with `ui.Overlay` over a dimmed frame. It replaces the current behaviour of swapping out the body.

- It is grouped into Global / active tab / Mouse.
- It scrolls, and `/` filters it.
- It is built from the same `key.Binding` values the panes export, so help can't drift from the real keys.

---

## 6. Layer 1: viewer, tree, LSP

### 6.1 Files tab layout

```
┌ tree ──────────────┐┌ Deck.Api › OrdersController › Get ─────────────┐┌ info ───────┐
│ ▾ src/             ││  12 ● public async Task<IActionResult> Get(…)  ││ Program.cs  │
│   ▾ Deck.Api/   M  ││  13 │ {                                        ││ 4.2 KB      │
│      Program.cs M  ││  14 │     var orders = await _svc.ListAsync(); ││ 132 lines   │
│   ▸ Deck.Core/     ││ …                                              ││ -rw-r--r--  │
│ ▸ tests/           ││ /orders 3/17 · Ln 14, Col 9 · C# ● · 12%       ││ 3h ago · M  │
└────────────────────┘└────────────────────────────────────────────────┘└─────────────┘
```

- **Tree:**
  - Width is `files.sidebar_width` (default 32 columns, capped at 40% of the width).
  - `ctrl+b` collapses and expands it. It collapses automatically below 90 columns.
  - `<` and `>` resize it, and dragging the divider resizes it with the mouse.
- **Viewer:** takes the remaining width. The header row is a breadcrumb (or the relative path), and the footer row shows position, search, chord and LSP state.
- **Info panel:** optional, 24 columns. `i` toggles it and `files.info_panel` sets the default.
- **Focus border:** the focused region uses the `ui.PaneActive` border, and the others use `ui.Pane`.

### 6.2 `internal/viewer`: the code viewer

**Document** (behind a pointer, so the value-type model stays cheap to copy):

```go
type Document struct {
    Path      string
    Raw       []string
    Plain     []string
    Styled    []string
    Lang      string
    Size      int64
    ModTime   time.Time
    Truncated bool
    Binary    bool
    Markdown  bool
    TabWidth  int
}
```

- **`Raw`:** the lines with `\r` stripped. Used for LSP positions and editor columns.
- **`Plain`:** the lines with tabs expanded and control bytes sanitised. Used for search, word detection and width.
- **`Styled`:** each line highlighted on its own and ending with `\x1b[0m`.

**Loading pipeline:**

1. **`LoadCmd(path, gen)`** does the following, then returns `DocLoadedMsg` with `Plain`, which is shown at once:
   - `os.Stat`;
   - binary check;
   - a full read up to 20 MB (`io.ReadFull`/`LimitReader`, with no short reads);
   - split, expand tabs and sanitise **once**.
2. **`HighlightCmd(doc, gen)`** tokenises the **whole** file once with chroma, so multi-line comments and strings stay correct. It maps token types to cached SGR prefixes and returns `DocHighlightedMsg`. Results with a stale `gen` are dropped.
3. **Large files:**
   - Over 2 MB or over 50k lines: highlight in 2000-line chunks around the viewport.
   - Over 20 MB: show `Truncated` and a banner (`showing 20 MB of 64 MB · e to open in editor`), and skip LSP `didOpen`.
   - Lines over 4000 columns are left unhighlighted.
4. **Cache:** an LRU of 16 documents, keyed by `path+mtime+size+theme+tabWidth`.
5. **Lexer map** checked before `lexers.Match`:

   | Extensions | Lexer |
   |---|---|
   | `.props` `.targets` `.resx` `.xaml` `.slnx` `.nuspec` `.config` `.csproj` `.fsproj` `.vbproj` | XML |
   | `.razor` `.cshtml` | HTML (chroma has no Razor lexer) |
   | `.sln` | INI |
   | `.http` `.rest` | plaintext with a `###` separator style (builder's call) |
   | `.editorconfig` | INI |

   `lexers.Analyse` is only a last resort for files without an extension.
6. **Markdown:** shown rendered by default (glamour, with a renderer cached per (theme, width) and mermaid output cached per block hash). `m` toggles the raw source, which has the cursor, search and outline.
7. **Directories:** selecting one shows a directory card:
   - the first N children with sizes and git markers;
   - for a folder containing a `*.csproj`, a project card (TargetFramework, Sdk, OutputType, PackageReference/ProjectReference counts) parsed with `encoding/xml`;
   - for a `.sln`, its project list.

**Rendering** is O(visible rows). For each row:
- draw the gutter: line number (muted), git marker, diagnostic marker;
- cut the line with `ansi.Cut(Styled[i], left, left+textW)`;
- paint spans for the cursor, search hits, word under the cursor and hover target, using a pure `Paint(styled, spans)`.

No lipgloss wrapping runs over the body.

**Pure geometry and helper files** (each is table-tested):

| File | Provides |
|---|---|
| `geometry.go` | `DispCol` / `RuneCol` (tabs, wide runes), `ScreenToDoc` / `DocToScreen` (gutter, horizontal offset, wrap rows), `ensureVisible` with 3-row and 8-column scroll-off |
| `words.go` | `WordAt(line, runeCol)`: C# `@verbatim` identifiers, generics stop at `<` |
| `search.go` | `Find(lines, query, opts)`: smart-case, regex toggle, wrap-around |
| `history.go` | jump list: `Push` / `Back` / `Forward`, max 200, consecutive duplicates removed |
| `chord.go` | prefix and count state machine for `g`, `z`, `]`, `[` (no timers) |
| `popup.go` | `PlacePopup(anchor, size, bounds)`: flips above or below, clamps to bounds |
| `symbols.go` | `Breadcrumb(symbols, pos)`; markdown heading outline as a fallback |

A property test checks the geometry round trip: `ScreenToDoc(DocToScreen(p)) == p` for lines with tabs, emoji and CJK, with wrap both on and off.

**Cursor and columns:**
- `Loc{Path, Line, Col}` uses 0-based **rune** columns in `Raw`.
- LSP UTF-16/UTF-8 conversion happens only in `internal/lsp/position.go`.
- Editor targets are 1-based rune columns (§7).

**Scrolling:**

| Input | Scroll amount |
|---|---|
| wheel | `viewer.scroll_lines` (default 3), ramping up to 3× when events are less than 40 ms apart |
| shift+wheel, wheel-left/right | horizontal |
| `ctrl+d` / `ctrl+u` | half a page |
| `pgdn` / `pgup` / `space` / `shift+space` | a full page |
| `J` / `K` | 10 lines |
| `gg` / `G` | top / bottom |

The footer shows `Ln y, Col x · NN%`.

**The viewer depends on an interface, not on `*lsp.Manager`:**

```go
type Navigator interface {
    Hover(ctx context.Context, path string, at Loc) (Hover, error)
    Definition(ctx context.Context, path string, at Loc) ([]Location, error)
    TypeDefinition(ctx context.Context, path string, at Loc) ([]Location, error)
    Implementation(ctx context.Context, path string, at Loc) ([]Location, error)
    References(ctx context.Context, path string, at Loc) ([]Location, error)
    DocumentSymbols(ctx context.Context, path string) ([]Symbol, error)
    WorkspaceSymbols(ctx context.Context, query string) ([]Symbol, error)
    Status(path string) ServerStatus
}
```

- The adapter in `files` or `app` wraps `*lsp.Manager`.
- Tests inject stubs.
- A nil Navigator means "no LSP", and the viewer degrades gracefully.
- `Hover`, `Location` and `Symbol` are viewer-side types. The adapter converts to them from `lsp` types.

### 6.3 LSP features in the viewer

Every request is a `tea.Cmd` with a per-kind `seq`. Results with a stale `seq` or path are dropped, and a new hover request cancels the previous one through a stored `CancelFunc`.

- **Hover** (`K`, or holding the mouse over a word for `hover_delay_ms` = 350):
  - A motion event only starts the dwell tick when the word span changes.
  - The popup is at most `min(80, textW-4)` wide and 16 rows tall, rendered with glamour and composed with `ui.Overlay`.
  - It scrolls with `ctrl+j`/`ctrl+k` or the wheel.
  - It closes on `esc`, a move or scroll, or after the mouse has been away for 300 ms.
- **Definition** (`gd`, `F12`, ctrl+click, double-click):
  - One result: push history and jump. A different file is loaded through the container and revealed in the tree.
  - Several results: open the picker.
  - A `csharp:/metadata/...` URI opens a read-only virtual document titled `[metadata] System.String`.
- **`gi`** goes to the implementation, and **`gy`** to the type definition.
- **References** (`gr`, `shift+F12`, alt+click):
  - Results open in the picker, grouped by file with the current file first, as `relpath:line:col  <trimmed line>`.
  - Moving the selection peeks at that location in the viewer. `enter` commits the jump, and `esc` restores the original position.
- **Outline** (`o`): document symbols in the picker, indented by depth, with kind glyphs. Without a server, markdown headings are used.
- **Workspace symbols** (`ctrl+t`): a debounced `workspace/symbol` query as you type. This is "go to type" across the solution.
- **Breadcrumb header:** the namespace › type › member that contains the cursor.
- **Diagnostics:**
  - `●`/`▲` markers in the gutter, from `publishDiagnostics`.
  - `]d`/`[d` jump to the next or previous one.
  - The footer shows the message when the cursor is on that line.
- **Empty results:**
  - While the server is indexing: `C#: still loading Foo.sln (23s) — results may be incomplete`.
  - When there is no server: `no language server for .cs — csharp-ls not found`.
- **Back/forward:**
  - Back: `ctrl+o`, `alt+←`, the mouse back button.
  - Forward: `ctrl+]`, `alt+→`, the mouse forward button.
  - The jump list lives in the `files` container, because jumps cross files.
  - Entries are pushed on definition, references, outline, finder and grep jumps, on `gg`/`G`/`:N` jumps longer than one screen, and on file switches from the tree.

### 6.4 `internal/lsp`: in-house client (standard library only, no Bubble Tea import)

```
conn.go       JSON-RPC 2.0, Content-Length framing, pending map, Call/Notify, $/cancelRequest, inbound dispatch
protocol.go   Position, Range, Location, LocationLink, MarkupContent, DocumentSymbol, SymbolInformation,
              Initialize subset, WorkDoneProgress, Diagnostic
decode.go     decodeLocations / decodeHover / decodeSymbols (all shapes incl. null)
position.go   rune <-> UTF-16 <-> UTF-8 columns; negotiated encoding
uri.go        PathToURI / URIToPath; csharp:/metadata passthrough
registry.go   ServerSpec, defaults, merge, Resolve(path), FindRoot(file, markers, boundary), binary lookup
client.go     one process per (spec, root): state machine, progress, open-doc LRU (30), requests
manager.go    lazy ClientFor(path), Prestart(cwd), Events() <-chan any, Status(), Shutdown(ctx)
lsptest/      scripted in-process fake server over net.Pipe
```

**Server defaults:**

| Name | Command | Extensions | Root markers (in priority order) |
|---|---|---|---|
| csharp | `csharp-ls --features metadata-uris [--solution <sln>]`, with `cmd.Dir = root` | .cs .csx | `*.sln`, `*.slnx`, `*.csproj` |
| go | `gopls` | .go | `go.work`, `go.mod` |
| typescript | `typescript-language-server --stdio` | .ts .tsx .js .jsx .mjs .cjs | `tsconfig.json`, `jsconfig.json`, `package.json` |

**Root finding:**
- `FindRoot` takes each marker in priority order and walks up from the file toward the boundary. The first marker found wins, so `repo/Foo.sln` beats `repo/src/Foo.Api/Foo.Api.csproj`.
- The boundary is the git top level, otherwise `$HOME`, and never `/`.
- If several `.sln` files tie, prefer the one whose name matches the directory, then the first alphabetically.
- `[lsp.servers.csharp] solution = "..."` overrides the choice.

**Binary lookup:** `exec.LookPath`, then `~/.dotnet/tools`, `~/go/bin`, `$GOPATH/bin`, `/opt/homebrew/bin`, `/usr/local/bin`. The lookup function can be injected for tests. If the binary is missing, the server's state becomes `Failed` with an install hint, and it is never retried in that session.

**Lifecycle:**

1. **Lazy start in a Cmd.**
   - Start the process with `Setpgid`, sending stderr to a 64 KB ring buffer.
   - Send `initialize`, advertising utf-8 and utf-16 position encodings, markdown hover, `linkSupport`, hierarchical symbols, `workDoneProgress` and `configuration`.
   - Send `initialized`.
2. **Indexing state.** The server stays `Indexing` until every `$/progress` token has ended, or until 2 s pass with no token created.
3. **Answering server requests.**
   - `workspace/configuration`: return the settings or `null` for each item.
   - `workDoneProgress/create`, `register/unregisterCapability` and `showMessageRequest`: return `null`.
   - `workspaceFolders`: return the root.
   - Anything else: error -32601.
4. **Open documents.**
   - `didOpen` is sent with the full text from disk, never a truncated copy.
   - A full-sync `didChange` is sent after the editor closes, on `FocusMsg`, or when the file's mtime changes.
   - When the document LRU evicts an entry, `didClose` is sent.
5. **Crash:** fail all pending calls with `ErrServerExited` and restart once on the next request, with backoff.
6. **Shutdown:** `main` defers `mgr.Shutdown(3s)`. This sends `shutdown` and waits up to 1 s, sends `exit` and waits up to 1 s, then runs `kill(-pgid, SIGKILL)`. The process-group kill also catches csharp-ls's MSBuild BuildHost children.
7. **Prestart:** `lsp.prestart = true` by default. After the first frame, `Init` runs `Prestart(cwd)`, which starts only the server whose root markers exist in the cwd project. This keeps the slow C# solution load off the critical path.

**Events:** plain structs: `StatusEvent`, `DiagnosticsEvent`, `LogEvent`, `ShowMessageEvent`.
- A re-arming `listenLSP` Cmd wraps them in `files.LSPEventMsg`, which has destination `DestFiles`. The status bar reads `files.LSPStatus()`.
- When the buffer of 256 events is full, `LogEvent`s are dropped first.

### 6.5 Tree sidebar, git badges, metadata hover

**Tree** (`files/tree.go`). The bubbles `list` used for the listing is replaced by:

```go
type Node struct {
    Path, Name                   string
    IsDir, IsSymlink, Expanded, Loaded bool
    Children                     []*Node
    Depth                        int
}

type Tree struct {
    root   *Node
    rows   []*Node
    sel    int
    top    int
    hide   HidePolicy
    badges map[string]Badge
}

func Flatten(root *Node, hide HidePolicy) []*Node
func (t *Tree) Reveal(path string)
```

- **Loading:** children are loaded lazily on first expand. Directories with more than 5k entries load in a Cmd.
- **Symlinks:** a symlinked directory is resolved with `os.Stat`, so it counts as a directory. A broken link is shown in red with `→ target`.
- **Hide policy:**
  - Default hidden names: `bin obj .git node_modules .vs .idea TestResults`.
  - `.` toggles dotfiles.
  - `I` toggles gitignored entries, using `--ignored=matching` from the same status call that produces the badges.
- **Rows** are truncated to the sidebar width with `ansi.Truncate(…, "…")`. A single row never wraps.
- **The root anchor is the launch directory or repo root.**
  - `-` moves the root up to the parent.
  - `=` makes the selected directory the root.
  - `~` returns to the launch root.
  - When the root moves up, the child directory you came from is selected.
- **Sort:** `s` cycles name → modified → size → extension, with directories always first.
- **Icons:** `files.icons = "unicode"|"nerd"|"none"`, default `"unicode"`.

**Git badges** (`files/gitbadges.go`):
- One call to `git.StatusSnapshot(ctx, root)` per refresh (porcelain v2 `-z`, §9).
- `Aggregate(changes) map[dir]Badge` is pure. A directory shows its strongest child status, in the order conflict > deleted > modified > added/untracked > renamed.
- They refresh on:
  - Files-tab focus;
  - editor close;
  - `tea.FocusMsg`;
  - git commit, stage and refresh messages (via `ui.Routed` fan-out to Files);
  - `R` (manual refresh).

**Metadata** (`files/meta.go`):
- `StatCmd(path, seq)` runs `os.Lstat`, counts lines for text files up to 10 MB, and walks directories (capped at 20k entries or 300 ms, and marked as partial when capped).
- `FormatMeta(Meta, w)` is pure. It shows:
  - human-readable size;
  - `3h ago (2026-11-02 09:14)`;
  - the mode string;
  - owner;
  - git status;
  - line count;
  - language;
  - for `.csproj`/`.sln`, extras such as TargetFramework or the project count.
- **Mouse hover:** resting on a tree row for 400 ms (tick plus seq) shows a tooltip to the right of the row.
- **`i`:** toggles the persistent info panel.
- **Cache:** keyed by `path+mtime`.

### 6.6 Picker, finder, grep

**`internal/ui/picker`:**

```go
type Item struct {
    Title, Detail, Path string
    Line, Col           int
    Group               string
}
```

- **Matching:** `sahilm/fuzzy` (promoted to a direct dependency) highlights the matched runes.
- **Streaming:** `Append(items)` adds results as they arrive.
- **Peek:** `PeekMsg{Item}` is sent when the selection moves.
- **Large lists:** matching runs in a Cmd, protected by `seq`, once there are more than 50k items.

**Finder (`ctrl+p` in the Files tab):**
- **Index sources, in this order:**
  1. `fd --type f --hidden --exclude .git --exclude bin --exclude obj`
  2. `git ls-files -co --exclude-standard -z`
  3. `WalkDir` with the hide policy.
- The index is capped at 200k paths. It is built on first use and refreshed on focus and when the editor closes.
- Matches on the file name rank above matches on the path.
- Typing `file.cs:120` jumps to that line.
- Moving the selection peeks at the file. `enter` reveals it in the tree, loads it and pushes history.

**Grep (`ctrl+f`):**
- **Engine:** `rg --json --smart-case --max-columns 300 --max-columns-preview -m 200`.
  - It runs 200 ms after typing stops, and each new search cancels the previous one.
  - Results stream as `GrepChunkMsg`/`GrepDoneMsg` with a `seq`.
  - The fallbacks are `git grep -n --column -I`, then a capped Go walk.
- **Parsing:** `parseRGJSON` is pure and converts byte columns to rune columns.
- **Prompt toggles:** `alt+r` regex, `alt+c` case, `alt+g` glob.

**File operations** (pure `files/ops.go`, tested against `t.TempDir`):

| Key | Operation |
|---|---|
| `a` | new file (a trailing `/` makes a directory); parent directories are created |
| `r` | rename inline |
| `D` | move to the trash: `~/.Trash` on macOS, `trash` if installed, otherwise refuse; asks for confirmation |
| `yy` / `yY` | copy the relative / absolute path (OSC52) |
| `o` | reveal in Finder (`open -R`) |
| `!` | `$SHELL` in this directory via `tea.ExecProcess` |

After each operation the tree reloads and keeps the selection.

---

## 7. Layer 2: micro

### 7.1 Launch

```go
type Target struct{ Path string; Line, Col, Top int }

type Mode int
const ( Suspend Mode = iota; Detach )

func Launch(editorCfg string, t Target, o Opts) (*exec.Cmd, Mode, error)
```

- **Target fields** are 1-based, and 0 means unset. `Col` is a rune column in the raw line.
- **`profiles.go`** builds the arguments for each editor:

  | Editor | Position arguments |
  |---|---|
  | micro | `+L:C` |
  | vim / nvim | `+call cursor(L,C)` |
  | hx / zed / subl | `file:L:C` |
  | code family | `-g file:L:C` (Detach unless `-w`) |
  | nano | `+L,C` |
  | emacs / kak | `+L:C` |
  | rider / idea | `--line L --column C` (Detach) |
  | template | `{file} {line} {col}` substituted |
  | unknown | path only |

- **micro specifics:**
  - `cmd.Dir = editor.ProjectRoot(path)`, because mlsp takes `rootUri` from the working directory.
  - `MICRO_CONFIG_HOME=~/.config/deck/micro`.
  - `MICRO_TRUECOLOR=1` is set **only** when `COLORTERM` is `truecolor` or `24bit`. It is forced on today, which garbles Apple Terminal.
  - `DECK_EDITOR_STATE=<tmpfile>` and `DECK_EDITOR_TOP=<T>`.
  - `-colorscheme dracula` is added unless the user already set one.
- **`files.OpenMsg{Path}` becomes `editor.OpenMsg{Target}`** (destination `DestApp`). Its sources:
  - viewer `e`: cursor line, column and top;
  - tree `e`: line 1;
  - later, Run problem-matcher jumps and Git hunks.

### 7.2 Bundled runtime

The bundle is embedded from `internal/editor/runtime/`. **Not** under any `micro/` directory, which `.gitignore` ignores.

```
runtime/colorschemes/dracula.micro
runtime/settings.json  runtime/bindings.json
runtime/plug/mlsp/{main.lua,json.lua,config.lua,LICENSE}      Andriamanitra/mlsp @ 91261a0926c9 (MIT)
runtime/plug/mlsp/z_deck.lua.tmpl                              generated → z_deck.lua (loads after main.lua)
runtime/plug/filemanager/…                                     micro-editor/updated-plugins filemanager @ 216ec3adaf3a (MIT)
runtime/plug/MicroOmni/…                                       Neko-Box-Coder/MicroOmni @ f770333935cf (Unlicense)
runtime/plug/editorconfig/…                                    10sr/editorconfig-micro @ 892aa42b50e6 (MIT)
runtime/plug/deck/{deck.lua,repo.json}                         ours: position bridge
scripts/vendor-micro-plugins.sh                                re-fetches pinned SHAs
THIRD_PARTY_NOTICES.md
```

- The bookmark plugin is deferred (P2).
- **`Sync(dir, microVersion)`** runs once as a startup Cmd. It:
  - writes plugin files only when the sha256 in the manifest has changed;
  - never deletes directories deck didn't write;
  - skips MicroOmni when micro is older than 2.0.14 or `fzf`/`rg`/`bat` are missing.
- **`merge.go`** merges `settings.json` and `bindings.json`:
  - it adds missing keys only;
  - it leaves a file alone if `encoding/json` can't parse it (micro accepts comments).
- **`servers.go`** renders `z_deck.lua` from `lsp.DefaultSpecs()` plus config, using the same registry as Layer 1. It contains:
  - an absolute `cmd` for each **installed** server;
  - `autostart` and `defaultLanguageServer`;
  - `showDiagnostics` with errors and warnings only;
  - `tabAutocomplete`;
  - a wrap of `LSPClient:handleRequest` that answers `workspace/configuration`, `client/registerCapability` and `window/workDoneProgress/create`.
- **Built-in plugins:** `comment`, `diff`, `status` and `ftoptions` are on, and **`linter` is off**.

**Default settings** (merged as missing keys only):

| Settings | Value |
|---|---|
| `colorscheme` | `dracula` |
| `mouse` | true |
| `clipboard` | `external` |
| `diffgutter`, `scrollbar`, `cursorline`, `ruler`, `matchbrace`, `hlsearch`, `incsearch` | true |
| `softwrap` | false |
| `tabstospaces` | true |
| `tabsize` | 4 |
| `scrollmargin` | 5 |
| `savecursor`, `saveundo`, `mkparents` | true |
| `reload` | `auto` |
| `parsecursor`, `linter`, `keymenu` | false |
| `filemanager.showignored` | false |
| `MicroOmni.AutoSaveEnabled` | false |
| `statusformatl` | includes `$(status.branch)` and `lsp:$(mlsp.status)` |
| `statusformatr` | `Alt-q deck · Alt-t tree · Ctrl-p files · Alt-k hover · F12 def` |

Existing users keep `softwrap: true` from today's seed. The merge never overwrites it.

**Bindings:**

| Key | Command |
|---|---|
| `Alt-q` | QuitAll (back to deck) |
| `F12` / `Alt-d` | lsp goto-definition |
| `Alt-i` | lsp goto-implementation |
| `Alt-y` | lsp goto-typedefinition |
| `Alt-r` | lsp find-references |
| `Alt-k` | lsp hover |
| `Alt-s` | lsp document-symbols |
| `Alt-e` | lsp diagnostic-info |
| `F2` | lsp rename (Ctrl-s still saves) |
| `F8` | lsp format |
| `CtrlSpace` | lsp autocomplete |
| `Alt-MouseLeft` | `lua:deck.gotoAtMouse` (unverified; F12 is the fallback) |
| `Alt-{` / `Alt-}` | OmniPreviousHistory / OmniNextHistory |
| `Ctrl-p` | OmniGotoFile |
| `Alt-F` | OmniGlobalSearch |
| `Alt-l` | OmniLocalSearch |
| `Alt-j` | OmniJump |
| `Alt-t` | tree |

Bindings whose tool is missing are left out.

### 7.3 Return to the same spot

The bridge plugin `plug/deck/deck.lua` does nothing unless `DECK_EDITOR_STATE` is set.
- **`postinit`:** scrolls the view so that `StartLine == Top-1`. If the scroll API doesn't respond from Lua, it falls back to `bp:Center()` (builder's call after a smoke test).
- **`onAnyEvent`:** takes a snapshot of the absolute path, line, column and top. It writes the state file only when the snapshot changes. `:q` doesn't fire quit hooks, so this hook is required.
- **`preQuit`/`preQuitAll`** write the state file as a backstop.
- **`onSave`** adds the saved path to a list.
- **State file format** (plain text):
  - line 1: path;
  - line 2: `line col top`;
  - lines 3 onward: saved paths.

When the editor closes, `app` does the following:
1. `editor.ReadState(file, launchedAt)` reads the file. Missing or stale files are ignored.
2. It decides where to return:
   - With `editor_return = "follow"` (the default), deck reveals the path micro ended on, if it is under the root. Otherwise it returns to the original Target.
   - `"origin"` always returns to the original Target.
3. It sends `files.RevealMsg{Target}`, `files.InvalidateMsg{Saved}` (document cache plus LSP `didChange`), `git.RefreshMsg` and a badge refresh.
4. A non-nil `err` becomes a toast.

vim and nvim can report their position through an `autocmd VimLeavePre` writefile (P2). Other editors return to the original Target.

### 7.4 `deck --doctor` (P2) / startup check (P1)

`editor.Check()` reports problems as toasts, only once per config change. It checks for:
- micro older than 2.0.14;
- missing `fzf`, `rg` or `bat`;
- missing language servers.

---

## 8. Keybindings

### 8.1 Global (app)

| Key | Action | Capture-proof |
|---|---|---|
| `ctrl+c` | quit (asks to stop running tasks, §10.3; press twice to force) | yes |
| `tab` / `shift+tab` | next / previous tab | yes |
| `1` `2` `3` `4` | Files / Git / Run / HTTP | no |
| `alt+1`…`alt+4` | same | yes |
| `q` | quit (asks to stop running tasks, §10.3) | no |
| `?` / `F1` | help overlay | `F1` yes |
| `alt+m` | toggle mouse capture | yes |
| `ctrl+r` | re-run the last Run task, from any tab (the viewer search prompt uses `ctrl+r` for regex while it is capturing) | no |
| click on tab bar | switch tab | — |

### 8.2 Files — tree focus (not capturing)

| Key | Action |
|---|---|
| `j`/`k`/↑/↓, wheel | move |
| `l`/→/`enter` | directory: expand/collapse; file: focus the viewer |
| `h`/← | collapse, or go to the parent |
| `space` | preview without moving focus |
| `e` | open in editor (line 1) |
| `-` `=` `~` | change the root: up / to the selection / back to the launch root |
| `/` | filter (captures) |
| `.` `I` `s` | toggle hidden files / toggle ignored files / cycle sort |
| `a` `r` `D` | new / rename / delete (trash) |
| `yy` `yY` | copy relative / absolute path |
| `o` `!` | reveal in Finder / open a shell here |
| `i` | info panel |
| `ctrl+b` | collapse the sidebar |
| `<` `>` | resize the sidebar |
| `R` | refresh |
| `ctrl+p` `ctrl+f` `ctrl+t` | find file / grep / workspace symbols |
| hover 400 ms | metadata tooltip |
| click / double-click | select / open (dir: expand; file: focus the viewer) |

### 8.3 Files — viewer focus (not capturing, except prompts, overlays and pending chords)

| Key | Action |
|---|---|
| `h j k l`, arrows | move the cursor (word-end `e` is not bound, because `e` means "edit") |
| `w` `b` | next / previous word |
| `0` `^` `$` | line start / first non-blank / line end |
| `gg` `G` | top / bottom |
| `H` `M` `L` | screen top / middle / bottom |
| `zz` `zt` `zb` | centre / top / bottom the cursor line |
| `%` | jump to the matching bracket |
| `ctrl+d/u`, `pgdn/pgup`, `space`/`shift+space`, `J`/`K`, wheel | scroll |
| `:` | go to `line[:col]` |
| `/` `n` `N` `*` | search, next, previous, word under the cursor (`ctrl+r` in the prompt toggles regex) |
| `K`, mouse dwell | hover |
| `gd` `F12` ctrl+click, double-click | definition |
| `gi` `gy` | implementation, type definition |
| `gr` `shift+F12` alt+click | references |
| `o` | outline |
| `]d` `[d` | next / previous diagnostic |
| `ctrl+o` `alt+←` mouse-back / `ctrl+]` `alt+→` mouse-forward | back / forward |
| `e` | open in editor at the cursor line, column and top |
| `y` / `Y` | copy `relpath:line` / the current line |
| `W` `#` `m` | toggle wrap / line numbers / markdown rendered↔raw |
| `B` | blame gutter (P2) |
| `esc` | close overlay → clear search → focus the tree |

Notes:
- Digit counts are **not** supported in the viewer, because `1`–`4` must keep switching tabs. Use `:N` instead.
- `?` is the global help key, not a backward search. Use `/` then `N`.

### 8.4 Overlays (picker, hover, help) — capturing

| Key | Action |
|---|---|
| typing | filter |
| ↑/↓ or `ctrl+j/k`, wheel | move (peek) |
| `enter` | accept |
| `esc` | cancel (restores the position after a peek) |
| `tab` | toggle the preview (picker) |

### 8.5 Git (changes)

| Key | Action |
|---|---|
| `j`/`k` | move within the Staged / Unstaged / Untracked / Conflicts sections; **never** scrolls the diff |
| `enter`/`l`, click on the diff | focus the diff; there, `j`/`k`/`ctrl+d/u`/`g`/`G` scroll and `esc`/`h` returns |
| `space` | stage or unstage the entry under the cursor (acts on its section) |
| `a` / `A` | stage all / unstage all |
| `X` | discard (confirm); untracked → `git clean -f -- path` |
| `c` | commit (textarea; `ctrl+s` commits, `ctrl+g` AI helper, `alt+a` amend toggle) |
| `b` | branches (`n` new, `D` delete with confirm; the cursor starts on the current branch) |
| `f` / `p` / `P` | fetch `--prune` / pull `--ff-only` / push (sets the upstream when there is none). All run async |
| `l` | log (list, `enter` = `show --stat -p`) — basic list in scope, full browser P2 |
| `e` | open the file in the editor at the first hunk's new-side line |
| `r` | refresh |

### 8.6 Run

| Key | Action |
|---|---|
| `enter` / `r` | run |
| `R` | restart |
| `x` | stop (process group) |
| `c` | clear |
| `tab` within the pane, or click | focus the log |
| log: `/` `n` `N` | search |
| log: `w` | wrap |
| log: `g` `G` | top / bottom |
| log: `F` | follow |
| log: `e` / `E` | next / previous problem |
| log: `enter` on a problem | open in the editor at `line:col` |
| `ctrl+r` (from any tab, global) | re-run the last task |

### 8.7 HTTP

- **Modes:** normal mode (does not capture) and insert mode (captures) when a field is focused.
  - `i`/`enter` or a click on a field enters insert mode.
  - `esc` returns to normal mode.
- **Normal mode:**
  - `j`/`k`/`ctrl+d/u`/`g`/`G`/wheel scroll the response.
  - `[`/`]` switch Body/Headers.
- **Fields:** `ctrl+n`/`ctrl+p` move between fields.
- **Sending:**
  - Send with `ctrl+s` or `ctrl+enter` (builder's call, depending on whether the terminal reports `ctrl+enter`). `enter` sends from the URL field.
  - `esc` while a request is in flight cancels it.
- **.http files:** `ctrl+o` opens the sidebar of requests discovered in `.http` files. `ctrl+e` cycles the environment.

---

## 9. Git overhaul (audit P0/P1)

- **`internal/git/exec.go`:**
  - `runGit(ctx, dir, args...) (stdout, stderr string, err error)` uses `cmd.Output` with a separate stderr buffer.
  - The environment includes `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0` and `LC_ALL=C`.
  - Calls go through `exec.CommandContext` with a 10 s timeout. Remote operations use 120 s.
  - Every command runs as `git -C <toplevel>`. The top level is resolved once (`rev-parse --show-toplevel`) and cached in a `Repo` value.
- **`internal/git/status.go`:**
  - Status comes from `git --no-optional-locks status --porcelain=v2 --branch -z --untracked-files=all [--ignored=matching]`.
  - `ParseStatusV2` (pure) handles:
    - type `1` ordinary entries, `2` renames with the original path, `u` unmerged, `?` untracked and `!` ignored;
    - the `# branch.head/upstream/ab` headers.
  - It returns `Snapshot{Branch, Upstream, Ahead, Behind, Entries}`.
  - This one call replaces `IsRepo`, `CurrentBranch` and `Status`.
- **Async pane:**
  - `reload` returns a Cmd that produces `StatusLoadedMsg{seq}`. The pane keeps the last known state and shows a spinner in the meantime.
  - Diff loads are debounced by 50 ms on cursor moves and cached per (path, section, index mtime).
  - Highlighted diffs are capped at 200 KB, followed by `… N more lines · e open`.
  - `NewPane` makes **no** git calls.
- **Sections:** Staged / Unstaged / Untracked / Conflicts. An `MM` file appears under both Staged and Unstaged. Each section uses its own diff: `--cached` or the working tree.
  - Untracked files diff with `diff --no-index -- /dev/null path`.
- **Windowed change list:**
  - It scrolls to keep the cursor visible.
  - Paths are truncated in the middle: `Business.Experience…/Migrations/2025…Designer.cs`.
  - Status text is a single line.
- **Commit:**
  - The commit box becomes a `bubbles/textarea`: subject plus body, with a counter that turns red past 72 characters on line 1.
  - The `huh` confirm becomes optional (`git.confirm_commit = true` by default, to keep today's behaviour).
  - Commit and checkout run async. Use `switch` rather than `checkout`.
- **Header:** `⎇ feat/supercharge → origin/feat/supercharge ↑2 ↓0 · 3 staged · 5 unstaged`.
- **Routing:** `RefreshMsg`, `CommitMsgReady` and `StatusLoadedMsg` are exported and implement `ui.Routed` (`DestGit`).
- **Deferred to P2:** hunk and line staging, stash UI, full log browser with graph, blame, file history, word diff, `delta` integration, conflict resolution helpers.

## 10. Run and HTTP (audit P0/P1)

### 10.1 Runner

- **Process groups:**
  - Start each task with `SysProcAttr{Setpgid: true}`.
  - Set `cmd.Cancel = func() error { return syscall.Kill(-pid, SIGTERM) }` and `cmd.WaitDelay = 3s`.
  - After the delay, escalate to `SIGKILL` on the whole group.
- **`StopAll(timeout)`** stops every task. It is called on the quit path and again from `main` after `p.Run()` as a backstop.
- **Output batching:**
  - `proc.next` drains up to 256 lines, or 16 ms worth, into `OutputMsg{lines}`.
  - The log buffer appends incrementally, so it doesn't rebuild the whole join on every line.
- **Carriage returns:** a `\r` inside a line keeps only the text after the last `\r`. A trailing `\r` replaces the current line.
- **Colour:** tasks run with `FORCE_COLOR=1`, `CLICOLOR_FORCE=1` and `DOTNET_SYSTEM_CONSOLE_ALLOW_ANSI_COLOR_REDIRECTION=1`. A PTY is P2.
- **.NET detection:**
  - Walk at most 3 directory levels, skipping `bin`, `obj`, `node_modules` and `.git`.
  - Classify each csproj as Web (`Microsoft.NET.Sdk.Web`), Exe or Test (`IsTestProject`, or a reference to `Microsoft.NET.Test.Sdk`/xunit/nunit/mstest).
  - Emit `dotnet build <sln>`, `dotnet test <sln>`, `dotnet run --project X` and `dotnet watch --project X`.
  - `.slnx` files are included.
- **Other detection:**
  - Detect pnpm, yarn or bun from their lockfiles.
  - Show `+N more` instead of silently capping the list.
- **Problem matchers** (`runner/problems.go`, pure):

  | Toolchain | Pattern |
  |---|---|
  | dotnet / msbuild | `path(line,col): error CS0103: …` |
  | go | `file.go:12:5:` |
  | tsc | `file.ts(3,4):` |

  Matches feed the `e`/`E` navigation and `enter` → `editor.OpenMsg`.
- **Log viewer:**
  - Has its own focus, `/` search, wrap, `g`/`G`, and a follow indicator (`↓ 23 new`).
  - The header shows the exit code.
  - A failure turns the Run tab badge red and raises a toast.
- **Config:**
  - A project-local `.deck.toml` (found by walking up to `.git`) is merged over the global config (loaded in M2; the runner only reads it).
  - `Task` gains `dir`, `env` and `kind`.
  - Detected tasks are merged with configured tasks and marked `auto`.

### 10.2 HTTP

- **Modes:** `Capturing()` is true only in insert mode. A new `focusResponse` target scrolls the response.
- **URL scheme:** a URL without a scheme defaults to `http://` for `localhost`, `127.0.0.1` and `::1`.
- **Content-Type:** if the body is valid JSON and no Content-Type is set, deck adds `Content-Type: application/json`. Auto-added headers show greyed out.
- **Cancellation:** requests use `NewRequestWithContext`, so `esc` cancels one in flight.
- **Response metadata:**
  - The response headers are kept, and `[`/`]` switch Body/Headers.
  - Latency covers reading the body.
  - The response shows a `truncated at 1 MiB` badge when it was capped.
- **`.http` files:**
  - `httpc/httpfile.go` is a pure parser covering:
    - `###` separators;
    - `# @name`;
    - `@var = value`;
    - `{{var}}`, `{{$guid}}`, `{{$timestamp}}`, `{{$dotenv KEY}}`;
    - `http-client.env.json` environments.
  - The sidebar is built by globbing `**/*.http` with depth and ignore limits.
  - `ctrl+e` cycles the environment.
  - From the Files tab, `x` on a `.http` file sends the request under the cursor to the HTTP tab.
- **Deferred to P2:** history, collections, copy/paste as curl, the timing breakdown.

### 10.3 Quit

`q` or `ctrl+c` with running tasks shows a modal: `2 tasks running (api, web): quit and stop them? [y/N]`. Answering `y` runs `StopAll` and then quits. Pressing `ctrl+c` twice force-quits.

---

## 11. Audit items: selection

**P0, in scope** (all confirmed in code):

| # | Item | Milestone |
|---|---|---|
| 1 | Frame overflow/underflow across tabs (frame contract) | M2 |
| 2 | Slow preview scrolling: no mouse, synchronous highlighting, wheel moves the file cursor | M2 + M3 |
| 3 | Git from a subdirectory, quoted paths, stderr in diffs, key fall-through, `MM` handling | M9 |
| 4 | Refresh and commit messages delivered only to the active tab | M1 |
| 5 | Run stop doesn't kill the process tree; tasks orphaned on quit | M10 |
| 6 | HTTP response can't be scrolled | M10 |

**P1, in scope:**

| Item | Milestone |
|---|---|
| Commit helper wipes the message on failure; argv-size limit; no spinner or timeout | M1 |
| Capturing gate (HTTP, preview); alt+N / F1 capture-proof keys | M2 |
| Mouse support end to end | M2 + M3 + M4 |
| Config parse errors and unknown keys reported as warnings; project-local `.deck.toml`; full config schema | M2 |
| Help as a modal overlay | M2 |
| Toasts | M2 |
| Synchronous highlighting; no cache; silent truncation; `readCapped` short reads | M3 |
| .NET lexer map, directory/csproj cards | M3 |
| Line numbers, position indicator, wrap, editor at line | M3 |
| Up-directory keeps your place; esc clears an applied filter; symlinked directories; `l` doesn't launch the editor | M4 |
| Hidden/ignored toggles, sort modes, git decorations | M4 |
| File operations, finder, grep | M6 |
| Synchronous git calls | M9 |
| Git core workflows: discard, stage/unstage all, fetch/pull/push, branch create/delete, amend, textarea | M9 |
| Basic log list | M9 |
| Runner `\r` handling and output batching | M10 |
| .NET task detection | M10 |
| Log search and problem matchers | M10 |
| Re-run last | M10 |
| Failure notification | M10 |
| `.http` files and environments | M10 |
| Local-friendly HTTP defaults | M10 |
| Response headers and cancel | M10 |
| Tests: frame invariant, routing, process tree, subdirectory git, porcelain v2, viewer benchmarks | throughout |

**P2, future (not in this plan):**

- **Command palette** (`ctrl+k`): the picker already makes this cheap.
- **Themeable chrome palette** with presets, plus a generated matching micro colorscheme.
- **Shared verb table** enforced across panes, and `[keys]` overrides.
- **fsnotify live refresh** for the tree, viewer and git.
- **Git:**
  - hunk and line staging;
  - stash UI;
  - full log browser with graph;
  - blame (viewer `B`);
  - file history;
  - word diff and `delta`;
  - side-by-side diffs;
  - conflict resolution.
- **Files:**
  - images (chafa or the kitty protocol);
  - hexdump;
  - archive listing;
  - PE/assembly info.
- **HTTP:**
  - history and collections;
  - curl import and export;
  - timing breakdown;
  - redirect chain.
- **Run:**
  - PTY (`creack/pty`);
  - ad-hoc commands with history;
  - watch mode;
  - OSC 9 desktop notifications;
  - `launchSettings.json` profiles.
- **Layer 2:**
  - `deck lsp-attach` shim, so micro reuses deck's warm csharp-ls (the biggest .NET win; about 400 lines of code);
  - vim/nvim position report;
  - bookmark plugin;
  - `deck --doctor`.
- **CLI:**
  - `deck [path|file:line]`;
  - `--tab`;
  - `--version`;
  - `DECK_DEBUG` log file.
- **teatest golden snapshots.**

---

## 12. Configuration (final shape)

```toml
ai_backend = "claude"
ai_model   = "sonnet"
editor     = "micro"
theme      = "dracula"
mouse      = "hover"
editor_return = "follow"
micro_bundle  = true

[files]
sidebar_width = 32
show_hidden   = false
hide          = ["bin", "obj", ".git", "node_modules", ".vs", ".idea", "TestResults"]
icons         = "unicode"
info_panel    = false

[viewer]
tab_width      = 4
wrap           = false
line_numbers   = true
scroll_lines   = 3
hover_delay_ms = 350
goto_click     = ["ctrl", "double", "right"]
max_highlight_bytes = 2000000

[lsp]
enabled            = true
prestart           = true
request_timeout_ms = 8000
[lsp.servers.csharp]
command      = ["csharp-ls", "--features", "metadata-uris"]
root_markers = ["*.sln", "*.slnx", "*.csproj"]
solution     = ""
enabled      = true

[git]
confirm_commit = true

[[tasks]]
name = "api"
cmd  = "dotnet watch --project src/Api"
dir  = ""
kind = "serve"
```

- **`Load` signature:** `Load() (Config, []Warning)`.
  - A decode error becomes a warning, and the defaults are kept.
  - Each key reported by `md.Undecoded()` becomes a warning with a suggestion for the closest known key.
- **Defaults** are written once in `Default()`. Zero fields are filled from `Default()`, so the defaults are no longer duplicated.
- **`$XDG_CONFIG_HOME`** is honoured.

---

## 13. Testing strategy

- **Keep the repo's style.** Heavy logic lives in pure functions with table tests, and panes are driven through `Update` with `tea.KeyMsg`/`tea.MouseMsg`. Commands returned by `Update` are run synchronously in tests. No `tea.Program`.
- **Invariant tests** (added in M2 and extended by each milestone):
  - `TestFrameFitsTerminal`: every tab × {60×16, 80×24, 120×40, 200×50} with long-name fixtures.
  - `TestAsyncRoutedWhileOtherTabActive`: each `ui.Routed` message type reaches its destination while a different tab is active.
- **Fakes:**
  - `lsptest` is a scripted in-process server over `net.Pipe`.
  - The viewer uses a `Navigator` stub.
  - Lookup functions (`LookPath`, home directory, micro version) are injected.
  - The runner uses a real `sh -c 'sleep 30 | cat'` for the process-tree test (finishes in under 5 s).
- **Integration tests** skip when their tool is missing, following the existing `git/integration_test.go` pattern:
  - real `git` in temp repos: running from a subdirectory, a path with spaces and unicode, `autocrlf`, `MM`;
  - real `gopls` against a temp module: hover and definition;
  - an optional `micro` smoke test behind the build tag `microsmoke`.
- **Benchmarks** (regression guards; CI doesn't need to enforce them):
  - `BenchmarkViewerView` (target under 1 ms);
  - `BenchmarkHighlight1MB`;
  - `BenchmarkTreeFlatten10k`.
- **Golden tests:**
  - `z_deck.lua` rendering;
  - the micro `settings.json` merge;
  - `FormatMeta`;
  - `parseRGJSON`;
  - `ParseStatusV2`;
  - the problem matchers;
  - `httpfile`.
- **Race detector:** `go test -race ./...` must stay clean for `lsp`, `runner` and `viewer`, since they use goroutines and channels.

## 14. Risks

| Risk | Mitigation |
|---|---|
| csharp-ls cold start (10–60 s on large solutions) | Prestart after the first frame; status-bar progress; "results may be incomplete" message; P2 attach shim for micro |
| Terminals turn ctrl+click into a right-click | `goto_click` accepts ctrl, double or right; `gd`/`F12` |
| All-motion mouse floods `Update` | Drop duplicate motion; hover-target-changed check; O(visible) View |
| mlsp internals change (monkeypatched `handleRequest`) | Pinned SHA; golden test; `micro_lsp = "off"` fallback |
| micro Lua scroll API unverified | Fall back to `bp:Center()`; smoke test |
| `ui.Overlay` breaks ANSI sequences | Tests that strip ANSI and compare visible width; never cut inside an escape sequence |
| Many milestones edit `app.go` | `ui.Routed` removes per-message routing cases; app changes are kept small in each milestone |
