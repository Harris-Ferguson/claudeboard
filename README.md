# claudeboard

A terminal control panel for Claude Code. See every session you have going, across
all your repos, grouped into workflows. Jump into one with Enter and back out with
Ctrl+Z, start new ones in a couple of keystrokes, and keep an eye on token usage.

It's built on Claude Code's background sessions (`claude --bg`), so sessions keep
running when you quit the board.

```
┌ claudeboard · 1 working · 1 needs you · 65k tokens ───────────────────────────┐
│ payments-v2  !             53k │ cb-payments-v2-2 · web · 12k tokens          │
│   ● api  wire the new Aft… 41k │ in 1.2k · out 8.4k · cache r 2.1k · w 300    │
│ ▸ ! web  hook PaymentForm… 12k │ ! needs you — permission prompt: Bash npm t… │
│ q4-search                  12k │                                              │
│   ✓ api  plan the search…  12k │ you: hook PaymentForm into checkout          │
│ quick                          │   › Edit  CheckoutPage.vue                   │
│   ○ scratch  new session       │   › Bash  npm test                           │
│ Other sessions                 │ claude: Wired it in. Running the tests…      │
│   ● arbybot-8d                 │                                              │
├────────────────────────────────┴──────────────────────────────────────────────┤
│ ↑↓ move  enter open  tab next waiting  n new  s quick  ? help  q quit         │
└───────────────────────────────────────────────────────────────────────────────┘
```

## Requirements

- macOS (Linux probably works but is untested)
- Claude Code 2.1.312 or newer, with background sessions: `claude agents --json` must print JSON
- Go 1.26 or newer. With an older Go, `go install` downloads 1.26 by itself (`GOTOOLCHAIN=auto`, the default)

## Install

```sh
git clone https://github.com/Harris-Ferguson/claudeboard
cd claudeboard
go install ./cmd/claudeboard
```

That puts `claudeboard` in `$(go env GOPATH)/bin`. If your shell can't find it, add that
folder to your `PATH` (for zsh, in `~/.zshrc`):

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

To update later, run `git pull && go install ./cmd/claudeboard`. `claudeboard --version`
shows which commit you're running.

## Using it

Run `claudeboard`.

| Key | Does |
|---|---|
| ↑↓ / j k | move |
| Enter | open the session; **Ctrl+Z inside it comes back to the board**. On a workflow: fold/unfold |
| Tab | jump to the next session that needs you |
| n | new session: pick a workflow, then a folder, and Claude opens straight away |
| N | new session at once, in a new workflow with a random name (like `amber-otter`), in the selected row's folder; asks only about a worktree if that folder is busy |
| s | quick one-off in the scratch folder, no questions |
| x | stop the session (keeps its conversation and worktree) |
| r | resume a stopped session in the background |
| D | delete the session and its worktree (asks first) |
| m | move a session from "Other sessions" into a workflow. One open in another terminal is continued in a background copy (`claude --bg --resume`), which opens straight away; close the original afterwards. A background session is added as it is |
| a / A | archive a workflow / show archived workflows |
| p | show or hide the preview (hidden by default below 90 columns) |
| PgUp / PgDn | scroll the preview |
| Ctrl+L | redraw the screen |
| ? | help |
| q | quit; sessions keep running |

