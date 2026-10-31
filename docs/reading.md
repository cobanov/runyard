# Read runs

## Web UI

Projects, a project's runs (filter by status), and a run page with charts, samples (images, videos, text tables),
logs, config, system usage, alerts and artifacts. Pick runs to compare them side by side. Everything updates live.
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
| `add_note`, `add_tags` | write a verdict or mark a run; need the API key |

[`SKILL.md`](https://github.com/cobanov/runyard/blob/main/SKILL.md) tells an agent how to use them.

## REST API

Reads are `GET` under `/api/v1`: `projects`, `runs?project=&status=&tag=`, `runs/{id}` (also an id prefix or
name), `runs/{id}/metrics?keys=&points=`, `runs/{id}/logs?after=&tail`, `runs/{id}/media?key=&kind=&step=`,
`runs/{id}/artifacts`, `artifacts/{id}/file`, `alerts?run=&project=`, and `events` (server-sent events, one line
per change). Writes need `Authorization: Bearer <key>`: `POST runs`, `PATCH runs/{id}`, and `POST` of
`runs/{id}/metrics`, `logs`, `media`, `heartbeat`, `alerts` and `artifacts`.
