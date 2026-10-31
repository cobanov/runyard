"""scikit-learn: runyard.integrations.sklearn.autolog(project="tabular"), then every fit() is a run."""
import functools
import time

from sklearn.utils import all_estimators

from .. import init, warn

_patched = {}  # class -> its own fit, put back by autolog(disable=True)
_depth = 0  # fits in progress, so a meta-estimator's inner fits stay inside its run


def autolog(project=None, disable=False, **init_kwargs):
    """Each outer fit() of a scikit-learn estimator becomes a run named after its class: config is
    get_params(deep=False), metrics are fit_seconds and, when y is given, train/score. A fit that raises marks its
    run failed. autolog(disable=True) puts the original fit methods back. Other keyword arguments go to runyard.init.
    """
    for cls, fit in _patched.items():
        cls.fit = fit
    _patched.clear()
    if disable:
        return
    # shortcut: only fit() is wrapped, so a fit_transform that skips fit() (PCA, TruncatedSVD) is not a run, and fits
    # running at once in several threads make one run; wrap those too if anyone uses them that way.
    for _, cls in all_estimators():
        # the class that defines fit: GridSearchCV and the forests inherit it from abstract bases all_estimators skips
        owner = next((c for c in cls.__mro__ if "fit" in vars(c)), None)
        if owner is not None and owner not in _patched:
            _patched[owner] = vars(owner)["fit"]
            owner.fit = _wrap(owner.fit, project, init_kwargs)


def _wrap(fit, project, init_kwargs):
    @functools.wraps(fit)
    def wrapped(self, *args, **kwargs):
        global _depth
        if _depth:
            return fit(self, *args, **kwargs)
        run = init(project, type(self).__name__, self.get_params(deep=False), **init_kwargs)
        _depth += 1
        try:
            t = time.time()
            out = fit(self, *args, **kwargs)
            values = {"fit_seconds": time.time() - t}
            X = args[0] if args else kwargs.get("X")
            y = args[1] if len(args) > 1 else kwargs.get("y")
            if y is not None and hasattr(self, "score"):
                try:
                    values["train/score"] = self.score(X, y)
                except Exception as e:  # scoring is ours: it must not break the user's fit
                    warn(f"{type(self).__name__}.score failed: {e!r}")
            run.log(values, step=0)
            run.finish()
            return out
        except BaseException:
            run.finish("failed")
            raise
        finally:
            _depth -= 1

    return wrapped