Statuses: `●` working, `!` needs you (with what it's waiting for), `✓` done (your turn),
`○` new (no prompt yet), `◌` stopped.

Token counts on rows, workflows and in the header are the tokens each session added:
input, output and cache writes. They leave out cache reads, which re-count the same
context on every turn and would otherwise be 95% of the number. The preview shows the
full breakdown (`in`, `out`, `cache r`, `w`).

"Other sessions" lists Claude sessions claudeboard didn't start, such as ones you
started in another terminal. You can preview them, and open, stop or delete the
background ones.

### From any terminal

```sh
claudeboard new                                  # quick session here, opens straight away
claudeboard new -w payments-v2 -r api            # into a workflow, in a configured repo
claudeboard new -w payments-v2 -r api --worktree # in its own git worktree
claudeboard new -w bugs "fix the flaky test"     # with a prompt: starts in the background and returns
```

Options can go before or after the prompt. `claudeboard new` tells you the session's name
and ID before opening it, and how to get back to it after Ctrl+Z. Without a terminal
(a script, an IDE run configuration) it needs a prompt, so sessions aren't left behind.

## Config

`~/.claudeboard/config.toml` is created on first run:

```toml
# scratch_dir = "~/.claudeboard/scratch"   # where quick one-offs run

[repos]               # repos you start sessions in: name = path
api = "~/work/api"
web = "~/work/web"

[defaults]
model = ""            # empty = Claude Code's default
permission_mode = ""  # empty = Claude Code's default
ide = false           # pass --ide so diffs open in your IDE
```

`CLAUDEBOARD_HOME` moves the whole folder somewhere else.

## PHPStorm

- Run `claudeboard` in PHPStorm's terminal tool window.
- By default, Esc in that terminal moves focus to the editor instead of reaching Claude.
  Turn it off in **Settings → Tools → Terminal**: untick **Move focus to the editor with Escape**.
- To have diffs open in PHPStorm, set `ide = true`. This is untested with background
  sessions; see the checklist.

## First-run checklist

1. `claude --version` shows 2.1.312 or newer, and `claude agents --json` prints JSON.
2. Run `claude` once in the clone folder and accept the trust prompt, then
   `go test -tags realclaude -v ./internal/claudecli`. This starts and deletes an idle
   session; no prompt is sent, so it uses no tokens.
3. Add your repos to `~/.claudeboard/config.toml`, then run `claudeboard`.
4. Press `n`, choose "+ new workflow…", name it, and pick a repo. Claude opens. Type a
   prompt, then press Ctrl+Z. The row shows `●` working, then `✓` done, and the preview
   shows the conversation and token counts.
5. When a session asks for permission, the header says "1 needs you" and Tab jumps to it.
6. `x` stops a session (`◌ stopped`). Enter resumes and reopens it.
7. `D` deletes it. For a worktree session, check the worktree folder is gone.
8. `s` the first time asks to trust the scratch folder. Press `y`, accept in Claude,
   type `/exit`, and the session starts.
9. In PHPStorm's terminal, after the Esc setting above, Esc interrupts Claude.
10. With `ide = true`, start a session that edits a file and see whether the diff opens
    in PHPStorm.

## Troubleshooting

- **"not trusted by Claude yet"**: Claude Code won't start background sessions in a folder
  you haven't trusted. Both the board and `claudeboard new` offer to open `claude` there so
  you can accept the trust prompt (type `/exit` afterwards); or run `claude` there yourself
  once. The scratch folder needs this the first time you press `s`.
- **"status stale"**: `claude agents --json` is failing or slow. Run it yourself to see why.
- **A session shows "no messages yet" after you've talked to it**: was it started from
  inside another Claude session's shell? claudeboard removes the variables that turn
  off conversation saving, but sessions started some other way may not have saved.
- **The board looks wrong after coming back from a session**: press Ctrl+L to redraw
  it. claudeboard also resets the terminal modes `claude attach` uses (mouse and focus
  reporting, synchronized output, scroll region, colours) before it takes the screen
  back, so this shouldn't be needed; if it is, please open an issue saying which terminal
  you use.

## How it works

claudeboard runs `claude --bg` to start sessions, polls `claude agents --json` for their
state, and hands your terminal to `claude attach` to open one. It reads each session's
conversation file under `~/.claude/projects/` for the preview and token counts, and
keeps its own list of workflows in `~/.claudeboard/state.json`. Sessions it starts are
named `cb-<workflow>-<n>`, so they're recognisable in `claude agents` too. The design is
in `docs/superpowers/specs/2026-10-28-claudeboard-design.md`.

`claude attach` runs with `DISABLE_TELEMETRY=1`: otherwise it waits to send telemetry
after Ctrl+Z, which made going back to the board take 0.3 s or more. While you're
attached, that viewer, including the agent view you reach with ← inside it, sends no
usage telemetry or error reports and fetches no feature flags. Your sessions run in
Claude's background service, which keeps the environment of the command that started it.
`claude attach` starts that service if it isn't running, and the service would inherit
`DISABLE_TELEMETRY=1`, so before opening a session Claude doesn't list as running
(stopped, failed, crashed or exited, or no longer listed), the board resumes it with
`claude --bg --resume`, which starts the service, if needed, with your own environment.
One case remains: if the service stops while you're attached and the viewer restarts
it, that service and the sessions it runs inherit `DISABLE_TELEMETRY=1` until it exits.

## Development

```sh
go test ./...                                     # everything, no real Claude needed
go test ./internal/board -update                  # rewrite the reference screens
go test -tags realclaude -v ./internal/claudecli  # check against your real Claude Code
```
