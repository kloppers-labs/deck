# deck agent support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let any coding agent drive a running deck and read where the user is looking, give Claude Code native IDE presence, refresh deck live as files change, and add a branch Review view to the Git tab.

**Architecture:**
- A per-instance Unix socket (`internal/control`) carries `deck open|review|diff|where` requests from a CLI client into the Bubble Tea loop.
- A shared `presence.Store` publishes what the user is looking at, and both `deck where` and a Claude Code IDE bridge (`internal/ide`, WebSocket MCP) read from it.
- `internal/launch` starts deck next to the agent in Orca, cmux, Paradev, tmux, Zellij, WezTerm, kitty, Ghostty, iTerm2, Apple Terminal or Alacritty.
- The Git tab gains a Review mode and a diff line cursor.
- The app polls the open file and the git status.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, lipgloss, `github.com/coder/websocket` (new), git CLI.

**Spec:** `docs/superpowers/specs/2026-11-13-agent-support-design.md`. Read it before every task; section numbers below refer to it.

## Global Constraints

- **Dependencies:** the only new module is `github.com/coder/websocket`. `internal/lsp` stays standard-library-only.
- **Comments:** zero by default. Write one only for a constraint the code can't express, and keep it to one line. Doc summaries are 2 lines at most. No narrative.
- **Pane conventions:**
  - Panes stay value types.
  - Async work runs as `tea.Cmd` returning `ui.Routed` messages.
  - Nothing blocks the UI goroutine.
  - `View` ends with `ui.Fit`.
  - `View` cost doesn't grow (`go test -bench ViewerView ./internal/viewer/`).
- **Socket directory:** `$XDG_RUNTIME_DIR/deck`, else `${XDG_STATE_HOME:-$HOME/.local/state}/deck/run`. **Never `TMPDIR`.** The directory is `0700`; sockets and sidecars are `0600`.
- **Claude lockfile:** `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/ide/<port>.lock`, mode `0600` in a `0700` directory, holding `{"pid","workspaceFolders":[root],"ideName":"deck","transport":"ws","authToken":<32 hex from crypto/rand>}`. It binds `127.0.0.1` only, on a port in 10000–65535.
- **Control protocol:** one JSON request then `\n` per connection, one JSON reply then `\n`, and `"v":1`.
- **CLI exit codes:** 0 ok, 1 error, 2 usage, 3 when no deck is running or deck can't be launched.
- **Agent label:** `--from`, defaulting to `Claude` when `CLAUDECODE=1`, else `agent`. The toast is `<From> → <relpath>:<line>`.
- **Held-jump key:** `alt+j` (capture-proof). The status text is `<From>: <relpath>:<line> · alt+j`.
- **Review header:** exactly `Review · vs <base> @ <short mb> · <n> commits · <n> files +<a> −<d> · <v>/<n> viewed`.
- **Sockets in tests:** create directories with `os.MkdirTemp("", "dk")`, never `t.TempDir()` (macOS has a 104-byte socket-path limit).
- **No commits.** The user reviews the whole branch `feat/agent-support` at the end. Each task ends with a checkpoint (tests green) instead.
- **Gate after every task:** `gofmt -l .` is empty, `go vet ./...` is clean, and `go test -race` passes for the touched packages.

## Review Focus

1. **An agent whose cwd differs from deck's** (a subdirectory, or a sibling worktree): `deck open rel/path` resolves against the agent's cwd and reaches the deck whose root contains the target. Test: Task 7 `TestOpenResolvesRelativeToCallerCwd` and Task 5 `TestPickOrder`.
2. **Lines with tabs, emoji or CJK:** the selection's display columns, presence rune columns and Claude's UTF-16 `character` all agree. Test: Task 2 `TestSelectionWithTabsAndWide` and Task 13 `TestSelectionChangedUTF16`.
3. **A file deleted or renamed by the agent while deck shows it:**
   - the viewer shows a note instead of stale text;
   - the review list drops or renames the entry;
   - a deleted file's viewed hash is `deleted`.

   Test: Task 9 `TestOpenFileDeletedShowsNote` and Task 10 `TestDeletedFileViewedHash`.
4. **A request that arrives while micro is open** (the event loop is blocked): the CLI gets `deck is busy: editor open` within about 2 s, and the late message is **dropped** after micro closes, not applied. Test: Task 5 `TestServeBusyTimeout` and Task 6 `TestControlExpiredRequestDropped`.
5. **A path argument containing colons or ending in digits** (`notes:v2.txt`, `build-2024`): only trailing `:N`, `:N:M` and `:N-M` suffixes are parsed. Test: Task 7 `TestParseTarget`.

---

### Task 1: Config — `[agent]` and `git.review_base`

