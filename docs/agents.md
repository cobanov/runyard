# Coding agents

When Claude Code, Codex, OpenCode or Gemini CLI start your training runs, runyard gives each agent session a card:
what it is doing, its runs, the project's main metric over those runs and the best so far, and what the agent last
said it found. The kiosk page shows the cards full screen, for a TV, a monitor or a phone on a stand.

## The card, with no setup

A run started from inside a coding agent's session lands on that session's card on its own. Each CLI puts its
session id in the environment of every command it runs, and the Python library reads it when the run starts:

| CLI | What the library reads |
|---|---|
| Claude Code | `CLAUDE_CODE_SESSION_ID` |
| Codex | `CODEX_SESSION_ID` |
| OpenCode | `OPENCODE_SESSION_ID` |
| Gemini CLI | `GEMINI_CLI` (no session id: one card per machine and directory) |
| anything else | `AI_AGENT`, the variable CLIs are starting to agree on |

`RUNYARD_AGENT=<name>` names the card by hand: every run with the same name shares one card, across sessions. Use
it for a long sweep you want to watch as one, or to split Gemini CLI's sessions.

Without hooks a card knows what its runs know: **training** while one runs, **done** for 30 minutes after one
finishes, **error** when the last one failed or crashed, **idle** otherwise, and **quiet** after two hours with
nothing new.

## The project's main metric

The big number on a card is the project's main metric: the last run's final value and ★ the best so far, with a line
across the runs. Pick it on the project's page ("Judged by"), with the MCP tool `set_metric`, or with
`PATCH /api/v1/projects/{name}` `{"metric": "eval/acc", "goal": "max"}`. Until then a card shows a loss. The score
always comes from the runs, never from what the agent says.

## Reports

The MCP tool `report` puts a sentence on the agent's card: what it tried and what came out of it, and what it will
try next. Codex and OpenCode say which session they are with every tool call; Claude Code does not, so it passes the
run the report is about (`run`), or its session (`session: claude:$CLAUDE_CODE_SESSION_ID`).

Put this in the repository's `AGENTS.md` or `CLAUDE.md` so the agent does it:

```markdown
## Training runs

Start training runs with the runyard library (`run = runyard.init(project=..., name=..., config=...)`); give each
run a name that says what it tries. After each run, call the runyard MCP tool `report` with what you tried, what
came out (the numbers), and what you will try next, and pass the run's name as `run`. Tag the runs worth keeping
`keep` with `add_tags`, and the dead ends `discard`.
```

A card counts the runs tagged `keep`.

## Hooks: waiting for you, working, idle

Hooks add what runs cannot show: an agent **waiting for you** (it asked for a permission and is stuck until someone
answers), **working** (mid-turn), and **idle** (the turn ended). A card waiting for you sorts first. A run starting
ends the wait, because the approved command is often the training itself. Any other long command shows waiting until
it exits, as no hook says the prompt was answered sooner. A wait with nothing new for two hours goes quiet: the CLI
was most likely killed at its prompt.

```sh
runyard hooks install claude --project   # this repository only: .claude/settings.local.json
runyard hooks install claude             # every session of this user: ~/.claude/settings.json
runyard hooks install codex              # every session of this user: ~/.codex/hooks.json
runyard hooks install claude --remove
```

Prefer `--project`, run in the repository you train from: no other session sends the server anything, and the card
starts at the first prompt, so the permission prompt before a first run shows. The file stays out of git.

They post each event to `RUNYARD_SERVER`'s `/api/v1/agents/events` with the key in `RUNYARD_API_KEY`, which must be
set in the shell you start the agent from; the key is read from the environment and never written to the file.
Claude Code's are `http` hooks; Codex's run `curl`. A hook that fails never holds the agent up. Installing twice
changes nothing, other hooks are left as they are, and the previous file is kept next to it as `.bak`. With hooks,
the card also shows the agent's last message when it has written no report. While the server is down Claude Code
prints a hook error line for each event and carries on.

Hooks installed per user hear from sessions that have nothing to do with training. Those get no card: it starts with
a session's first run or report, and until then its last message is not kept. That also means a permission prompt
before a session's first run does not show. What a hook posts includes the tool's input and output; the server reads
five fields and drops the rest, but it does travel, so use `https` or a private network for `RUNYARD_SERVER`.

OpenCode and Gemini CLI have no `hooks install` yet; their cards work from runs and reports.

## Codex's sandbox

Codex runs commands in a sandbox with the network off, so a run it starts cannot reach the server, and
`~/.cache/runyard` is not writable. The library says so when the run starts. To let runs through, in
`~/.codex/config.toml`:

```toml
[sandbox_workspace_write]
network_access = true
```

and set `RUNYARD_CACHE` to a directory inside the workspace (add it to `.gitignore`). For the MCP tools to run
without asking each time, add `default_tools_approval_mode = "approve"` under `[mcp_servers.runyard]`.

## The kiosk

`/#/kiosk` on the server, Agents in the menu: the cards with no menu, most urgent first (waiting for you, error, done, training and
working, idle, then quiet and ended, dimmed). Everything is in the address:

| Parameter | |
|---|---|
| `project=<name>` | only this project's agents |
| `hours=24` | how far back (default a day) |
| `theme=light` | dark by default, kinder to an always-on screen |

The page keeps the screen awake, reloads every minute even when the live stream has quietly dropped (phones do
that), goes grey when it has not heard from the server for three minutes, and shifts by a pixel every few minutes
against burn-in. A card turning done or waiting pulses five times, then stays still. With `RUNYARD_AUTH` on, log in
once on that screen.

## API

`GET /api/v1/agents?project=&hours=` lists the cards; the MCP tool `list_agents` returns the same. A run's `agent`
field is its session, `<tool>:<session id>`.
