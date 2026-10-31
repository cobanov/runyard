"""Enough of the wandb API for tools that only speak W&B (ai-toolkit, diffusers examples, HF report_to="wandb")
to write to runyard instead, without changing their code. Turned on by RUNYARD_WANDB=1; see wandb_hook.

Supported: init, log (step/commit), finish, config(.update), summary, run, define_metric (its step_metric is honoured),
Image, Video, Table, alert. Anything else warns once and does nothing.
"""
import os
import sys

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


class AlertLevel:
    INFO, WARN, ERROR = "INFO", "WARN", "ERROR"


class Run:
    def __init__(self, ry_run):
        self._ry = ry_run
        self.id, self.name, self.project, self.url = ry_run.id, ry_run.name, ry_run.project, ry_run.url
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


def init(project=None, name=None, config=None, tags=None, notes=None, id=None, reinit=None, **kwargs):
    global run
    if run is not None and not reinit:
        return run
    if run is not None:
        run.finish()
    if config is not None and not isinstance(config, dict):
        config = vars(config)  # argparse.Namespace and the like
    project = project or os.environ.get("WANDB_PROJECT")
    name = name or os.environ.get("WANDB_NAME") or os.environ.get("WANDB_RUN_NAME")
    tags = list(tags or []) + ([kwargs["group"]] if kwargs.get("group") else [])
    ry_run = core.Run(project, name, dict(config or {}), tags=tags, notes=notes or "")
    run = Run(ry_run)
    _set_globals()
    return run


def _set_globals():
    global config, summary
    config = run.config if run else None
    summary = run.summary if run else None


def log(data, step=None, commit=None, sync=None):
    if run is None:
        core.warn("wandb.log() before wandb.init(), ignored", once=True)
        return
    run.log(data, step=step, commit=commit)


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


class _Stub:
    """Stands in for an unsupported part of wandb: warns once when used, then behaves as a no-op."""

    def __init__(self, name, quiet=False):
        object.__setattr__(self, "_name", name)
        if not quiet:
            core.warn(f"wandb.{name} is not supported by the runyard bridge; ignored", once=True)

    def __call__(self, *args, **kwargs):
        return self

    def __getattr__(self, name):
        return self

    def __setattr__(self, name, value):
        pass

    def __iter__(self):
        return iter(())

    def __bool__(self):
        return False


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
