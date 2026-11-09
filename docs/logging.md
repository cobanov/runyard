# Log runs

## The Python library

```sh
pip install "runyard[images,system]"   # extras: images, system, video, tensorboard
export RUNYARD_SERVER=https://my-server RUNYARD_API_KEY=ry_...
```

```python
import runyard as ry

run = ry.init(project="sft", name="lr3e-4", config=vars(args), tags=["qwen"], notes="first try")
run.log({"loss": loss.item(), "lr": sched.get_last_lr()[0]}, step=step)
run.image("samples", grid, step=step, caption="seed 0")   # PIL image, numpy array, torch tensor, path; or a list
run.video("rollout", frames, fps=8)                         # frames become an H.264 mp4 (`video` extra), else a GIF
run.text("generations", [{"prompt": p, "output": o}], step=step)
run.artifact("out/checkpoint-500", kind="checkpoint", step=500)  # where it is; upload=True also sends a copy
run.alert("loss is NaN", level="error")                     # info, warn or error; also notifies
run.config.update(batch=64)
run.summary["best_eval"] = 0.31
run.finish()                                                # optional
```

The same calls exist as module functions (`ry.log(...)`) for the current run.

- **Steps:** without `step`, each `log` is the next step after the last one.
- **Status:** a clean exit marks the run finished; an exception or non-zero exit, failed; five minutes without a
  heartbeat (sent every 30 seconds), crashed.
- **Recorded on its own:** stdout and stderr (`capture_output=False` turns it off), GPU, CPU and RAM usage as
  `sys/` metrics (`system` extra, `system=False` turns it off), the host, the git commit and uncommitted diff,
  the command line, the Python version, the OS and the hardware. No environment variables. A config key or a
  command-line flag whose name ends in `key`, `token`, `secret` or `password` is recorded as `***`.
- **Never in the way:** calls are queued and sent by a background thread; `log()` costs a few microseconds.
  While the server is unreachable the queue is also written to `~/.cache/runyard/pending` (`RUNYARD_CACHE`)
  every 30 seconds, so a killed process loses seconds, not the run. The next run on the machine sends what
  earlier ones left, and `runyard sync` does it by hand (`--server URL` to send it somewhere else).
- **Artifacts:** by default only the record is kept (this host and the path, size, step). `upload=True` sends a
  copy, a directory as one `.tar.gz`, streamed from disk. `runyard artifacts RUN --get NAME` downloads it.
- **Settings:** `RUNYARD_SERVER`, `RUNYARD_API_KEY`, `RUNYARD_PROJECT` and `RUNYARD_RUN_NAME` fill in what
  `init` is not given.

## Frameworks, with one line

| Framework | One line |
|---|---|
| transformers `Trainer`, TRL, Unsloth | `callbacks=[runyard.integrations.hf.RunyardCallback(project="sft")]` (records each saved checkpoint too) |
| Accelerate | `Accelerator(log_with=[runyard.integrations.accelerate.RunyardTracker(project="unet")])` |
| Lightning | `Trainer(logger=runyard.integrations.lightning.RunyardLogger(project="mnist"))` |
| Keras 3 | `model.fit(..., callbacks=[runyard.integrations.keras.RunyardCallback(project="mnist")])` |
| XGBoost | `xgb.train(..., callbacks=[runyard.integrations.xgboost.RunyardCallback(project="tabular")])` |
| LightGBM | `lgb.train(..., callbacks=[runyard.integrations.lightgbm.runyard_callback(project="tabular")])` |
| scikit-learn | `runyard.integrations.sklearn.autolog(project="tabular")`: every outer `fit()` is a run |
| torchtune | `metric_logger: {_component_: runyard.integrations.torchtune.RunyardLogger, project: sft}` |

Other keyword arguments go to `runyard.init`. On macOS, xgboost and lightgbm need `brew install libomp` and must
be imported before torch.

## Tools you cannot change

**Weights & Biases.** For tools that only speak W&B, install runyard into their environment and set
`RUNYARD_SERVER` and `RUNYARD_API_KEY`. `import wandb` then loads a small bridge that writes to runyard:

- no `wandb` package installed: the bridge is on by itself (the import would fail otherwise);
- `wandb` installed too: `RUNYARD_WANDB=1` makes the bridge win;
- `RUNYARD_WANDB=0`: never.

It covers `init` (also as `with wandb.init() as run:`), `log`, `config`, `summary`, `define_metric`, `Image`,
`Video`, `Table`, `Artifact` with `log_artifact` and `save` (recorded where the files are, not uploaded),
`alert`, `finish`, `run.name` and `run.tags`; `mode="disabled"`, `WANDB_MODE=disabled` and `WANDB_DISABLED` make
no run, as in wandb; `WANDB_PROJECT` and `WANDB_NAME` are read. Anything else prints one warning and is skipped.
In code you can change, `import runyard.wandb as wandb` is the same module without the environment's help.

| Tool | Checked with | Besides the two settings |
|---|---|---|
| [ai-toolkit](https://github.com/ostris/ai-toolkit) | its own `toolkit/logging_aitk.py`: loss, learning rate, one sample key per prompt | `logging: { use_wandb: true, project_name: my-lora }` in the job's YAML |
| kohya-ss sd-scripts | Accelerate's wandb tracker and the sample-image calls it makes | `--log_with wandb` (its `--wandb_api_key` is not needed and is masked) |
| transformers `Trainer`, TRL, Unsloth | transformers' own `WandbCallback` | `report_to="wandb"`, `WANDB_PROJECT` for the project |
| diffusers example scripts | `train_unconditional.py` | `--report_to wandb` (`--logger wandb` on older scripts) |

**TensorBoard.** For tools that write event files (kohya sd-scripts, DeepSpeed, `SummaryWriter`):

```sh
pip install "runyard[tensorboard]"
runyard watch ./logs --project lora          # new events every 10 s until Ctrl-C
runyard watch ./old-logs --once              # import finished trainings and exit
```

Each directory with event files becomes a run named by its path, with its scalars, images, text and hparams.
A restarted watcher continues the same runs.

**MLflow.** The server speaks enough of MLflow's tracking API for MLflow's own client:

```sh
export MLFLOW_TRACKING_URI=https://my-server MLFLOW_TRACKING_TOKEN=ry_...
```

`mlflow.start_run`, `log_param`/`log_metric`/`set_tag`, `log_artifact`, `mlflow.autolog()` with its logged models
(loadable again through `models:/<id>`), `report_to="mlflow"` and Lightning's `MLFlowLogger` work. Experiments are
projects, params go to the config, artifacts to the run's artifacts. MLflow clients send no heartbeats, so their
runs count as crashed only after an hour without writes. Searches ignore filters and ordering (newest first).
