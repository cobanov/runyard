"""Enough of the wandb API for tools that only speak W&B (ai-toolkit, diffusers examples, HF report_to="wandb")
to write to runyard instead, without changing their code. Turned on by RUNYARD_WANDB=1; see wandb_hook.

Supported: init (also as a context manager), log (step/commit), finish, config(.update), summary, run (name and tags
can be set), define_metric (its step_metric is honoured), Image, Video, Table, Artifact with log_artifact and save
(recorded where they are, not uploaded), alert. mode="disabled", WANDB_MODE=disabled and WANDB_DISABLED make init a
no-op, as in wandb. Anything else warns once and does nothing.

Also importable by name, when the code is yours to change: `import runyard.wandb as wandb`.
"""
import os
import sys
import uuid
from pathlib import Path

from . import core

__version__ = "0.21.0"  # what version checks in callers see
__path__ = []  # a package, so `import wandb.sdk...` reaches the stub finder in wandb_hook

run = None
config = None
summary = None


class Config(dict):
    """wandb.config: a dict with attribute access whose changes go to the runyard run."""

    def __init__(self, ry_run, data=None):
        super().__init__(data or {})
        object.__setattr__(self, "_ry", ry_run)

    def update(self, d=None, allow_val_change=None, **kw):
        d = dict(vars(d) if hasattr(d, "__dict__") and not isinstance(d, dict) else (d or {}), **kw)
        super().update(d)
        self._ry.config.update(d)

    def setdefaults(self, d):
        self.update({k: v for k, v in dict(d).items() if k not in self})

    def __setitem__(self, k, v):
        self.update({k: v})

    def __getattr__(self, k):
        try:
            return self[k]
        except KeyError:
            raise AttributeError(k) from None

    def __setattr__(self, k, v):
        self.update({k: v})

    def as_dict(self):
        return dict(self)


class Image:
    def __init__(self, data_or_path, mode=None, caption=None, **kwargs):
        self.data, self.caption = data_or_path, caption


class Video:
    def __init__(self, data_or_path, caption=None, fps=4, format=None, **kwargs):
        self.data, self.caption, self.fps = data_or_path, caption, fps


class Table:
    def __init__(self, columns=None, data=None, rows=None, dataframe=None, **kwargs):
        if dataframe is not None:
            columns, data = list(dataframe.columns), dataframe.values.tolist()
        self.columns = list(columns or [])
        self.data = [list(r) for r in (data or rows or [])]

    def add_data(self, *row):
        self.data.append(list(row))

    def rows(self):
        cols = self.columns or [f"c{i}" for i in range(len(self.data[0]) if self.data else 0)]
        return [dict(zip(cols, r)) for r in self.data]


class Artifact:
    """wandb.Artifact: remembers the files added to it; log_artifact records each as an artifact of the run."""

    def __init__(self, name, type="file", description=None, metadata=None, **kwargs):
        self.name, self.type, self.metadata = name, type, metadata or {}
        self.paths = []

    def add_file(self, local_path, name=None, **kwargs):
        self.paths.append(local_path)

    add_dir = add_file

    def __getattr__(self, name):  # add_reference, wait, aliases ...: nothing to do
        if name.startswith("__"):
            raise AttributeError(name)
        return _Stub(name, quiet=True)


class AlertLevel:
    INFO, WARN, ERROR = "INFO", "WARN", "ERROR"


