"""A run queues every call and sends it from a background thread, so a slow or missing server never blocks training.

Nothing here raises into the training script: failures become a warning on the real stderr.
"""
import atexit
import functools
import json
import logging
import math
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import threading
import time
import uuid
from collections import deque
from datetime import datetime, timezone
from pathlib import Path

from . import media
from .client import HTTPError, request

CACHE = Path(os.environ.get("RUNYARD_CACHE") or Path.home() / ".cache" / "runyard")
HEARTBEAT_EVERY = 30
SYSTEM_EVERY = 15
BATCH = 5000

_active = []  # open runs: the output tee and the exit hooks act on these
_warned = set()


def _now():
    return datetime.now(timezone.utc).isoformat()


def warn(msg, once=False):
    if once:
        if msg in _warned:
            return
        _warned.add(msg)
    err = sys.__stderr__  # the real stderr, so a warning never turns into a captured log line
    if err:
        try:
            print("runyard: " + msg, file=err, flush=True)
        except OSError:  # stderr was a pipe whose reader is gone
            pass


def _safe(f):
    @functools.wraps(f)
    def g(*a, **kw):
        try:
            return f(*a, **kw)
        except Exception as e:
            warn(f"{f.__name__} failed: {e!r}")

    return g


def _number(v):
    """A JSON-ready metric value, or None for anything that is not a number."""
    if hasattr(v, "item"):  # numpy / torch scalar
        try:
            v = v.item()
        except Exception:
            return None
    if isinstance(v, bool):
        return int(v)
    if not isinstance(v, (int, float)):
        return None
    if isinstance(v, float) and not math.isfinite(v):
        return "NaN" if math.isnan(v) else ("Infinity" if v > 0 else "-Infinity")
    return v


class Sender:
    """The ordered queue of API calls for one run. drain() sends from the head and stops at the first failure."""

    def __init__(self, server, key, run_id, items=()):
        self.server, self.key, self.run_id = server, key, run_id
        self.items = deque(items)
        self.lock = threading.Lock()

    # shortcut: the queue only lives in memory until exit; a server down for days on a chatty run grows it unbounded.
    def put(self, kind, data):
        with self.lock:
            self.items.append((kind, data))

    def drain(self):
        """True once the queue is empty, False when the server could not take the next call (it stays queued)."""
        while True:
            with self.lock:
                if not self.items:
                    return True
                kind = self.items[0][0]
                n = 1
                if kind in ("metrics", "logs"):
                    while n < len(self.items) and n < BATCH and self.items[n][0] == kind:
                        n += 1
                batch = [self.items[i][1] for i in range(n)]
            try:
                self._send(kind, batch)
            except HTTPError as e:
                if e.status in (401, 403, 408, 429) or e.status >= 500:
                    if e.status in (401, 403):
                        warn(f"the server refused the API key ({e.status}); data is kept and saved at exit", once=True)
                    return False
                warn(f"the server rejected {kind} ({e.status}), dropped: {e.body[:200]}")
            except (OSError, ValueError):  # unreachable, timed out, or a reply that is not JSON
                return False
            with self.lock:
                for _ in range(n):
                    k, data = self.items.popleft()
                    if k == "media":
                        Path(data[1]).unlink(missing_ok=True)

    def _send(self, kind, batch):
        base = f"{self.server}/api/v1/runs"
        if kind == "create":
            request("POST", base, self.key, batch[0])
        elif kind == "patch":
            request("PATCH", f"{base}/{self.run_id}", self.key, batch[0])
        elif kind in ("metrics", "logs"):
            request("POST", f"{base}/{self.run_id}/{kind}", self.key, batch)
        elif kind == "alert":
            request("POST", f"{base}/{self.run_id}/alerts", self.key, batch[0])
        elif kind == "media":
            fields, path = batch[0]
            if not Path(path).exists():
                return  # already sent by an earlier process (runyard sync after a partial upload)
            request("POST", f"{base}/{self.run_id}/media", self.key, fields, file=(Path(path).name, Path(path)), timeout=300)
        elif kind == "artifact":
            fields, path = batch[0]
            url = f"{base}/{self.run_id}/artifacts"
            if path is None:
                request("POST", url, self.key, {**fields, "meta": json.loads(fields["meta"])})
                return
            p = Path(path)
            if not p.exists():
                warn(f"artifact {path} is gone, not uploaded")
                return
            if p.is_file():
                request("POST", url, self.key, fields, file=(p.name, p), timeout=3600)
                return
            # a directory goes up as one .tar.gz, packed here in the sender thread so training does not wait
            archive = Path(shutil.make_archive(str(CACHE / "uploads" / fields["id"]), "gztar", p))
            try:
                request("POST", url, self.key, {**fields, "name": fields["name"] + ".tar.gz"}, file=(archive.name, archive), timeout=3600)
            finally:
                archive.unlink(missing_ok=True)

    def save_pending(self):
        """Writes what is still queued to the cache so `runyard sync` can send it later; returns the file or None."""
        with self.lock:
            items = list(self.items)
        if not items:
            return None
        p = CACHE / "pending" / f"{self.run_id}.json"
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(json.dumps({"server": self.server, "run_id": self.run_id, "items": items}))
        return p


