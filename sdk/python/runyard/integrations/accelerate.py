"""Accelerate: Accelerator(log_with=[RunyardTracker(project=...)]); accelerator.log() and init_trackers(config=...) then
write to runyard. The run starts when Accelerate first uses the tracker, on the main process only."""
from accelerate.tracking import GeneralTracker

from .. import init


class RunyardTracker(GeneralTracker):
    name = "runyard"
    requires_logging_directory = False
    main_process_only = True

    def __init__(self, project=None, run_name=None, **init_kwargs):
        super().__init__()
        self.project, self.run_name, self.init_kwargs = project, run_name, init_kwargs
        self.run = None

    @property
    def tracker(self):
        """The runyard Run, which accelerator.get_tracker("runyard", unwrap=True) returns."""
        if self.run is None:
            self.run = init(self.project, self.run_name, **self.init_kwargs)
        return self.run

    def start(self):
        self.tracker

    def store_init_configuration(self, values):
        self.tracker.config.update(values)

    def log(self, values, step=None, **kwargs):
        self.tracker.log(values, step=step)

    def log_images(self, values, step=None, **kwargs):
        for k, images in values.items():
            self.tracker.image(k, list(images) if isinstance(images, (list, tuple)) else images, step=step)

    def finish(self):
        if self.run is not None:
            self.run.finish()