class Run:
    def __init__(self, ry_run):
        self._ry = ry_run
        self.id, self._name, self.project, self.url = ry_run.id, ry_run.name, ry_run.project, ry_run.url
        self._tags = ()
        self.entity = ""
        self.config = Config(ry_run, ry_run.config)
        self.summary = ry_run.summary
        self.dir = os.getcwd()
        self._pending = {}
        self._pstep = None  # step the pending values were logged with, if any
        self._auto = 0  # wandb's own step counter
        self._step_metric = None

    @property
    def step(self):
        return self._auto

    # wandb.run.name = "..." and wandb.run.tags = [...] rename and retag the run, as they do in wandb
    name = property(lambda self: self._name, lambda self, v: (setattr(self, "_name", v), self._ry._patch({"name": str(v)}))[0])
    tags = property(lambda self: self._tags, lambda self, v: (setattr(self, "_tags", tuple(v)), self._ry._patch({"tags": list(v)}))[0])

    def get_url(self):
        return self.url

    def project_name(self):
        return self.project

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.finish(1 if exc_type else 0)
        return False

    def log_artifact(self, artifact_or_path, name=None, type=None, **kwargs):
        """Records the files where they are (this host and path); nothing is uploaded, checkpoints are large."""
        a = artifact_or_path
        paths = a.paths if isinstance(a, Artifact) else [a]
        for p in paths:
            one = isinstance(a, Artifact) and len(paths) == 1
            self._ry.artifact(p, name=(a.name if one else name) or Path(p).name, kind=type or getattr(a, "type", None) or "file",
                              step=self._auto - 1 if self._auto else None, meta=getattr(a, "metadata", None))
        return a

    def save(self, glob_str=None, base_path=None, policy=None):
        for p in sorted(Path().glob(str(glob_str))) if glob_str and not os.path.isabs(str(glob_str)) else [glob_str]:
            if p and Path(p).exists():
                self._ry.artifact(p, step=self._auto - 1 if self._auto else None)

    def log(self, data, step=None, commit=None, sync=None):
        if step is not None and self._pending and self._pstep is not None and step != self._pstep:
            self._flush()  # an earlier step logged with commit=False and never committed
        self._pending.update(data or {})
        if step is not None:
            self._pstep = int(step)
        # wandb would hold uncommitted values until the step moves on; sending now is equivalent
        # because the server merges everything logged for the same step.
        if commit is not False:
            self._flush()

    def _flush(self):
        data, self._pending = self._pending, {}
        step, self._pstep = self._pstep, None
        if not data:
            return
        if step is None and self._step_metric in data:
            step = int(data[self._step_metric])
        if step is None:
            step = self._auto
        self._auto = max(self._auto, step + 1)
        numbers = {}
        for k, v in data.items():
            items = v if isinstance(v, (list, tuple)) and v and isinstance(v[0], (Image, Video)) else None
            if isinstance(v, Image):
                self._ry.image(k, v.data, step=step, caption=v.caption)
            elif items and isinstance(items[0], Image):
                self._ry.image(k, [i.data for i in items], step=step, caption=[i.caption for i in items])
            elif isinstance(v, Video) or items:
                for vid in items or [v]:
                    self._ry.video(k, vid.data, fps=vid.fps, step=step, caption=vid.caption)
            elif isinstance(v, Table):
                self._ry.text(k, v.rows(), step=step)
            elif isinstance(v, dict):
                numbers.update({f"{k}/{kk}": vv for kk, vv in v.items()})
            else:
                numbers[k] = v
        if numbers:
            self._ry.log(numbers, step=step)

    def define_metric(self, name, step_metric=None, **kwargs):
        if step_metric:
            self._step_metric = step_metric

    def alert(self, title, text="", level=None, wait_duration=None):
        self._ry.alert(title, text, level={"WARN": "warn", "ERROR": "error"}.get(str(level).split(".")[-1], "info"))

    def finish(self, exit_code=None, quiet=None):
        global run
        self._flush()
        self._ry.finish("finished" if exit_code in (None, 0) else "failed")
        if run is self:
            run = None
            _set_globals()

    def __getattr__(self, name):
        if name.startswith("__"):
            raise AttributeError(name)
        if name.startswith("_"):  # private wandb calls such as run._label(): silently nothing
            return _Stub(name, quiet=True)
        return _Stub("run." + name)


def _disabled(mode):
    env = os.environ
    return (mode or env.get("WANDB_MODE", "")).lower() == "disabled" or env.get("WANDB_DISABLED", "").lower() in ("1", "true", "yes")


