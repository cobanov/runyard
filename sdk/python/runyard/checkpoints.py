"""Watches the directory a training saves its checkpoints to and records each new one as an artifact: where it is,
how big, which step, when. Nothing is uploaded; `runyard artifacts RUN` and the run page say where to find them."""
import re
import threading
import time
from datetime import datetime, timezone
from pathlib import Path

POLL = 30  # seconds between looks
SETTLE = 20  # a checkpoint whose newest file is younger than this is still being written

# What a checkpoint looks like: a directory (HF checkpoint-500, a sharded save), or a file with weights in it.
WEIGHTS = {".safetensors", ".pt", ".pth", ".ckpt", ".bin", ".gguf", ".pkl", ".npz", ".h5", ".keras", ".msgpack", ".onnx"}

_STEP = re.compile(r"(?:global_step|step|steps|checkpoint|ckpt|iter|iteration)[-_=]?0*(\d+)", re.I)
_NUMBER = re.compile(r"(\d{3,})")  # ai-toolkit's my_lora_000002000, model-7350: a number long enough to be a step


def step_of(name):
    """The step a checkpoint's name says, or None: checkpoint-500, epoch=2-step=7350.ckpt, my_lora_000002000."""
    stem = name.split(".")[0] if not name.startswith(".") else name
    named = _STEP.findall(stem)
    if named:
        return int(named[-1])
    numbers = _NUMBER.findall(stem)
    return int(numbers[-1]) if numbers else None


def _skipped(p):
    n = p.name
    return n.startswith((".", "tmp", "~")) or n.endswith(("~", ".tmp", ".part", ".partial", ".lock")) or \
        (p.is_file() and p.suffix.lower() not in WEIGHTS)


def _measure(p):
    """Size in bytes and the newest modification time of a file, or of everything under a directory."""
    if p.is_file():
        st = p.stat()
        return st.st_size, st.st_mtime
    size, newest = 0, p.stat().st_mtime
    for f in p.rglob("*"):
        try:
            if f.is_file():
                st = f.stat()
                size += st.st_size
                newest = max(newest, st.st_mtime)
        except OSError:  # removed while we looked (a rotated checkpoint)
            pass
    return size, newest


class Watcher:
    """Looks at directory every POLL seconds from a thread of its own (a 50 GB checkpoint is many stat calls, which
    must not hold up the sender) and records what appeared since the run started once it stopped changing."""

    def __init__(self, run, directory, since):
        self.run, self.dir, self.since = run, Path(directory).expanduser(), since
        self.seen = set()
        self.last = {}  # path -> size at the previous look, to tell a finished write from a pause
        self.lock = threading.Lock()
        self.stop = threading.Event()
        threading.Thread(target=self._loop, name="runyard-checkpoints", daemon=True).start()

    def _loop(self):
        while not self.stop.wait(POLL):
            try:
                self.scan()
            except Exception:
                pass

    def scan(self, final=False):
        """Records new checkpoints; final (the run is ending) takes what is there, written or not."""
        with self.lock:
            if not self.dir.is_dir():
                return
            for p in sorted(self.dir.iterdir()):
                try:
                    if p in self.seen or _skipped(p):
                        continue
                    size, mtime = _measure(p)
                except OSError:
                    continue
                if mtime < self.since - 1:
                    continue  # there before the run: an earlier run's, or the one a resumed run started from
                settled = time.time() - mtime > SETTLE and self.last.get(p) == size
                self.last[p] = size
                if not (settled or final):
                    continue
                self.seen.add(p)
                modified = datetime.fromtimestamp(mtime, timezone.utc).isoformat()
                self.run._checkpoint(p, step_of(p.name), size, modified)

    def close(self):
        self.stop.set()
        try:
            self.scan(final=True)
        except Exception:
            pass
