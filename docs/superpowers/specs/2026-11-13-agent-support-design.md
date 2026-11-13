# deck agent support — design spec

Date: 2026-11-13 · Branch: `feat/agent-support` · Status: approved to build ("let it ride")

---

## 1. Intent

The user works with a coding agent (Claude Code today) on a branch and wants to explore the code and the branch's changes **together with the agent, inside deck**. Both of them drive:

- **The user drives deck** as usual.
- **The agent can take the wheel.** It can open a file at a line, reveal a directory, or jump to a changed file's diff, so it can walk the user through the code.
- **The agent always knows where the user is.** It sees the tab, the file, the line, the selection, and the diff hunk. So "what's this?", "rename this" and "why did you do this bit?" work without pasting anything.
- **The user reviews the agent's changes after it has made them, not edit by edit.** deck picks up changes live and has a proper review view for "everything this branch changed".

The agent runs in its own terminal: an Orca pane or tab, a tmux split, a WezTerm tab, or a separate window. deck runs somewhere else. Nothing may assume Orca or Claude. They are the first targets, not the only ones.

### 1.1 Research this design rests on (verified 2026-11-13)

**Claude Code's IDE connection** is the mechanism behind VS Code's "⧉ Selected N lines", the `/ide` command and the `--ide` flag. Sources: the claudecode.nvim `PROTOCOL.md` (reverse-engineered, written against CLI 2.1.312 and 2.1.322), the official IDE docs page, and the local `claude --help`.

- **Lockfile.** The IDE writes `~/.claude/ide/<port>.lock`, or `$CLAUDE_CONFIG_DIR/ide/` when that is set. The file is `0600` inside a `0700` directory and holds `{pid, workspaceFolders, ideName, transport:"ws", authToken}`.
- **Server.** The IDE serves MCP (spec 2025-03-26) as JSON-RPC 2.0 over a WebSocket on `127.0.0.1`, using a random port in 10000–65535.
- **Auth.** Claude sends the token in the `x-claude-code-ide-authorization` upgrade header.
- **IDE → Claude notifications:**
  - `selection_changed`: `{text, filePath, fileUrl, selection{start{line,character}, end{line,character}, isEmpty}}`. Positions are 0-based, `end` is exclusive, and VS Code counts `character` in UTF-16 units.
  - `at_mentioned`: `{filePath, lineStart, lineEnd}`. Lines are 0-based and `lineEnd` is inclusive. Claude shows it as `@path#Lstart-end` in its prompt.
- **Claude → IDE tools:** `openFile`, `openDiff` (blocks until it gets `FILE_SAVED` or `DIFF_REJECTED`), `getCurrentSelection`, `getLatestSelection`, `getOpenEditors`, `getWorkspaceFolders`, `getDiagnostics`, `checkDocumentDirty`, `saveDocument`, `close_tab`, `closeAllDiffTabs`, `executeCode`. Results take the MCP shape `{content:[{type:"text",text}]}`, with structured data JSON-stringified inside `text`.
- **What the model can call.** Only `getDiagnostics` and `executeCode` are exposed to the model as `mcp__ide__*` tools. **The model cannot steer the IDE through this connection.** `openFile` is called by Claude Code itself.
- **This machine:**
  - Claude Code is 2.1.322.
  - `~/.claude.json` has `"autoConnectIde": true`, so a Claude started while a matching IDE is running connects by itself. An already-running Claude connects with `/ide`.
  - No lockfiles exist today.

**Other agents:**

- **Gemini CLI** has a documented equivalent (`$TMPDIR/gemini/ide/gemini-ide-server-<pid>-<port>.json`, MCP over HTTP, `ide/contextUpdate`). It is a good later target.
- **Codex CLI** has an undocumented, pull-only Unix-socket hub. It is fragile, so we skip it.
- **OpenCode v2 and Crush** have no editor channel.
- **Zed's Agent Client Protocol** makes the editor host the agent's chat, which is the wrong shape for us.

**Starting deck next to the agent:**

