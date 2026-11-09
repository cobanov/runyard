import importlib.util
import io
import os
import subprocess
import sys
import types
import urllib.request
from types import SimpleNamespace

from PIL import Image

from conftest import get
from test_run import wait_for

# The call patterns of ai-toolkit (toolkit/logging_aitk.py) and transformers' WandbCallback, run through the bridge.
WANDB_SCRIPT = """
import importlib.metadata
import numpy as np
import wandb
from wandb.sdk.lib.config_util import ConfigError  # transformers imports this before init

assert wandb.__name__ == "runyard.wandb_shim" and importlib.metadata.version("wandb")
run = wandb.init(project="bridge", name="aitk", config={"lr": 1e-4})
for step in range(3):
    wandb.log({"learning_rate": 1e-4}, commit=False)
    wandb.log({"loss/loss": 0.5 - step * 0.1}, commit=False)
    if step == 2:
        wandb.log({"sample_0": wandb.Image(np.zeros((8, 8, 3), np.uint8), caption="owl")}, commit=False)
    wandb.log({}, step=step, commit=True)
run.finish()

wandb.init(project="bridge", name="hf")
wandb.config.update({"learning_rate": 5e-5}, allow_val_change=True)
wandb.define_metric("train/global_step")
wandb.define_metric("*", step_metric="train/global_step", step_sync=True)
wandb.run._label(code="transformers_trainer")
wandb.config["model/num_parameters"] = 10
for gs in (10, 20):
    wandb.log({"train/loss": 1.0 / gs, "train/global_step": gs})
wandb.run.summary["train_runtime"] = 1.5
wandb.log({"completions": wandb.Table(columns=["prompt", "out"], data=[["a", "b"]])})
wandb.save("no-such-model.bin")  # nothing there: nothing recorded, no error
wandb.run.log_code(".")  # unsupported: a warning, not an error
open("lora.safetensors", "w").write("weights")
art = wandb.Artifact("lora", type="model")
art.add_file("lora.safetensors")
wandb.log_artifact(art)
wandb.run.name = "hf-renamed"
try:
    pass
except wandb.errors.CommError:  # a class, so the except clause itself does not raise
    pass
frames = np.zeros((4, 3, 8, 8), np.uint8)  # wandb's layout: time, channel, height, width
frames[:, 0] = np.arange(4)[:, None, None] * 60
wandb.log({"rollout": wandb.Video(frames, fps=8, caption="red ramp")})
wandb.finish()

with wandb.init(project="bridge", name="ctx") as run:  # ends the run; an exception inside would fail it
    run.log({"x": 1})
assert wandb.run is None

off = wandb.init(project="bridge", name="off", mode="disabled")  # as in wandb: every call works, nothing is recorded
off.log({"x": 1}); wandb.log({"x": 1}); off.config.update({"a": 1}); off.summary["b"] = 2; off.finish()
assert off and wandb.run is None

import runyard.wandb
assert runyard.wandb is wandb  # the same module by its own name, for code that can change its import

import os
os.environ["WANDB_RUN_ID"] = "abc123"  # how a trainer resumes: the same id is the same run, going on at its next step
r1 = wandb.init(project="bridge", name="resumed")
r1.log({"x": 1}); r1.log({"x": 2}); r1.finish()
r2 = wandb.init(project="bridge", name="resumed", resume="allow")
assert r2.id == "abc123" and r2.step == 2, (r2.id, r2.step)
r2.log({"x": 3}); r2.finish()
"""


def test_wandb_bridge(server, tmp_path):
    script = tmp_path / "train.py"
    script.write_text(WANDB_SCRIPT)
    env = dict(os.environ, RUNYARD_WANDB="1", RUNYARD_CACHE=str(tmp_path / "cache"))
    p = subprocess.run([sys.executable, str(script)], env=env, capture_output=True, text=True, timeout=60, cwd=tmp_path)
    assert p.returncode == 0, p.stderr
    assert "wandb.run.log_code is not supported" in p.stderr and "before wandb.init" not in p.stderr

    runs = {r["name"]: r for r in get(server + "/api/v1/runs?project=bridge")}
    assert {n: r["status"] for n, r in runs.items()} == {"aitk": "finished", "hf-renamed": "finished", "ctx": "finished", "resumed": "finished"}
    x = get(server + "/api/v1/runs/" + runs["resumed"]["id"])["metrics"]["x"]
    assert (x["count"], x["step"], x["last"]) == (3, 2, 3)
    runs["hf"] = runs["hf-renamed"]
    [art] = get(server + f"/api/v1/runs/{runs['hf']['id']}/artifacts")
    assert (art["name"], art["kind"], art["size"]) == ("lora", "model", 7) and art["uri"].endswith("lora.safetensors")
    api = server + "/api/v1/runs/"
    aitk = get(api + runs["aitk"]["id"])
    assert aitk["config"] == {"lr": 1e-4}
    assert aitk["metrics"]["loss/loss"]["count"] == 3 and aitk["metrics"]["learning_rate"]["step"] == 2
    [img] = get(api + runs["aitk"]["id"] + "/media")
    assert (img["key"], img["step"], img["meta"]["caption"]) == ("sample_0", 2, "owl")

    hf = get(api + runs["hf"]["id"])
    assert hf["config"] == {"learning_rate": 5e-5, "model/num_parameters": 10}
    assert hf["summary"] == {"train_runtime": 1.5}
    assert hf["metrics"]["train/loss"]["step"] == 20 and hf["metrics"]["train/loss"]["count"] == 2
    media = {m["kind"]: m for m in get(api + runs["hf"]["id"] + "/media")}
    assert get(server + media["text"]["url"]) == [{"prompt": "a", "out": "b"}]
    video = media["video"]
    assert video["key"] == "rollout" and video["meta"] == {"caption": "red ramp", "fps": 8}
    with urllib.request.urlopen(server + video["url"]) as r:
        data = r.read()
    if importlib.util.find_spec("imageio_ffmpeg"):  # the same function writes an mp4 when it can (test_video_mp4_or_gif)
        assert video["url"].endswith(".mp4") and data[4:8] == b"ftyp"
    else:
        gif = Image.open(io.BytesIO(data))
        assert video["url"].endswith(".gif") and gif.n_frames == 4 and gif.size == (8, 8)


