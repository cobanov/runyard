# Read runs

## Web UI

Projects, a project's runs (search, filter by status, sort by any column), and a run page with charts (smoothing,
log scale), samples (images, videos, text tables), logs, config, system usage, alerts and artifacts; its notes and
tags are edited in place. Pick runs to compare them side by side. Machines shows every machine running
[`runyard beacon`](server.md#machines): its GPUs, the processes on them and the runs it trains. Alerts can be
dismissed. `/#/kiosk` shows the cards of the coding agents that started runs, full screen ([Coding agents](agents.md)).
Everything updates live.
It is made for the phone first: add it to the home screen to install it, and tap the bell to get push
notifications on that device.

## CLI

```sh
runyard runs ls [--project P] [--status S]
runyard runs show RUN            # RUN: id, id prefix or run name
runyard logs RUN [-n 100] [-f]
runyard samples RUN [--out DIR]  # downloads the latest samples
runyard artifacts RUN [--get NAME] [--out DIR]
runyard sync                     # sends runs saved while the server was unreachable
runyard watch DIR [--project P] [--once] [--interval 10]
runyard beacon [--name NAME]     # this machine's GPUs, CPU, memory and disk, for Machines
runyard hooks install claude|codex|gemini|opencode [--remove]   # agent hooks, see Coding agents
```

Every read command takes `--json` for scripts and agents. With `RUNYARD_AUTH` on, set `RUNYARD_API_KEY` for reads too.

## MCP for agents

The server is an MCP server at `/mcp`:

```sh
claude mcp add --transport http runyard https://my-server/mcp --header "Authorization: Bearer $RUNYARD_API_KEY"
```

The web UI's Settings page shows this command with the server's address and a key filled in.

| Tool | What it returns |
|---|---|
| `list_projects`, `list_runs` | projects with run counts; runs with status, config and summary |
| `get_run` | status, timing, host, git, config, summary, and per metric its count, last, min and max |
| `get_metrics` | curves summarized as a short `[step, value]` series per key |
| `get_samples` | the sample images, as images the agent can look at, and text samples |
| `tail_logs` | the last lines the run printed (stderr lines start with `!`) |
| `compare_runs` | runs side by side: the config keys that differ, last/min of shared metrics |
| `list_alerts`, `list_artifacts` | alerts raised by the run or by checks; checkpoints and models |
| `list_machines` | each machine's GPUs (utilization, memory, the processes on them), CPU, memory and disk, and its runs |
| `add_note`, `add_tags` | write a verdict or mark a run; need the API key |
| `list_agents` | the coding agent sessions that started runs: what each is doing, its runs, its last report |
| `report`, `set_metric` | an agent's report on its card; a project's main metric; need the API key |

[`SKILL.md`](https://github.com/cobanov/runyard/blob/main/SKILL.md) tells an agent how to use them.

## REST API

Reads are `GET` under `/api/v1`: `projects`, `runs?project=&status=&tag=&q=&limit=&before=&sort=ended&metrics=1`
(`q`: part of a name or tag; `before`: the `started_at` of the last run you have, for the next page; `sort=ended`:
newest end first; `metrics=1`: each run's last, min and max per metric), `runs/{id}` (also an id prefix or
name; it carries `env`, what the run started as, and `progress_at`, its last metric or output line),
`runs/{id}/metrics?keys=&points=` (per metric `step`, `last`, `min`, `max` and `t`, the unix time of each point),
`runs/{id}/logs?after=&before=&tail`, `runs/{id}/media?key=&kind=&step=` (an image's `url` takes `?w=320|640|1280`
for a smaller JPEG),
`runs/{id}/artifacts`, `artifacts/{id}/file`, `alerts?run=&project=&active` (active: not dismissed), `hosts`,
`hosts/{name}/history?hours=`, `agents?project=&hours=` and `events` (server-sent events, one line per change). Writes need
`Authorization: Bearer <key>`: `POST runs`, `PATCH runs/{id}`, `POST hosts/{name}` (the beacon), `POST agents/events?tool=` (agent hooks), and `POST` of
`runs/{id}/metrics`, `logs`, `media`, `heartbeat`, `alerts` and `artifacts`. `PATCH runs/{id}/meta`
`{"notes", "tags"}` and `POST alerts/dismiss` `{"ids"}` (none: all) are what the web UI edits in place, and `PATCH projects/{name}` `{"metric", "goal"}`.

Responses are gzipped for clients that send `Accept-Encoding: gzip`.