**Files:**
- Modify: `internal/config/config.go` (struct, `Default()`, `validate`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Agent struct { Enabled bool \`toml:"enabled"\`; Launch string \`toml:"launch"\`; Watch bool \`toml:"watch"\`; WatchMS int \`toml:"watch_ms"\`; Claude bool \`toml:"claude"\` }`
  - the field `Config.Agent Agent \`toml:"agent"\``
  - `Git.ReviewBase string \`toml:"review_base"\``

- [ ] **Step 1: Write the failing tests**

```go
func TestAgentDefaults(t *testing.T) // Default().Agent == Agent{Enabled:true, Launch:"split", Watch:true, WatchMS:1000, Claude:true}; Default().Git.ReviewBase == ""
func TestAgentLaunchEnumWarns(t *testing.T) // file `[agent]\nlaunch="pane"` → Launch=="split", one Warning containing "agent.launch"
func TestWatchMSClamp(t *testing.T) // watch_ms=100 → WatchMS==250 and a Warning; watch_ms=0 → 1000 (fillZero), no warning
func TestAgentFalseSticks(t *testing.T) // `[agent]\nclaude=false\nwatch=false` → Claude==false, Watch==false
func TestUnknownAgentKeySuggests(t *testing.T) // `[agent]\nlanuch="tab"` → warning text contains `did you mean "launch"`
func TestReviewBaseDecodes(t *testing.T) // `[git]\nreview_base="origin/develop"` → Git.ReviewBase=="origin/develop"
```

- [ ] **Step 2: Run them and confirm they fail.** Run `go test ./internal/config -run 'Agent|WatchMS|ReviewBase'`. Expected: compile errors (no `Agent` type).
- [ ] **Step 3: Implement.** Add the types and defaults. Add `{"agent.launch", &c.Agent.Launch, d.Agent.Launch, []string{"split","tab","window","off"}}` to the enums, and clamp `WatchMS < 250` with a warning in `validate`.
- [ ] **Step 4: Run `go test -race ./internal/config`.** Expected: PASS.

---

### Task 2: Viewer selection, `@` mention, rendered-markdown section

**Files:**
- Create: `internal/viewer/selection.go`, `internal/viewer/selection_test.go`, `internal/ui/mention.go`
- Modify: `internal/viewer/viewer.go` (keys `v`/`V`/`@`/`y`/`Y`/`esc`, mouse drag and shift+click, clearing on `Open`, clamping on `Reload`, `WantsEsc`), `internal/viewer/render.go` (selection spans; rows with a selection go through `Paint`), `internal/app/app.go` (handle `ui.MentionMsg` by copying to the clipboard as the fallback)
- Test: `internal/viewer/selection_test.go`, `internal/app/app_test.go`

**Interfaces:**
- Consumes: `viewer.Range{Start, End Pos}` and `Pos{Line, Col}` (0-based lines, rune columns), `Paint`, `copyCmd`, `MarkdownOutline`, `SetRawMarkdown`.
- Produces:
  - `func (m Model) Selection() (Range, bool)`: normalised so `Start` comes before `End`, with `End.Col` exclusive. Line-wise selections run from `Start.Col=0` to `End.Col=len(runes of End line)`.
  - `func (m Model) SelectionText() string`.
  - `func (m Model) SelectRange(r Range) Model`: sets a character-wise selection, puts the cursor at `r.Start` and centres it.
  - `func (m Model) Section() (title string, startLine, endLine int, ok bool)`: 0-based source lines. `ok` only while rendered markdown is showing.
  - `func renderedSection(rendered []string, headings []Symbol, row int) (int, bool)`: the index of the heading whose section contains the rendered row.
  - `ui.MentionMsg{Path string; LineStart, LineEnd int}`: 0-based, inclusive, `RouteTo() == ui.DestApp`.

- [ ] **Step 1: Write the failing tests** (fixture `a.go` = `"ab\tc\nxyz\n😀é\n"`, opened through the existing test helpers)

```go
func TestVisualCharSelectionExtends(t *testing.T)  // keys v l l j → Selection()==Range{{0,0},{1,3}}, true
func TestVisualLineSelection(t *testing.T)         // V j → Range{{0,0},{1,3}}
func TestSelectionWithTabsAndWide(t *testing.T)    // select line 2 cols 0..2 ("😀é") → SelectionText()=="😀é"; painted span widths use DispCol (emoji = 2 cells)
func TestEscClearsSelectionFirst(t *testing.T)     // v l → WantsEsc()==true; esc → no selection, WantsEsc()==false
func TestYankSelection(t *testing.T)               // v l y → clipboardOut contains OSC52 of "ab"
func TestYankRef(t *testing.T)                     // V j Y → clipboard text "a.go:1-2"
func TestMouseDragSelects(t *testing.T)            // press (row0,col0) motion+left (row1,col2) release → Selection Range{{0,0},{1,2}}
func TestShiftClickExtends(t *testing.T)           // cursor (0,0), shift+click (1,1) → Range{{0,0},{1,1}}
func TestSelectionClearsOnOtherFile(t *testing.T)  // select, Open("b.go") → ok==false
func TestSelectionSurvivesReloadClamped(t *testing.T) // select to line 2, file shrinks to 1 line, Reload → End.Line==0
func TestSelectionNotCapturing(t *testing.T)       // v → Capturing()==false
func TestMentionEmits(t *testing.T)                // V j @ → cmd yields ui.MentionMsg{Path:abs, LineStart:0, LineEnd:1}; no selection → LineStart==LineEnd==cursor line
func TestRenderedSection(t *testing.T)             // table: rendered rows ["  Title","","  ## Goals","x","  ## Plan"], headings Title(0),Goals(4),Plan(9): row 3→1, row 0→0, row 4→2
func TestVOnRenderedMarkdownSwitchesToRaw(t *testing.T) // md doc rendered, v → RawMarkdown()==true, cursor at the section's start line
func TestSectionWhileRendered(t *testing.T)        // rendered md, cursor on "Goals" body → Section()=="Goals",4,8,true
```

App: `TestMentionFallsBackToClipboard`. Feeding `ui.MentionMsg{Path: root+"/a.go", LineStart: 39, LineEnd: 51}` puts `@a.go#L40-52` on the clipboard and shows the toast `copied @a.go#L40-52`.

- [ ] **Step 2: Run `go test ./internal/viewer -run 'Visual|Selection|Esc|Yank|Mouse|Shift|Mention|Rendered|Section'`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Store `sel struct{ active, lines bool; anchor Pos }` on `Model`. The cursor is the moving end.
  - The selection SGR is a new `selectionSGR` (`ui.ColorSelection` background) inserted **before** the cursor spans in `rowSpans`.
  - `renderRows` paints any row intersecting the selection.
  - `y`/`Y` keep their old behaviour without a selection.
  - The mouse drag uses `tea.MouseActionMotion` with `MouseButtonLeft`. A press without motion still only places the cursor.
- [ ] **Step 4: Run `go test -race ./internal/viewer/ ./internal/files/ ./internal/app/` and `go test -bench ViewerView ./internal/viewer/`.** Expected: PASS, and the benchmark stays under 1 ms.

---

### Task 3: `internal/presence`

**Files:**
- Create: `internal/presence/presence.go`, `internal/presence/presence_test.go`

**Interfaces:**
- Produces (lines and columns are **1-based**, columns in runes; `EndLine` inclusive, `EndCol` exclusive):

```go
type Range struct{ StartLine, StartCol, EndLine, EndCol int }
type GitSpot struct{ Mode, Base, Side string } // Mode changes|review|log; Side new|old
type Diagnostic struct{ Line, Col, EndLine, EndCol int; Severity, Message, Source string }
type Snapshot struct {
    Root, Tab, File, Dir string
    Line, Col            int
    Sel                  *Range
    SelText, Section     string
    Visible              [2]int
    Git                  *GitSpot
    Recent               []string
    Seq                  uint64
}
func New() *Store
func (s *Store) Publish(snap Snapshot) bool       // ignores snap.Seq; bumps Seq and notifies only when content differs
func (s *Store) Get() Snapshot                    // deep copy
func (s *Store) Subscribe() (<-chan uint64, func()) // buffer 1, latest Seq wins, never blocks Publish; func unsubscribes
func (s *Store) SetDiagnostics(path string, d []Diagnostic)
func (s *Store) Diagnostics(path string) map[string][]Diagnostic // all paths when path == ""
const MaxSelText = 16 << 10
```

- [ ] **Step 1: Write the failing tests:**
  - `TestPublishBumpsSeqOnlyOnChange`: publishing the same snapshot twice gives `Seq==1`; changing `Line` gives `Seq==2`.
  - `TestSubscribeCoalesces`: 5 publishes, then a non-blocking read gets `5`, and a second read finds nothing.
  - `TestGetIsDeepCopy`: mutating `Get().Recent[0]` and `*Get().Sel` doesn't change the store.
  - `TestDiagnosticsByPath`.
  - `TestUnsubscribeStops`.
  - `TestConcurrentPublishGet`, run under `-race`.
- [ ] **Step 2: Run `go test ./internal/presence`.** Expected: FAIL (the package doesn't exist).
- [ ] **Step 3: Implement.** Use a `sync.Mutex`, compare with `reflect.DeepEqual` on everything except `Seq`, and send subscriber notifications non-blocking (drain and replace).
- [ ] **Step 4: Run `go test -race ./internal/presence`.** Expected: PASS.

---

### Task 4: App publishes presence

**Files:**
- Create: `internal/app/presence.go`, `internal/app/presence_test.go`
- Modify: `internal/app/app.go` (rename `Update` to `update` and add a wrapper `Update` that publishes), `internal/files/files.go` (accessors), `internal/files/lspevents.go` (diagnostics accessor)

**Interfaces:**
- Consumes: Task 2's viewer API and Task 3's Store.
- Produces:
  - `func (m Model) WithAgent(store *presence.Store, controlDir string) Model` in `app`. A nil store means nothing is published.
  - `files.Model`: `Selection() (viewer.Range, bool)`, `SelectionText() string`, `Section() (string, int, int, bool)`, `VisibleLines() (first, last int)` (0-based), `RecentFiles(n int) []string` (unique paths from the history, newest first), `SelectedDir() string` (`""` unless the tree selection is a directory).
  - `func (e LSPEventMsg) Diagnostics() (path string, d []viewer.Diagnostic, ok bool)`.
- Mapping rules (in `snapshot()`):
  - `Tab` is the lowercase tab name.
  - **Files tab with a file shown:** `File` is the absolute path, `Line`/`Col` are `+1`, and `Sel` is converted to 1-based. `SelText` is capped at `MaxSelText`.
  - **Rendered markdown:** `Section` is the title, `Line` is the section start `+1`, `Sel` spans the whole section, and `Col` is 1.
  - **Directory:** `Dir` is set and `File` is `""`.
  - **Root** is the launch root.

- [ ] **Step 1: Write the failing tests** (app built with `WithAgent(presence.New(), "")`, driven through `Update`):
  - `TestPresenceFollowsViewerCursor`: open `a.go`, then `j` gives `Get().Line == 2`.
  - `TestPresenceTabSwitch`: `2` gives `Tab == "git"`.
  - `TestPresenceSelection`: `V j` gives `Sel == &Range{1,1,2,4}` and `SelText == "ab\tc\nxyz"`.
  - `TestPresenceSelTextCapped`: a 20 KiB selection gives `len(SelText) == MaxSelText`.
  - `TestPresenceRenderedMarkdownSection`.
  - `TestPresenceDirectory`.
  - `TestPresenceDiagnosticsStored`: feeding an `LSPEventMsg` diagnostics event fills `Diagnostics(path)`.
  - `TestNoStoreNoPanic`.
- [ ] **Step 2: Run `go test ./internal/app -run Presence`.** Expected: FAIL.
- [ ] **Step 3: Implement** `snapshot()` and `publish()`. Publish only when the store isn't nil. Extract the selection text only when the range differs from the last published one (keep `lastSel` in a pointer field so it survives value copies).
- [ ] **Step 4: Run `go test -race ./internal/app/ ./internal/files/`.** Expected: PASS, including `TestFrameFitsTerminal`.

---

### Task 5: `internal/control` — directory, instances, server, client

**Files:**
- Create: `internal/control/dir.go`, `instance.go`, `proto.go`, `server.go`, `client.go`, and a test file for each

**Interfaces:**
- Consumes: `presence.Snapshot`.
- Produces:

```go
type Request struct {
    V int `json:"v"`; Cmd string `json:"cmd"`            // ping|where|open|review|diff
    Path string `json:"path,omitempty"`; Line, Col, EndLine int `json:",omitempty"` // 1-based
    Base string `json:"base,omitempty"`; Uncommitted bool `json:"uncommitted,omitempty"`
    From string `json:"from,omitempty"`
}
type Reply struct {
    OK bool `json:"ok"`; Error string `json:"error,omitempty"`; Message string `json:"message,omitempty"`
    Where *presence.Snapshot `json:"where,omitempty"`
}
type RequestMsg struct{ Req Request; Reply chan<- Reply; Deadline time.Time } // RouteTo ui.DestApp
type Instance struct{ PID int; Root, Cwd, Version, Socket string; Started, Focused time.Time }
func Dir(getenv func(string) string, home string) (string, error)
func Register(dir string, inst Instance) (cleanup func(), err error)  // writes <pid>.json 0600
func Touch(dir string, pid int, at time.Time) error
func List(dir string) ([]Instance, error)                              // sweeps dead pid / refusing socket
func Pick(insts []Instance, pid int, root, target string) (Instance, bool) // pure; spec §6.1 order
func Serve(ctx context.Context, sock string, query func(Request) Reply, send func(tea.Msg)) error
func Call(sock string, req Request, timeout time.Duration) (Reply, error)
var navTimeout = 2 * time.Second
```

- [ ] **Step 1: Write the failing tests:**
  - `TestDirPrefersXDGRuntime` gives `/run/user/501/deck`. `TestDirIgnoresTMPDIR`: with only `TMPDIR` and `HOME=/h` set, the result is `/h/.local/state/deck/run`. `TestDirStateHome` covers `XDG_STATE_HOME`.
  - `TestRegisterListCleanup`: the sidecar has mode `0600` and the directory `0700`; `List` returns it; after `cleanup` it is gone.
  - `TestListSweepsDeadPid`: a sidecar with pid 999999 is removed.
  - `TestListSweepsRefusingSocket`.
  - `TestPickOrder`, a table:
    - an explicit pid wins;
    - an exact root beats a containing root;
    - a root containing `target` is picked when no exact root matches;
    - a tie goes to the newer `Focused`, then the newer `Started`;
    - nothing matching gives `ok == false`.
  - `TestServeRoundTripQuery`: `where` returns `query`'s reply.
  - `TestServeNavigateViaSend`: `open` reaches `send` as a `RequestMsg`, the test replies on `Reply`, and the client gets it.
  - `TestServeBusyTimeout`: `send` blocks; with `navTimeout = 50ms` the client gets `Error == "deck is busy: editor open"`, and `Deadline` is about `now + navTimeout`.
  - `TestUnknownCmdAndVersion`: `cmd:"nope"` gives `unknown command "nope"`, and `v:2` gives `unsupported protocol version 2`.
  - `TestSocketPerms`: `0600`.
- [ ] **Step 2: Run `go test ./internal/control`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `Serve` answers `ping` and `where` through `query` directly. Every other command calls `send` from a goroutine with a buffered (1) reply channel and waits up to `navTimeout`.
  - Liveness is checked with `syscall.Kill(pid, 0)`, plus a 200 ms dial for sockets.
  - `Serve` removes the socket file on `ctx.Done`.
- [ ] **Step 4: Run `go test -race ./internal/control`.** Expected: PASS.

---

### Task 6: App handles control requests; `main` wires it

**Files:**
- Create: `internal/app/control.go`, `internal/app/control_test.go`
- Modify: `internal/app/app.go` (cases `control.RequestMsg` and `tea.FocusMsg` → `control.Touch`; status-bar held text), `internal/ui/keys.go` (`alt+j`), `internal/app/help.go` (an Agent group: `alt+j`, `@`), `internal/files/nav.go` (agent open), `internal/git/messages.go` (`ReviewMsg` type only), `main.go`, `internal/app/layout_test.go`

**Interfaces:**
- Consumes: Task 5's `RequestMsg`/`Reply`, Task 3's Store, Task 2's `SelectRange`.
- Produces:
  - `func (m files.Model) AgentOpen(path string, line, col, endLine int) (files.Model, tea.Cmd)`: 1-based, 0 means unset. It pushes the current `Loc` onto the history first. For markdown with `line > 0` it calls `SetRawMarkdown(true)`. With `endLine >= line > 0` it selects the whole lines `line..endLine`.
  - `func (m files.Model) AgentReveal(dir string) (files.Model, tea.Cmd)`.
  - `git.ReviewMsg{Base string; Uncommitted bool; Path string; Line int}` with `RouteTo() == ui.DestGit`. The pane ignores it until Task 11.
- Behaviour (spec §6.3):
  - Requests whose `Deadline` has passed are dropped silently.
  - While capturing, `open`/`review`/`diff` is **held** (the latest replaces the previous) and the reply is `OK` with the message `held: user is typing`. Otherwise it is applied with the toast `<From> → <rel>:<line>`.
  - `review`/`diff` switch to the Git tab and deliver `git.ReviewMsg`.
  - A non-existent path gives `OK:false`, `Error:"no such file: <path>"`.
- `main.go`, when `cfg.Agent.Enabled`:
  1. Work out `dir := control.Dir(os.Getenv, home)` and `store := presence.New()`.
  2. Compute the root with `git.OpenRepo(ctx, "")` (its `Top`), falling back to the cwd.
  3. `Register`, then `go control.Serve(ctx, sock, queryFromStore, p.Send)`.
  4. Defer cleanup.

  The `query` function returns `Reply{OK:true, Where:&snap}`.

- [ ] **Step 1: Write the failing tests:**
  - `TestControlOpenSwitchesToFiles`: from the Git tab, `RequestMsg{open a.go:2}` gives the active tab Files, `ViewerLoc().Line == 1`, a reply with `OK`, and a toast `Claude → a.go:2`.
  - `TestControlOpenPushesHistory`: then `ctrl+o` returns to the previous file.
  - `TestControlOpenRangeSelects`: `Line:1, EndLine:2` gives `Selection() == {{0,0},{1,3}}`.
  - `TestControlOpenDirReveals`.
  - `TestControlHeldWhileCapturing`: with the viewer `/` prompt open, the request is held, the status contains `Claude: a.go:2 · alt+j`, and `esc` then `alt+j` opens it.
  - `TestControlExpiredRequestDropped`: with `Deadline` in the past, nothing changes and nothing is sent on the reply.
  - `TestControlReviewSwitchesToGit`: the active tab is Git and the returned cmds include a `git.ReviewMsg`.
  - `TestControlMissingFile`.
  - Layout: add a held-jump case to `TestFrameFitsTerminal`.
- [ ] **Step 2: Run `go test ./internal/app -run Control`.** Expected: FAIL.
- [ ] **Step 3: Implement** as specified above.
- [ ] **Step 4: Run `go test -race ./internal/app/ ./internal/files/ ./internal/git/` and `go build ./...`.** Expected: PASS.

---

### Task 7: CLI subcommands

**Files:**
- Create: `cli_agent.go`, `cli_agent_test.go`, `internal/control/target.go`, `internal/control/target_test.go`
- Modify: `cli.go` (dispatch and usage), `main.go`

**Interfaces:**
- Consumes: Task 5's `Dir`/`List`/`Pick`/`Call`, plus a `launch` hook (nil until Task 8).
- Produces:
  - `func ParseTarget(arg string) (path string, line, col, endLine int)`: only numeric trailing suffixes count.
  - `type agentEnv struct{ Getenv func(string) string; Getwd func() (string, error); Home string; Launch func(dir string) error; Now func() time.Time }`.
  - `func runAgent(name string, args []string, env agentEnv, stdout, stderr io.Writer) int`.
  - Subcommands: `open review diff where ls launch guide`. `deck -- <dir>` forces a directory.
- `where` output (human form):
  - **Files with a selection:**
    ```
    files  <rel>:<line>:<col>  selection <l1>-<l2>
    <SelText>
    ```
  - **Files without a selection:** `files  <rel>:<line>:<col>`.
  - **Rendered markdown:** `files  <rel>  section "<title>" <l1>-<l2>`.
  - **Directory:** `files  <rel>/`.
  - **Git review:** `git review  <rel>:<line> (<side>)  vs <base>`.
  - **Other tabs:** just the tab name.
  - `--json` prints the snapshot as indented JSON.
- `ls` prints `PID  ROOT  FOCUSED` rows.

- [ ] **Step 1: Write the failing tests:**
  - `TestParseTarget`, a table:

    | input | result |
    |---|---|
    | `"x.go"` | `x.go,0,0,0` |
    | `"x.go:12"` | `x.go,12,0,0` |
    | `"x.go:12:3"` | `x.go,12,3,0` |
    | `"x.go:10-20"` | `x.go,10,0,20` |
    | `"notes:v2.txt"` | unchanged |
    | `"build-2024"` | unchanged |
    | `"a:b:7"` | `a:b,7,0,0` |
    | `"x.go:20-10"` | `x.go,20,0,0` |

  - `TestSubcommandBeatsDir`: `deck where` dispatches to `where` even when `./where` exists.
  - `TestDoubleDashOpensDir`: `deck -- open` gives `launch == true` and a chdir to `open`.
  - `TestOpenResolvesRelativeToCallerCwd`: caller cwd `<root>/sub`, `deck open x.go` makes the server receive `<root>/sub/x.go`.
  - `TestWhereHumanFormat`, a table over the snapshot shapes above.
  - `TestWhereNoDeckExit3`: with `--no-launch`, exit 3 and stderr `no deck running for <root>`.
  - `TestFromDefaultsToClaude`: `CLAUDECODE=1` puts `From == "Claude"` in the request.
  - `TestLs`.
  - `TestUsageListsSubcommands`.
- [ ] **Step 2: Run `go test . ./internal/control -run 'ParseTarget|Subcommand|DoubleDash|Resolves|Where|From|Ls|Usage'`.** Expected: FAIL.
- [ ] **Step 3: Implement** with a fake server from Task 5 in the tests. The caller's root comes from `git rev-parse --show-toplevel` in the caller's cwd, falling back to the cwd.
- [ ] **Step 4: Run `go test -race . ./internal/control`.** Expected: PASS.

---

### Task 8: `internal/launch` — start deck next to the agent

**Files:**
- Create: `internal/launch/detect.go`, `command.go`, `start.go`, and a test file for each
- Modify: `cli_agent.go` (auto-launch when no instance is found and `cfg.Agent.Launch != "off"`, unless `--no-launch`; the `deck launch [--split|--tab|--window] [dir]` subcommand)

**Interfaces:**
- Produces:

```go
type Kind int // Orca, Cmux, Paradev, Tmux, Zellij, WezTerm, Kitty, Ghostty, ITerm2, AppleTerminal, Alacritty, Unknown
type Target struct{ Kind Kind; Handle string } // Handle: ORCA_TERMINAL_HANDLE, SUPERSET_WORKSPACE_ID, …
type Mode string // "split" | "tab" | "window"
var ErrNoRoute = errors.New("no way to open a terminal here")
func Detect(getenv func(string) string, goos string) Target
func Command(t Target, mode Mode, dir, deckBin string) ([]string, error)
func Start(ctx context.Context, argv []string) error // via execCommand seam
func Hint(t Target, dir string) string // "deck isn't running here — start it with: cd <dir> && deck" (+ kitty remote-control hint)
```

- Detection order: `ORCA_TERMINAL_HANDLE`, `CMUX_SURFACE_ID`, `SUPERSET_WORKSPACE_ID`, `TMUX`, `ZELLIJ_PANE_ID`/`ZELLIJ`, `WEZTERM_PANE`, `KITTY_WINDOW_ID`, `TERM_PROGRAM` (`ghostty`, `iTerm.app`, `Apple_Terminal`), `ALACRITTY_SOCKET`/`ALACRITTY_WINDOW_ID`. Ghostty, iTerm2 and Apple Terminal count only on `darwin`.
- Commands: the spec §1.1 table. The deck command is `cd <q dir> && exec <q deckBin>` for Orca, cmux, Paradev and iTerm2, which have no cwd flag. Every other kind passes the directory with its own flag. Fallbacks:
  - **No splits** (Apple Terminal, Alacritty): `window`.
  - **Paradev split:** `tab`.
  - **Orca window:** `tab`.
  - **Unknown or Warp:** `ErrNoRoute`.
- After `Start`, the CLI polls `control.List` every 100 ms for up to 5 s for an instance whose `Root` matches, then sends the request.

- [ ] **Step 1: Write the failing tests:**
  - `TestDetectOrder`, a table that includes "Orca and TMUX both set → Orca" and "CMUX and TERM_PROGRAM=ghostty → Cmux".
  - `TestCommandArgv`, a table over every kind × mode with the exact argv. For example, tmux split is `["tmux","split-window","-h","-c","/p","/bin/deck"]`, and Orca split is `["orca","terminal","split","--terminal","H","--direction","vertical","--command","cd '/p' && exec '/bin/deck'","--json"]`.
  - `TestCommandQuotesDir`: `/my proj/it's` is shell-quoted correctly.
  - `TestGhosttyScript`: the `osascript -e` text contains `new surface configuration`, `initial working directory`, `"/p"` and `split`.
  - `TestITermScript`.
  - `TestUnknownNoRoute`.
  - `TestHintKitty`.
  - `TestStartUsesSeam`.
  - CLI: `TestAutoLaunchThenSends`. A fake `Launch` registers a fake instance, and the request is then delivered with exit 0. `TestLaunchOffExit3`.
- [ ] **Step 2: Run `go test ./internal/launch . -run 'Detect|Command|Script|NoRoute|Hint|Start|AutoLaunch|LaunchOff'`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run `go test -race ./internal/launch .`.** Expected: PASS.
- [ ] **Step 5: Manual check in Orca.** Run `go build -o /tmp/deck-dev . && /tmp/deck-dev launch` from an Orca terminal in a scratch repo. Expected: a vertical split opens running deck in that directory. If Orca ignores the `cd`, record the fix in `Command`.

---

### Task 9: Live changes (`internal/app/watch.go`)

**Files:**
- Create: `internal/app/watch.go`, `internal/app/watch_test.go`, `internal/git/fingerprint.go` (+ test)
- Modify: `internal/app/app.go` (`Init` starts the tick when `cfg.Agent.Watch`), `internal/files/files.go` (`ReloadFileMsg` reloads only the viewer in place), `internal/viewer/viewer.go` (a missing file on `Reload` gives the note `file deleted`)

**Interfaces:**
- Produces:
  - `func git.Fingerprint(ctx context.Context, dir string) (string, error)`: sha256 of `status --porcelain=v2 -z` plus `rev-parse HEAD`. Not a repository gives `ErrNotRepo`.
  - `files.ReloadFileMsg{}`, routed to Files.
  - App-internal: `watchTickMsg{n int}` (every `WatchMS`), `fileStampMsg{path string; stamp int64; err error}`, `treePrintMsg{fp string; took time.Duration; err error}`.
  - The seam `var fingerprint = git.Fingerprint`.
- Rules:
  - **Every tick:** stat the viewer's path. If the mtime^size changed, `ReloadFileMsg`.
  - **Every 2nd tick,** when no check is in flight: fingerprint. If it changed, batch `git.Refresh`, `files.ReloadMsg{}` and `files.RefreshBadges`.
  - **Slow status:** `took > 1s` switches to a 10 s interval and toasts once: `git status is slow here; checking every 10s`.
  - `ErrNotRepo` turns tree checks off.

- [ ] **Step 1: Write the failing tests:**
  - `TestWatchReloadsChangedOpenFile`: rewrite `a.go` with a new mtime, feed a tick and run the cmds; the viewer shows the new text and the cursor line is kept.
  - `TestWatchFingerprintChangeRefreshes`: in a temp repo, modify a file and feed tick 2; the cmds yield a `git.RefreshMsg`.
  - `TestWatchNoChangeNoRefresh`.
  - `TestWatchBackoffOnSlowStatus`: the seam reports `took = 2s`; the next tick is scheduled at 10 s and the toast appears once.
  - `TestWatchDisabled`: `Watch == false` means `Init` returns no tick.
  - `TestOpenFileDeletedShowsNote`.
  - `TestFingerprintChangesOnEdit` (git package).
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.** All stat and git work runs inside cmds, and the in-flight flag lives on `Model`.
- [ ] **Step 4: Run `go test -race ./internal/app/ ./internal/files/ ./internal/git/ ./internal/viewer/`.** Expected: PASS.

---

### Task 10: Review data layer (`internal/git`)

**Files:**
- Create: `internal/git/review.go`, `internal/git/viewed.go`, `internal/git/review_test.go`, `internal/git/review_integration_test.go`

**Interfaces:**
- Produces:

```go
type Commit struct{ Hash, Subject string }
type ReviewFile struct{ Path, OldPath string; Status byte; Added, Deleted int; Binary, Untracked bool }
type Review struct{ Base, MergeBase string; Uncommitted bool; Fallback string; Commits []Commit; Files []ReviewFile }
func (r *Repo) ResolveBase(ctx context.Context, explicit, configured string) (string, error) // spec §6.8 order; "" + nil when none
func (r *Repo) LoadReview(ctx context.Context, base string, uncommitted bool) (Review, error)
func (r *Repo) ReviewDiff(ctx context.Context, rv Review, f ReviewFile) (string, error)
func parseNameStatusZ(out string) []ReviewFile
func parseNumstatZ(out string) map[string][2]int // binary → {-1,-1}
func BlobHash(data []byte) string
func FileHash(top, path string) string // "deleted" when missing
type Viewed struct{ Base string `json:"base"`; Files map[string]string `json:"files"` }
func LoadViewed(gitDir string) Viewed
func (v Viewed) Save(gitDir string) error // <gitDir>/deck/review.json, temp file + rename
func (v Viewed) Is(path, hash string) bool
```

- `LoadReview`:
  - With no base, or a merge base equal to `HEAD` with zero commits ahead, it sets `Uncommitted = true` and `Fallback = "no commits ahead of <base>; showing uncommitted changes"`. With no base at all the message is `"no base branch found; showing uncommitted changes"`.
  - It compares against the merge base, or `HEAD` when `Uncommitted`.
  - The commit list is `log --format=%H%x00%s <mb>..HEAD`.

- [ ] **Step 1: Write the failing tests:**
  - `TestParseNameStatusZ`, covering `M`, `A`, `D` and `R100` with old and new paths.
  - `TestParseNumstatZ`, including binary files and renames.
  - `TestBlobHashMatchesGit`: compare against `git hash-object --stdin` for `""`, `"x\n"` and a 70 KB buffer.
  - `TestResolveBaseOrder`, an integration test:
    - a bare origin with `origin/HEAD → origin/main` gives `origin/main`;
    - a repo with only local `main` gives `main`;
    - an explicit argument wins over a configured one;
    - with nothing available the result is `""`.
  - `TestLoadReviewAllChangeKinds`: a branch with 2 commits plus a staged, an unstaged, an untracked and a renamed file gives 2 commits, 5 files (the untracked one with `Untracked == true` and `Status == 'A'`) and correct counts.
  - `TestLoadReviewOnBaseFallsBack`.
  - `TestUncommittedOnly`: a committed-only change doesn't appear.
  - `TestReviewDiffUntracked`: the diff output contains `+++ b/new.txt`.
  - `TestViewedClearsWhenFileChanges`.
  - `TestViewedFreshOnBaseChange`.
  - `TestDeletedFileViewedHash`.
  - `TestViewedPerWorktree`: run `git worktree add`; ticks are saved under the worktree's `GitDir`.
- [ ] **Step 2: Run `go test ./internal/git -run 'Parse|Blob|ResolveBase|LoadReview|Uncommitted|ReviewDiff|Viewed|Deleted'`.** Expected: FAIL.
- [ ] **Step 3: Implement** using `r.git` (`runGit`), with `-z` everywhere and `--no-index /dev/null <path>` for untracked files.
- [ ] **Step 4: Run `go test -race ./internal/git`.** Expected: PASS.

---

### Task 11: Review mode in the Git pane

**Files:**
- Create: `internal/git/reviewpane.go`, `internal/git/reviewpane_test.go`
- Modify: `internal/git/pane.go` (`modeReview`, the `v` key in the list, routing `ReviewMsg`/`reviewLoadedMsg`/`reviewDiffMsg`, reloading the review on `RefreshMsg` in review mode), `internal/git/view.go`, `internal/git/messages.go`, `internal/app/app.go` (pass `cfg.Git.ReviewBase` via `Options.ReviewBase`), `internal/app/layout_test.go`

**Interfaces:**
- Consumes: Task 10's API and Task 6's `ReviewMsg`.
- Produces:
  - `Options.ReviewBase string`.
  - Review-list keys:
    - `v` and `esc` go back to Changes.
    - `u` toggles uncommitted-only.
    - `space` toggles the viewed tick.
    - `n` jumps to the next unviewed file.
    - `enter` on the "Commits (N)" row expands or collapses it; on a commit it shows the commit (reusing `showLoadedMsg`).
    - `enter` or `→` on a file focuses the diff.
    - `e` opens the editor.
    - `r` refreshes.
  - Help bindings for the above.
  - `ReviewMsg{Path, Line}` selects that file, focuses the diff and puts the diff cursor on the new-side `Line` (Task 12 adds the cursor; until then it scrolls to that hunk).
- Rows: `M path +a −d ✓`, and `R old → new` for renames. The header is in the Global Constraints. When `Fallback != ""` the header is followed by a muted line with the fallback text.

- [ ] **Step 1: Write the failing tests** (temp repo, same fixture as Task 10):
  - `TestVEntersReviewAndBack`.
  - `TestReviewHeaderText`: `Review · vs main @ <7 chars> · 2 commits · 5 files +N −M · 0/5 viewed`.
  - `TestReviewMsgSelectsPath`.
  - `TestSpaceTogglesViewedAndPersists`: a new pane on the same repo shows `1/5`.
  - `TestNNextUnviewed`.
  - `TestUToggleUncommitted`.
  - `TestReviewRefreshKeepsCursorPath`.
  - `TestReviewCommitsToggle`.
  - `TestReviewFallbackLine`.
  - Layout: `TestFrameFitsTerminal` also renders the Git tab in review mode at every size.
- [ ] **Step 2: Run `go test ./internal/git -run Review` and `go test ./internal/app -run Frame`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Loads run as commands with a `reviewSeq`, and stale results are dropped. Rendering is O(visible rows).
- [ ] **Step 4: Run `go test -race ./internal/git ./internal/app`.** Expected: PASS.

---

### Task 12: Diff cursor, selection, `enter`/`e`/`@`, and git presence

**Files:**
- Create: `internal/git/diffcursor.go`, `internal/git/diffcursor_test.go`, `internal/ui/showfile.go`
- Modify: `internal/git/pane.go` (`diffKeys`, mouse), `internal/git/view.go` (cursor and selection highlight), `internal/app/app.go` (handle `ui.ShowFileMsg`), `internal/app/presence.go` (git mapping)

**Interfaces:**
- Produces:
  - `func diffLineToFile(diff string, row int) (line int, side byte, ok bool)`: `line` is the 1-based new-side line and `side` is one of `' '`, `'+'`, `'-'`. A `-` row maps to the new-side line it would sit before. Header and `@@` rows give `ok == false`.
  - `type Spot struct{ Mode, Base, Path, Side string; Line, EndLine int }` and `func (p Pane) Spot() (Spot, bool)`.
  - `ui.ShowFileMsg{Path string; Line int}` with `RouteTo() == ui.DestApp`. The app focuses Files and delivers `files.RevealMsg{Target: editor.Target{Path, Line}}`.
- Diff keys:
  - `j`/`k`/`g`/`G`/`ctrl+d`/`ctrl+u` move the cursor, and the view scrolls with 3 rows of margin.
  - `v` toggles a line selection.
  - `enter` sends `ShowFileMsg`.
  - `e` opens the editor at the cursor line when it maps, otherwise at `firstHunkLine`.
  - `@` sends `ui.MentionMsg` (0-based new-side lines).
  - `esc` clears the selection first, then returns to the list.
  - A click sets the cursor.
- The cursor applies in both Changes and Review modes.
- Presence on the Git tab: `File` is the absolute path, `Line` is the cursor's new-side line, `Sel` spans the selected lines with columns 1..1 (whole lines), and `Git` is `&GitSpot{Mode, Base, Side}`.

- [ ] **Step 1: Write the failing tests:**
  - `TestDiffLineToFile`, a table over a two-hunk diff with context, `+` and `-` rows: a header gives `ok == false`; the first context row in hunk 1 `@@ -10,3 +10,4 @@` gives `10`; a `+` row gives its new line; a `-` row gives the next new line; a row in hunk 2 maps correctly; a rename header gives `ok == false`.
  - `TestDiffCursorMovesAndScrolls`.
  - `TestEnterOnDiffLineShowsFile`: the cmd yields `ui.ShowFileMsg{Path: abs, Line: 11}`.
  - `TestAtInDiffEmitsMention`.
  - `TestDiffSelection`: `v j j @` gives `LineStart:10, LineEnd:12`.
  - `TestEUsesCursorLine`.
  - `TestEscClearsDiffSelectionFirst`.
  - App: `TestShowFileSwitchesToFiles`, `TestPresenceFromGitReview`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run `go test -race ./internal/git ./internal/app ./internal/files`.** Expected: PASS.

---

### Task 13: Claude Code IDE bridge (`internal/ide`)

**Files:**
- Create: `internal/ide/lockfile.go`, `server.go`, `mcp.go`, `tools.go`, `notify.go`, `utf16.go`, and a test file for each
- Modify: `go.mod`/`go.sum` (`github.com/coder/websocket`), `main.go` (start when `cfg.Agent.Enabled && cfg.Agent.Claude`), `internal/app/app.go` (a status badge from `ide.ClientsMsg`; `ui.MentionMsg` goes to the bridge first and falls back to the clipboard), `internal/app/layout_test.go`

**Interfaces:**
- Consumes: the presence Store, `control.RequestMsg` (`openFile` navigates through it with `From: "Claude"`), and Task 2's `ui.MentionMsg`.
- Produces:

```go
type Options struct{ Root, ConfigDir string; PID int; Store *presence.Store; Send func(tea.Msg); Debounce time.Duration /*100ms*/ }
func Start(ctx context.Context, o Options) (*Bridge, error)
func (b *Bridge) Mention(path string, lineStart, lineEnd int) bool // false when no clients
func (b *Bridge) Clients() int
func (b *Bridge) Close()
type ClientsMsg struct{ N int } // RouteTo ui.DestApp
func utf16Col(line string, runeCol int) int
var openDiffMode = "unlisted" // Task 14 sets the final value
```

- The app gets the bridge through `func (m Model) WithIDE(b interface{ Mention(string, int, int) bool }) Model`.
- Badge: `✦ Claude`, or `✦ Claude ×N` for N > 1, left of the LSP badges. It is truncated with the right-hand side of the status line.
- `selection_changed`:
  - The source is `Store.Subscribe()`, debounced over `Debounce`.
  - It is sent only when `File != ""` and the file, line or selection differs from the last one sent.
  - Lines are 0-based, and `character` is computed with `utf16Col` on the file's line, read from the file and cached per path+mtime.
  - `end` is exclusive. An empty selection gives `start == end` at the cursor and `isEmpty == true`.
  - `fileUrl` is `"file://" + path`.
- Tools (exact names): `openFile`, `getCurrentSelection`, `getLatestSelection`, `getOpenEditors`, `getWorkspaceFolders`, `getDiagnostics`, `checkDocumentDirty`, `saveDocument`, `close_tab`, `closeAllDiffTabs`, plus `openDiff` only per `openDiffMode`. `executeCode` is never listed. The result shapes are in spec §6.5.

- [ ] **Step 1: Write the failing tests** (a real WebSocket client from `coder/websocket`, `ConfigDir` set to a temp directory):
  - `TestLockfileWrittenAndRemoved`: the fields, `0600`/`0700`, and the file gone after `Close`.
  - `TestSweepDeadDeckLockfiles`: a dead pid with `ideName:"deck"` is removed; `ideName:"VS Code"` is kept.
  - `TestRejectsBadToken` (401).
  - `TestRejectsOrigin` (403).
  - `TestInitializeAndToolsList`: the names equal the set above, without `executeCode`.
  - `TestGetCurrentSelectionShape`.
  - `TestGetWorkspaceFolders`.
  - `TestGetDiagnosticsShape`.
  - `TestCheckDocumentDirtyFalse`.
  - `TestSaveDocumentReadOnly`.
  - `TestCloseTabs`: `TAB_CLOSED` and `CLOSED_0_DIFF_TABS`.
  - `TestSelectionChangedDebounced`: 3 publishes within 30 ms give exactly one notification with the final state.
  - `TestSelectionChangedUTF16`: the line `"😀x"` with a cursor at rune column 1 gives `character == 2`.
  - `TestNoNotificationForRunTab`.
  - `TestAtMentioned`: `Mention(p, 39, 51)` makes the client receive `{"filePath":p,"lineStart":39,"lineEnd":51}`.
  - `TestOpenFileStartTextSendsRequest`: `startText:"func main"` gives a `RequestMsg` with the matching line.
  - `TestClientsMsgOnConnectDisconnect`.
  - App: `TestMentionUsesBridgeWhenConnected`, and the Claude-badge case in `TestFrameFitsTerminal`.
- [ ] **Step 2: Run `go test ./internal/ide`.** Expected: FAIL.
- [ ] **Step 3: Implement** JSON-RPC 2.0 over WebSocket:
  - Handle `initialize` (`protocolVersion:"2025-03-26"`, `serverInfo{name:"deck", version}`, `capabilities{tools:{}}`), `notifications/initialized`, `ping`, `tools/list` and `tools/call`.
  - Unknown methods get error `-32601`.
  - `ide_connected` is accepted.
  - Each client has one writer goroutine.
- [ ] **Step 4: Run `go test -race ./internal/ide ./internal/app`.** Expected: PASS.

---

### Task 14: Verify with the real Claude (`openDiff` and discovery)

**Files:**
- Modify: `internal/ide/tools.go` (`openDiffMode`), `docs/superpowers/specs/2026-11-13-agent-support-design.md` (§6.5 findings)

- [ ] **Step 1: Build and start deck.** Run `go build -o /tmp/deck-dev .`. In a scratch git repo, start `/tmp/deck-dev` in a tmux window, or in an Orca split through `deck launch`.
- [ ] **Step 2: Start Claude in the same repo** in another window with `claude --ide`. Check that `/ide` lists `deck`, and that `/status` shows the IDE connected. Move the cursor and select lines in deck, then send "what file and lines do I have selected?". Expected: Claude names them. The prompt shows `⧉ Selected N lines from …`.
- [ ] **Step 3: Test `openDiffMode="unlisted"`.** Ask Claude for a one-line edit in normal permission mode. Expected: the terminal permission prompt appears, and accepting it applies the edit. If Claude errors or hangs, switch to `"pending"` (list `openDiff` and answer `DIFF_REJECTED` on `close_tab` or disconnect) and repeat. If that also fails, set `"unlisted"` and document the user step `/config → Diff tool: terminal` in `deck guide`.
- [ ] **Step 4: Check pointing.** Press `@` on a selection in deck. Expected: `@file#Lx-y` appears in Claude's input.
- [ ] **Step 5: Record** the outcome, with the Claude version, in spec §6.5, and set the constant.

---

### Task 15: `deck guide`, skill install, README, CLAUDE.md

**Files:**
- Create: `internal/control/guide.md` (embedded), `internal/control/guide.go`, `internal/control/guide_test.go`
- Modify: `cli_agent.go` (`guide [--install claude] [--yes]`), `README.md` (an "Agents" section), `CLAUDE.md` (architecture: the new packages, routing for `control.RequestMsg`, `ui.MentionMsg`, `ui.ShowFileMsg`, `ide.ClientsMsg`, Review mode, the watch loop and the CLI subcommands)

**Interfaces:**
- Produces:
  - `func Guide() string`.
  - `func InstallClaudeSkill(configDir string, yes bool, in io.Reader, out io.Writer) (path string, err error)`, which writes `<configDir>/skills/deck/SKILL.md` with the front matter below followed by `Guide()`.

    ```yaml
    name: deck
    description: Use when the user mentions deck, asks what they're looking at or have selected, says "show me", "walk me through", "open X in deck", or wants to review the branch's changes together.
    ```

- [ ] **Step 1: Write the failing tests:**
  - `TestGuideMentionsCommands`: it contains `deck where`, `deck open`, `deck diff`, `deck review`, `--no-launch` and `exit code 3`.
  - `TestInstallWritesSkill`: the front matter `name: deck`.
  - `TestInstallAsksBeforeOverwrite`: with an existing file, `yes=false` and input `n\n`, the file is unchanged and the error is `cancelled`.
  - `TestInstallYesOverwrites`.
  - `TestGuideCLI`: `deck guide` prints the guide and exits 0.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Write `guide.md`** (about 60 lines, from spec §6.10) and the install code. The guide's text includes:
  - start deck before Claude, or run `/ide`;
  - the `diffTool` note if Task 14 required it;
  - the `deck where` habit;
  - one jump per point.

  Then update the README and CLAUDE.md.
- [ ] **Step 4: Run the final gates:** `gofmt -l .` (empty), `go vet ./...`, `go test -race ./...` and `go test -bench 'ViewerView|TreeFlatten' -run x ./internal/viewer/ ./internal/files/`. Expected: all clean and passing, and the benchmarks not regressed.
- [ ] **Step 5: End-to-end smoke in Orca.** From a Claude pane in a scratch repo:
  1. `deck open README.md:1-3` launches a split and selects lines 1–3.
  2. `deck where` prints it.
  3. `deck review` shows the Review view.
  4. Edit a file from Claude; deck reloads within about 1 s.
