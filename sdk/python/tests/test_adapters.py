"""Each adapter trains a tiny real model with its framework; the run is then read back through the server API."""
import importlib
import os

import numpy as np
import pytest

from conftest import get

for m in ("xgboost", "lightgbm"):  # before torch: on macOS they segfault once torch's own libomp is loaded first
    try:
        importlib.import_module(m)
    except Exception:
        pass  # the test's importorskip reports it

X = np.random.default_rng(0).random((32, 4), dtype=np.float32)
Y = X @ np.array([1, -2, 3, 0.5], dtype=np.float32)


def only_run(server, project):
    [r] = get(server + "/api/v1/runs?project=" + project)
    return get(server + "/api/v1/runs/" + r["id"])


def test_lightning(server):
    pytest.importorskip("lightning")
    import torch
    from lightning.pytorch import LightningModule, Trainer

    from runyard.integrations.lightning import RunyardLogger

    class Tiny(LightningModule):
        def __init__(self, lr=0.1):
            super().__init__()
            self.save_hyperparameters()
            self.net = torch.nn.Linear(4, 1)

        def training_step(self, batch, _):
            loss = torch.nn.functional.mse_loss(self.net(batch[0]).squeeze(1), batch[1])
            self.log("train_loss", loss)
            return loss

        def configure_optimizers(self):
            return torch.optim.SGD(self.parameters(), lr=self.hparams.lr)

    class Diverges(Tiny):
        def training_step(self, batch, i):
            raise FloatingPointError("loss is NaN")

    data = torch.utils.data.DataLoader(torch.utils.data.TensorDataset(torch.tensor(X), torch.tensor(Y)), batch_size=8)
    logger = RunyardLogger(project="lit", name="tiny", system=False)

    def trainer():
        return Trainer(max_epochs=2, logger=logger, log_every_n_steps=1, enable_checkpointing=False,
                       enable_progress_bar=False, enable_model_summary=False, accelerator="cpu")

    trainer().fit(Tiny(), data)
    with pytest.raises(FloatingPointError):
        trainer().fit(Diverges(), data)  # the same logger: a second run, failed

    runs = sorted(get(server + "/api/v1/runs?project=lit"), key=lambda r: r["status"])
    assert [(r["name"], r["status"]) for r in runs] == [("tiny", "failed"), ("tiny", "finished")]
    r = get(server + "/api/v1/runs/" + runs[1]["id"])
    assert r["config"] == {"lr": 0.1}
    assert r["metrics"]["train_loss"]["step"] == 7 and r["metrics"]["train_loss"]["count"] == 8


def test_keras(server):
    os.environ.setdefault("KERAS_BACKEND", "torch")
    keras = pytest.importorskip("keras")
    from runyard.integrations.keras import RunyardCallback

    model = keras.Sequential([keras.Input((4,)), keras.layers.Dense(1)])
    model.compile(optimizer="sgd", loss="mse", metrics=["mae"])
    cb = RunyardCallback(project="keras", log_every_n_batches=2, system=False)
    model.fit(X, Y, validation_split=0.25, epochs=3, batch_size=8, verbose=0, callbacks=[cb])

    r = only_run(server, "keras")
    assert r["status"] == "finished" and r["config"] == {"verbose": 0, "epochs": 3, "steps": 3, "model_params": 5}
    m = r["metrics"]
    assert (m["train/loss"]["step"], m["val/mae"]["count"], m["batch/loss"]["step"]) == (2, 3, 8)


def test_xgboost(server):
    xgb = pytest.importorskip("xgboost")
    from runyard.integrations.xgboost import RunyardCallback

    dtrain, dval = xgb.DMatrix(X[:24], Y[:24]), xgb.DMatrix(X[24:], Y[24:])
    xgb.train({"eta": 0.3, "max_depth": 2, "eval_metric": ["rmse", "mae"]}, dtrain, num_boost_round=4,
              evals=[(dtrain, "train"), (dval, "val")], callbacks=[RunyardCallback(project="xgb", system=False)])

    r = only_run(server, "xgb")
    assert r["status"] == "finished" and r["config"]["max_depth"] == "2" and r["config"]["objective"] == "reg:squarederror"
    assert set(r["metrics"]) == {"train/rmse", "train/mae", "val/rmse", "val/mae"}
    assert (r["metrics"]["val/mae"]["step"], r["metrics"]["val/mae"]["count"]) == (3, 4)


