import os
import subprocess
import sys
import time
import urllib.request

import numpy as np
import pytest

from runyard.cli import main as cli

from conftest import get
from test_run import wait_for

tb = pytest.importorskip("torch.utils.tensorboard")
from tensorboard.compat.proto.event_pb2 import Event  # noqa: E402  only once the skip above did not fire
from tensorboard.plugins.scalar.summary_v2 import scalar_pb  # noqa: E402
from tensorboard.summary.writer.event_file_writer import EventFileWriter  # noqa: E402


def runs(server, project):
    return {r["name"]: r for r in get(server + "/api/v1/runs?project=" + project)}


def test_watch_once(server, tmp_path, monkeypatch):
    monkeypatch.delenv("RUNYARD_PROJECT", raising=False)
    logs = tmp_path / "tblogs"
    w = tb.SummaryWriter(logs / "lr1e-4")
    for step in range(3):
        w.add_scalar("train/loss", 1 / 2 ** step, step * 10)
    w.add_scalar("lr", 1e-4, 20)
    w.add_image("samples", np.full((3, 4, 6), 255, np.uint8), 20)
    w.add_text("prompt", "an owl", 20)
    w.add_hparams({"lr": 1e-4, "optimizer": "adamw"}, {})  # written to a <logdir>/<time> subdirectory
    w.close()
    w = tb.SummaryWriter(logs / "sweep" / "b")
    w.add_scalar("train/loss", 0.75, 5)
    w.close()
    w = EventFileWriter(str(logs / "tf2"))  # TF2's layout: tensor scalars, the tag's metadata on its first value only
    for step in range(2):
        s = scalar_pb("acc", 0.5 * step)
        if step:
            s.value[0].ClearField("metadata")
        w.add_event(Event(step=step, wall_time=time.time(), summary=s))
    w.close()

    assert cli(["watch", str(logs), "--once"]) == 0
    got = runs(server, "tblogs")
    assert {n: r["status"] for n, r in got.items()} == {"lr1e-4": "finished", "sweep/b": "finished", "tf2": "finished"}
    assert get(server + f"/api/v1/runs/{got['tf2']['id']}/metrics")["acc"] == {"step": [0, 1], "min": [0, 0.5],
                                                                                 "max": [0, 0.5], "last": [0, 0.5]}
    api = server + "/api/v1/runs/" + got["lr1e-4"]["id"]
    assert get(api)["config"] == {"lr": 1e-4, "optimizer": "adamw"}
    m = get(api + "/metrics")
    assert m["train/loss"]["step"] == [0, 10, 20] and m["train/loss"]["last"] == [1, 0.5, 0.25]
    assert m["lr"]["step"] == [20] and m["lr"]["last"][0] == pytest.approx(1e-4)
    media = {x["kind"]: x for x in get(api + "/media")}
    assert (media["image"]["key"], media["image"]["step"]) == ("samples", 20) and media["image"]["url"].endswith(".png")
    with urllib.request.urlopen(server + media["image"]["url"]) as r:
        assert r.read()[:4] == b"\x89PNG"
    assert media["text"]["key"] == "prompt" and get(server + media["text"]["url"]) == [{"text": "an owl"}]
    assert get(server + f"/api/v1/runs/{got['sweep/b']['id']}/metrics")["train/loss"]["step"] == [5]


def test_watch_live(server, tmp_path):
    logs = tmp_path / "live"
    logs.mkdir()
    env = dict(os.environ, RUNYARD_CACHE=str(tmp_path / "cache"))
    p = subprocess.Popen([sys.executable, "-m", "runyard.cli", "watch", str(logs), "--interval", "0.2"], env=env,
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    w = tb.SummaryWriter(logs / "late")  # appears after the watcher started
    w.add_scalar("loss", 2.0, 3)
    w.flush()
    run = wait_for(lambda: runs(server, "live").get("late"))
    wait_for(lambda: get(server + f"/api/v1/runs/{run['id']}/metrics").get("loss"))
    assert get(server + "/api/v1/runs/" + run["id"])["status"] == "running"
    w.close()
    p.terminate()  # SIGTERM stops it like Ctrl-C
    out, err = p.communicate(timeout=30)
    assert p.returncode == 0, err
    assert "late: " in out and get(server + "/api/v1/runs/" + run["id"])["status"] == "finished"


def test_watch_again_continues_the_run(server, tmp_path):
    logs = tmp_path / "logs"
    w = tb.SummaryWriter(logs)
    w.add_scalar("loss", 1.0, 0)
    w.add_image("samples", np.zeros((3, 2, 2), np.uint8), 0)
    w.close()
    assert cli(["watch", str(logs), "--once", "--project", "resume"]) == 0
    w = tb.SummaryWriter(logs)  # the training resumed: a second event file in the same directory
    w.add_scalar("loss", 0.5, 1)
    w.close()
    assert cli(["watch", str(logs), "--once", "--project", "resume"]) == 0

    [run] = runs(server, "resume").values()
    assert run["name"] == "logs" and run["status"] == "finished"
    api = server + "/api/v1/runs/" + run["id"]
    assert get(api + "/metrics")["loss"]["step"] == [0, 1]
    assert len(get(api + "/media")) == 1  # the image read again is not stored twice