class _Synced(dict):
    """A dict whose changes go to the server too (run.config, run.summary)."""

    def __init__(self, run, field, data):
        super().__init__(data)
        self._run, self._field = run, field

    def __setitem__(self, k, v):
        super().__setitem__(k, v)
        self._run._patch({self._field: {k: v}})

    def update(self, *a, **kw):
        d = dict(*a, **kw)
        super().update(d)
        self._run._patch({self._field: d})


class Run:
    def __init__(self, project=None, name=None, config=None, tags=None, notes="", id=None, server=None,
                 api_key=None, capture_output=True, system=True):
        self.id = id or str(uuid.uuid4())
        self.project = project or os.environ.get("RUNYARD_PROJECT") or "default"
        self.name = name or os.environ.get("RUNYARD_RUN_NAME") or self.id[:8]
        server = (server or os.environ.get("RUNYARD_SERVER") or "http://localhost:8080").rstrip("/")
        self.url = f"{server}/#/r/{self.id}"
        key = api_key or os.environ.get("RUNYARD_API_KEY", "")
        if not key:
            warn("RUNYARD_API_KEY is not set; the server will refuse this run", once=True)
        self._sender = Sender(server, key, self.id)
        self._dir = CACHE / "media" / self.id
        self._step = 0
        self._seq = 0
        self._lock = threading.Lock()
        self._capture = capture_output
        self._closing = False
        self._wake = threading.Event()
        self.finished = False
        self.config = _Synced(self, "config", config or {})
        self.summary = _Synced(self, "summary", {})
        self._sender.put("create", {"id": self.id, "project": self.project, "name": self.name, "config": dict(self.config),
                                    "tags": list(tags or []), "notes": notes, "host": socket.gethostname(), "git": _git(),
                                    "started_at": _now(), "agent": _agent()})
        _check_sandbox()
        self._system = _System() if system else None
        self._t0 = time.time()
        _active.append(self)
        _install_hooks()
        self._thread = threading.Thread(target=self._loop, name="runyard-sender", daemon=True)
        self._thread.start()

    def _patch(self, body):
        if not self.finished:
            self._sender.put("patch", body)

    def _step_for(self, step):
        """The step for media logged without one: the last metrics step."""
        return int(step) if step is not None else max(self._step - 1, 0)

    @_safe
    def log(self, data, step=None, ts=None):
        """Logs numbers. Without step, each call is the next step after the last one; ts (ISO 8601) defaults to now."""
        values = {}
        for k, v in data.items():
            n = _number(v)
            if n is None:
                warn(f"skipped {k!r}: log() takes numbers, use image()/video()/text() for the rest", once=True)
            else:
                values[str(k)] = n
        with self._lock:
            if step is None:
                step = self._step
            step = int(step)
            self._step = max(self._step, step + 1)
        if values:
            self._sender.put("metrics", {"step": step, "ts": ts or _now(), "values": values})

    def _media(self, key, kind, step, path, meta):
        mid = path.stem
        self._sender.put("media", ({"id": mid, "key": key, "step": self._step_for(step), "kind": kind,
                                    "meta": json.dumps(meta)}, str(path)))

    @_safe
    def image(self, key, images, step=None, caption=None):
        """One image or a list: PIL images, numpy arrays, torch tensors or image file paths."""
        many = isinstance(images, (list, tuple))
        for i, im in enumerate(images if many else [images]):
            cap = caption[i] if isinstance(caption, (list, tuple)) else caption
            try:
                path = media.save_image(im, self._dir / f"{uuid.uuid4()}.png")
            except Exception as e:
                warn(f"could not save image {key!r}: {e!r}")
                continue
            self._media(key, "image", step, path, {"caption": cap, "index": i} if many else {"caption": cap})

    @_safe
    def video(self, key, video, fps=30, step=None, caption=None):
        """A video file path, or frames (arrays, tensors, PIL images) that become an mp4 (a GIF without imageio-ffmpeg)."""
        path = media.save_video(video, self._dir / f"{uuid.uuid4()}.mp4", fps)
        self._media(key, "video", step, path, {"caption": caption, "fps": fps})

    @_safe
    def text(self, key, rows, step=None):
        """Text samples: a string, or a list of strings or dicts (e.g. prompt/output pairs), shown as a table."""
        rows = [rows] if isinstance(rows, str) else list(rows)
        rows = [r if isinstance(r, dict) else {"text": str(r)} for r in rows]
        self._dir.mkdir(parents=True, exist_ok=True)
        path = self._dir / f"{uuid.uuid4()}.json"
        from .client import clean

        path.write_text(json.dumps(clean(rows)))
        self._media(key, "text", step, path, {"rows": len(rows)})

    @_safe
    def artifact(self, path, name=None, kind="file", step=None, upload=False, meta=None):
        """Records a file or directory the run produced: a checkpoint, a LoRA, an exported model. By default only where
        it lives (this host and path) is recorded; upload=True also sends a copy (a directory as one .tar.gz)."""
        p = Path(path).resolve()
        if not p.exists():
            warn(f"artifact {path} does not exist, not recorded")
            return
        size = p.stat().st_size if p.is_file() else sum(f.stat().st_size for f in p.rglob("*") if f.is_file())
        fields = {"id": str(uuid.uuid4()), "name": name or p.name, "kind": kind, "uri": str(p), "size": size,
                  "step": self._step_for(step), "meta": json.dumps(meta or {})}
        self._sender.put("artifact", (fields, str(p) if upload else None))

    @_safe
    def alert(self, title, body="", level="info"):
        """Records an alert and notifies the project's channels. level: info, warn or error."""
        self._sender.put("alert", {"title": str(title), "body": str(body), "level": level})

    def _log_line(self, stream, line):
        with self._lock:
            self._seq += 1
            seq = self._seq
        self._sender.put("logs", {"seq": seq, "ts": _now(), "stream": stream, "line": line[:10000]})

    @_safe
    def finish(self, status="finished", timeout=60):
        """Marks the run finished (or failed) and waits up to timeout seconds for the queue to reach the server."""
        if self.finished:
            return
        if self in _active:
            _active.remove(self)
        self._sender.put("patch", {"status": status})
        self.finished = True
        self._closing = True
        self._wake.set()
        self._thread.join(min(3, timeout))
        if self._thread.is_alive():
            warn(f"sending what is left of this run, up to {timeout} seconds", once=True)
            self._thread.join(max(0, timeout - 3))
        p = self._sender.save_pending()
        if p:
            warn(f"server unreachable, {len(self._sender.items)} unsent calls saved to {p}; send them with `runyard sync`")

    def _loop(self):
        last_beat = last_sys = 0.0
        fails = 0
        while True:
            self._wake.wait(1.0 if not fails else min(60, 2 ** fails))
            self._wake.clear()
            now = time.time()
            if self._system and now - last_sys >= SYSTEM_EVERY and not self._closing:
                last_sys = now
                values = self._system.sample()
                if values:
                    self._sender.put("metrics", {"step": int(now - self._t0), "ts": _now(), "values": values})
            closing = self._closing  # read before draining: finish() queues its last call before setting it
            ok = self._sender.drain()
            fails = 0 if ok else fails + 1
            if closing:
                return
            if ok and now - last_beat >= HEARTBEAT_EVERY:
                try:
                    request("POST", f"{self._sender.server}/api/v1/runs/{self.id}/heartbeat", self._sender.key)
                    last_beat = now
                except Exception:
                    pass