def test_wandb_bridge_when_it_is_on(tmp_path):
    """Off without runyard's settings; with an API key it stands in only for a wandb that is not installed."""
    if importlib.util.find_spec("wandb"):
        return  # a real wandb here: the fallback rightly never shows
    base = {k: v for k, v in os.environ.items() if k not in ("RUNYARD_WANDB", "RUNYARD_API_KEY")}

    def imported(**env):
        p = subprocess.run([sys.executable, "-c", "import wandb; print(wandb.__name__)"], env=dict(base, **env), capture_output=True, text=True)
        return p.stdout.strip() or "ModuleNotFoundError"

    assert imported() == "ModuleNotFoundError"
    assert imported(RUNYARD_API_KEY="ry_x") == "runyard.wandb_shim"
    assert imported(RUNYARD_API_KEY="ry_x", RUNYARD_WANDB="0") == "ModuleNotFoundError"
    assert imported(RUNYARD_WANDB="1") == "runyard.wandb_shim"


def _fake_framework(monkeypatch, module, **attrs):
    """Stands in for transformers/accelerate, which the test environment does not install."""
    parts = module.split(".")
    for i in range(len(parts)):
        name = ".".join(parts[: i + 1])
        monkeypatch.setitem(sys.modules, name, sys.modules.get(name) or types.ModuleType(name))
    for k, v in attrs.items():
        setattr(sys.modules[module], k, v)


def test_hf_callback(server, monkeypatch):
    _fake_framework(monkeypatch, "transformers", TrainerCallback=object)
    monkeypatch.delitem(sys.modules, "runyard.integrations.hf", raising=False)
    hf = importlib.import_module("runyard.integrations.hf")

    cb = hf.RunyardCallback(project="hf", system=False)
    args = SimpleNamespace(to_dict=lambda: {"learning_rate": 2e-5}, run_name="sft-small", output_dir="out")
    state = SimpleNamespace(is_world_process_zero=True, global_step=0)
    model = SimpleNamespace(config=SimpleNamespace(to_dict=lambda: {"hidden_size": 64}))
    cb.on_train_begin(args, state, None, model=model)
    for gs in (5, 10):
        state.global_step = gs
        cb.on_log(args, state, None, logs={"loss": 1 / gs, "learning_rate": 2e-5})
    cb.on_log(args, state, None, logs={"eval_loss": 0.3})
    cb.on_log(args, state, None, logs={"train_runtime": 3.0, "train_loss": 0.15})
    cb.run.finish()

    r = get(server + "/api/v1/runs/" + cb.run.id)
    assert r["name"] == "sft-small" and r["config"] == {"learning_rate": 2e-5, "model": {"hidden_size": 64}}
    assert r["metrics"]["train/loss"]["step"] == 10 and r["metrics"]["eval/loss"]["last"] == 0.3
    assert r["summary"] == {"train_runtime": 3.0, "train_loss": 0.15}


def test_accelerate_tracker(server, monkeypatch, tmp_path):
    _fake_framework(monkeypatch, "accelerate.tracking", GeneralTracker=object)
    monkeypatch.delitem(sys.modules, "runyard.integrations.accelerate", raising=False)
    acc = importlib.import_module("runyard.integrations.accelerate")

    t = acc.RunyardTracker(project="acc", run_name="unet", system=False)
    t.start()  # what Accelerator.init_trackers calls, then store_init_configuration
    t.store_init_configuration({"lr": 1e-4})
    t.log({"loss": 0.2}, step=3)
    import numpy as np

    t.log_images({"samples": [np.zeros((4, 4, 3), np.uint8)] * 2}, step=3)
    t.finish()

    api = server + "/api/v1/runs/" + t.run.id
    r = wait_for(lambda: get(api))
    assert (r["status"], r["config"], r["metrics"]["loss"]["step"]) == ("finished", {"lr": 1e-4}, 3)
    assert [m["step"] for m in get(api + "/media")] == [3, 3]
