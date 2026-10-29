# claudeboard — design

Date: 2026-10-28 · Status: draft for review

## 1. Purpose

Running three or more Claude Code workflows at once across several repos (say a multi-agent feature build, a planning session and a batch of backlog bugs) means juggling terminal windows and losing track of which session needs attention. claudeboard is one command that opens a control panel for all of them: see every session's state at a glance, jump into any session and back, start new sessions quickly, and see token usage.

Success criteria:

- From one terminal (including PHPStorm's), every session claudeboard started is visible, grouped by workflow, with live status.
- Getting from the board into a session and back is one key each way.
- Starting a new session takes at most two confirmations; a quick one-off takes one key.
- Sessions waiting on the user (e.g. a permission prompt) are obvious and reachable with one key.
- Token totals are shown per session and per workflow.
- Quitting the board never stops a session.

## 2. Decisions

| Decision | Why |
|---|---|
| The user orchestrates; claudeboard does not automate handoffs between sessions | The user starts workflows with their own prompts; the problem is keeping track, not automation |
| Built on Claude Code's background sessions (`claude --bg`, `claude agents --json`, `claude attach`) | Claude Code already keeps sessions alive, reports their state and manages worktrees (verified in a spike, section 4). tmux was the fallback |
| Go, Bubble Tea v2, Bubbles, Lip Gloss | One fast-starting binary for both the board and the CLI |
| No Claude Code hooks in v1 | `agents --json` gives status and transcripts give detail, so nothing is injected into sessions |
| Tokens only, no cost estimates. Totals leave out cache reads (input + output + cache writes); the preview shows the full breakdown | User preference. Cache reads re-count the same context every turn and were 95%+ of real totals (e.g. 401M), which made totals meaningless (DX review, 2026-10-29) |
| Install by cloning and running `go install` | Go is available at work |
| PHPStorm: run in its terminal for v1; a native plugin is a later phase | The board works in PHPStorm's terminal from day one |

## 3. Scope

**v1:** the board (section 7), `claudeboard new` (section 8), config and state files (section 6).

**Later, not v1:** usage breakdown by day/repo/model; macOS notifications when a session needs the user; hooks for instant updates; launch templates; replying to a session without opening it; search; PHPStorm plugin.

**Non-goals:** automated multi-agent pipelines; replacing Claude Code's own interface. macOS is the target platform; nothing Mac-specific is planned, but other platforms are untested.

## 4. Claude Code contract

Verified on 2026-10-28 against Claude Code 2.1.312. Everything claudeboard assumes about Claude Code is listed here, and all code that depends on it lives in `internal/claudecli` and `internal/transcript`.

### Commands

| Purpose | Command | Notes |
|---|---|---|
| Start | `claude --bg --name <name> [--worktree <wt>] [--model <m>] [--permission-mode <p>] [--ide] [<prompt>]`, run with the repo as working directory | Output contains `backgrounded · <shortId> · <name>`, followed by ` (idle — send a prompt to start)` when no prompt is given. ~0.6 s; ~4 s when the background service has to start first |
| List | `claude agents --json --all` | JSON array; see below |
| Open | `claude attach <shortId>` | Fully drawn in under 1 s. Ctrl+Z exits it with code 0 and the session keeps running. Left arrow inside a session opens Claude's own agent view |
| Stop | `claude stop <shortId>` | Keeps the conversation and worktree |
| Delete | `claude rm <shortId>` | Deletes the session and its worktree; may print a message about a retained worktree or unpushed commits, which the board shows verbatim |
| Resume | `claude --bg --resume <sessionId>`, run in the session's directory | On a stopped session: prints `note: woke session <shortId> …` then the usual `backgrounded · <shortId> · <name>` line, keeping the same IDs. On a running session it starts a copy instead (`backgrounded · <newShortId>`, with no name), so claudeboard only resumes stopped sessions |
| Continue elsewhere | `claude --bg --name <name> --resume <sessionId>`, run in the session's directory | Verified 2026-10-29. For a session open in an interactive terminal: starts a background copy with a new ID under `<name>`, carrying the conversation, and prints `note: session <id> is open in another Claude Code process, so this started a copy as <shortId>. The original conversation is unchanged.` For a background session, passing any option (such as `--name`) also starts a copy, because a background session keeps its own saved options; so claudeboard never uses this on background sessions |

- `--bg` ignores `--session-id`. The full session ID is found by matching the short ID in the list.
- Claude can't rename a session, and an interactive session drops out of `claude agents --json --all` once its terminal closes.
- `--worktree <wt>` creates `<repo>/.claude/worktrees/<wt>` on branch `worktree-<wt>`. The session's reported `cwd` becomes the worktree path.

List entries: `{pid, id, cwd, kind, sessionId, name, status, state, waitingFor}`. `id` is the short ID. `kind` is `"background"` or `"interactive"`; interactive entries carry `status` (`busy`/`idle`) but no `state` or `id`. A stopped background session is listed as `state: "stopped"` with no `pid` or `status`. A session that failed to start is listed as `state: "failed"`. Right after a launch, an entry can briefly appear with no `pid` yet, so a missing `pid` does not mean stopped.

### Status mapping

| From `agents --json` (and transcript) | Board shows |
|---|---|
| `state: working` | `● working` |
| `state: working` with `status: idle` (a resumed session idling at its prompt) | `○ new` if no prompt sent yet, else `✓ done` |
| `state: blocked` with `waitingFor` set | `! needs you — <waitingFor>` |
| `state: blocked`, no `waitingFor`, no prompt sent yet | `○ new` |
| `state: blocked`, no `waitingFor`, prompt already sent | `! needs you` |
| `state: done` | `✓ done` |
| interactive `status: busy` / `idle` | `● working` / `✓ done` |
| `state: stopped`, or in `state.json` but not listed | `◌ stopped` |
| anything else | the raw `state` text, dimmed |

"Prompt sent yet" means the transcript contains at least one user prompt.

### Transcripts

- Main conversation: `~/.claude/projects/<dir>/<sessionId>.jsonl`, located with the glob `~/.claude/projects/*/<sessionId>.jsonl`. Nothing relies on how `<dir>` is encoded, so worktree sessions work too.
- Subagents: `~/.claude/projects/<dir>/<sessionId>/subagents/agent-*.jsonl`, same record format.
- One JSON object per line. Only two record types are used. All other types are ignored:
  - `type: "user"`: `message.content` is a string or an array of `text` / `tool_result` blocks. Records with `isMeta: true` are skipped.
  - `type: "assistant"`: `message.id`, `message.model`, `message.content` (array of `text` / `tool_use` blocks) and `message.usage`.
- One API response can be written as several records with the same `message.id`. Usage is counted once per `message.id`.
- Usage fields summed: `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`.

### Gotchas

- `--bg` refuses folders that have never been trusted (`Workspace not trusted. Run claude in <dir> once…`).
- The background service keeps the environment of whatever process started it. Environment variables set when launching a session do not reach that session, so per-session data is never passed through the environment.
- Launching from inside a Claude Code session inherits marker variables that turn off transcript saving. claudeboard removes exactly these from the environment of every `claude` command it runs: `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_EXECPATH`, `CLAUDE_CODE_MESSAGING_SOCKET`, `CLAUDE_CODE_MESSAGING_TOKEN`, `CLAUDE_CODE_SESSION_ATTENDED`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_EFFORT`, `CLAUDE_PID`. Other `CLAUDE_CODE_*` and `ANTHROPIC_*` variables (for example a company's provider settings) are left alone.
- After Ctrl+Z, `claude attach` restores the screen within a few milliseconds but then sends telemetry before exiting: ~0.3 s here (median 335 ms), longer on a slow network, and the board can't redraw until it exits. With `DISABLE_TELEMETRY=1` it exits in ~7 ms (verified 2026-10-29), so claudeboard sets it on `claude attach` (and on no other command). In the attach viewer, including the agent view reached with ← inside it, that turns off usage telemetry, error reports and feature flags.
- `claude attach` starts the background service if it isn't running, and that service inherits the attach process's environment, `DISABLE_TELEMETRY=1` included, for every session it runs (verified 2026-10-29). Its code also restarts the service if the connection drops mid-attach. The service exits 5 s after its last session and client are gone, so "not running" is a normal state. claudeboard therefore never attaches to a session that is stopped, failed, crashed or exited, or that Claude doesn't list: it resumes it first (section 7), which starts the service, if needed, with the user's environment. Remaining risk: if the service dies while the user is attached and the viewer restarts it, that service and the sessions it runs inherit `DISABLE_TELEMETRY=1` until it exits.
- `claude logs` prints raw terminal output with spinner frames and is not used.
- Not yet verified: whether `--ide` works from background sessions.

## 5. Architecture

```
cmd/claudeboard/     main: `claudeboard`, `claudeboard new`, `--version`
internal/
  claudecli/         runs `claude`; parses its output
  transcript/        reads conversation files incrementally → preview items + token totals
  store/             ~/.claudeboard/state.json: workflows and session membership
  config/            ~/.claudeboard/config.toml
  board/             Bubble Tea UI
```

**claudecli.** Pure parsing functions (`ParseAgents([]byte)`, `ParseLaunch(string)`) plus thin wrappers that run `claude` with the cleaned environment: `List`, `Launch`, `Stop`, `Remove`, `Resume` and `AttachCmd`. `AttachCmd` returns an `*exec.Cmd` for the board to hand the terminal to. Every call has a timeout (5 s for list/stop/rm, 15 s for launch/resume).

**transcript.** A `Reader` per session holds the main and subagent file paths and a byte offset per file. `Update()` reads only appended bytes, keeps an incomplete trailing line until it is finished, and starts over if a file shrinks. It produces a `Snapshot`:
- the last 200 preview items, each a prompt ("you"), text ("claude") or tool call (the tool's name and its most telling argument; for Bash, Claude's own description of the command). Calls Claude makes to manage itself, such as `ToolSearch`, are left out
- the pending tool call: the last `tool_use` with no matching `tool_result`
- token totals split into input, output, cache read and cache write
- the first prompt
- the prompt count
- the subagent count

Tool calls are summarised as the tool name plus one key argument: the file's base name for file tools, the first 60 characters of the command for Bash, the pattern for Grep/Glob, otherwise the name alone.

**store.** Loads and saves `state.json` (section 6) through `Update(func(*State) error)`. The callback runs while holding an exclusive `flock` on `~/.claudeboard/state.lock`, and the result is written atomically (temp file, then rename). This allows the board and `claudeboard new` to run at the same time.

**config.** Reads `config.toml` and expands `~`. On first run it creates the file with commented examples, and it creates the scratch folder if it is missing. `CLAUDEBOARD_HOME` overrides `~/.claudeboard`; this is used by tests.

**board.** The data sources are:
- **Live status:** `claudecli.List` every 1.5 s. It runs in the background, the next check is skipped while one is still running, and it times out after 5 s.
- **Transcripts:** readers updated on the same tick, giving token totals for all sessions and preview data for the selected one.
- **The store:** re-read on every refresh. The file is small, so this is simpler than watching for changes.

Store sessions are joined to listed sessions on `sessionId`. Listed sessions that are not in the store go into an "Other sessions" group.

## 6. Data files

`~/.claudeboard/config.toml`:

```toml
# Where quick one-offs run. Default: <claudeboard home>/scratch
# scratch_dir = "~/.claudeboard/scratch"

# Repos you start sessions in: name = path
[repos]
# api = "~/work/api"
# web = "~/work/web"

[defaults]
model = ""            # empty = Claude Code's default
permission_mode = ""  # empty = Claude Code's default
ide = false           # pass --ide so diffs open in your IDE (untested with background sessions)
```

(`scratch_dir` has to come before the first `[table]`; in TOML, keys below a table header belong to that table.)

`~/.claudeboard/state.json`:

```json
{
  "version": 1,
  "workflows": [
    {"slug": "payments-v2", "name": "payments-v2", "created_at": "2026-10-28T22:00:00Z",
     "archived": false, "last_repo": "api"}
  ],
  "sessions": [
    {"session_id": "301e18a1-a405-…", "short_id": "301e18a1", "name": "cb-payments-v2-1",
     "workflow": "payments-v2", "repo": "api", "dir": "/Users/me/work/api",
     "worktree": "payments-v2-1", "created_at": "2026-10-28T22:01:00Z"}
  ]
}
```

`repo` is the config name when the folder matches a configured repo, otherwise the absolute path. `dir` is the repo folder; the worktree path is derived from `dir` and `worktree`.

**Naming.**
- **Workflow slug:** the name lowercased, with every run of characters outside `[a-z0-9]` replaced by `-` and the result trimmed to 24 characters. The built-in quick workflow is `quick`.
- **Session name:** `cb-<workflow-slug>-<n>`, where `n` is one more than the highest `n` used in that workflow.
- **Worktree name:** `<workflow-slug>-<n>`.

**Re-linking.** On every refresh, any listed session whose name matches `cb-<slug>-<n>` but which `state.json` doesn't know is adopted. It goes into workflow `<slug>` (created if missing), with `dir` set to its reported `cwd`. The slug is the text between `cb-` and the last `-`. A known session whose short ID changed gets the new one. If `state.json` cannot be parsed, it is renamed to `state.json.corrupt-<unix-time>` and the board starts empty; re-linking then restores the sessions that are still listed.

## 7. The board

```
┌ claudeboard · 1 working · 1 needs you · 65k tokens ───────────────────────────┐
│ payments-v2  !             53k │ cb-payments-v2-2 · web · 12k tokens          │
│   ● api  wire the new Aft… 41k │ in 1.2k · out 8.4k · cache r 2.1k · w 0.3k   │
│ ▸ ! web  hook PaymentForm… 12k │ ! needs you — permission prompt: Bash npm t… │
│ q4-search                  12k │                                              │
│   ✓ api  plan the search…  12k │ you: hook PaymentForm into checkout          │
│ quick                        0 │   › Edit  CheckoutPage.vue                   │
│   ○ scratch  new session       │   › Bash  npm test                           │
│ Other sessions                 │ claude: Wired it in. Running the tests…      │
│   ● arbybot-8d                 │                                              │
├────────────────────────────────┴──────────────────────────────────────────────┤
│ ↑↓ move  enter open  tab next waiting  n new  s quick  ? help  q quit         │
└───────────────────────────────────────────────────────────────────────────────┘
```

(Illustrative layout; exact widths are set during implementation.)

- **Header:** counts by state and the token total, both for claudeboard's own sessions. "Other sessions" are not included. When the last successful status check is more than 5 s old, the header shows `status stale (12s)`.
- **Left pane:** workflows in creation order, each followed by its sessions, then "Other sessions".
  - **Workflow rows** show the name, a `!` when any of its sessions needs the user, and the workflow's token total.
  - **Session rows** show the status icon, repo, label and tokens. The label is the first ~40 characters of the first prompt, or `new session`.
  - **Order never changes because of status,** so rows don't move under the cursor.
- **Right pane (preview):** the session name, repo (and worktree), token total and breakdown, and subagent count. If the session needs the user, it then shows what it is waiting on, including the pending tool call. Below that are the most recent preview items, following new output. PgUp/PgDn scroll. Prompts and replies are labelled in colour, with wrapped lines indented under the label; replies get light markdown (bold, inline code, headings, bullets, code blocks). Each tool call is one dimmed line (`› Bash  Run the tests`), a run of identical calls is shown once with a count, and the waiting-on line shows the command itself, since that is what a permission prompt asks about.
- **Narrow terminals:** below 90 columns the preview is hidden by default; `p` toggles it.
- **Token format:** `950`, `41k`, `1.24M`.

### Keys

| Key | Action |
|---|---|
| ↑↓ / j k | move |
| Enter | open the selected session (see below); on a workflow row, collapse/expand it |
| Tab | jump to the next session that needs the user, in list order |
| n | new session |
| N | new session at once, in a new workflow with a random name |
| s | quick one-off |
| x | stop session (keeps conversation and worktree) |
| r | resume a stopped session |
| D | delete session and worktree, after a `y/N` confirmation |
| m | move an Other session into a workflow (see below) |
| a | archive / unarchive the selected workflow (hides it; does not stop its sessions) |
| A | show / hide archived workflows |
| p | show / hide preview |
| PgUp / PgDn | scroll preview |
| Ctrl+L | redraw the screen |
| ? | help |
| q | quit (sessions keep running) |

`x` and `D` work on any background session, including ones in "Other sessions". `r` applies only to claudeboard's own sessions. On an interactive session, all three show "started in another terminal; switch to it there".

Enter on a session:
- **Running background session** (listed in any state other than `stopped`, `failed`, `crashed` or `exited`; resuming one that is running would start a copy), claudeboard's own or under Other sessions: opens it with `claude attach`. Ctrl+Z inside the session returns to the board, with the same row selected and the data refreshed. Before the board takes the terminal back it turns off the modes `claude attach` uses that the board doesn't set again itself (synchronized output, mouse, focus and colour-scheme reporting, scroll region, attributes, character set), in case attach didn't exit cleanly. The same applies after the trust-prompt `claude`.
- **Background session that isn't running** (`stopped`, `failed`, `crashed` or `exited`, or a claudeboard session Claude no longer lists), claudeboard's own or under Other sessions: resumes it with `claude --bg --resume <sessionId>` in its folder, with no other options, so Claude continues it under the same ID, then opens the short ID the resume prints. The board never attaches to such a session directly: `claude attach` could start the background service, which would inherit its `DISABLE_TELEMETRY=1` (section 4, Gotchas). Until Claude has listed sessions once, every row looks stopped, so Enter says it is still waiting instead.
- **Other interactive session:** says it runs in another terminal, and that `m` continues it here.

`m` on an Other session opens the workflow picker (including "+ new workflow…"), then:
- **Interactive session:** claudeboard starts `claude --bg --resume <sessionId> --name cb-<slug>-<n>` in its folder, records the copy in the workflow and opens it. The original stays open in its terminal until the user closes it; the footer says so. A session with no conversation yet has nothing to continue, so `m` says that instead.
- **Background session:** recorded in the workflow as it is, keeping its ID and name (or its short ID if it has no name). Nothing is restarted.

### New session (`n`)

1. **Workflow:** defaults to the selected workflow. You can also pick another or type a new name.
2. **Repo:** defaults to the workflow's `last_repo`, otherwise the folder the board was started from. The choices are the configured repos, that folder, and the scratch folder.
3. **Worktree:** asked only if another claudeboard session that isn't stopped already uses the same `dir`: "`api` already has a session, use a worktree? y/N".
4. claudeboard launches with no prompt, records the session, and opens it straight away. The user types the prompt in Claude. Model and permission mode come from config.

`s` skips every question: workflow `quick`, the scratch folder, no worktree. It then opens immediately.

`N` skips the pickers: a new workflow with a random two-word name that no workflow uses yet (like `amber-otter`), in the selected row's folder: a session's repo, a workflow's last folder, or an Other session's folder; otherwise the folder the board was started in. It asks the same worktree question as `n` if a session is already running there, then opens the new session.

If a launch fails because the folder isn't trusted, the board offers to open an interactive `claude` there so the user can accept the trust prompt, then retries the launch.

## 8. CLI

```
claudeboard                                   open the board
claudeboard new [-w workflow] [-r repo|path] [--worktree] [prompt…]
claudeboard --version
```

`new` follows the same rules as the board. `-r` takes a configured repo name or a path, and defaults to the current folder. `-w` defaults to `quick`. `--worktree` is explicit; the CLI never asks. Without a prompt, it launches the session and attaches in the current terminal, and Ctrl+Z leaves the session running. With a prompt, it launches in the background, prints `started cb-<slug>-<n> (<shortId>)` and exits.

## 9. Error handling

The board never crashes on bad data; problems appear in a one-line status message.

- **Startup:** if `claude` is missing, or `claude agents --json` fails, the board exits with a message saying what is missing. If `claude --version` is below 2.1.312, it shows a warning but still runs.
- **Status check fails or times out:** the last known state is kept, and the header shows `status stale (Ns)`.
- **Launch, stop, delete or resume fails:** Claude's own message is shown. The untrusted-folder case gets the offer described in section 7.
- **Conversation files:** a missing file shows "no messages yet". Unrecognised or malformed lines are skipped.
- **`state.json` is corrupt:** recovery as in section 6.
- **Attach exits with a non-zero code:** the message is shown and the session list is refreshed.

## 10. Testing

No automated test runs the real `claude`.

- **claudecli:** `ParseAgents` and `ParseLaunch` against fixture outputs in `testdata/`. The fixtures use the shapes recorded in the spike, with made-up IDs and paths.
- **transcript:** made-up `.jsonl` fixtures covering:
  - several records sharing one `message.id`
  - `isMeta` records
  - pairing `tool_use` with `tool_result` (for the pending tool call)
  - an incomplete trailing line
  - a file that shrinks
  - subagent files
- **Status mapping:** a table test covering every row of the mapping in section 4.
- **Naming:** slug, session name, worktree name, and re-link parsing.
- **store:** load/save round trip, concurrent `Update` calls, and corrupt-file recovery.
- **config:** defaults, `~` expansion, and creating the example file.
- **board:** model tests against an in-memory data source built from the fixtures, plus a few reference screens (normal, needs-you, narrow). These render `View()`, strip the colours, and compare the result with saved files under `testdata/`.
- **GitHub Actions:** runs `go vet ./...` and `go test ./...` on every push to `main`.
- **README first-run checklist** for the real environment:
  - launch, attach and Ctrl+Z
  - stop, resume and delete
  - the quick one-off and its trust prompt
  - the PHPStorm terminal Esc setting
  - `ide = true` with PHPStorm

## 11. Installing

```
git clone https://github.com/Harris-Ferguson/claudeboard && cd claudeboard
go install ./cmd/claudeboard          # update later: git pull && go install ./cmd/claudeboard
```

This requires `$(go env GOPATH)/bin` on `PATH`, and GitHub access for the private repo.

In PHPStorm, Esc moves focus to the editor by default instead of reaching Claude. The README explains how to turn that off: Settings → Tools → Terminal, untick "Move focus to the editor with Escape".

## 12. Risks

- **Claude Code's `agents --json` and transcript formats are undocumented** and may change. They are confined to two packages, pinned by fixture tests, and the board warns on versions older than the one verified.
- **Unverified behaviour:** `--ide` from background sessions. It is a config flag, off by default, plus a checklist item.
- **The cost of running `claude agents --json` every 1.5 s** is unmeasured. It will be measured early, and the interval raised if needed.