| Environment | Detect | Command |
|---|---|---|
| Orca | `ORCA_TERMINAL_HANDLE` | `orca terminal split --terminal $ORCA_TERMINAL_HANDLE --direction vertical --command …` (or `terminal create --worktree active --command …`) |
| cmux | `CMUX_SURFACE_ID` | `cmux new-split right --command …` |
| Paradev | `SUPERSET_WORKSPACE_ID` | `superset panes new-tab --workspace <id> --command …` |
| tmux | `TMUX` | `tmux split-window -h -c DIR …` |
| Zellij | `ZELLIJ_PANE_ID` / `ZELLIJ` | `zellij action new-pane -d right --cwd DIR -- …` |
| WezTerm | `WEZTERM_PANE` | `wezterm cli split-pane --right --cwd DIR -- …` |
| kitty | `KITTY_WINDOW_ID` | `kitten @ launch --type=window --location=vsplit --cwd=DIR …` (needs `allow_remote_control`) |
| Ghostty (macOS) | `TERM_PROGRAM=ghostty` | AppleScript `split … with configuration` (1.3+) |
| iTerm2 | `TERM_PROGRAM=iTerm.app` | AppleScript `split vertically … command` (no cwd parameter, so wrap the command in `cd`) |
| Apple Terminal | `TERM_PROGRAM=Apple_Terminal` | AppleScript `do script` (new window only) |
| Alacritty | `ALACRITTY_SOCKET` | `alacritty msg create-window --working-directory DIR -e …` |
| Warp, anything else | — | none; print the command for the user |

Agent workspaces (Orca, Conductor, Paradev, Claude Squad, Emdash) run each agent in **its own git worktree**, so a deck instance is identified by its worktree top, not by the repo.

---

## 2. Goals

1. **G1. Control.** Any agent that can run a shell command can drive a running deck through `deck` subcommands: open a file or line range, reveal a directory, open the review view, jump to a file's diff, and read where the user is.
2. **G2. Presence.** deck keeps one live record of what the user is looking at: tab, file, cursor, selection, and the diff file and line. Agents read it with `deck where`. Claude Code also receives it automatically through the IDE connection, so every prompt carries the user's current file and selection.
3. **G3. Pointing.** One key in deck (`@`) sends the current selection or line to the connected Claude as an `@file#Lx-y` mention.
4. **G4. Live changes.** Edits made outside deck (by the agent or by the user in another editor) show up without switching focus: the open file reloads in place, and the tree badges, Git tab and review view refresh.
5. **G5. Review.** The Git tab gets a Review view: the branch's commits, plus every file changed compared to where the branch left its base (committed and uncommitted), each file's diff, per-file "viewed" ticks that clear when a file changes again, and an uncommitted-only toggle.
6. **G6. Start anywhere.** When an agent asks a deck that isn't running, deck starts one in a split or tab of whatever terminal or agent workspace it is in (see the table in §1.1), then carries out the request. When there is no route, it prints the exact command for the user to run.
7. **G7. Agents learn it.** `deck guide` prints a short guide for agents. `deck guide --install claude` installs it as a Claude Code skill.

## 3. Non-goals (v1)

- **No edit approval in deck.** Claude keeps asking for edit permission in its own pane, and deck shows no diff per edit.
- **No agent chat inside deck**, and no ACP.
- **No Gemini, Codex or OpenCode presence adapters.** The presence record is shared, so Gemini's companion protocol can be added later without touching the panes.
- **No editing through deck.** Agents write files themselves.
- **No remote, SSH or Windows decks.** Windows is already unsupported.
- **No multi-user sharing.** The socket and lockfile are the user's own.

## 4. Anti-goals: failure even if it "works"

- deck yanks the view away while the user is typing in a prompt, filter or commit box.
- Agent support makes deck feel slower. All polling, git calls and socket I/O stay off the UI goroutine, and `View` cost does not change.
- Anything listens beyond loopback, or without the user's own file permissions or token.
- `deck [dir]` stops working, or plain deck (no agent) behaves differently.
- It only works in Orca or only with Claude. A plain terminal plus any agent must get G1, G2 (via `deck where`), G4 and G5.

