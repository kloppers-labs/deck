# deck: exploring code with the user

deck is the terminal code browser the user has open beside you. It shows the
project's files, its git changes and a Review of everything this branch changed.
You and the user drive it together: they move around in it, and you can show
them things in it.

## Know what the user means

- When the user says "this", "here", "these lines" or "what I'm looking at",
  run `deck where` first. It prints the tab, `file:line:col` and any selected
  text, also in the Git tab's diffs. `deck where --json` prints everything.
- When Claude Code is connected to deck (deck's status bar shows `✦ Claude`),
  each prompt already carries the user's file and selection, and the user can
  press `@` in deck to put `@file#Lx-y` into your prompt.

## Show the user code

- `deck open path/to/file.go:120` opens a file at a line.
- `deck open path/to/file.go:118-130` opens it with lines 118-130 selected.
- `deck open path/to/dir` reveals a directory.
- Paths are relative to your working directory.
- Make one jump per point you are explaining, and don't jump while talking
  about something else. The user gets back with `ctrl+o`.
- If the user is typing in deck, the jump is held as a hint they can accept
  (the command prints `held`); don't repeat it.

## Walk through changes

- `deck review` shows everything this branch changed against its base (the
  merge base with origin's default branch or main), committed and uncommitted.
  `--uncommitted` narrows it to uncommitted work; `--base ref` picks the base.
- `deck diff path/to/file.go:42` opens that file's change with the cursor on
  new-side line 42.
- The user ticks reviewed files. A tick clears when the file changes again, so
  files you edit show up as unreviewed.

## Starting deck

- With no deck running for this project, these commands start one beside your
  terminal (Orca, tmux, Zellij, WezTerm, kitty, cmux, Paradev, Ghostty, iTerm2,
  Terminal, Alacritty). Pass `--no-launch` to fail instead.
- `deck launch [--split|--tab|--window]` starts one explicitly; `deck ls` lists
  running decks and `--pid N` picks one.

## Results

- Success prints one line on stdout.
- exit code 1: an error; the reason is on stderr.
- exit code 2: a usage error.
- exit code 3: no deck is running and none could be started; stderr says how
  the user can start it.

## Claude Code

- To connect, start deck before Claude, or run `/ide` and pick deck.
- Edits still ask for permission in Claude's own pane. Claude may print
  "Opened changes in deck" above that prompt; `/config` → Diff tool: terminal
  removes the line.
