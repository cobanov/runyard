# runyard

A lightweight, self-hosted training tracker: one Go binary plus PostgreSQL. It shows each training run's
status, metrics, samples, logs and checkpoints, tells your phone when a run finishes, fails or goes silent, and
lets agents read everything over MCP and a `--json` CLI. Far fewer features than W&B or MLflow, on purpose.

## Five minutes

```sh
git clone https://github.com/cobanov/runyard && cd runyard
docker compose up -d --build                                    # http://localhost:8080
docker compose exec app /runyard-server key create my-laptop     # prints an API key (ry_...)
```

```sh
pip install "runyard[images,system] @ git+https://github.com/cobanov/runyard#subdirectory=sdk/python"
export RUNYARD_SERVER=http://localhost:8080 RUNYARD_API_KEY=ry_...
```

```python
import runyard as ry

run = ry.init(project="my-project", name="lr3e-4", config={"lr": 3e-4})
for step in range(1000):
    run.log({"loss": 1 / (step + 1)}, step=step)
```

Open the web UI: the run is there, live. A clean exit marks it finished, an exception failed, and five minutes
without a heartbeat crashed.

## What it does

- **Logging** from Python in three lines, or with no code change at all from tools that speak Weights & Biases,
  TensorBoard or MLflow. [Log runs](logging.md)
- **Reading** in a phone-first web UI, a CLI and an MCP server for agents. [Read runs](reading.md)
- **Notifications** to ntfy, Slack, Discord, Telegram, a webhook or the installed web app, plus checks that raise
  alerts from the data: a NaN loss, a crossed threshold, a stuck run. [Notifications](notifications.md)
- **Self-hosted**: PostgreSQL, a media directory or an S3/R2 bucket, optional users and teams. [Run the server](server.md)

The library never stops training: calls are queued and sent from a background thread, and what could not be sent
is saved at exit for `runyard sync`.
