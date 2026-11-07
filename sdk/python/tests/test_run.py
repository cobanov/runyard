import http.server
import json
import math
import os
import signal
import socket
import subprocess
import sys
import threading
import time
import urllib.request
import zlib
from pathlib import Path

import numpy as np
import pytest

import runyard as ry
import runyard.core
from runyard import media
from runyard.cli import main as cli

from conftest import get


def wait_for(fn, timeout=10):
    end = time.time() + timeout
    while True:
        try:
            v = fn()
            if v:
                return v
        except Exception:
            if time.time() > end:
                raise
        if time.time() > end:
            raise AssertionError("timed out")
        time.sleep(0.1)


def test_end_to_end(server):
    run = ry.init(project="e2e", name="first", config={"lr": 1e-3}, tags=["t"], system=False)
    for step in range(5):
        ry.log({"loss": 1 / (step + 1), "ok": True}, step=step * 10)
    ry.log({"loss": float("nan")}, step=50)
    ry.log({"text": "not a number"})  # skipped with a warning, never raised
    run.config.update(batch=64)
    run.summary["best"] = 0.2
    print("hello from the training script")
    ry.image("samples", [bytes_png_path(run), bytes_png_path(run)], step=50, caption=["a", "b"])
    ry.text("generations", [{"prompt": "hi", "output": "there"}], step=50)
    ry.alert("loss went NaN", level="error")
    ry.finish()

    api = server + "/api/v1/runs/" + run.id
    r = get(api)
    assert r["status"] == "finished" and r["ended_at"]
    assert r["config"] == {"lr": 1e-3, "batch": 64} and r["summary"] == {"best": 0.2} and r["tags"] == ["t"]
    assert r["metrics"]["loss"]["count"] == 6 and r["metrics"]["loss"]["last"] == "NaN" and r["metrics"]["ok"]["last"] == 1
    assert "text" not in r["metrics"]
    assert any(line["line"] == "hello from the training script" for line in get(api + "/logs"))
    m = get(api + "/media")
    assert sorted((x["kind"], x["step"]) for x in m) == [("image", 50), ("image", 50), ("text", 50)]
    text = next(x for x in m if x["kind"] == "text")
    assert get(server + text["url"]) == [{"prompt": "hi", "output": "there"}]
    assert get(server + "/api/v1/alerts?run=" + run.id)[0]["level"] == "error"


def bytes_png_path(run, n=[0]):
    """A tiny PNG file written by the zlib-only encoder."""
    n[0] += 1
    p = run._dir.parent / f"src{n[0]}.png"
    p.parent.mkdir(parents=True, exist_ok=True)
    media.write_png(np.zeros((2, 2, 3), np.uint8), p)
    return p


def test_png_encoder(tmp_path):
    a = np.arange(2 * 3 * 3, dtype=np.uint8).reshape(2, 3, 3)
    p = tmp_path / "x.png"
    media.write_png(a, p)
    b = p.read_bytes()
    assert b[:8] == b"\x89PNG\r\n\x1a\n"
    assert (int.from_bytes(b[16:20], "big"), int.from_bytes(b[20:24], "big")) == (3, 2)
    i = b.index(b"IDAT")
    raw = zlib.decompress(b[i + 4:i + 4 + int.from_bytes(b[i - 4:i], "big")])
    assert raw == b"\x00" + a[0].tobytes() + b"\x00" + a[1].tobytes()


def test_to_array():
    f = np.full((3, 4, 5), -1.0, np.float32)  # CHW in [-1, 1]
    a = media.to_array(f)
    assert a.shape == (4, 5, 3) and a.dtype == np.uint8 and a.max() == 0
    assert media.to_array(np.full((4, 4), 255.0)).max() == 255


