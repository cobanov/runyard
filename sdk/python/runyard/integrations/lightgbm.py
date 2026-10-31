"""LightGBM: lgb.train(params, dtrain, valid_sets=[dval], callbacks=[runyard_callback(project="tabular")])."""
from .. import init


def runyard_callback(project=None, name=None, **init_kwargs):
    """A LightGBM callback: one run per training, config is the params, every eval result is logged as data/metric at
    step=iteration. Other keyword arguments go to runyard.init.
    """
    state = {}

    def callback(env):
        if env.iteration == env.begin_iteration:
            state["run"] = init(project, name, dict(env.params), **init_kwargs)
        run = state["run"]
        # (data, metric, value, is_higher_better), plus stdv from lgb.cv
        run.log({f"{r[0]}/{r[1]}": r[2] for r in env.evaluation_result_list}, step=env.iteration)
        # shortcut: early stopping ends training before the last iteration, so that run is finished at exit instead;
        # upgrade when someone needs the status right after training (wrap lgb.train, or catch EarlyStopException).
        if env.iteration == env.end_iteration - 1:
            run.finish()

    return callback