def init(project=None, name=None, config=None, tags=None, notes=None, id=None, reinit=None, mode=None, **kwargs):
    global run, _off
    _off = _disabled(mode)
    if _off:  # what wandb does: every call still works, nothing is recorded
        return _Off()
    if run is not None and not reinit:
        return run
    if run is not None:
        run.finish()
    if config is not None and not isinstance(config, dict):
        config = vars(config)  # argparse.Namespace and the like
    project = project or os.environ.get("WANDB_PROJECT")
    name = name or os.environ.get("WANDB_NAME") or os.environ.get("WANDB_RUN_NAME")
    tags = list(tags or []) + ([kwargs["group"]] if kwargs.get("group") else [])
    # A run id (init(id=...), WANDB_RUN_ID) is how tools resume after a restart: the same id is the same run here,
    # going on at its next step. wandb ids are short strings, runyard's are UUIDs, so one is derived from the other.
    wid = id or os.environ.get("WANDB_RUN_ID")
    rid = str(uuid.uuid5(uuid.NAMESPACE_URL, f"wandb:{project or os.environ.get('RUNYARD_PROJECT') or 'default'}:{wid}")) if wid else None
    ry_run = core.Run(project, name, dict(config or {}), tags=tags, notes=notes or "", id=rid, resume=bool(rid))
    run = Run(ry_run)
    if wid:
        run.id = str(wid)  # what the tool saved and will pass again
    run._auto = ry_run._step
    run._tags = tuple(tags)
    _set_globals()
    return run


def _set_globals():
    global config, summary
    config = run.config if run else None
    summary = run.summary if run else None


def log(data, step=None, commit=None, sync=None):
    if run is None:
        if not _off:
            core.warn("wandb.log() before wandb.init(), ignored", once=True)
        return
    run.log(data, step=step, commit=commit)


def log_artifact(artifact_or_path, name=None, type=None, **kwargs):
    return run.log_artifact(artifact_or_path, name=name, type=type) if run else artifact_or_path


def save(glob_str=None, base_path=None, policy=None):
    if run:
        run.save(glob_str)


def define_metric(name, step_metric=None, **kwargs):
    if run:
        run.define_metric(name, step_metric=step_metric)


def finish(exit_code=None, quiet=None):
    if run:
        run.finish(exit_code)


def alert(title, text="", level=None, wait_duration=None):
    if run:
        run.alert(title, text, level)


def login(*args, **kwargs):
    return True


def watch(*args, **kwargs):
    pass


def termlog(msg="", *a, **kw):
    print(f"wandb: {msg}", file=sys.stderr)


termwarn = termerror = termlog


_off = False  # the last init was disabled: calls without a run are expected, not a mistake


class _Stub:
    """Stands in for an unsupported part of wandb: warns once when used, then behaves as a no-op."""

    def __init__(self, name, quiet=False):
        object.__setattr__(self, "_name", name)
        if not quiet:
            core.warn(f"wandb.{name} is not supported by the runyard bridge; ignored", once=True)

    def __call__(self, *args, **kwargs):
        return self

    def __getattr__(self, name):
        if name[:1].isupper() and name.endswith(("Error", "Exception")):
            return type(name, (Exception,), {})  # wandb.errors.CommError in an except clause
        return self

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def __setattr__(self, name, value):
        pass

    def __iter__(self):
        return iter(())

    def __bool__(self):
        return False


class _Off(_Stub):
    """The run of a disabled init: a dict-like config and summary, everything else a silent no-op."""

    def __init__(self):
        object.__setattr__(self, "_name", "run")
        object.__setattr__(self, "config", Config(_Off._Sink()))
        object.__setattr__(self, "summary", {})

    class _Sink:
        config = {}

    def __bool__(self):
        return True


def stub_attr(module_name, name):
    """Module __getattr__ for the shim and its stub submodules: exception classes stay usable in except clauses."""
    if name.startswith("__"):
        raise AttributeError(name)
    full = f"{module_name}.{name}".removeprefix("wandb.")
    if name[:1].isupper() and name.endswith(("Error", "Exception")):
        return type(name, (Exception,), {})
    if name[:1].isupper():  # a class, so isinstance() checks and subclassing work
        return type(name, (_Stub,), {"__init__": lambda self, *a, **kw: _Stub.__init__(self, full)})
    return _Stub(full)


def __getattr__(name):
    return stub_attr("wandb", name)