def test_video_mp4_or_gif(server, monkeypatch, tmp_path):
    imageio_ffmpeg = pytest.importorskip("imageio_ffmpeg")
    pytest.importorskip("imageio")
    frames = np.random.randint(0, 255, (4, 7, 9, 3), np.uint8)  # odd sides: padded to even for yuv420p
    run = ry.Run(project="video", name="mp4", system=False)
    run.video("rollout", frames, fps=8, step=1)
    monkeypatch.setitem(sys.modules, "imageio_ffmpeg", None)  # import fails: the GIF fallback
    run.video("rollout", frames, fps=8, step=2)
    run.finish()

    mp4, gif = sorted(get(server + f"/api/v1/runs/{run.id}/media"), key=lambda m: m["step"])
    assert mp4["url"].endswith(".mp4") and gif["url"].endswith(".gif")
    with urllib.request.urlopen(server + mp4["url"]) as r:
        data = r.read()
    assert data[4:8] == b"ftyp"
    p = tmp_path / "check.mp4"
    p.write_bytes(data)
    meta = next(imageio_ffmpeg.read_frames(str(p)))
    assert meta["codec"].startswith("h264") and meta["pix_fmt"].startswith("yuv420p") and meta["size"] == (10, 8)
    with urllib.request.urlopen(server + gif["url"]) as r:
        assert r.read()[:4] == b"GIF8"


def test_server_down_never_blocks_then_sync(server, monkeypatch, capsys):
    with socket.socket() as s:  # a port nobody listens on
        s.bind(("127.0.0.1", 0))
        dead = f"http://127.0.0.1:{s.getsockname()[1]}"
    t0 = time.time()
    run = ry.Run(project="offline", name="no-server", server=dead, system=False)
    for i in range(2000):
        run.log({"loss": i}, step=i)
    run.image("samples", bytes_png_path(run), step=1999)
    run.finish()
    assert time.time() - t0 < 10
    pending = runyard.core.CACHE / "pending" / f"{run.id}.json"
    assert pending.exists()

    data = json.loads(pending.read_text())
    data["server"] = server  # the server came back (here: under another address)
    pending.write_text(json.dumps(data))
    assert cli(["sync"]) == 0
    assert not pending.exists()
    r = get(server + "/api/v1/runs/" + run.id)
    assert r["status"] == "finished" and r["metrics"]["loss"]["count"] == 2000
    assert len(get(server + f"/api/v1/runs/{run.id}/media")) == 1


SCRIPT = """
import sys, runyard as ry
ry.init(project="exit", name=sys.argv[1], system=False)
ry.log({"x": 1})
print("before end")
if sys.argv[1] == "raise": raise RuntimeError("boom")
if sys.argv[1] == "exit3": sys.exit(3)
"""


def test_exit_status(server, tmp_path):
    script = tmp_path / "train.py"
    script.write_text(SCRIPT)
    env = dict(os.environ, RUNYARD_CACHE=str(tmp_path / "cache"))
    want = {"raise": "failed", "exit3": "failed", "clean": "finished"}
    for name in want:
        subprocess.run([sys.executable, str(script), name], env=env, capture_output=True, timeout=60)
    runs = {r["name"]: r for r in get(server + "/api/v1/runs?project=exit")}
    assert {n: r["status"] for n, r in runs.items()} == want
    lines = [line["line"] for line in get(server + f"/api/v1/runs/{runs['raise']['id']}/logs")]
    assert "before end" in lines and any("RuntimeError: boom" in line for line in lines)


def test_cli(server, tmp_path, capsys):
    run = ry.init(project="cli", name="pixel32", config={"size": 32}, system=False)
    run.log({"loss": 0.5}, step=7)
    print("from the script")
    run.image("samples", np.zeros((4, 4, 3), np.uint8), step=7)
    run.finish()
    capsys.readouterr()

    assert cli(["runs", "ls", "--project", "cli"]) == 0
    assert "finished  cli/pixel32" in capsys.readouterr().out
    assert cli(["runs", "show", "pixel32", "--json"]) == 0
    assert json.loads(capsys.readouterr().out)["metrics"]["loss"]["last"] == 0.5
    assert cli(["logs", run.id[:8]]) == 0
    assert "from the script" in capsys.readouterr().out
    assert cli(["samples", "pixel32", "--out", str(tmp_path / "s"), "--json"]) == 0
    files = [m["file"] for m in json.loads(capsys.readouterr().out)]
    assert len(files) == 1 and Path(files[0]).read_bytes()[:4] == b"\x89PNG"
    assert cli(["runs", "show", "no-such-run"]) == 1