## 5. Constraints

- **Code and dependencies.** Go 1.26, Bubble Tea v1.3.10. Follow the CLAUDE.md conventions: value-type panes, `ui.Routed` messages, `ui.Fit` frames, pure helpers that are table-tested, and seams for exec. **Zero comments by default**, per the user's global rule.
- **One new dependency is allowed** for the WebSocket server: `github.com/coder/websocket`. Everything else uses the standard library. `internal/lsp` stays standard-library-only.
- **Unix socket paths.** macOS caps them at 104 bytes, so keep socket paths short.
- **Config.** The config schema grows (§6.9), with `Default()`, enum validation and "did you mean" kept in step.
- **Docs.** Update the project `CLAUDE.md` architecture section and the `README.md` with the new packages and keys.

---

## 6. Design

### 6.1 Instances and the control socket (`internal/control`)

- **Where a deck listens.** Each running deck listens on a Unix socket at `<dir>/<pid>.sock`, with a sidecar `<dir>/<pid>.json` holding `{pid, root, cwd, version, started, focused}`. `<dir>` is `$XDG_RUNTIME_DIR/deck` when that is set, else `${XDG_STATE_HOME:-~/.local/state}/deck/run`. It is never derived from `TMPDIR`, because an agent's sandbox may point `TMPDIR` elsewhere and the agent's `deck` must find the user's deck. The directory is created `0700` and the socket is `0600`. The path stays well under macOS's 104-byte limit, and the builder errors clearly if a home path would exceed it. Both files are removed on exit, and stale ones (dead pid, or a socket that refuses connections) are swept by every client and at startup.
- **`root`** is the git worktree top of deck's cwd (`git rev-parse --show-toplevel`), or the cwd when it isn't a repository.
- **`focused`** is updated, throttled, on `tea.FocusMsg`, so "the deck you last looked at" wins ties.
- **Protocol.** One request per connection: a JSON object, then a newline. The reply is a JSON object, then a newline. Every request carries `"v":1`.
  - Requests are `{v, cmd, args…, from}`.
  - Replies are `{ok, error?, …result}`.
  - Unknown commands and versions get `ok:false` with a clear message.
- **Server.** A goroutine accepts connections.
  - **Navigation commands** go into Bubble Tea via `p.Send(control.RequestMsg{Req, Reply chan})`. The app handles each one in `Update` and answers on `Reply`. While deck is suspended in micro (`tea.ExecProcess` blocks the event loop), the server times out after 2 s and replies `{ok:false, error:"deck is busy: editor open"}`.
  - **Read-only queries** (`where`, `ping`) are answered from the presence store (§6.2) without going through the loop, so they work even while micro is open.
- **Picking an instance (client side), first match wins:**
  1. `--pid N` or `$DECK_PID`.
  2. Live instances whose `root` equals the caller's worktree top.
  3. Live instances whose `root` contains the target path (for `open`) or the caller's cwd.
  4. If several match, the most recent `focused`, then the most recent `started`.

### 6.2 Presence (`internal/presence`, owned by `internal/app`)

`presence.Store` is a mutex-guarded snapshot shared by pointer, like `viewer.History`:

```go
type Snapshot struct {
    Root, Tab       string            // tab: files|git|run|http
    File            string            // absolute; "" when none
    Dir             string            // tree selection when it is a directory
    Line, Col       int               // 1-based cursor, rune columns
    Sel             *Range            // nil when nothing is selected; 1-based, End inclusive line, exclusive col
    SelText         string            // capped at 16 KiB
    Visible         [2]int            // first/last visible line
    Git             *GitSpot          // mode changes|review|log, file, diff side, base ref
    Seq             uint64            // bumps on every change
}
```

- **Publishing.** At the end of `app.Update`, the model builds the snapshot from cheap accessors (`files.ViewerLoc`, a new viewer `Selection()`, and a new git `Spot()`). It publishes only when something changed, which bumps `Seq`. Selection text is extracted only when the selection range changes.
- **Subscribers.** `Store.Subscribe()` returns a channel of `Seq` bumps, coalesced and never blocking, for the IDE bridge (§6.5).
- **Diagnostics.** The store also keeps the latest LSP diagnostics per absolute path, fed from `files.LSPEventMsg` diagnostics events, for `getDiagnostics`.

