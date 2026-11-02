# deck supercharge: implementation plan

Spec: `docs/superpowers/specs/2026-11-02-deck-supercharge-design.md` (section numbers below refer to it)
Branch: `feat/supercharge`. Each milestone is built on its own branch or Orca worktree, cut from `feat/supercharge`, and merged back when it is done.

## Rules for every milestone

- **Green gates.** Every milestone must leave all of these passing:
  - `gofmt -l . | wc -l` == 0
  - `go vet ./...`
  - `go build ./...`
  - `go test ./...`
  - `go test -race` for the packages it touches that use goroutines (`lsp`, `runner`, `viewer`, `files`, `git`)
- **Write tests first.** Every task lists the failing test(s) to write before the code. Pure helpers get table tests. Panes are tested by calling `Update` with `tea.KeyMsg`/`tea.MouseMsg` and running the returned commands synchronously. Never use a `tea.Program`.
- **Comments: zero by default.** Use one line, and only for constraints the code can't express. Narrative goes in commit messages. Keep doc summaries to at most 2 lines. Before committing, check the diff for comments.
- **Keep startup fast.** No milestone may add synchronous I/O beyond the cwd listing to `app.New`/`main`. Anything slower is a `tea.Cmd` started from `Init`.
- **Update docs as you go.** Change `CLAUDE.md` in the same milestone that changes the architecture it describes.
- **Commit trailer:** `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Dependency graph and parallelism

```
M1 ──► M2 ──┬─► M3 ──┬────────────► M4 ──► M6 ──► M7
            │        │               ▲              ▲
            ├─► M9 ──┼───────────────┘              │
            │        │                              │
            └─► M10  └─► M8 ◄── M5 ─────────────────┘
