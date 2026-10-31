---
name: runyard
description: Start, watch and judge model training runs tracked by a runyard server. Use when a task involves a training run (diffusion, LoRA, LLM fine-tuning, pretraining) whose metrics, logs or samples live in runyard, or when asked how a run is going, why it failed, or which run is best.
---

# runyard

runyard records each training run's status, metrics, output and sample images. Read it through the MCP server
(`/mcp` on the runyard server) or the `runyard` CLI; both work without an API key unless the server runs with
`RUNYARD_AUTH`, in which case they need it too. Writing (notes, tags) always needs the key.

Setup once:

```sh
claude mcp add --transport http runyard https://<runyard-host>/mcp --header "Authorization: Bearer $RUNYARD_API_KEY"
pip install runyard            # the CLI and the Python library
export RUNYARD_SERVER=https://<runyard-host> RUNYARD_API_KEY=ry_...
```

## Start a run

In a script you can change, three lines:

```python
import runyard as ry
run = ry.init(project="my-project", name="lr3e-4", config=vars(args))
run.log({"loss": loss.item(), "lr": sched.get_last_lr()[0]}, step=step)   # every N steps
run.image("samples", grid, step=step)        # PIL image, numpy array, torch tensor or path
run.alert("loss is NaN", level="error")      # also pings the phone
run.artifact(ckpt_dir, kind="checkpoint", step=step)   # where the checkpoint is; upload=True sends a copy
```

`run.finish()` is optional: a clean exit marks the run finished, an exception or non-zero exit marks it failed, and a
run that stops sending heartbeats for 5 minutes is marked crashed. The library never stops training: when the server is
down it queues, and `runyard sync` sends what was saved at exit.

Without touching the code:
- Hugging Face `Trainer` / TRL: `callbacks=[runyard.integrations.hf.RunyardCallback()]`, or `report_to="wandb"` with the bridge below.
- Accelerate: `Accelerator(log_with=[runyard.integrations.accelerate.RunyardTracker()])`.
- Lightning, Keras, XGBoost, LightGBM, scikit-learn, torchtune: one line each from `runyard.integrations` (see the README).
- Anything that supports Weights & Biases (ai-toolkit, diffusers examples): set `RUNYARD_WANDB=1`; `import wandb` then writes to runyard.
- Anything that writes TensorBoard event files (kohya, DeepSpeed): `runyard watch ./logs`.
- Anything that speaks MLflow: `MLFLOW_TRACKING_URI=https://<runyard-host> MLFLOW_TRACKING_TOKEN=$RUNYARD_API_KEY`.

## Watch and judge

1. `list_runs` (MCP) or `runyard runs ls`: what is running, what failed.
2. `get_run`: config, how long it ran, and last/min/max of every metric. Is the loss still going down?
3. `get_metrics` for the curve when the shape matters (plateau, divergence, spikes).
4. `get_samples`: look at the images yourself; numbers alone do not say whether a diffusion model is good.
5. `tail_logs` when a run failed or went silent: the traceback is in the last lines (stderr lines start with `!`).
   `list_alerts` shows what the server caught on its own: a NaN loss, a crossed threshold, or "No progress", which
   means heartbeats still arrive but the training loop has stopped logging (a hang, not a crash).
6. `compare_runs` to pick between runs: it shows only the config keys that differ.
7. `list_artifacts` (or `runyard artifacts RUN`) for the run's checkpoints and models: which step, where they are,
   and a download URL when a copy was uploaded (`runyard artifacts RUN --get NAME`).

Report what you saw, not just the numbers: "loss flat since step 30k, samples sharp but repetitive". Record a verdict
with `add_note`, and mark the keeper with `add_tags` (for example `best`).

If something is wrong mid-run (NaN loss, exploding grad norm, samples collapsed to noise), say so plainly and
suggest stopping it; do not kill a run yourself unless asked.