def test_artifacts(server, tmp_path):
    ckpt = tmp_path / "checkpoint-500"
    (ckpt / "sub").mkdir(parents=True)
    (ckpt / "model.safetensors").write_bytes(b"w" * 3000)
    (ckpt / "sub" / "config.json").write_text("{}")
    lora = tmp_path / "owl.safetensors"
    lora.write_bytes(b"lora-weights")
    run = ry.init(project="arts", name="a", system=False)
    run.artifact(ckpt, kind="checkpoint", step=500)  # a record only
    run.artifact(lora, kind="model", upload=True, meta={"rank": 16})
    run.artifact(ckpt, name="ckpt", upload=True)  # a directory goes up as .tar.gz
    run.artifact(tmp_path / "missing")  # a warning, nothing recorded
    run.finish()
    arts = get(f"{server}/api/v1/runs/{run.id}/artifacts")
    assert [(a["name"], a["kind"], a["step"]) for a in arts] == [
        ("checkpoint-500", "checkpoint", 500), ("owl.safetensors", "model", 0), ("ckpt.tar.gz", "file", 0)]
    assert arts[0]["uri"] == str(ckpt.resolve()) and arts[0]["size"] == 3002 and "url" not in arts[0]
    assert arts[1]["meta"] == {"rank": 16} and arts[1]["size"] == len(b"lora-weights")
    with urllib.request.urlopen(server + arts[1]["url"]) as r:
        assert r.read() == b"lora-weights"

    out = tmp_path / "dl"
    assert cli(["artifacts", run.id, "--get", "ckpt.tar.gz", "--out", str(out)]) == 0
    import tarfile

    with tarfile.open(out / "ckpt.tar.gz") as t:
        assert sorted(t.getnames()) == [".", "./model.safetensors", "./sub", "./sub/config.json"]
    assert cli(["artifacts", run.id, "--get", "checkpoint-500"]) == 1  # not uploaded, nothing to download


def test_ctrl_c_through_tee_in_tmux_still_ends_the_run(tmp_path):
    ends = []

    class Slow(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("content-length") or 0))
            time.sleep(0.4)
            if self.command == "PATCH":
                ends.append(body)
            self.send_response(200)
            self.send_header("content-length", "2")
            self.end_headers()
            self.wfile.write(b"{}")

        do_PATCH = do_PUT = do_GET = do_POST

    srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Slow)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    script = """
import sys, time, runyard
runyard.init(project="p", name="n", system=False)
print("ready", flush=True)
try:
    time.sleep(60)
except KeyboardInterrupt:  # how ai-toolkit stops
    print("stopping", flush=True)
    time.sleep(0.5)
    sys.exit(0)
"""
    env = {**os.environ, "RUNYARD_SERVER": f"http://127.0.0.1:{srv.server_port}", "RUNYARD_CACHE": str(tmp_path)}
    p = subprocess.Popen([sys.executable, "-c", script], env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    assert p.stdout.readline().strip() == "ready"
    p.stdout.close()  # `| tee log` in tmux: Ctrl-C kills the reader too, and the closing pane sends a hangup
    p.send_signal(signal.SIGINT)
    time.sleep(0.2)  # the script is handling the interrupt
    p.send_signal(signal.SIGHUP)
    time.sleep(0.2)  # now in the exit, waiting on the server
    p.send_signal(signal.SIGINT)
    p.wait(30)
    srv.shutdown()
    assert any(b'"finished"' in b for b in ends)
