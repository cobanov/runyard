"""XGBoost: xgb.train(params, dtrain, evals=[(dval, "val")], callbacks=[RunyardCallback(project="tabular")])."""
import json

from xgboost.callback import TrainingCallback

from .. import init


def _train_params(c):
    """The "*_train_param" sections of the booster's JSON config, merged: objective, eta, max_depth and the rest."""
    out = {}
    for k, v in c.items():
        if isinstance(v, dict):
            out.update(v if k.endswith("_train_param") else _train_params(v))
    return out


class RunyardCallback(TrainingCallback):
    """One run per training (also XGBClassifier(callbacks=[...])). Config is the booster's training parameters;
    every eval metric is logged as data/metric at step=iteration. Other keyword arguments go to runyard.init.
    """

    def __init__(self, project=None, name=None, **init_kwargs):
        super().__init__()
        self.project, self.name, self.init_kwargs = project, name, init_kwargs
        self.run = None

    def before_training(self, model):
        self.run = init(self.project, self.name, **self.init_kwargs)
        if hasattr(model, "save_config"):  # xgb.cv passes a bundle of fold boosters instead
            self.run.config.update(_train_params(json.loads(model.save_config())))
        return model

    def after_iteration(self, model, epoch, evals_log):
        values = {}
        for data, metrics in evals_log.items():
            for metric, history in metrics.items():
                v = history[-1]
                values[f"{data}/{metric}"] = v[0] if isinstance(v, tuple) else v  # xgb.cv logs (mean, std)
        self.run.log(values, step=epoch)
        return False

    def after_training(self, model):
        self.run.finish()
        return model