def sync(path, api_key=None):
    """Sends a pending file written at exit by a run that could not reach its server. True when all of it landed."""
    d = json.loads(Path(path).read_text())
    s = Sender(d["server"], api_key or os.environ.get("RUNYARD_API_KEY", ""), d["run_id"], [tuple(i) for i in d["items"]])
    if s.drain():
        Path(path).unlink()
        return True
    s.save_pending()
    return False


def _git():
    def git(*args):
        return subprocess.run(["git", *args], capture_output=True, text=True, timeout=5).stdout.strip()

    try:
        commit = git("rev-parse", "HEAD")
        if not commit:
            return None
        return {"commit": commit, "branch": git("rev-parse", "--abbrev-ref", "HEAD"), "dirty": bool(git("status", "--porcelain")),
                "remote": git("config", "--get", "remote.origin.url")}
    except Exception:
        return None


# Coding agent CLIs that put their session id in the environment of every command they run.
_AGENT_SESSIONS = (("claude", "CLAUDE_CODE_SESSION_ID"), ("codex", "CODEX_SESSION_ID"), ("opencode", "OPENCODE_SESSION_ID"))


def _agent():
    """The coding agent session this run was started from, so the server can put the run on that agent's card.
    RUNYARD_AGENT names it by hand (runs with the same name share a card); None outside an agent."""
    env = os.environ
    tool, session = next(((t, env[v]) for t, v in _AGENT_SESSIONS if env.get(v)), (None, None))
    if not tool and env.get("GEMINI_CLI"):
        tool = "gemini"
    if not tool and env.get("AI_AGENT"):  # the proposed cross-CLI variable: "claude-code_2-1-316_agent" -> "claude-code"
        tool = re.sub(r"[^a-z0-9.-]", "", re.split(r"[_\s/@]", env["AI_AGENT"].strip().lower())[0])[:40] or None
    label = env.get("RUNYARD_AGENT", "").strip()[:100]
    if label:
        tool, session = tool or "agent", label
    if not tool:
        return None
    cwd = os.getcwd()
    # shortcut: a CLI that names no session (Gemini CLI) gets one card per machine and directory; RUNYARD_AGENT splits it
    return {"tool": tool, "session": (session or f"{socket.gethostname()}:{cwd}")[:200], "label": label, "cwd": cwd[:1000]}


