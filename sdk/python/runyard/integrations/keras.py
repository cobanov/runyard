"""Keras: model.fit(..., callbacks=[RunyardCallback(project="mnist")])."""
import keras

from .. import init


def _key(k):
    return "val/" + k[4:] if k.startswith("val_") else "train/" + k


class RunyardCallback(keras.callbacks.Callback):
    """One run per fit. Config is the fit parameters (epochs, steps) and the model's parameter count; epoch metrics
    are keyed train/... and val/... at step=epoch. log_every_n_batches > 0 also logs batch/... every n batches, at the
    global batch number. Other keyword arguments go to runyard.init.
    """

    def __init__(self, project=None, name=None, log_every_n_batches=0, **init_kwargs):
        super().__init__()
        self.project, self.name, self.every, self.init_kwargs = project, name, log_every_n_batches, init_kwargs
        self.run = None

    def on_train_begin(self, logs=None):
        self.run = init(self.project, self.name, **self.init_kwargs)
        self.run.config.update({**self.params, "model_params": self.model.count_params()})
        self.batches = 0

    def on_train_batch_end(self, batch, logs=None):
        if self.every and logs and self.batches % self.every == 0:
            self.run.log({"batch/" + k: v for k, v in logs.items()}, step=self.batches)
        self.batches += 1

    def on_epoch_end(self, epoch, logs=None):
        if logs:
            self.run.log({_key(k): v for k, v in logs.items()}, step=epoch)

    def on_train_end(self, logs=None):
        self.run.finish()