def test_lightgbm(server):
    lgb = pytest.importorskip("lightgbm")
    from runyard.integrations.lightgbm import runyard_callback

    dtrain = lgb.Dataset(X[:24], Y[:24])
    lgb.train({"objective": "regression", "learning_rate": 0.2, "min_data_in_leaf": 2, "verbose": -1}, dtrain,
              num_boost_round=4, valid_sets=[dtrain, lgb.Dataset(X[24:], Y[24:], reference=dtrain)],
              valid_names=["train", "val"], callbacks=[runyard_callback(project="lgb", name="gbm", system=False)])

    r = only_run(server, "lgb")
    assert (r["name"], r["status"], r["config"]["learning_rate"]) == ("gbm", "finished", 0.2)
    assert set(r["metrics"]) == {"train/l2", "val/l2"} and r["metrics"]["val/l2"]["step"] == 3


def test_sklearn_autolog(server):
    pytest.importorskip("sklearn")
    from sklearn.linear_model import LogisticRegression
    from sklearn.model_selection import GridSearchCV

    from runyard.integrations import sklearn as ry_sklearn

    labels = (Y > Y.mean()).astype(int)
    ry_sklearn.autolog(project="skl", system=False)
    try:
        GridSearchCV(LogisticRegression(), {"C": [0.1, 1.0]}, cv=2).fit(X, labels)  # 5 inner fits, one run
        with pytest.raises(ValueError):
            LogisticRegression().fit(X, labels[:3])
    finally:
        ry_sklearn.autolog(disable=True)
    LogisticRegression().fit(X, labels)  # restored: no run

    runs = get(server + "/api/v1/runs?project=skl")
    assert sorted((r["name"], r["status"]) for r in runs) == [("GridSearchCV", "finished"), ("LogisticRegression", "failed")]
    r = get(server + "/api/v1/runs/" + next(r["id"] for r in runs if r["name"] == "GridSearchCV"))
    assert r["config"]["cv"] == 2 and r["config"]["param_grid"] == {"C": [0.1, 1.0]}
    assert 0.5 < r["metrics"]["train/score"]["last"] <= 1 and r["metrics"]["fit_seconds"]["step"] == 0


def test_torchtune(server, tmp_path):
    pytest.importorskip("torchtune")
    import torch
    from omegaconf import OmegaConf
    from torchtune import config

    # what a recipe does: instantiate the YAML's metric_logger, log the config, then log_dict per step
    cfg = OmegaConf.create(f"""
output_dir: {tmp_path}
optimizer: {{lr: 0.01}}
metric_logger:
  _component_: runyard.integrations.torchtune.RunyardLogger
  project: tune
  name: tiny
  log_dir: ${{output_dir}}/logs
  system: false
""")
    logger = config.instantiate(cfg.metric_logger)
    logger.log_config(cfg)
    net = torch.nn.Linear(4, 1)
    opt = torch.optim.SGD(net.parameters(), lr=cfg.optimizer.lr)
    for step in range(1, 4):
        loss = torch.nn.functional.mse_loss(net(torch.tensor(X)).squeeze(1), torch.tensor(Y))
        opt.zero_grad()
        loss.backward()
        opt.step()
        logger.log_dict({"loss": loss.item(), "lr": cfg.optimizer.lr}, step=step)
    logger.log("tokens_per_second_per_gpu", 123.0, step=3)
    logger.close()

    r = only_run(server, "tune")
    assert (r["name"], r["status"]) == ("tiny", "finished")
    assert r["config"]["metric_logger"]["log_dir"] == f"{tmp_path}/logs"  # resolved
    assert r["metrics"]["loss"]["step"] == 3 and r["metrics"]["loss"]["count"] == 3
    assert r["metrics"]["tokens_per_second_per_gpu"]["last"] == 123.0