M1 ──► M5
```

| ID | Title | Depends on | Parallel-safe with |
|---|---|---|---|
| M1 | Remove AI tab; extract commit helper; fix cross-tab routing | none | none (must run first) |
| M2 | Chrome foundation: routing contract, frame contract, mouse, toasts, help overlay, config schema | M1 | M5 |
| M3 | Viewer core: fast async code viewer in the Files tab | M2 | M5, M9, M10 |
| M4 | File tree sidebar, metadata hover, info panel, git badges, directory cards | M3, M9 | M5, M8, M10 |
| M5 | `internal/lsp` client core (no UI) | M1 | M2, M3, M4, M6, M9, M10 |
| M6 | Picker, find-file, grep, file operations | M4 | M5, M8, M10 |
| M7 | LSP navigation in the viewer (hover, definition, references, outline, symbols, diagnostics, status) | M5, M6 | M8, M10 |
| M8 | Layer 2: supercharged micro (bundle, positions, return-to-spot) | M3, M5 | M4, M6, M7, M9, M10 |
| M9 | Git overhaul (porcelain v2, repo-root, async, sections, workflows) | M2 | M3, M5, M8, M10 |
| M10 | Run and HTTP overhaul (process groups, StopAll, .NET detection, problems, HTTP modes, .http files) | M2 | M3, M4, M5, M6, M7, M8, M9 |

**What "parallel-safe" means here.** The milestones edit disjoint package directories. There are known small merge points:
- `internal/app/app.go`: M3, M7, M8 and M10 each add one isolated block.
- `main.go`: M7 and M10.

M2 removes most of the routing churn by adding the `ui.Routed` contract and by defining the **whole** config schema up front. Later milestones only read config. They never add fields.

**Suggested waves:**
1. M1
2. M2 ∥ M5
3. M3 ∥ M9 ∥ M10
4. M4 ∥ M8
5. M6
6. M7

---

## M1: Remove the AI tab, extract the commit helper, fix cross-tab routing

**Goal.** Remove the AI tab completely. `ctrl+g` in Git keeps generating commit messages through a new `internal/aicommit` package, and it no longer wipes what the user typed. Git refresh and commit messages reach the Git pane whichever tab is active.

**Depends on:** none.
**Parallel-safe with:** none.

**Files.**
- **Create:**
  - `internal/aicommit/aicommit.go`
  - `internal/aicommit/aicommit_test.go`
- **Delete:**
  - `internal/ai/ai.go`
  - `internal/ai/ai_test.go`
  - `internal/ai/pane.go`
  - `internal/ai/pane_test.go`
- **Modify:**
  - `internal/app/app.go`
  - `internal/app/app_test.go`
  - `internal/ui/keys.go`
  - `internal/files/files.go`
  - `internal/files/files_test.go` (if it references `a`/`t`)
  - `internal/git/pane.go`
  - `internal/git/pane_test.go`
  - `internal/config/config.go` (doc line only)
  - `main.go` (doc line)
  - `CLAUDE.md`

**Tasks.**

1. **aicommit package.**
   - **Tests first:**
     - Move the existing `BuildArgs` table tests from `internal/ai/ai_test.go` and add a case for each backend.
     - `TestGenerateSendsDiffOnStdin`: inject `var execCommand = exec.CommandContext` and point it at a helper process (`os.Args[0] -test.run=TestHelperProcess`) that echoes stdin. Assert that the diff arrives on stdin, not in argv.
     - `TestGenerateStripsFences`: input wrapped in triple-backtick fences comes back as plain text.
     - `TestGenerateTimeout`: a helper that sleeps, called with a 50 ms context, returns `context.DeadlineExceeded`.
     - `TestCapDiff`: a diff over 60 KB is cut at a line boundary and `\n[diff truncated; N more bytes]` is appended.
   - **Implement:**
     - `BuildArgs(backend, model, instruction string) (string, []string)`.
     - `Generate(ctx context.Context, backend, model, diff string) (string, error)` with stdin = the capped diff, stderr captured into the error, and fences stripped.
     - `Available(backend string) bool`, using an injectable `exec.LookPath`.
   - **Builder's call:** confirm that each CLI (`claude -p`, `codex exec`, `crush run`) reads stdin as context. If one does not, that backend falls back to appending the capped diff to the argv instruction. Record the result in the test table.

2. **Git pane commit message.**
   - **Tests first** (`internal/git/pane_test.go`):
     - `TestCommitMsgErrorKeepsTypedText`: the box holds `"wip"`, `CommitMsgReady{Err: errors.New("x")}` arrives, the box still holds `"wip"` and the status line contains `x`.
     - `TestCommitMsgSuccessSetsText`.
     - `TestGenerateShowsSpinnerUntilReady`.
   - **Implement:**
     - `CommitMsgReady{Text string; Err error}`.
     - A `generating bool` flag that drives a spinner in the commit-box prompt.
     - `esc` while generating cancels: the pane stores the `context.CancelFunc` behind a pointer.
     - Hide the `ctrl+g` hint when `aicommit.Available(cfg.AIBackend)` is false. `git.NewPane` takes a `commitAI bool`.

3. **Export Refresh.**
   - **Test first** (`internal/app/app_test.go`): `TestEditorCloseRefreshesGitWhileOnFiles`. Construct the model, stay on Files, deliver `editorClosedMsg{}`, run the returned cmd, feed its message back, and assert that the Git pane handled it. Use a test seam: `git.Pane.Reloads() int`, exposed through `export_test.go`.
   - **Implement:** rename `refreshMsg` → `RefreshMsg`. Route `git.RefreshMsg` and `git.CommitMsgReady` to `m.git` from `app.Update`, whatever tab is active. This is a temporary type switch; M2 replaces it with `ui.Routed`.

4. **Remove the AI tab from app.**
   - **Tests first:** update `app_test.go`:
     - `TestNumberKeysJump` expects `'3'` → `tabRun` and `'4'` → `tabHTTP`.
     - `TestTabCyclesPanes`: shift+tab from Files lands on `tabHTTP`.
     - The AI-capture tests are replaced by HTTP-tab equivalents (`'4'` then `q` does not quit; `ctrl+c` quits).
     - `TestStatusLine("main", 3, 2, "deck")` == `"deck · ⎇ main · 3 dirty · ▶ 2 running"`.
     - `TestNoAITab`: `len(tabNames) == 4`, and `"AI"` is not among them.
   - **Implement:**
     - Delete `tabAI` and every reference to it: the struct field, `New`, `resize`, `capturing`, `globalKey`, `routeToPane`, `paneView`, `paneBindings`, and the spinner fan-out.
     - `generateCommitMsg` calls `aicommit.Generate` with a 90 s context.
     - `statusLine(project, branch, dirty, running)`, where project = the basename of the git toplevel or the cwd. It is computed once in `New`; the cwd is enough here, and M9 refines it.
     - `ui.Keys`: remove `AI`, renumber Run to `3` and HTTP to `4`.

5. **Files cleanup.**
   - Remove `SendToAIMsg`, `SendTestsMsg`, the `a`/`t` cases and their help entries, and the doc reference.
   - **Test:** `TestNoAIKeys`. Pressing `a` on a file returns a nil cmd. This frees the key for M6.

6. **Docs.** Update `CLAUDE.md`:
   - four tabs;
   - remove the AI pane and streaming bullets;
   - describe `aicommit`;
   - the routing note.

   Also fix the config doc comment that mentions aiassist, and the `main.go` doc comment.

**Acceptance.**
- `internal/ai` does not exist, and `grep -rn '"deck/internal/ai"'` finds nothing.
- The tabs are Files, Git, Run, HTTP, reached with `1`–`4`.
- `ctrl+g` with the claude CLI fills the commit box. If the CLI is missing, the message is kept, an error status shows, and the hint is hidden.
- After closing micro from Files, the Git pane and the dirty count in the status bar are current without visiting Git.
- All gates are green.

---

## M2: Chrome foundation

**Goal.** Build the app-level infrastructure every later milestone relies on:
- the `ui.Routed` routing contract;
- the exact-size frame contract;
- mouse plumbing and tab-bar clicks;
- toasts;
- the help modal overlay;
- capture-proof keys;
- the complete config schema, with warnings and a project-local `.deck.toml`.

**Depends on:** M1.
**Parallel-safe with:** M5.

**Files.**
- **Create:**
  - `internal/ui/route.go`
  - `internal/ui/rect.go`
  - `internal/ui/overlay.go`
  - `internal/ui/fit.go`
  - `internal/ui/toast.go`
  - `internal/ui/click.go`
  - their `_test.go` files
  - `internal/app/mouse.go`
  - `internal/app/layout_test.go`
- **Modify:**
  - `internal/app/app.go`
  - `internal/app/app_test.go`
  - `internal/ui/keys.go`
  - `internal/ui/styles.go` (add `ColorWarn`, `ColorInfo`, `Dim`)
  - `internal/config/config.go`
  - `internal/config/config_test.go`
  - `main.go`
  - each pane's `View`/`SetSize`, only to call `ui.Fit` and honour the outer-box contract: `internal/files/files.go`, `internal/git/pane.go`, `internal/runner/pane.go`, `internal/httpc/pane.go`
  - `go.mod`: promote `github.com/charmbracelet/x/ansi` to a direct dependency

**Tasks.**

1. **`ui.Fit` and the frame contract.**
   - **Tests first:**
     - `TestFitPadsAndClips` (ANSI-aware: clips wide styled lines without splitting escape sequences, pads to exactly `h` rows).
     - `internal/app/layout_test.go` `TestFrameFitsTerminal`: for each tab × {60×16, 80×24, 120×40, 200×50}, with a temp cwd holding 60-character file names, assert `lipgloss.Height(View()) == h` and `lipgloss.Width(View()) <= w`. It fails today.
     - `TestTooSmall`: at 50×12 the frame contains `terminal too small`.
   - **Implement:**
     - `Fit(s string, w, h int) string`.
     - Remove `paneSlack`, so `paneH = h - 5`.
     - Each pane treats `SetSize(w, h)` as the outer box and ends `View` with `ui.Fit`.
     - In the Files pane, show the header path relative and left-truncated (`ansi.TruncateLeft`), truncate rows in `fileDelegate.Render`, and move errors into the footer or status area.
     - In the Git pane, a minimal fix for this milestone: clip the list to height and keep the cursor row visible with an offset. M9 replaces this.
     - In HTTP, `rh` follows `h`, and the minimum is 1, not 3.

2. **Routing contract.**
   - **Tests first:** `TestAsyncRoutedWhileOtherTabActive`. A table of (message, destination): `git.RefreshMsg`, `git.CommitMsgReady`, `runner.OutputMsg`, `runner.ExitMsg`, `httpc.RespMsg`. Each is delivered while a different tab is active, and the test asserts the destination pane changed through test seams (`export_test.go` per package).
   - **Implement:**
     - `ui.Dest` and `ui.Routed`.
     - `RouteTo()` methods on those messages.
     - `app.Update` gets `case ui.Routed: return m.routeTo(msg.RouteTo(), msg)`. Delete the per-type cases.

3. **Toasts.**
   - **Tests first:**
     - `TestToastExpires`: show the toast, deliver `toastExpireMsg{id}`, and it is gone.
     - `TestToastNewestWins`.
     - `TestStatusBarShowsToastRight`, which uses a pure `statusLine(left, right []string, w)`.
   - **Implement:**
     - `ui.ToastMsg{Level, Text, TTL}` with `RouteTo() == DestApp`.
     - A toast queue in the app model; expiry runs through `tea.Tick`.
     - A non-nil `editorClosedMsg.err` becomes an error toast.

4. **Mouse plumbing.**
   - **Tests first:**
     - `TestTabHitTest`: a pure `tabHitTest(x, widths)`.
     - `TestClickOnTabSwitches`.
     - `TestMouseYTranslated`: a mouse message reaching the active pane has `Y - tabBarHeight`, checked through a seam that records the last mouse message the Files pane received.
     - `TestDuplicateMotionDropped`.
     - `TestClickTrackerDouble`: two presses within 400 ms on the same cell count as a double-click. Different cells, or a gap over 400 ms, do not.
   - **Implement:**
     - `config.Mouse` maps to program options in `main.go`: `hover` → `WithMouseAllMotion`, `click` → `WithMouseCellMotion`. Always pass `WithReportFocus`.
     - `alt+m` toggles the mouse with `tea.DisableMouse` / `tea.EnableMouseAllMotion`, and the footer shows `mouse off`.
     - `ui.Rect`, `ui.ClickTracker`.
     - The Files pane routes wheel events over the preview to the viewport, by X: `x >= listW` scrolls the preview 3 lines; otherwise it moves the list. This is an interim fix for slow scrolling until M3.

5. **Capture rules and capture-proof keys.**
   - **Tests first:**
     - `TestAltDigitJumpsWhileCapturing`: on HTTP, which captures until M10, `alt+1` → Files.
     - `TestF1HelpWhileCapturing`.
     - `TestFilesPreviewDoesNotCapture`: after focusing the preview, `q` quits.
     - `TestFooterWhileCapturing` shows `esc normal` and not `q quit`.
   - **Implement:**
     - Add `alt+1..4`, `F1` and `alt+m` to `ui.Keys` and check them before the gate.
     - Files `Capturing()` returns `list.FilterState()==Filtering` only.

6. **Help overlay.**
   - **Tests first:**
     - `TestOverlayPreservesWidth`: `ui.Overlay(bg, fg, x, y)`. Width per row is unchanged, `ansi.Strip` of untouched regions equals the original, and escape sequences are never split.
     - `TestHelpIsModal`: the help view still contains the tab bar row, and `/` filters the bindings.
   - **Implement:**
     - `ui.Overlay`.
     - The help modal is a box centred over a dimmed body (`ui.Dim`), grouped Global / tab / Mouse, scrollable with `j`/`k`, filtered with `/`, closed with `esc`/`?`/`F1`.

7. **Config schema (final, spec §12).**
   - **Tests first** (`config_test.go`):
     - `TestDefaultsFilledOnce`.
     - `TestDecodeErrorBecomesWarning`.
     - `TestUnknownKeyWarningSuggests`: `[[task]]` gives a warning mentioning `tasks`.
     - `TestProjectLocalMerges`: `.deck.toml` at the repo root overrides `theme` and appends `tasks`.
     - `TestXDGConfigHome`.
     - `TestNestedDefaults`: `viewer.scroll_lines == 3` and `lsp.servers` is empty, because the defaults live in `lsp`.
   - **Implement:**
     - The structs `Files`, `Viewer`, `LSP{Enabled, Prestart, RequestTimeoutMS, Servers map[string]LSPServer}`, `Git{ConfirmCommit}`.
     - `Task{Name, Cmd, Dir, Env, Kind}`.
     - Top-level `Mouse`, `EditorReturn`, `MicroBundle`.
     - `Load() (Config, []Warning)`. `main` passes the warnings to `app.New(cfg, warnings)`, and they show as toasts.

8. **Docs.** Update `CLAUDE.md`: the routing contract, the frame contract, the capture rules, mouse, and toasts.

**Acceptance.**
- At any terminal size from 60×16 upwards, the tab bar never scrolls off and the frame is exactly the terminal height on every tab.
- Mouse wheel over the Files preview scrolls the text, not the file list.
- Clicking a tab switches to it.
- `alt+1..4` and `F1` work while typing.
- A broken `config.toml` shows a toast instead of silently using the defaults.
- All gates are green.

---

## M3: Viewer core (fast async code viewer)

**Goal.** Replace the Files preview viewport with `internal/viewer`. It provides:
- async load, highlighting and an LRU cache;
- rendering whose cost depends only on the visible rows;
- a cursor;
- scrolling (keys and wheel, with acceleration);
- in-file search and goto line;
- line numbers, wrap, and a markdown toggle;
- the .NET lexer map and a truncation banner;
- `e` to open the editor at the exact line, column and top.

This milestone fixes the slow scrolling.

**Depends on:** M2.
**Parallel-safe with:** M5, M9, M10.

**Files.**
- **Create** in `internal/viewer/`:
  - `document.go`, `highlight.go`, `cache.go`, `geometry.go`, `spans.go`, `words.go`, `search.go`, `history.go`, `chord.go`, `lexmap.go`, `viewer.go`, `render.go`, `messages.go`
  - their tests, plus `bench_test.go`
- **Modify:**
  - `internal/files/files.go`: embed `viewer.Model`, add the focus model, route mouse by region, handle `files.RevealMsg` (list-based selection plus viewer goto).
  - `internal/files/preview.go`: shrink to the markdown/glamour and mermaid helpers with a cached renderer, or move them into `viewer/markdown.go` (builder's call).
  - `internal/files/*_test.go`
  - `internal/editor/editor.go`: add `Target{Path; Line, Col, Top int}`, `OpenMsg{Target}` with `RouteTo()==DestApp`, and `Command(editor string, t Target)`, which adds `+L:C` for micro only. M8 generalises this.
  - `internal/editor/editor_test.go`
  - `internal/app/app.go`: the editor-open case uses `editor.OpenMsg`.
  - `CLAUDE.md`

**Tasks.**

1. **Pure geometry and text helpers.**
   - **Tests first:**
     - `geometry_test.go`: table tests for `ExpandTabs`, `DispCol`/`RuneCol` with tabs, CJK and emoji; `ScreenToDoc`/`DocToScreen` with gutter, horizontal offset and wrap rows; and a property round-trip over generated lines (`testing/quick` or a seeded rand).
     - `words_test.go`: `@class`, `List<T>` stops at `<`, `_field`, unicode identifiers.
     - `search_test.go`: smart-case, regex, wrap-around, Next/Prev.
     - `chord_test.go`: `gg`, `gd` vs `gg`, `zz`, `]d`, abort on a non-matching key.
     - `history_test.go`: Push, Back, Forward, truncation of the forward list, dedupe, max size.
   - **Implement** the helpers as pure functions or types.

2. **Document load and highlighting.**
   - **Tests first:**
     - `TestLoadReadsWholeFileNoShortRead`: uses an `iotest.OneByteReader` seam (`readFile` takes an `io.Reader`).
     - `TestLoadDetectsBinary`.
     - `TestLoadTruncatesOver20MB`: inject the limit and check the banner.
     - `TestHighlightLinesPreserveCount`: every line ends with `\x1b[0m`, and a multi-line C# `/* */` comment is styled on each line.
     - `TestHighlightSanitisesControlBytes`.
     - `TestLexMap`: `.csproj`/`.props`/`.targets`/`.resx`/`.xaml`/`.slnx` → XML, `.razor`/`.cshtml` → HTML, `.sln` → INI.
     - `TestCacheLRU`: the key includes mtime and size, and the oldest entry is evicted.
     - `TestStaleGenDropped`.
   - **Implement:**
     - `LoadCmd(path, gen)` → `DocLoadedMsg`.
     - `HighlightCmd(doc, gen, theme)` → `DocHighlightedMsg`, both with `RouteTo()==DestFiles`.
     - Chunked highlighting for documents over 2 MB or 50k lines.
     - A cache of SGR prefixes per (theme, colour profile). Truecolour is used when `COLORTERM` says so, otherwise 256 colours.

3. **Viewer model, keys and rendering.**
   - **Tests first** (drive `Update`):
     - `TestJScrollsWithCursor`.
     - `TestCtrlDHalfPage`.
     - `TestWheelScrollsThreeLines`.
     - `TestWheelAcceleration`: inject a clock and check events less than 40 ms apart.
     - `TestGGandG`.
     - `TestColonGotoLineCol`.
     - `TestSearchHighlightsAndCounts` (`3/17` in the footer).
     - `TestEscClearsSearch`.
     - `TestToggleLineNumbersAndWrap`.
     - `TestViewHeightExact` (frame contract).
     - `TestEOpensEditorAtCursor`: `e` emits `editor.OpenMsg{Target{Line: cur+1, Col: rune+1, Top: top+1}}`.
     - `TestCapturingOnlyInPromptOrChord`.
     - `TestYCopiesRelPathLine`: OSC52 runs as a cmd, and the test asserts the emitted payload through a seam.
   - **Benchmark:** `BenchmarkViewerView` with a 200-row viewport over 50k lines, target under 1 ms.
   - **Implement:**
     - `Model` with `Loc`, top/left, visible rows computed in `Update` (`relayout`), the gutter, the footer and the breadcrumb placeholder (relative path).
     - Mouse handling: wheel scrolling, horizontal scrolling with shift+wheel, and click to place the cursor through `ScreenToDoc`.
     - Markdown is rendered by default. `m` switches to raw.

4. **Files integration.**
   - **Tests first** (`files_test.go`):
     - `TestSelectionLoadsAsync`: moving the list returns a `LoadCmd`, and `Update` never calls highlight synchronously. Assert through a counter seam.
     - `TestEnterOnFileFocusesViewer`, plus `l`/→.
     - `TestEscReturnsToList`.
     - `TestLDoesNotLaunchEditor`.
     - `TestUpDirSelectsChild`.
     - `TestEscClearsAppliedFilter`.
     - `TestSymlinkedDirIsDir`: `os.Symlink` in `t.TempDir`.
     - `TestMouseClickFocusesRegion`.
     - `TestRevealMsgSelectsAndGoes`.
     - `TestPerDirPositionRemembered`.
   - **Implement:**
     - Replace the preview viewport with `viewer.Model`.
     - Focus rules from spec §6.1 (interim: the list stays on the left).
     - `e` from the list opens the editor at line 1.
     - Fix `readDir` symlinks.
     - Keep a map of cursor and viewer `Loc` per directory.

5. **app.** `editor.OpenMsg` → `editor.Command(cfg.Editor, t)` + `tea.ExecProcess`. `editorClosedMsg` returns `tea.Batch(git.Refresh, files.ReloadCurrent)`, which restores the same `Loc`.

6. **Docs.** Update `CLAUDE.md` (Files and viewer architecture).

**Acceptance.**
- Wheel and `ctrl+d` through a 5k-line `.cs` file stay smooth, and the benchmark is under 1 ms.
- Holding `j` in the list never stalls: the preview shows plain text at once and the highlighting replaces it.
- A 472 KB `Designer.cs` shows in full.
- `.csproj` and `.razor` files are highlighted.
- `e` opens micro at the cursor line and column, and closing it returns to the same position.
- All gates are green.

---

## M4: File tree sidebar, metadata hover, info panel, git badges, directory cards

**Goal.** Replace the flat listing with a lazily loaded, collapsible tree that stays visible while you read. It includes:
- hide and ignore policies and sort modes;
- git status badges;
- a metadata tooltip on mouse hover and an `i` info panel;
- directory and `.csproj`/`.sln` cards in the viewer.

**Depends on:** M3, M9. M9 provides `git.Repo`/`StatusSnapshot` for the badges and the ignored set.
**Parallel-safe with:** M5, M8, M10.

**Files.**
- **Create** in `internal/files/`:
  - `tree.go`, `hide.go`, `sort.go`, `icons.go`, `gitbadges.go`, `meta.go`, `dircard.go`, `layout.go`
  - their tests
- **Modify:**
  - `internal/files/files.go`: the container now holds the tree, the viewer and the info panel. The `bubbles/list` listing is removed.
  - `internal/files/files_test.go`
  - `internal/app/app.go`: one line to pass a `files.BadgeSource` adapter built from `git.Repo`.
  - `CLAUDE.md`

**Tasks.**

1. **Tree model.**
   - **Tests first:**
     - `TestFlattenRespectsExpandAndHide`.
     - `TestLazyLoadOnExpand`.
     - `TestRevealExpandsAncestorsAndSelects`.
     - `TestRerootUpSelectsChild`.
     - `TestTildeReturnsToLaunchRoot`.
     - `TestSortModes` (name, mtime, size, ext; directories first).
     - `TestHidePolicy` (defaults, `.` toggles dotfiles, `I` toggles ignored).
     - `TestRowTruncatedToWidth`.
     - `TestSymlinkBrokenMarked`.
     - `BenchmarkTreeFlatten10k`.
   - **Implement:** `Node`, `Tree`, `Flatten`, `Reveal`, and loading of very large directories (over 5k entries) asynchronously as a Cmd.

2. **Layout and focus.**
   - **Tests first:**
     - `TestSidebarCollapseCtrlB`.
     - `TestAutoCollapseBelow90Cols`.
     - `TestResizeAngleBrackets`.
     - `TestDragDivider` (press, motion with the button held, release).
     - `TestFocusBorders` (the active region uses the `PaneActive` border colour).
     - `TestViewFitsAllSizes` with the info panel on and off.
   - **Implement:** `layout.go` computes the rects for the tree, viewer and info panel, and mouse events are routed through `Rect.Contains`/`Local`.

3. **Git badges.**
   - **Tests first:**
     - `TestAggregateStrongestChild`.
     - `TestBadgeColours`.
     - `TestBadgesRefreshOnRoutedGitMsg`: a `git.StatusLoadedMsg` fanned out to `DestFiles` updates the badges.
   - **Implement:**
     - `type BadgeSource func(ctx context.Context, root string) (Badges, error)`, where `Badges` holds status per path plus the ignored set.
     - `Aggregate`.
     - The refresh triggers listed in spec §6.5.
     - The app adapter in `app.New` wraps `git.Repo.StatusSnapshot(ctx, ignored=true)`.

4. **Metadata hover and the info panel.**
   - **Tests first:**
     - `TestFormatMeta` golden: file, directory (including partial counts) and `.csproj` extras.
     - `TestStatCmdCapsDirWalk` (inject limits).
     - `TestHoverTooltipAfterDwell`: a motion event, then `hoverDueMsg{seq}`, shows the tooltip. Moving to another row invalidates the seq.
     - `TestInfoPanelToggleI`.
   - **Implement:**
     - `Meta`, `StatCmd` (`RouteTo()==DestFiles`), `FormatMeta`.
     - The tooltip is placed with `viewer.PlacePopup` (reused) and composed with `ui.Overlay`.
     - Promote `go-humanize` to a direct dependency, or write a 10-line formatter (builder's call).

5. **Directory and project cards.**
   - **Tests first:**
     - `TestCsprojCard` parses `TargetFramework`, `Sdk`, `OutputType`, `PackageReference`/`ProjectReference` counts from fixture XML.
     - `TestSlnProjects` parses `Project(...)` lines.
     - `TestDirCardListsChildrenWithBadges`.
   - **Implement:** a viewer "virtual document" source for directories. The viewer accepts a pre-rendered `*Document` built from the card lines.

**Acceptance.**
- The tree sits beside the viewer and switching files keeps the tree's expansion.
- `bin` and `obj` are hidden by default.
- Hovering a file for 400 ms shows its size, mtime, mode, git status and line count.
- Folders with changes show aggregated badges.
- `ctrl+b` collapses the tree.
- All gates are green.

---

## M5: `internal/lsp` client core (no UI)

**Goal.** A standalone, fully tested LSP client package with no Bubble Tea import, as described in spec §6.4.

**Depends on:** M1.
**Parallel-safe with:** M2, M3, M4, M6, M9, M10. It touches only `internal/lsp/**`.

**Files.**
- **Create** in `internal/lsp/`:
  - `conn.go`, `protocol.go`, `decode.go`, `position.go`, `uri.go`, `registry.go`, `client.go`, `manager.go`, `events.go`, `ring.go`
  - `lsptest/server.go`
  - their tests, plus `integration_test.go` (gopls; skipped when absent)

**Tasks.**

1. **Framing and RPC.**
   - **Tests first** (`conn_test.go`, over `net.Pipe`):
     - `TestFramingSplitReads` (a header split across reads).
     - `TestConcurrentCallsMatched`.
     - `TestCancelSendsCancelRequest`.
     - `TestServerRequestConfigurationAnswered`.
     - `TestUnknownServerRequestMethodNotFound`.
     - `TestEOFFailsPending`.
     - `TestRPCErrorCancelledIsQuiet`.
   - **Implement:** `Conn`.

2. **Protocol and decoders.**
   - **Tests first** (`decode_test.go`):
     - Locations in every shape: `null`, a single `Location`, `[]Location`, `[]LocationLink` (using `targetSelectionRange`).
     - Hover as `MarkupContent`, `MarkedString`, `[]MarkedString` (fenced).
     - Symbols, both hierarchical and flat, with flat results rebuilt by containment and flattened in DFS order with `Depth` and `Parent`.
   - **Implement:** the types and decoders.

3. **Positions and URIs.**
   - **Tests first:**
     - Surrogate pairs and CJK, clamping past the end of a line, utf-8 vs utf-16.
     - Path ↔ URI round trips with spaces, `#` and unicode.
     - `csharp:/metadata/...` passthrough.

4. **Registry.**
   - **Tests first** (`registry_test.go`, on temp trees):
     - `TestFindRootSlnBeatsCsproj`.
     - `TestGoWorkBeatsGoMod`.
     - `TestMultipleSlnTieBreak`.
     - `TestBoundaryStops`.
     - `TestMergeOverDefaults` (field by field, plus adding a new server).
     - `TestBinaryFallbackDirs` (injected `lookPath` and home directory).
     - `TestResolveLanguageID` (`.tsx` → `typescriptreact`).
   - **Implement:**
     - `ServerSpec`, `DefaultSpecs()`.
     - `type Config struct{ Enabled, Prestart bool; RequestTimeout time.Duration; Servers map[string]ServerOverride }`, which `app` builds from `config.LSP` in M7.
     - `Resolve`, `FindRoot`.

5. **Client and Manager.**
   - **Tests first** (`client_test.go` with `lsptest`):
     - `TestStateStartingIndexingReady` (progress begin/report/end, checking the emitted `StatusEvent`s).
     - `TestReadyAfter2sWithoutProgress` (injected clock).
     - `TestDidOpenOnceAndLRUClose`.
     - `TestReloadSendsDidChangeVersionBump`.
     - `TestShutdownSequenceThenKill`.
     - `TestMissingBinaryFailsOnceNoRetry`.
     - `TestCrashRestartsOnce`.
     - `TestEventsDropLogWhenFull`.
     - `TestPrestartOnlyMatchingRoot`.
     - The client process factory is injectable, so tests run the fake server in process.
   - **Implement:** `Client` and `Manager` (`ClientFor`, `Prestart`, `Events`, `Status`, `Shutdown`). Processes start with `Setpgid`, and stderr goes to the ring buffer.

6. **Integration.** `TestGoplsHoverAndDefinition` against a temp module. It is skipped if `gopls` is not found through the registry lookup.

**Acceptance.**
- `go test -race ./internal/lsp/...` is green.
- `internal/lsp` imports nothing from `deck/internal/*` and nothing from `charmbracelet`.
- The gopls integration test passes on this machine.

---

## M6: Picker, find-file, grep, file operations

**Goal.** Build the shared fuzzy picker overlay and use it for `ctrl+p` find-file and `ctrl+f` project grep. Add tree file operations: new, rename, trash, copy path, reveal, shell.

**Depends on:** M4.
**Parallel-safe with:** M5, M8, M10.

**Files.**
- **Create:**
  - `internal/ui/picker/picker.go` and its test
  - In `internal/files/`: `finder.go`, `grep.go`, `ops.go`, `prompt.go`, `clipboard.go`, and their tests
- **Modify:**
  - `internal/files/files.go`
  - `internal/files/files_test.go`
  - `go.mod`: promote `github.com/sahilm/fuzzy`
  - `CLAUDE.md`

**Tasks.**

1. **Picker.**
   - **Tests first:**
     - `TestFuzzyRanksAndHighlights`.
     - `TestAppendStreams`.
     - `TestPeekEmittedOnMove`.
     - `TestEscCancels`.
     - `TestEnterAccepts`.
     - `TestGroupsRendered`.
     - `TestViewFits`.
     - `TestLargeListMatchesAsync` (over 50k items: matching runs as a cmd with a seq).
   - **Implement:**
     - `picker.Model` with title, items, query (`textinput`), selection and scroll.
     - Messages `PeekMsg`, `AcceptMsg`, `CancelMsg`.
     - `Capturing()` is always true while the picker is open.

2. **Finder.**
   - **Tests first:**
     - `TestIndexSourceSelection` (fd → git ls-files → walk, with injected lookups).
     - `TestFilenameBeatsPathScore`.
     - `TestPathColonLineJumps`.
     - `TestIndexCapped`.
     - `TestAcceptRevealsAndPushesHistory`.
   - **Implement:**
     - `FinderIndexMsg` (`DestFiles`), built on first use.
     - Peek loads the file into the viewer.
     - `enter` reveals the file in the tree.

3. **Grep.**
   - **Tests first:**
     - `TestParseRGJSON` on captured sample output, with byte→rune column conversion.
     - `TestGitGrepFallbackParse`.
     - `TestDebounceAndCancel` (the seq increases and stale chunks are dropped).
     - `TestTogglesRegexCaseGlob`.
   - **Implement:** a streaming `GrepChunkMsg`/`GrepDoneMsg` using the runner channel pattern, with `exec.CommandContext`.

4. **File operations.**
   - **Tests first** (`ops_test.go`, on `t.TempDir`):
     - `TestCreateFileAndDirWithParents`.
     - `TestRenameKeepsSelection`.
     - `TestTrashMovesToTrashDir` (injected trash directory).
     - `TestRefuseTrashWithoutTarget`.
     - `TestCopyPathRelAbs` (OSC52 payload seam).
     - `TestYYChord`.
   - **Implement:**
     - Inline prompts (`prompt.go`): `a` new, `r` rename (cursor before the extension), and `D` with a `y/N` confirm.
     - `o` runs `open -R`.
     - `!` runs `$SHELL` through `tea.ExecProcess`.
     - After each operation, reload and keep the selection.

**Acceptance.**
- In the .NET repo, `ctrl+p` → `LeadSnap` → `enter` opens and reveals the file in under 200 ms after the index is built.
- `ctrl+f` streams rg results, the selection peeks, and `enter` jumps.
- `a`, `r` and `D` work and keep your place.
- All gates are green.

---

## M7: LSP navigation in the viewer

**Goal.** Connect `internal/lsp` to the viewer through the `Navigator` interface. Add:
- hover (key and mouse dwell);
- definition, type definition and implementation (keys and ctrl/double/right-click);
- references with peek;
- the outline;
- workspace symbols;
- breadcrumbs;
- diagnostics;
- metadata documents;
- the LSP status segment;
- prestart, document re-sync after the editor closes, and shutdown.

**Depends on:** M5, M6 (the picker).
**Parallel-safe with:** M8, M10.

**Files.**
- **Create:**
  - In `internal/viewer/`: `navigator.go` (the interface and viewer-side types), `lspcmd.go`, `hover.go`, `popup.go`, `symbols.go`, `diagnostics.go`, and their tests
  - In `internal/files/`: `lspadapter.go` (wraps `*lsp.Manager` as a `viewer.Navigator`), `lspevents.go` (`LSPEventMsg` with `DestFiles`, `listenLSP`), and their tests
- **Modify:**
  - `internal/viewer/viewer.go`, `render.go`
  - `internal/files/files.go`: history across files, opening virtual documents
  - `internal/app/app.go`: `New(cfg, warnings, mgr)`, `Init` batches `listenLSP` and the prestart cmd, LSP badges in the status bar
  - `main.go`: build the manager from `config.LSP` → `lsp.Config`, `defer mgr.Shutdown(3s)`
  - `CLAUDE.md`

**Tasks.**

1. **Navigator and the stub.**
   - **Tests first:**
     - `TestNilNavigatorDegrades`: `gd` shows `no language server for .cs` once and never panics.
     - `TestStaleSeqDropped` for hover, definition, references and symbols.
   - **Implement:** the `Navigator` interface and the request cmds with per-kind seq, timeout and cancel.

2. **Definition, implementation and type definition.**
   - **Tests first:**
     - `TestGdSingleJumpPushesHistory`.
     - `TestGdMultipleOpensPicker`.
     - `TestCtrlClickRequestsAtClickedCell`: a stub records the `Loc`, and the adapter test checks the UTF-16 column for a line containing an emoji.
     - `TestDoubleClickAndRightClickPerConfig`.
     - `TestCrossFileJumpRevealsInTree`.
     - `TestMetadataURIOpensVirtualDoc`.
     - `TestIndexingMessageOnEmpty`.
     - `TestBackForwardAcrossFiles` (`ctrl+o`, `ctrl+]`, mouse back/forward).
   - **Implement** those features.

3. **Hover.**
   - **Tests first:**
     - `TestPlacePopupFlips` (pure).
     - `TestKShowsHover`.
     - `TestDwellOnlyOnWordChange`.
     - `TestHoverClosesOnMoveEscAndLeave`.
     - `TestHoverPopupScrolls`.
     - `TestHoverMarkdownRenderedCached`.
   - **Implement:** the hover popup composed with `ui.Overlay`, with an underline span on the target.

4. **References, outline and workspace symbols.**
   - **Tests first:**
     - `TestReferencesGroupedCurrentFileFirst`.
     - `TestPeekAndEscRestores`.
     - `TestOutlineIndentedWithGlyphs`.
     - `TestOutlineMarkdownFallback`.
     - `TestWorkspaceSymbolsDebounced`.
   - **Implement** these through the picker.

5. **Breadcrumbs and diagnostics.**
   - **Tests first:**
     - `TestBreadcrumbNesting` (pure).
     - `TestDiagnosticsGutterAndJump` (`]d`/`[d`).
     - `TestFooterShowsDiagnosticOnLine`.

6. **Wiring.**
   - **Tests first** (`app` and `files`):
     - `TestLSPEventsRoutedWhileOnGit`.
     - `TestStatusBarLSPBadges` (Indexing with percent, Ready, Failed).
     - `TestEditorCloseResyncsSavedDocs`: `files.InvalidateMsg` → `Reload` on the adapter stub.
     - `TestShutdownCalledOnQuit`: a fake manager with an injected interface.
   - **Implement:**
     - `main`/`app` wiring.
     - `L` in the Files tab opens the LSP log (ring buffer plus `LogEvent`s) in the picker.

7. **Manual check** on `~/Development/acc/Business-Experience-API`. The status bar shows the C# load progress, and each of these works: hover, `gd` into another project, into BCL metadata, `gr`, `o`, `ctrl+t`, and back/forward.

**Acceptance.**
- In the .NET solution, every G2 feature works once the status bar shows `C# ●`.
- With csharp-ls renamed away, the viewer still works and shows one hint.
- Quitting leaves no `csharp-ls` or `BuildHost` processes behind (`pgrep -f csharp-ls` is empty).
- All gates are green, including `-race`.

---

## M8: Layer 2, supercharged micro

**Goal.**
- Editor launch profiles that open at a position for every common editor.
- An embedded, pinned micro runtime: mlsp, filemanager, MicroOmni, editorconfig, the deck bridge plugin, settings, bindings, and a generated `z_deck.lua`.
- Return to the exact place in deck after micro closes.

**Depends on:** M3 (`editor.Target`/`OpenMsg`, `files.RevealMsg`) and M5 (`lsp.DefaultSpecs` and the registry lookup).
**Parallel-safe with:** M4, M6, M7, M9, M10.

**Files.**
- **Create:**
  - In `internal/editor/`: `profiles.go`, `micro.go`, `assets.go`, `merge.go`, `servers.go`, `state.go`, `root.go`, `doctor.go`, and their tests
  - `internal/editor/runtime/**` (layout in spec §7.2; **no directory named `micro`**)
  - `scripts/vendor-micro-plugins.sh`
  - `THIRD_PARTY_NOTICES.md`
- **Modify:**
  - `internal/editor/editor.go` (becomes a thin `Launch`)
  - `internal/editor/editor_test.go`
  - Move `internal/editor/colorschemes/dracula.micro` to `runtime/colorschemes/`
  - `internal/app/app.go`: open and close handling, including Detach mode
  - `internal/files/files.go`: `InvalidateMsg` handling, if not already present from M7; coordinate
  - `CLAUDE.md`

**Tasks.**

1. **Vendor the plugins.** Write `scripts/vendor-micro-plugins.sh`, which uses `curl` at pinned SHAs into `runtime/plug/<name>/` and includes each `LICENSE`. Run it once and commit the files. Write `THIRD_PARTY_NOTICES.md` with the URL, SHA and licence of each plugin.

2. **Profiles.**
   - **Tests first** (`profiles_test.go`): a table covering every editor in spec §7.1, including:
     - a user-supplied `-colorscheme`;
     - the template `{file}:{line}:{col}`;
     - `code` with and without `-w` (Detach vs Suspend);
     - an unknown editor gets only the path;
     - `Line == 0` gives no position arguments.
   - **Implement:** `Launch(editorCfg, Target, Opts) (*exec.Cmd, Mode, error)`.

3. **micro environment and root.**
   - **Tests first:**
     - `TestTruecolorOnlyWhenColorterm`.
     - `TestStateEnvSet`.
     - `TestProjectRootPrecedence`: sln, then go.mod/tsconfig/package.json, then `.git`, never above the cwd.
     - `TestCmdDirIsProjectRoot`.

4. **Asset sync and merge.**
   - **Tests first:**
     - `TestSyncWritesAllFiles`.
     - `TestSyncNoRewriteWhenManifestSame` (compare mtimes).
     - `TestSyncKeepsForeignDirs`.
     - `TestSyncSkipsOmniOnOldMicroOrMissingTools`.
     - `TestMergeAddsMissingOnly` (a user's `softwrap: true` survives).
     - `TestMergeLeavesUnparseableFile`.
     - `TestBindingsOmittedWhenToolMissing`.
   - **Implement:** `Sync` runs once as an `Init` cmd when `micro_bundle` is true. A micro version probe (`micro -version`) runs in the same cmd.

5. **Generated `z_deck.lua`.**
   - **Tests first:**
     - `TestZDeckGolden` for {csharp, go} detected and typescript missing.
     - `TestZDeckLuacParses`, run only when `luac` is on PATH.
   - **Implement** it from `lsp.DefaultSpecs()` merged with config, plus the `handleRequest` patch.

6. **Bridge plugin and return state.**
   - **Tests first** (`state_test.go`):
     - `TestReadStateGood`.
     - `TestReadStateMissing`.
     - `TestReadStateStaleIgnored`.
     - `TestReadStateOutsideRootFallsBack`.
     - `TestFollowVsOrigin`.
   - **Implement:**
     - `plug/deck/deck.lua`: `postinit` scroll, the `onAnyEvent` snapshot written only on change, `preQuit` and `preQuitAll` backstops, `onSave`, and `gotoAtMouse`.
     - In app: create a temp state file, then `Launch`, then `ExecProcess` or `Start`. On close: `ReadState`, then `files.RevealMsg`, `files.InvalidateMsg{Saved}`, `git.RefreshMsg`, and a toast on error. For Detach, show a toast `opened in VS Code`.

7. **Doctor.** `Check()` returns a list of issues, shown as toasts after `Sync`, once per change to the manifest or config hash. Test it with injected lookups.

8. **Smoke test** behind the build tag `microsmoke`. It launches micro in a pty (`script -q /dev/null micro +3:5 file` with `DECK_EDITOR_STATE`), sends `Alt-q`, and checks that the state file contains `3 5`.
   - **Builder's call:** check `GetView().StartLine` and `ScrollDown` from Lua here. If they fail, switch `postinit` to `bp:Center()`.

**Acceptance.**
- `e` in the viewer opens micro at the same line and column, with the same top line when the scroll API works.
- In micro:
  - `F12`/`Alt-d` goes to the definition with csharp-ls;
  - `Alt-k` shows the hover;
  - `Ctrl-p` opens the fuzzy finder;
  - `Alt-t` opens the tree.
- `Alt-q` returns to deck at the place micro was in.
- The user's existing `~/.config/deck/micro/settings.json` values are unchanged.
- `~/.config/micro` is untouched.
- All gates are green.

---

## M9: Git overhaul

**Goal.** Make the Git tab correct and fast:
- repo-root aware;
- porcelain v2 `-z`;
- stdout and stderr kept apart;
- async status and diff;
- separate Staged / Unstaged / Untracked / Conflicts sections;
- no keys falling through to the diff;
- a windowed list;
- a textarea commit box;
- discard, stage all and unstage all;
- fetch, pull and push;
- branch create and delete;
- amend;
- a basic log list.

It also exposes `Repo.StatusSnapshot` for the M4 badges.

**Depends on:** M2.
**Parallel-safe with:** M3, M5, M8, M10.

**Files.**
- **Create** in `internal/git/`:
  - `exec.go`, `repo.go`, `status.go`, `sections.go`, `pathtrunc.go`, `remote.go`, `loglist.go`
  - their tests, plus additions to `integration_test.go`
- **Modify:**
  - `internal/git/git.go` (thin wrappers over `Repo`, or remove it)
  - `internal/git/pane.go`
  - `internal/git/pane_test.go`
  - `internal/app/app.go`: `aicommit` now uses `Repo.StagedDiff`; the project name comes from `Repo.Top`
  - `CLAUDE.md`

**Tasks.**

1. **Exec and Repo.**
   - **Tests first:**
     - `TestRunSeparatesStderr` (integration test with `core.autocrlf=true`: the diff has no `warning:` line).
     - `TestEnvHasNoPromptAndNoLocks`.
     - `TestRepoFromSubdirStageAndDiff` (integration: chdir into `src/Api` and Stage/Diff a root-relative path).
     - `TestNotARepo`.
   - **Implement:** `runGit(ctx, dir, args...)`, `OpenRepo(ctx, cwd) (*Repo, error)`, and `-C top` on every call.

2. **Porcelain v2.**
   - **Tests first** (`status_test.go`, table):
     - ordinary entries;
     - a path with spaces and unicode;
     - a rename whose name contains ` -> `;
     - unmerged entries;
     - untracked entries;
     - ignored entries;
     - the branch headers, including `ab +2 -1` and no upstream.

     Plus the integration test `TestStatusQuotedPathStages`.
   - **Implement:** `ParseStatusV2` and `Repo.StatusSnapshot(ctx, includeIgnored)`.

3. **Sections and keys.**
   - **Tests first** (`pane_test.go`):
     - `TestMMInBothSections`.
     - `TestSpaceOnUnstagedEntryStages`.
     - `TestDiffYOffsetZeroAfterJAndSpace`.
     - `TestDiffFocusScrollsOnly`.
     - `TestLogModeJMovesLogCursor`.
     - `TestWindowedListKeepsCursorVisible`.
     - `TestPathMiddleTruncate` (pure).
     - `TestViewFits`.
   - **Implement:**
     - The section model.
     - Explicit focus (list vs diff).
     - Handled cases return early.
     - `a`/`A` stage all and unstage all.
     - `X` discards, after a confirm.

4. **Async loading.**
   - **Tests first:**
     - `TestNewPaneRunsNoGit` (a counter seam on `runGit`).
     - `TestReloadReturnsCmdAndKeepsState`.
     - `TestStaleStatusSeqDropped`.
     - `TestDiffDebounced`.
     - `TestDiffCachedPerIndexMtime`.
     - `TestDiffCappedAt200KB`.
   - **Implement:** `StatusLoadedMsg` and `DiffLoadedMsg` (`DestGit`; `StatusLoadedMsg` is also fanned out to `DestFiles` through a small `ui.Multi` destination or a second message, builder's call). Add a spinner.

5. **Commit box.**
   - **Tests first:**
     - `TestTextareaSubjectCounterRedOver72`.
     - `TestCtrlSCommits`.
     - `TestConfirmOptionalByConfig`.
     - `TestAmendToggle`.
     - `TestCommitAsync`.
     - `TestCtrlGKeepsTextOnError` (carried over from M1).
   - **Implement** the commit box with `bubbles/textarea`.

6. **Remotes and branches.**
   - **Tests first:**
     - `TestPushSetsUpstreamWhenMissing` (argument building).
     - `TestFetchPullPushAsyncStatusLine`.
     - `TestBranchPickerCursorOnCurrent`.
     - `TestBranchCreateDelete` (integration).
     - `TestSwitchNotCheckout`.
   - **Implement** these, using a 120 s context for remote operations.

7. **Header and log list.**
   - **Tests first:**
     - `TestHeaderAheadBehind`.
     - `TestLogListEnterShowsCommit`.
   - **Implement** a basic log list using `bubbles/list` over `log --format=%h%x00%ar%x00%an%x00%D%x00%s -n 200`, and `show --stat -p` in the diff pane.

8. **`e` on an entry** emits `editor.OpenMsg` at the first hunk's new-side line, using a pure `firstHunkLine(diff)`. If M3 has not merged yet, fall back to `editor.OpenMsg{Target{Path}}`. This is coordinated in the merge order.

**Acceptance.**
- Launched from `src/Api` of the .NET repo, staging and diffs work.
- A file named `My File.cs` stages correctly.
- CRLF warnings no longer appear in diffs.
- `MM` files show in both sections.
- `j`/`k` never move the diff.
- Switching to the Git tab never blocks on a 360 ms `git status`.
- All gates are green.

---

## M10: Run and HTTP overhaul

**Goal.**
- **Run:** process-group stop, `StopAll` on quit with a confirmation modal, batched output and `\r` handling, colour environment variables, better .NET detection, problem matchers with jump-to-editor, a focusable and searchable log, failure badges and toasts, and global re-run of the last task.
- **HTTP:** normal and insert modes, a scrollable response with headers, cancel, friendlier local defaults, `.http` files with environments.

**Depends on:** M2.
**Parallel-safe with:** M3, M4, M5, M6, M7, M8, M9.

**Files.**
- **Create:**
  - In `internal/runner/`: `procgroup.go`, `problems.go`, `dotnet.go`, `logview.go`, and their tests
  - In `internal/httpc/`: `httpfile.go`, `env.go`, `sidebar.go`, and their tests
  - `internal/app/quit.go` and its test
- **Modify:**
  - `internal/runner/runner.go`, `pane.go`, `logbuf.go`, and their tests
  - `internal/httpc/httpc.go`, `pane.go`, `httpc_test.go`
  - `internal/app/app.go`: the quit path, global `ctrl+r`, and the Run badge when a task fails
  - `main.go`: call `StopAll` after `p.Run()` as a backstop. The pane is reachable through the final model returned by `p.Run()`.
  - `CLAUDE.md`

**Tasks.**

1. **Process groups.**
   - **Tests first** (`runner_test.go`):
     - `TestStopKillsPipeline`: `sleep 30 | cat`, stopped, gives an `ExitMsg` within 5 s, and `pgrep -f "sleep 30"` is empty.
     - `TestRestartAfterStopFires`.
     - `TestStopAllWaits`.
   - **Implement:**
     - `Setpgid`.
     - `cmd.Cancel` sends SIGTERM to `-pid`.
     - `WaitDelay` of 3 s, then SIGKILL to the group.
     - `Pane.StopAll(timeout)`.

2. **Quit confirmation.**
   - **Tests first** (`app/quit_test.go`):
     - `TestQuitWithRunningAsks`.
     - `TestYStopsThenQuits`.
     - `TestDoubleCtrlCForces`.
     - `TestQuitIdleImmediate`.

3. **Output pipeline.**
   - **Tests first:**
     - `TestBatchedOutputMsgLines`.
     - `TestCarriageReturnReplacesLine`.
     - `TestLogBufAppendIncremental` (no full rebuild; checked through a benchmark or a counter).
     - `TestColorEnvSet`.
   - **Implement** these.

4. **.NET and toolchain detection.**
   - **Tests first** (`dotnet_test.go`, on temp trees):
     - Web, Exe and Test classification.
     - The `sln` at the root with projects under `src/`.
     - `.slnx`.
     - Depth limit and skipped directories.
     - pnpm, yarn and bun chosen from lockfiles.
     - `+N more` shown instead of a silent cap.
     - Detected tasks merged with configured ones and marked `auto`.
     - Task `dir`/`env` applied.

5. **Problem matchers and the log viewer.**
   - **Tests first:**
     - `TestMatchDotnet` with `src/Api/Foo.cs(12,5): error CS0103: …`.
     - `TestMatchGo`.
     - `TestMatchTsc`.
     - `TestEJumpsNextProblem`.
     - `TestEnterOpensEditorAtProblem`: emits `editor.OpenMsg{Target{Line:12, Col:5}}`.
     - `TestLogSearchCounts`.
     - `TestWrapToggle`.
     - `TestFollowIndicator`.
   - **Implement** these.

6. **Failure surfacing and re-run.**
   - **Tests first:**
     - `TestFailedExitBadgeRedAndToast`.
     - `TestCtrlRRerunsLastFromOtherTab`.
     - `TestHeaderShowsExitCode`.
     - `TestElapsedRefreshesOnTick`.
   - **Implement** these.

7. **HTTP modes and response.**
   - **Tests first** (new `httpc` pane tests):
     - `TestNormalModeNotCapturing`.
     - `TestIEntersInsert`.
     - `TestEscToNormal`.
     - `TestResponseScrollsJK`.
     - `TestHeadersTab`.
     - `TestEscCancelsInFlight` (an `httptest.Server` handler that blocks).
     - `TestLocalhostDefaultsHTTP`.
     - `TestJSONBodyAddsContentType`.
     - `TestTruncatedBadge`.
     - `TestLatencyIncludesBody`.
   - **Implement** these.

8. **.http files.**
   - **Tests first** (`httpfile_test.go`):
     - `###` separators.
     - `# @name`.
     - File variables.
     - `{{var}}`, `{{$guid}}` (format check), `{{$timestamp}}` and `{{$dotenv KEY}}`.
     - Merging `http-client.env.json` with a selected environment.
     - Comments, and a body after a blank line.
     - The ASP.NET template sample.
   - **Implement:**
     - The sidebar (`ctrl+o`) built from a bounded glob.
     - `ctrl+e` cycles the environment.
     - Files tab `x` on a `.http` file: an `httpc.LoadRequestMsg` with `DestHTTP` switches to the HTTP tab. The tab switch is done by app through a small `ui.FocusTabMsg`.

**Acceptance.**
- Stopping `dotnet watch` kills the app host.
- Quitting with tasks running asks first and leaves no orphaned processes.
- `dotnet build` errors can be jumped to and opened in micro at the error line.
- The HTTP response scrolls, and `q`/`?`/`1-4` work in normal mode.
- A template `WebApi.http` file runs against `localhost:5xxx`.
- All gates are green, including `-race` for `runner`.

---

## Final verification (after M10 and M7 have merged)

1. Run `go build ./... && go vet ./... && go test -race ./...`.
2. **Manual pass** on `~/Development/acc/Business-Experience-API`, launched from both the repo root and `src/<Project>`:
   - tree and viewer, hover, `gd`, `gr`, outline, finder, grep;
   - `e` into micro, then `Alt-q` back;
   - Git staging and commit with `ctrl+g`;
   - Run `dotnet build` with an error jump;
   - HTTP with a `.http` file.
3. **Resize** from 60×16 to 250×70 with no overflow.
4. **Startup:** `time deck` to the first frame should be comparable to today. Measure it with `DECK_DEBUG` timing if needed.
5. **Process check:** `pgrep -fl 'csharp-ls|gopls|BuildHost|sleep'` is empty after quitting.
6. Run `/code-review high` over the branch diff, then use `superpowers:finishing-a-development-branch`.