### 6.3 CLI (`cli.go`, `internal/control/client.go`)

A first argument that names a subcommand is that subcommand. To open a directory that happens to have one of these names, use `deck ./review` or `deck -- review`. `deck [dir]`, `--version` and `--help` are unchanged.

| Command | Effect |
|---|---|
| `deck open <path>[:line[:col]]` · `<path>:<l1>-<l2>` | **File:** switch to Files, open the viewer at the line (and select `l1`–`l2` when a range is given), reveal it in the tree. **Directory:** reveal and select it (directory card). Relative paths resolve against the **caller's** cwd. |
| `deck review [--uncommitted] [--base <ref>]` | Switch to Git → Review (§6.8). |
| `deck diff <path>[:line]` | Review view, select that file, focus its diff with the cursor on that new-side line. |
| `deck where [--json]` | Print the presence. The default is a short human or agent form: `files  internal/git/pane.go:120:5  selection 118-130` followed by the selected text. `--json` prints the full snapshot. Exit code 3 when no deck is running. |
| `deck ls` | List running decks: pid, root, focused. |
| `deck launch [--split\|--tab\|--window] [dir]` | Start a deck for `dir` (default: the caller's worktree top) next to the caller, per §6.4. |
| `deck guide [--install claude] [--yes]` | Print the agent guide, or install it (§6.10). |

Common flags:

- `--pid N` picks an instance.
- `--from NAME` labels the requester; the default is `Claude` when `CLAUDECODE=1` is set, otherwise `agent`.
- `--no-launch` means "fail with exit 3 when no deck is running" instead of starting one.

Output is one line on success and a non-zero exit with a one-line reason on failure, so agents can act on it.

**Agent navigation in the app:**

- Each jump pushes the origin onto the jump history, so `ctrl+o` returns to where the user was.
- Each jump shows a toast: `Claude → pane.go:120`.
- **If deck is capturing** (the user is typing or a modal is open), the jump is **held, not applied**. The status bar shows `Claude: pane.go:120 · alt+j`, and `alt+j` (capture-proof, currently unbound) applies it once. A newer request replaces the held one. Read-only queries are never held.

### 6.4 Starting deck next to the agent (`internal/launch`)

- **`Detect(env) Target`** is pure. It checks agent workspaces first, then multiplexers, then terminals, in the order of the table in §1.1.
- **`Command(target, mode, dir, deckBin) (argv []string, script string)`** is pure and builds the command; `script` holds the AppleScript text for Ghostty, iTerm2 and Apple Terminal.
- **Runner.** `run` executes the command through an `execCommand` seam, waits up to 5 s for a new instance whose `root` matches to appear in the instance directory, then sends the pending request.
- **Failure or unknown terminal.** Print `deck isn't running here — start it with: cd <dir> && deck` (plus a kitty hint when remote control is off) and exit 3.
- **Modes.**
  - `split` is the default: side by side, about 45% wide where the terminal supports it.
  - `tab` opens a new tab.
  - `window` opens a new window.
  - Config `agent.launch` sets the default; `off` disables auto-launch.
- **Paths.** `deckBin` is `os.Executable()`, and the target's `dir` is always passed explicitly.
- **Orca's working directory.** `orca terminal split --command` may not inherit the cwd; when in doubt, use `cd <dir> && exec <deck>`. The builder verifies against the real `orca`.

### 6.5 Claude Code IDE bridge (`internal/ide`)

This is on by default (`agent.claude = true`) and starts with deck.

- **Listener.** Listen on `127.0.0.1` with a random port in 10000–65535, retrying on collision. Write the lockfile `{pid, workspaceFolders:[root], ideName:"deck", transport:"ws", authToken}` with a 32-hex token from `crypto/rand`. Remove it on exit, and sweep lockfiles whose `ideName` is `deck` and whose pid is dead.
- **Handshake.** Reject upgrades without the right `x-claude-code-ide-authorization` header. Also reject any request carrying an `Origin` header, since browsers send one and Claude does not.
- **MCP.** Handle `initialize` (protocol `2025-03-26`, `serverInfo{name:"deck"}`, tools capability), `notifications/initialized`, `ping`, `tools/list` and `tools/call`. `ide_connected` sets a status-bar badge `✦ Claude` (several clients: `✦ Claude ×2`) and shows a one-time toast `Claude connected`. Disconnecting clears the badge.
- **`selection_changed`.** On each presence `Seq` bump, debounced 100 ms, send `selection_changed` to every client whenever the file, cursor line or selection changed:
  - **Positions.** Lines are 0-based; `character` is in **UTF-16 units**, as VS Code sends it. `end` is exclusive.
  - **No selection.** Send `isEmpty:true` with start equal to end at the cursor.
  - **In a review diff.** `filePath` is the file and the range covers the new-side lines under the diff cursor or selection.
  - **No file shown** (Run or HTTP tab, or a directory): nothing is sent, so Claude keeps the last one, as VS Code does.
- **Tools:**
  - `openFile{filePath, startText?, endText?}`: an agent navigation (§6.3). With `startText`/`endText`, select from the first match of `startText` to the end of the first `endText` after it. Returns `Opened file: <path>`.
  - `getCurrentSelection` / `getLatestSelection`: from presence, in the documented shapes.
  - `getOpenEditors`: the current file (`isActive`) plus up to 10 recent files from the jump history.
  - `getWorkspaceFolders`: `{success, folders:[{name, uri, path}], rootPath}`.
  - `getDiagnostics{uri?}`: from the presence diagnostics map.
  - `checkDocumentDirty`: always `isDirty:false`, because deck never holds unsaved text.
  - `saveDocument`: `{success:true, saved:false, message:"deck is read-only"}`.
  - `close_tab`: `TAB_CLOSED`.
  - `closeAllDiffTabs`: `CLOSED_0_DIFF_TABS`.
  - `executeCode`: not listed.
- **`openDiff` (decided behaviour).** Edit approval stays in Claude's own pane, exactly as without deck, and deck shows no diff UI. The builder **verifies with the real Claude 2.1.322** which of these keeps Claude's terminal prompt working, and picks the first that does:
  - (a) leave `openDiff` out of `tools/list`;
  - (b) list it, keep the call pending without showing anything until `close_tab` or disconnect, then answer `DIFF_REJECTED`;
  - (c) document setting Claude's `/config` diff tool to `terminal`.

  The finding goes into this spec.
- **`at_mentioned`.** The `@` key (§6.6, §6.8) sends `{filePath, lineStart, lineEnd}` (0-based, inclusive) to every connected client and toasts `sent @pane.go#L40-52 to Claude`. With no client connected, it copies `@<relpath>#L40-52` to the clipboard over OSC52 and toasts that instead, which works with any agent.
- **Status.** The status line shows the `✦ Claude` badge to the left of the LSP badges. The layout tests must still pass.
- **Discovery (verified from the 2.1.322 binary, 2026-11-13):**
  - **Matching.** Claude reads every `*.lock` in the `ide` directory. A lockfile is valid when one of its `workspaceFolders` contains Claude's cwd. A folder *inside* the cwd also counts for listing, but not for auto-connect. Deleting an unreadable lockfile is Claude's job.
  - **The pid check** (the lockfile's pid must be Claude's parent or an ancestor) applies **only when Claude runs inside a supported IDE's own terminal** (VS Code family, JetBrains). From Orca, tmux, Ghostty and the rest there is no pid check. `ideName` is free text used as the display name.
  - **Auto-connect.** It runs when `autoConnectIde` is true (shown in `/config` as "Auto-connect to IDE (external terminal)"; true on this machine), with `--ide`, or with `CLAUDE_CODE_AUTO_CONNECT_IDE=1`. At startup Claude polls for up to **30 s** and connects only when **exactly one** valid lockfile contains the cwd.
  - **Later.** A deck started after that window, or a second deck for the same folder, needs `/ide`.
  - **Implication.** deck's lockfile lists exactly `[root]`. `deck guide` and the README say: start deck first, or run `/ide`.
- **Live verification (Claude Code 2.1.322, 2026-11-13, tmux, scratch repo).**
  - **The `mcp` subprotocol is required.** Claude opens the socket with `protocols:["mcp"]` and fails ("Failed to connect to deck") unless the server selects `mcp`. The bridge now accepts it.
  - **Auto-connect.** `claude --ide`, or `autoConnectIde`, started after deck auto-connects (`ws-ide` in about 50 ms).
  - **`/ide` reconnects** a running Claude after deck restarts.
  - **Presence.** `deck open main.go:3-5` shows `[⧉ 3 lines selected]` in Claude's prompt, and the transcript records `⧉ Selected 3 lines from main.go in deck`.
  - **Pointing.** `@` in deck inserts `@main.go#L3-5` into Claude's input.
  - **Live reload.** Claude's edits show in deck within about a second: the file reloads in place, the tree shows `M` and the status shows `1 dirty`.
- **`openDiff`: option (a) chosen.** `openDiffMode = "unlisted"` works:
  - **Auto mode:** edits apply directly.
  - **Manual mode:** Claude calls `openDiff`, gets `unknown tool`, logs "IDE diff view failed", and falls back to its normal terminal prompt ("Do you want to make this edit?"). Approving applies the edit.
  - **The one cosmetic oddity:** Claude prints "Opened changes in deck ⧉" above that prompt. Setting `/config → Diff tool: terminal` removes it, and `deck guide` mentions this. Option (b) wasn't needed.
- **`diffTool` (verified).** Claude has a `diffTool` setting, `auto` (default) or `terminal`. With `auto` and an IDE connected, every edit calls `openDiff` and awaits its result: `FILE_SAVED` (accepted, using the returned contents), `DIFF_REJECTED`, or a third outcome that leaves the file unchanged. An error is thrown otherwise. The spike in the `openDiff` bullet above still decides between (a), (b) and (c). If neither (a) nor (b) leaves the terminal prompt working, (c) applies, and `deck guide --install claude` tells the user to set `diffTool` to `terminal`. It never edits `~/.claude.json` itself.

### 6.6 Selection in the viewer (`internal/viewer`)

- **Keys.** `v` starts a character-wise selection at the cursor and `V` a line-wise one. Every cursor motion extends it. `esc` clears it first, before closing anything else.
- **Copying.** With a selection, `y` copies the selected text and `Y` copies `relpath:l1-l2`. Without one, both keep their current behaviour.
- **Mouse.** Press and drag selects (a press without motion still just places the cursor), and shift+click extends from the cursor. Double-click behaviour (go to definition per `viewer.goto_click`) is unchanged.
- **Painting.** The selection is painted through the existing `Paint(styled, spans)` with a new `ui` selection style, and only on visible rows, so `View` cost doesn't change.
- **Capture.** Selection is **not** capturing: `q`, `1`–`4` and `?` still work.
- **`@`** sends the selection (or the cursor line) as an `at_mentioned` (§6.5).
- **API.** `Selection() (start, end Pos, ok bool)` and `SelectRange(start, end Pos)` support §6.3's `:l1-l2` and `openFile`'s `startText`.
- **Reset.** The selection clears when another file is opened. A live reload of the same file (§6.7) keeps it, clamped to the new line count.
- **Rendered markdown (my call).** When markdown is rendered, cursor rows are glamour output, not source lines, so:
  - Anything that needs exact source lines switches the viewer to raw source first, the way the outline already does: an agent's `open file.md:40[-52]`, `openFile` with `startText`, and `v`/`V`. `m` switches back.
  - `deck open file.md` with no line keeps the rendered view.
  - While the user reads rendered markdown, presence carries the file plus the **section** the cursor is in: its heading text and source line range. The pure `renderedSection(md, row)` matches rendered heading rows, in order, to `MarkdownOutline` headings.
  - `deck where`, `selection_changed` and `@` all use that section's line range. Exact line mapping inside rendered blocks is out of scope.

### 6.7 Live changes (`internal/app/watch.go`)

The watcher polls rather than using fsnotify: polling works on every filesystem, and kqueue needs one descriptor per file.

- **Open file.** Every `agent.watch_ms` (default 1000; `agent.watch = false` turns polling off), stat the open file off the UI goroutine. If its mtime or size changed, reload it in place, keeping cursor and scroll. This reuses the `ReloadCurrent` path.
- **Working tree.** Every 2 × `watch_ms`, run `git status --porcelain=v2 -z` plus `rev-parse HEAD` and hash the output. If the hash changed, return `git.Refresh`, `files.RefreshBadges`, a tree `Reload` (identity-preserving, so expansion and selection survive) and a review refresh. Only one check is in flight at a time, with a 10 s timeout. A repository whose status takes more than 1 s backs off to 10 s and toasts once.
- **Paused** while deck is suspended in micro, because the event loop is blocked anyway.

### 6.8 Review view (`internal/git`)

The Git tab gets a third mode next to Changes and Log.

- **Entry.** `v` from the list (my call: currently unbound there), or `deck review` / `deck diff`. `v` again, or `esc` from the list, returns to Changes.
- **Base** is the first that exists:
  1. `--base`;
  2. config `git.review_base`;
  3. the target of `refs/remotes/origin/HEAD`;
  4. `origin/main`;
  5. `origin/master`;
  6. `main`;
  7. `master`.

  The review compares against `merge-base <base> HEAD`. With no base, or when HEAD is the base with nothing ahead, it falls back to uncommitted changes against HEAD and the header says so.
- **Header:** `Review · vs origin/main @ 3f2a1c9 · 5 commits · 12 files +340 −88 · 3/12 viewed`.
- **List:**
  - **Commits:** `git log --oneline <mb>..HEAD`, collapsed by default and toggled with `enter` on the header. `enter` on a commit shows it with `show --stat -p`, reusing the log's renderer.
  - **Files:** `git diff --name-status -z -M <mb>` (the working tree against the merge base, so staged and unstaged together), plus `--numstat` for `+/−`, plus untracked files (`ls-files --others --exclude-standard -z`) shown as added. Each row has a status letter, the path (`old → new` for renames), the counts and a `✓` when viewed.
- **`u`** toggles uncommitted-only: the same list against `HEAD` instead of the merge base.
- **Viewed ticks.**
  - `space` toggles the tick on the selected file, and `n` jumps to the next unviewed file.
  - Ticks are stored in `<GitDir>/deck/review.json` as `{base, files:{path: blobHash}}`. A file counts as viewed only while its current working-tree content hashes to the stored value (git blob SHA-1 computed in Go, `deleted` for removals), so any later change clears the tick.
  - Ticks are per worktree (`GitDir`) and survive restarts. Changing the base starts a fresh set.
- **Diff.** The diff is fetched with `git diff -M <mb> -- <path>` (`--no-index /dev/null` for untracked files), cached and capped like today's diffs.
- **Diff cursor**, shared with Changes mode:
  - The focused diff gets a line cursor (a highlighted row) moved by `j`/`k`/`g`/`G`/`ctrl+d`/`ctrl+u` and the mouse. `v` starts a line selection.
  - The pure `diffLineToFile(diff, row) (newLine, side)` maps rows through hunk headers. Removed lines map to the nearest new-side line, with `side=old`.
  - `enter` on a diff line opens Files at that file and line (a `ui.FocusTabMsg` plus a reveal at the target); `e` opens the editor there; `@` mentions the line or selection.
  - The cursor and selection feed presence, via a git `Spot()`.
- **Refresh.** Live changes (§6.7) refresh the list, the counts, the ticks and the shown diff. The cursor stays on the same path.
- **Scale.** All git calls run as commands with seqs, like the existing pane, and the list is rendered O(visible).

### 6.9 Configuration

```toml
[agent]
enabled  = true      # control socket + presence; false = none of §6.1–6.7
launch   = "split"   # split | tab | window | off
watch    = true      # live polling (§6.7); false turns it off
watch_ms = 1000      # poll interval, minimum 250 (fillZero turns 0 into the default, hence the separate bool)
claude   = true      # Claude Code IDE bridge (§6.5)

[git]
review_base = ""     # "" = auto (§6.8)
```

`launch` is enum-validated. Unknown keys get "did you mean" like the rest of the schema.

### 6.10 Teaching agents (`deck guide`)

- **`deck guide`** prints about 60 lines of markdown, embedded from `internal/control/guide.md`. It covers:
  - what deck is;
  - always run `deck where` when the user says "this", "here" or "what I'm looking at";
  - `deck open path:line` and `path:l1-l2` to show code, and `deck diff`/`deck review` to walk through changes;
  - don't jump while explaining unrelated things, one jump per point;
  - exit codes;
  - `--no-launch`.
- **`deck guide --install claude`** writes it as `${CLAUDE_CONFIG_DIR:-~/.claude}/skills/deck/SKILL.md`, with front matter `name: deck` and a description that triggers on "deck", "show me", "walk me through", "review the changes", "what am I looking at". It asks before overwriting unless `--yes`.
- **README** gets an "Agents" section: the commands, the Claude setup (`deck guide --install claude`; `/ide` if Claude started before deck), and an AGENTS.md snippet for other agents.

---

## 7. Testing

- **Pure tables:**
  - instance selection;
  - `launch.Detect`/`Command` for every row of §1.1, including the AppleScript text;
  - CLI argument parsing (subcommand vs directory, `path:l`, `path:l:c`, `path:l1-l2`);
  - UTF-16 column conversion;
  - `diffLineToFile`;
  - review base resolution;
  - `name-status`/`numstat` parsing;
  - blob hashing (matches `git hash-object`);
  - viewed-state logic.
- **Control server.** A real socket in a short temp dir (`os.MkdirTemp("", "dk")`, never `t.TempDir()` on macOS because of the path limit). Covers round trips, the busy timeout, stale sweeping and version errors.
- **IDE bridge.** A real WebSocket client in tests (from `coder/websocket`). Covers auth rejection, `Origin` rejection, initialize, `tools/list`, each tool's shape, debounced `selection_changed` after presence bumps, `at_mentioned`, and lockfile create and remove with `CLAUDE_CONFIG_DIR` pointed at a temp dir.
- **App:**
  - a control `RequestMsg` switches tab and opens the target;
  - a held jump while capturing, then `alt+j`;
  - presence publishes on cursor, selection and tab changes;
  - `TestFrameFitsTerminal` with the Claude badge and a held-jump status.
- **Review.** Temp repos through the existing integration style: a branch with commits plus staged, unstaged and untracked changes; a rename; the base fallbacks; ticks clearing after a change.
- **Viewer.** Selection keys and the mouse drag. `BenchmarkViewerView` must not regress.
- **Gates.** `go test -race ./...`, `gofmt`, `go vet` and the benchmark.

## 8. Builder's calls (left open on purpose)

- Exact package boundaries beyond those named, file layout and message names.
- Toast wording, the badge glyph and the selection colour, within the existing `ui` palette.
- The Ghostty and iTerm2 AppleScript details. They must open a split running deck in the right directory; if targeting the calling surface is unreliable, use the front window's focused terminal.
- Whether `deck where` also prints the visible range in human mode.
- The `openDiff` option (§6.5), decided by the verification spike, and the IDE discovery findings. Both get recorded back into this spec.

## 9. Risks

- **The Claude IDE connection is unofficial and may change.** G1, G2 (via `deck where`), G4, G5 and the clipboard fallback for G3 do not depend on it.
- **Claude Code's Bash sandbox, when enabled, may block Unix-socket connects.** If the builder sees this, `deck guide` documents the allowlist entry.
- **`git status` polling on huge repos** is mitigated by the back-off in §6.7.
