"""Hugging Face Trainer (and TRL, Unsloth, which use it): Trainer(..., callbacks=[RunyardCallback()])."""
from transformers import TrainerCallback

from .. import init

# what Trainer logs once at the end; they go to the run's summary, as W&B does
SUMMARY = ("train_runtime", "train_samples_per_second", "train_steps_per_second", "train_loss", "total_flos")


def _key(k):
    for p in ("eval_", "test_"):
        if k.startswith(p):
            return p[:-1] + "/" + k[len(p):]
    return "train/" + k


class RunyardCallback(TrainerCallback):
    """One run per training. Config is the TrainingArguments plus the model config; metrics are keyed by global step.

    project and name default to RUNYARD_PROJECT and the Trainer's run_name; other keyword arguments go to runyard.init.
    """

    def __init__(self, project=None, name=None, **init_kwargs):
        self.project, self.name, self.init_kwargs = project, name, init_kwargs
        self.run = None

    def on_train_begin(self, args, state, control, model=None, **kwargs):
        if self.run is not None or not state.is_world_process_zero:
            return
        config = args.to_dict()
        mc = getattr(model, "config", None)
        if mc is not None:
            config["model"] = mc if isinstance(mc, dict) else mc.to_dict()
        name = self.name or (args.run_name if args.run_name and args.run_name != args.output_dir else None)
        self.run = init(self.project, name, config, **self.init_kwargs)

    def on_log(self, args, state, control, logs=None, **kwargs):
        if self.run is None or not logs:
            return
        self.run.summary.update({k: v for k, v in logs.items() if k in SUMMARY})
        values = {_key(k): v for k, v in logs.items() if k not in SUMMARY}
        if values:
            self.run.log(values, step=state.global_step)
