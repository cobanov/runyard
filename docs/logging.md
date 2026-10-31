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
  `sys/` metrics (`system` extra, `system=False` turns it off), the host and the git commit.
- **Never in the way:** calls are queued and sent by a background thread. When the server is unreachable,
  what was not sent is saved at exit under `~/.cache/runyard/pending` (`RUNYARD_CACHE`); `runyard sync` sends it.
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

**Weights & Biases.** For tools that only speak W&B (ai-toolkit, diffusers examples, `report_to="wandb"`): install
runyard into their environment and set `RUNYARD_WANDB=1`. `import wandb` then loads a small bridge that writes to
runyard: `init`, `log`, `config`, `summary`, `define_metric`, `Image`, `Video`, `Table`, `alert`, `finish`.
Anything else prints a warning and is skipped.

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
