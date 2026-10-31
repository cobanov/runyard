"""`runyard watch DIR`: sends TensorBoard event files to runyard, for tools that write nothing else (kohya sd-scripts,
DeepSpeed, diffusers --report_to tensorboard, torch SummaryWriter). Needs the tensorboard package.

Each directory holding events.out.tfevents.* files is one run, named by its path under DIR. Scalars, images, text
and hparams are read; other summaries (histograms, audio, graphs) are skipped.
"""
import json
import os
import re
import socket
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path

from tensorboard.backend.event_processing.event_file_loader import EventFileLoader
from tensorboard.plugins.hparams import plugin_data_pb2
from tensorboard.util import tensor_util

from .core import Run, _safe, warn


def run_dir(f, root):
    """An event file's run directory: its own, or the parent of torch's add_hparams subdirectory (<logdir>/<time>)."""
    d = f.parent
    return d.parent if d != root and re.fullmatch(r"\d{9,}\.\d+", d.name) else d


def start(d, root, project, live):
    name = d.relative_to(root).as_posix() if d != root else root.name
    # The same directory on the same machine is always the same run, so a restarted watcher continues it.
    rid = str(uuid.uuid5(uuid.NAMESPACE_URL, f"{socket.gethostname()}:{d}"))
    # shortcut: started_at is when the watcher first saw the directory, not the first event's time; upgrade by letting
    # Run take started_at if imports of old trainings sort wrongly in the run list.
    run = Run(project, name, id=rid, capture_output=False, system=False)
    if live:
        run._patch({"status": "running"})  # an earlier `watch --once` may have finished it
    print(f"{name}: {run.url}", flush=True)
    return run


def save(run, kind, key, ev, i, data, ext, meta):
    # A fixed id per sample: the server ignores the ones a restarted watcher reads again.
    mid = uuid.uuid5(uuid.UUID(run.id), f"{key}/{ev.step}/{i}/{ev.wall_time}")
    path = run._dir / f"{mid}{ext}"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    run._media(key, kind, ev.step, path, meta)


@_safe
def send(run, ev, plugins):
    """Sends one event's summaries. plugins maps tag to plugin name, which TF2 writers put on a tag's first value only."""
    scalars = {}
    for v in ev.summary.value:
        plugin = plugins.setdefault(v.tag, v.metadata.plugin_data.plugin_name)
        if plugin == "scalars":
            scalars[v.tag] = tensor_util.make_ndarray(v.tensor).item()
        elif plugin == "images":
            imgs = v.tensor.string_val[2:]  # width, height, then the encoded images
            for i, b in enumerate(imgs):
                ext = ".png" if b[:4] == b"\x89PNG" else ".gif" if b[:4] == b"GIF8" else ".jpg"
                meta = {"caption": None, "index": i} if len(imgs) > 1 else {"caption": None}
                save(run, "image", v.tag, ev, i, b, ext, meta)
        elif plugin == "text":
            rows = [{"text": s.decode("utf-8", "replace")} for s in v.tensor.string_val]
            key = v.tag.removesuffix("/text_summary")  # torch's add_text appends it
            save(run, "text", key, ev, 0, json.dumps(rows).encode(), ".json", {"rows": len(rows)})
        elif plugin == "hparams":
            hp = plugin_data_pb2.HParamsPluginData.FromString(v.metadata.plugin_data.content).session_start_info.hparams
            if hp:
                run.config.update({k: getattr(x, x.WhichOneof("kind")) for k, x in hp.items() if x.WhichOneof("kind")})
        else:
            warn(f"skipped {plugin or 'untyped'} summaries: watch reads scalars, images, text and hparams", once=True)
    if scalars:
        run.log(scalars, step=ev.step, ts=datetime.fromtimestamp(ev.wall_time, timezone.utc).isoformat())


# shortcut: a run stays running while the watcher runs, as event files say nothing about a training's end; upgrade to
# finishing a run after some idle time if long-lived watchers over many finished trainings become common.
def watch(root, project, once=False, interval=10):
    """Reads every event file under root, then (unless once) polls for more until Ctrl-C; finishes the runs."""
    root = Path(root).resolve()
    project = project or os.environ.get("RUNYARD_PROJECT") or root.name
    runs, loaders, plugins = {}, {}, {}
    try:
        while True:
            for f in sorted(root.rglob("events.out.tfevents.*")):
                d = run_dir(f, root)
                if d not in runs:
                    runs[d] = start(d, root, project, live=not once)
                if f not in loaders:
                    loaders[f] = EventFileLoader(str(f))  # keeps its place in the file, so polls read only new events
                for ev in loaders[f].Load():
                    send(runs[d], ev, plugins.setdefault(d, {}))
            if once:
                break
            time.sleep(interval)
    except KeyboardInterrupt:
        pass
    for run in runs.values():
        run.finish()