def _check_sandbox():
    """Says why a run inside a sandbox will not arrive, instead of failing quietly at exit."""
    if os.environ.get("CODEX_SANDBOX_NETWORK_DISABLED") == "1":
        warn("Codex's sandbox has no network, so this run cannot reach the server: set network_access = true under "
             "[sandbox_workspace_write] in ~/.codex/config.toml", once=True)
    try:
        CACHE.mkdir(parents=True, exist_ok=True)
    except OSError:
        warn(f"{CACHE} is not writable (a sandbox?), so images and unsent data have nowhere to go: "
             "set RUNYARD_CACHE to a directory the run may write", once=True)


def warn_no_nvml():
    """A GPU with nothing to read it looks like a machine without one, so say so."""
    if shutil.which("nvidia-smi"):
        warn("this machine has an NVIDIA GPU but nvidia-ml-py is missing, so its GPU numbers are not collected: "
             "pip install 'runyard[system]'", once=True)


class _System:
    """GPU (nvidia-ml-py) and CPU/RAM (psutil) numbers, each only when its package is installed."""

    def __init__(self):
        self.nvml = self.psutil = None
        try:
            import pynvml

            pynvml.nvmlInit()
            self.gpus = [pynvml.nvmlDeviceGetHandleByIndex(i) for i in range(pynvml.nvmlDeviceGetCount())]
            self.nvml = pynvml
        except ImportError:
            warn_no_nvml()
        except Exception:
            pass
        try:
            import psutil

            psutil.cpu_percent()
            self.psutil = psutil
        except Exception:
            pass

    def sample(self):
        v = {}
        try:
            n = self.nvml
            for i, h in enumerate(self.gpus if n else []):
                v[f"sys/gpu{i}.util"] = n.nvmlDeviceGetUtilizationRates(h).gpu
                v[f"sys/gpu{i}.mem_gb"] = n.nvmlDeviceGetMemoryInfo(h).used / 1e9
                v[f"sys/gpu{i}.temp_c"] = n.nvmlDeviceGetTemperature(h, 0)
                v[f"sys/gpu{i}.power_w"] = n.nvmlDeviceGetPowerUsage(h) / 1000
            if self.psutil:
                v["sys/cpu"] = self.psutil.cpu_percent()
                v["sys/ram_gb"] = self.psutil.virtual_memory().used / 1e9
        except Exception:
            pass
        return v


