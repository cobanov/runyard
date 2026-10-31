# runyard

A lightweight, self-hosted training tracker. One Go binary plus PostgreSQL, started with `docker compose up`.
It shows each training run's status, metrics, samples and logs, sends phone notifications, and lets agents
read everything over MCP and a `--json` CLI. Far fewer features than W&B or MLflow, on purpose.

> Status: v0.1, used for our own training runs. APIs may still change.

## Run the server

```sh
git clone https://github.com/cobanov/runyard && cd runyard
docker compose up -d --build                          # http://localhost:8080
docker compose exec app /runyard-server key create my-laptop   # prints an API key (ry_...)
```

Reading needs no key, so the port is bound to `127.0.0.1` by default. Reach it from elsewhere through
`tailscale serve --bg --https=443 http://127.0.0.1:8080` or a tunnel, or set `RUNYARD_BIND=0.0.0.0:8080`.
Set `RUNYARD_URL` (in `.env`) to the public address so notifications link to the run.
Backup: `docker compose exec db pg_dump -U runyard runyard` plus the `media` volume.

## Log a run

```sh
pip install "runyard[images,system] @ git+https://github.com/cobanov/runyard#subdirectory=sdk/python"
export RUNYARD_SERVER=https://my-server RUNYARD_API_KEY=ry_...
```

```python
import runyard as ry

run = ry.init(project="my-project", name="lr3e-4", config=vars(args))
run.log({"loss": loss.item(), "lr": sched.get_last_lr()[0]}, step=step)
run.image("samples", grid, step=step)       # PIL image, numpy array, torch tensor or path; also a list
run.text("generations", [{"prompt": p, "output": o}], step=step)
run.alert("loss is NaN", level="error")     # also notifies the phone
```

The library never stops training: calls are queued and sent from a background thread, and what could not be
sent is saved at exit for `runyard sync`. stdout/stderr, GPU/CPU usage and the git commit are recorded.
A clean exit marks the run finished; an exception or non-zero exit, failed; 5 minutes without a heartbeat, crashed.

Frameworks, with one line:

```python
from runyard.integrations.hf import RunyardCallback            # transformers Trainer, TRL, Unsloth
trainer = SFTTrainer(..., callbacks=[RunyardCallback(project="sft")])

from runyard.integrations.accelerate import RunyardTracker     # Accelerate
accelerator = Accelerator(log_with=[RunyardTracker(project="unet")])
```

Tools that only speak Weights & Biases (ai-toolkit, diffusers examples, `report_to="wandb"`), without changing
their code: install runyard into their environment and set `RUNYARD_WANDB=1`. `import wandb` then loads a small
bridge that writes to runyard (`init`, `log`, `config`, `summary`, `define_metric`, `Image`, `Video`, `Table`,
`alert`, `finish`); anything else prints a warning and is skipped.

## Read runs

- Web UI: runs by project, a run page with charts, samples, logs, config and system, and run comparison. Live.
- CLI: `runyard runs ls`, `runyard runs show RUN`, `runyard logs RUN -f`, `runyard samples RUN`; `--json` everywhere.
- MCP for agents, at `/mcp`: `list_runs`, `get_run`, `get_metrics`, `get_samples` (returns the images),
  `tail_logs`, `compare_runs`, `list_alerts`, plus `add_note` and `add_tags` with an API key.
  `claude mcp add --transport http runyard https://my-server/mcp --header "Authorization: Bearer $RUNYARD_API_KEY"`.
  [SKILL.md](SKILL.md) tells an agent how to use it.

## Notifications

```sh
NTFY_TOKEN=tk_... docker compose exec -e NTFY_TOKEN app /runyard-server channel add ntfy https://ntfy.sh/my-topic
docker compose exec app /runyard-server channel add -project sft -events failed,crashed webhook https://example.com/hook
docker compose exec app /runyard-server channel test 1
```

Events: `finished`, `failed`, `crashed`, `alert`. ntfy messages carry the last metrics, the latest sample image
and a link to the run.

## Layout

| Path | What |
|---|---|
| `server/` | Go server: REST API, SSE, MCP, notifications, embedded web UI |
| `web/` | Svelte + Vite web UI (built into `server/webdist/dist`) |
| `sdk/python/` | Python client library, CLI and framework adapters |

## Development

```sh
make server     # builds the UI, then server/runyard-server with it embedded
make test       # starts PostgreSQL in Docker (:55432), then go vet + go test and pytest against it
```

## License

Apache-2.0
