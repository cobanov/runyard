# runyard

A lightweight, self-hosted training tracker. One Go binary plus PostgreSQL, started with `docker compose up`.
It shows each training run's status, metrics, samples and logs, sends phone notifications, and lets agents
read everything over MCP and a `--json` CLI. Far fewer features than W&B or MLflow, on purpose.

> Status: used for our own training runs; not released yet, APIs may still change.
> Documentation: [cobanov.github.io/runyard](https://cobanov.github.io/runyard) (also in [`docs/`](docs/)).

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
Media and artifacts can live in an S3-compatible bucket (Cloudflare R2, S3, MinIO) instead: set `S3_BUCKET`,
`S3_ENDPOINT`, `S3_ACCESS_KEY_ID` and `S3_SECRET_ACCESS_KEY` (see `docker-compose.yml`).

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
run.video("rollout", frames, fps=8)         # frames: an H.264 mp4 with the `video` extra, else a GIF; or a path
run.text("generations", [{"prompt": p, "output": o}], step=step)
run.alert("loss is NaN", level="error")     # also notifies the phone
run.artifact("out/checkpoint-500", kind="checkpoint", step=500)   # records where it is; upload=True sends a copy
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

from runyard.integrations.lightning import RunyardLogger       # Lightning (lightning.pytorch or pytorch_lightning)
trainer = Trainer(logger=RunyardLogger(project="mnist"))

from runyard.integrations.keras import RunyardCallback         # Keras 3
model.fit(x, y, validation_split=0.1, callbacks=[RunyardCallback(project="mnist")])

from runyard.integrations.xgboost import RunyardCallback       # XGBoost, also XGBClassifier(callbacks=[...])
xgb.train(params, dtrain, evals=[(dval, "val")], callbacks=[RunyardCallback(project="tabular")])

from runyard.integrations.lightgbm import runyard_callback     # LightGBM
lgb.train(params, dtrain, valid_sets=[dval], callbacks=[runyard_callback(project="tabular")])

from runyard.integrations.sklearn import autolog               # scikit-learn: every fit() becomes a run
autolog(project="tabular")
```

```yaml
metric_logger:                                                  # torchtune, in the recipe config
  _component_: runyard.integrations.torchtune.RunyardLogger
  project: sft
```

Tools that only speak Weights & Biases (ai-toolkit, diffusers examples, `report_to="wandb"`), without changing
their code: install runyard into their environment and set `RUNYARD_WANDB=1`. `import wandb` then loads a small
bridge that writes to runyard (`init`, `log`, `config`, `summary`, `define_metric`, `Image`, `Video`, `Table`,
`alert`, `finish`); anything else prints a warning and is skipped.

TensorBoard: tools that only write event files (kohya sd-scripts `--log_with tensorboard`, DeepSpeed, diffusers
`--report_to tensorboard`, torch `SummaryWriter`) need no change either. Install runyard with the `tensorboard` extra
and run `runyard watch ./logs`. Each directory with event files becomes a run named by its path; scalars, images, text
and hparams are sent, new events every 10 seconds until Ctrl-C. `--once` imports finished trainings; a restarted
watcher continues the same runs.

MLflow: the server speaks enough of MLflow's tracking API for MLflow's own client. Point it at runyard and
`mlflow.start_run`, `log_param`/`log_metric`/`set_tag`, `log_artifact`, `mlflow.autolog()` (models included),
`report_to="mlflow"` and Lightning's `MLFlowLogger` write runs here: experiments are projects, params the config.

```sh
export MLFLOW_TRACKING_URI=https://my-server MLFLOW_TRACKING_TOKEN=ry_...
```

## Read runs

- Web UI: runs by project, a run page with charts, samples, logs, config, system and artifacts, and run
  comparison. Live. Installs as an app on the phone; the bell turns on push notifications for that device.
- CLI: `runyard runs ls`, `runyard runs show RUN`, `runyard logs RUN -f`, `runyard samples RUN`,
  `runyard artifacts RUN [--get NAME]`; `--json` everywhere.
- MCP for agents, at `/mcp`: `list_runs`, `get_run`, `get_metrics`, `get_samples` (returns the images),
  `tail_logs`, `compare_runs`, `list_alerts`, `list_artifacts`, plus `add_note` and `add_tags` with an API key.
  `claude mcp add --transport http runyard https://my-server/mcp --header "Authorization: Bearer $RUNYARD_API_KEY"`.
  [SKILL.md](SKILL.md) tells an agent how to use it.

## Notifications

```sh
NTFY_TOKEN=tk_... docker compose exec -e NTFY_TOKEN app /runyard-server channel add ntfy https://ntfy.sh/my-topic
docker compose exec app /runyard-server channel add -project sft -events failed,crashed webhook https://example.com/hook
docker compose exec app /runyard-server channel add slack https://hooks.slack.com/services/...
TELEGRAM_TOKEN=123:abc docker compose exec -e TELEGRAM_TOKEN app /runyard-server channel add telegram CHAT_ID
docker compose exec app /runyard-server channel test 1
```

Kinds: `ntfy`, `webhook`, `slack`, `discord`, `telegram`; the web UI's bell adds a web push subscription
(over HTTPS, or an app installed to the home screen on iOS). Events: `finished`, `failed`, `crashed`, `alert`.
Messages carry the last metrics, the latest sample image where the channel allows it, and a link to the run.

Checks raise alerts from the data itself, once per run, through the channels listening to `alert`. One comes
built in: any `*loss*` metric going NaN or ±Inf. Add your own per project:

```sh
docker compose exec app /runyard-server check add -project sft -after 100 above train/loss 10   # diverged, after warmup
docker compose exec app /runyard-server check add -project sft below eval/accuracy 0.2
docker compose exec app /runyard-server check add -project gba stall 30m   # heartbeats, but no metric or output: stuck
docker compose exec app /runyard-server check ls
```

`*` in a metric name matches anything. Data older than five minutes (a `runyard sync` of an old run) is not checked.

## Users and teams

By default reads are open and the API key is for writing. For a shared server set `RUNYARD_AUTH=1`: then every
read needs a login (web UI) or an API key, and teams keep their projects to themselves.

```sh
docker compose exec app /runyard-server user add -admin mert        # prints a generated password
docker compose exec app /runyard-server user add ege
docker compose exec app /runyard-server team add vision
docker compose exec app /runyard-server team join vision ege
docker compose exec app /runyard-server project team sft vision     # only vision (and admins) see sft
docker compose exec app /runyard-server key create -user ege ege-laptop
```

A project without a team is seen by every user; a user's new project goes to their first team. A key made
without `-user` acts as the server and sees everything.

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
