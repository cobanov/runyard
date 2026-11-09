"""`import runyard.wandb as wandb`: the wandb API over runyard (see wandb_shim), for code that is yours to change.
Tools you cannot change get the same module as plain `import wandb`, see wandb_hook."""
import sys

from . import wandb_shim

sys.modules[__name__] = wandb_shim  # the module itself, so wandb.run and wandb.config follow init() and finish()
