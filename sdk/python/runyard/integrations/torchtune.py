"""torchtune: in the recipe config,
metric_logger: {_component_: runyard.integrations.torchtune.RunyardLogger, project: sft}."""
from omegaconf import OmegaConf
from torchtune.training import get_world_size_and_rank
from torchtune.training.metric_logging import MetricLoggerInterface

from .. import init


class RunyardLogger(MetricLoggerInterface):
    """One run per recipe, on rank 0 only. Config is the resolved recipe config; metrics are keyed by the recipe's step.
    Other keyword arguments go to runyard.init.
    """

    # log_dir: torchtune's configs give every metric logger one; runyard writes nothing there
    def __init__(self, project=None, name=None, log_dir=None, **init_kwargs):
        self.run = init(project, name, **init_kwargs) if get_world_size_and_rank()[1] == 0 else None

    def log(self, name, data, step):
        if self.run:
            self.run.log({name: data}, step=step)

    def log_dict(self, payload, step):
        if self.run:
            self.run.log(payload, step=step)

    def log_config(self, config):
        if self.run:
            self.run.config.update(OmegaConf.to_container(config, resolve=True))

    def close(self):
        if self.run:
            self.run.finish()
