"""runyard: a lightweight, self-hosted training tracker.

    import runyard as ry
    run = ry.init(project="gba-diffusion", name="pixel32_h512", config={"size": 32})
    run.log({"loss": 0.031}, step=1000)
    run.image("samples", grid, step=1000)
    run.finish()

Settings come from RUNYARD_SERVER, RUNYARD_API_KEY and RUNYARD_PROJECT.
"""
from .core import Run, sync, warn

__version__ = "0.1.0.dev0"

run = None  # the run the module-level shortcuts act on: the last one init() made


def init(project=None, name=None, config=None, **kwargs):
    """Starts a run. kwargs: tags, notes, id, resume, server, api_key, capture_output, system.
    id with resume=True continues that run after a restart: its steps and output go on where they were."""
    global run
    run = Run(project, name, config, **kwargs)
    return run


def _current(name):
    if run is None:
        warn(f"{name}() called before init(), ignored", once=True)
    return run


def log(data, step=None):
    if _current("log"):
        run.log(data, step=step)


def image(key, images, step=None, caption=None):
    if _current("image"):
        run.image(key, images, step=step, caption=caption)


def video(key, video, fps=30, step=None, caption=None):
    if _current("video"):
        run.video(key, video, fps=fps, step=step, caption=caption)


def text(key, rows, step=None):
    if _current("text"):
        run.text(key, rows, step=step)


def artifact(path, name=None, kind="file", step=None, upload=False, meta=None):
    if _current("artifact"):
        run.artifact(path, name=name, kind=kind, step=step, upload=upload, meta=meta)


def alert(title, body="", level="info"):
    if _current("alert"):
        run.alert(title, body=body, level=level)


def finish(status="finished"):
    if _current("finish"):
        run.finish(status)
