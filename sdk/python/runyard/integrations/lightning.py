"""Lightning: Trainer(logger=RunyardLogger(project="mnist")). Works with lightning.pytorch and pytorch_lightning."""
from argparse import Namespace

try:
    from lightning.pytorch.loggers.logger import Logger, rank_zero_experiment
    from lightning.pytorch.utilities import rank_zero_only
except ImportError:
    from pytorch_lightning.loggers.logger import Logger, rank_zero_experiment
    from pytorch_lightning.utilities import rank_zero_only

from .. import init


class RunyardLogger(Logger):
    """One run per Trainer call (fit, then test, are two runs), on the global-zero process only. Config is the
    hyperparameters (save_hyperparameters); metrics are keyed by global step. Other keyword arguments go to runyard.init.
    """

    def __init__(self, project=None, name=None, **init_kwargs):
        super().__init__()
        self.project, self._name, self.init_kwargs = project, name, init_kwargs
        self.run = None

    @property
    def name(self):
        return self._name

    @property
    def version(self):
        return self.run.id if self.run else None

    @property
    @rank_zero_experiment
    def experiment(self):
        """The runyard Run: trainer.logger.experiment.image("samples", grid)."""
        if self.run is None:
            self.run = init(self.project, self._name, **self.init_kwargs)
        return self.run

    @rank_zero_only
    def log_hyperparams(self, params, *args, **kwargs):
        self.experiment.config.update(vars(params) if isinstance(params, Namespace) else params)

    @rank_zero_only
    def log_metrics(self, metrics, step=None):
        self.experiment.log(metrics, step=step)

    @rank_zero_only
    def finalize(self, status):
        if self.run is not None:
            self.run.finish("failed" if status == "failed" else "finished")  # Lightning says success, failed or finished
            self.run = None  # a later trainer.test() on this logger starts its own run instead of writing to a closed one