class _Tee:
    """Wraps sys.stdout/stderr: everything still reaches the terminal, and whole lines go to the open runs."""

    def __init__(self, stream, number):
        self._stream, self._number = stream, number
        self._buf = ""
        self._buf_lock = threading.Lock()

    def write(self, s):
        n = len(s)
        try:
            n = self._stream.write(s)
        except BrokenPipeError:  # `| tee log` gets the same Ctrl-C and dies first; the run must still end
            pass
        try:
            with self._buf_lock:
                self._buf += s
                if "\n" not in self._buf and len(self._buf) < 10000:
                    return n
                *lines, self._buf = self._buf.split("\n")
            for line in lines:
                line = line.rstrip("\r").rsplit("\r", 1)[-1]  # a progress bar redrawn with \r: keep its last state
                for run in list(_active):
                    if run._capture:
                        run._log_line(self._number, line)
        except Exception:
            pass
        return n

    def flush(self):
        try:
            self._stream.flush()
        except BrokenPipeError:
            pass

    def __getattr__(self, name):
        return getattr(self._stream, name)


_hooked = False
_exit_code = [0]
_STOPS = [getattr(signal, n) for n in ("SIGINT", "SIGTERM", "SIGHUP") if hasattr(signal, n)]  # no SIGHUP on Windows


def _install_hooks():
    _install_tee()
    global _hooked
    if _hooked:
        return
    _hooked = True
    _install_exit_hooks()


def _install_tee():
    # Runs at every init: something may have replaced sys.stdout since the last run started.
    for name, number in (("stdout", 1), ("stderr", 2)):
        orig = getattr(sys, name)
        if orig is not None and not isinstance(orig, _Tee):
            tee = _Tee(orig, number)
            setattr(sys, name, tee)
            # Logging handlers made before init (transformers makes one at import) hold the bare stream.
            loggers = [logging.getLogger()] + [lg for lg in logging.root.manager.loggerDict.values() if isinstance(lg, logging.Logger)]
            for lg in loggers:
                for h in lg.handlers:
                    if type(h) is logging.StreamHandler and h.stream is orig:
                        h.setStream(tee)


def _install_exit_hooks():
    prev_hook = sys.excepthook

    def excepthook(t, v, tb):
        prev_hook(t, v, tb)  # print the traceback first, so the tee sends it along with the run
        _hold_signals()
        for r in list(_active):
            r.finish("failed")

    sys.excepthook = excepthook

    orig_exit = sys.exit

    def exit(code=0):
        _exit_code[0] = code
        orig_exit(code)

    sys.exit = exit
    atexit.register(_at_exit)
    if threading.current_thread() is threading.main_thread():
        for sig in _STOPS[1:]:  # a closed terminal or tmux pane too, so atexit runs and marks it failed
            if signal.getsignal(sig) == signal.SIG_DFL:
                signal.signal(sig, _stop)


def _stop(number, _):
    # Ctrl-C on `python train.py | tee log` in tmux closes the pane, so a hangup lands while the script handles the
    # interrupt: it is still the stop someone asked for, not a failure.
    sys.exit(0 if isinstance(sys.exc_info()[1], KeyboardInterrupt) else 128 + number)


def _hold_signals():
    # The last calls can take a while on a slow link, which looks like a hang: a second Ctrl-C or a closing tmux
    # pane would lose the run's end, and it would show as crashed.
    if _active and threading.current_thread() is threading.main_thread():
        for sig in _STOPS:
            signal.signal(sig, signal.SIG_IGN)


def _at_exit():
    code = _exit_code[0]
    _hold_signals()
    for r in list(_active):
        r.finish("finished" if code in (None, 0) else "failed")
