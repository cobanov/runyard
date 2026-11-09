# runyard (Python client)

See the [main README](https://github.com/cobanov/runyard).

Optional extras: `images` (numpy, Pillow), `system` (GPU, CPU and RAM usage), `video` (imageio-ffmpeg: frames
become an H.264 mp4 instead of a GIF) and `tensorboard` (for `runyard watch DIR`, which sends TensorBoard event
files: one run per directory, `--once` to import and exit). `runyard --help` lists the commands.

With `RUNYARD_SERVER` and `RUNYARD_API_KEY` set and no `wandb` package installed, `import wandb` is runyard's
bridge, so tools that log to Weights & Biases (ai-toolkit, kohya sd-scripts, `report_to="wandb"`) write to
runyard unchanged; `RUNYARD_WANDB=1` makes the bridge win over an installed wandb, `RUNYARD_WANDB=0` turns it off,
and `import runyard.wandb as wandb` is the same module by name.

## Framework adapters

`runyard.integrations.<module>` imports its framework, which you install yourself. Keyword arguments other than
`project` and `name` go to `runyard.init` (`tags`, `notes`, `system`, ...).

| Module | One line | Run, config, metrics |
|---|---|---|
| `hf` | `Trainer(..., callbacks=[RunyardCallback()])` | per training; TrainingArguments and model config; `train/`, `eval/` by global step |
| `accelerate` | `Accelerator(log_with=[RunyardTracker(project=...)])` | per `init_trackers`; its config; `accelerator.log()` |
| `lightning` | `Trainer(logger=RunyardLogger(project=...))` | per Trainer call; hyperparameters; logged metrics by global step |
| `keras` | `model.fit(..., callbacks=[RunyardCallback(project=...)])` | per fit; fit params and parameter count; `train/`, `val/` by epoch, `batch/` with `log_every_n_batches` |
| `xgboost` | `xgb.train(..., callbacks=[RunyardCallback(project=...)])` | per training; booster training params; `<eval name>/<metric>` by iteration |
| `lightgbm` | `lgb.train(..., callbacks=[runyard_callback(project=...)])` | per training; params; `<valid name>/<metric>` by iteration |
| `sklearn` | `autolog(project=...)`; `autolog(disable=True)` undoes it | per outer `fit()`, named after the class; `get_params(deep=False)`; `fit_seconds`, `train/score` |
| `torchtune` | `metric_logger: {_component_: runyard.integrations.torchtune.RunyardLogger, project: ...}` | per recipe; resolved recipe config; the recipe's metrics by step |

On macOS, xgboost and lightgbm need `brew install libomp`, and they crash if `torch` was imported first: import them
before torch.
