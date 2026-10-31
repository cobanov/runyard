# Upstream integration drafts

Two patches that make runyard a built-in reporting target (plan.md §13, v0.3).

- **accelerate:** sent as [huggingface/accelerate#4483](https://github.com/huggingface/accelerate/pull/4483) (2026-10-31).
- **transformers:** not sent. Its PR template and `.ai/AGENTS.md` ask first-time contributors not to submit
  PRs written by a code agent, and such PRs get closed and their authors may be blocked. This patch was
  written with one, so it goes in only if a maintainer asks for it (for example after the accelerate PR, or
  through an issue), or as a PR the author writes themselves.

| Patch | Upstream | Base commit | Result |
|---|---|---|---|
| [transformers.patch](transformers.patch) | huggingface/transformers | `66f51e76efd8` (2026-10-30) | `TrainingArguments(report_to="runyard")` |
| [accelerate.patch](accelerate.patch) | huggingface/accelerate | `3008af926ffe` (2026-10-30) | `Accelerator(log_with="runyard")` |

**The PRs need runyard on PyPI first.** Both libraries detect the integration with `find_spec("runyard")`
and tell users to `pip install runyard`, and accelerate's `test_trackers` extra installs it. Until the first
release (see [RELEASING.md](../RELEASING.md)) that name does not resolve, and reviewers will check it.

Apply to a fresh clone and re-test before opening a PR, since both repos move fast:

```sh
git clone https://github.com/huggingface/transformers && cd transformers
git apply ../runyard/upstream/transformers.patch      # 3-way: git apply -3
```

## How they were tested

Re-checked on 2026-10-31 against `runyard==0.3.0` from PyPI: both patches still apply to the base commits above,
the unit tests pass, and transformers' `make check-repo` passes except `types`, which fails in
`cli/serving/response.py`, a file the patch does not touch.

Both clones were installed editable in one venv (Python 3.12, torch 2.15.0, transformers 5.23.0.dev0,
accelerate 1.16.0.dev0), with runyard installed editable from this repo. A server built from this repo ran on a
fresh database of the `make test` Postgres container, with a key from `runyard-server key create smoke`.

- A 1-layer GPT-2 `Trainer` with `report_to="runyard"`, `run_name="smoke-trainer"`, 6 steps, eval every 3.
- `Accelerator(log_with="runyard")` with `init_trackers("upstream-smoke", config=..., init_kwargs={"runyard": {"name": "smoke-accelerate", "tags": ["smoke"]}})`,
  5 logged steps, one `log_images` call, `end_training()`.

`curl $SERVER/api/v1/runs` afterwards (trimmed):

```
{"project":"upstream-smoke","name":"smoke-accelerate","status":"finished","config":{"lr":0.01,"steps":5},"tags":["smoke"],...}
{"project":"upstream-smoke","name":"smoke-trainer","status":"finished","config":{...TrainingArguments...,"model":{"model_type":"gpt2","n_layer":1,...}},
 "summary":{"total_flos":3815424.0,"train_loss":3.407,"train_runtime":0.0898,"train_steps_per_second":66.847,"train_samples_per_second":267.389},...}
```

Metrics: the Trainer run has `train/loss`, `train/grad_norm`, `train/learning_rate`, `train/epoch` at steps
2, 4, 6 and `eval/loss`, `eval/runtime`, ... at steps 3, 6; the Accelerate run has `train/loss` at steps 0–4
and one `samples` image at step 4.

The unit tests in each patch pass (`pytest tests/trainer/test_trainer_callback.py -k Runyard`: 2 passed;
`pytest tests/test_tracking.py -k "Runyard or runyard"`: 2 passed), and the changed files pass each repo's
pinned ruff (`ruff==0.14.10` check + format for transformers, `ruff==0.13.1` for accelerate).
Not run: transformers' `make check-repo` (repo consistency scripts) and either repo's full test suite.

---

## transformers

**Title:** `Add RunyardCallback: report_to="runyard"`

**Description:**

> [runyard](https://github.com/cobanov/runyard) is a lightweight, self-hosted training tracker (Apache-2.0):
> one Go binary plus PostgreSQL, a web UI with live charts, samples and logs, phone notifications when a run
> finishes, fails or crashes, and an MCP endpoint so coding agents can read runs. The Python client is
> `pip install runyard` and has no required dependencies.
>
> This PR adds `RunyardCallback` next to the other reporting integrations, so
> `TrainingArguments(report_to="runyard")` works without passing a callback:
>
> - `is_runyard_available()`, registration in `INTEGRATION_TO_CALLBACK` and in
>   `get_available_reporting_integrations()` (so `report_to="all"` includes it when installed),
>   `require_runyard` in `testing_utils`, exports, docs (`main_classes/callback.md`) and the `report_to` /
>   `run_name` help text.
> - One run per training, started on the main process in `on_train_begin`. Its config is
>   `TrainingArguments.to_dict()` plus the model config under `model`; its name is `run_name`.
> - `on_log` sends metrics keyed by `state.global_step`, renamed with the existing `rewrite_logs`
>   (`train/`, `eval/`, `test/`). The end-of-training scalars (`train_runtime`, `train_loss`, `total_flos`, ...)
>   go to the run summary, as `WandbCallback` and `TrackioCallback` do.
> - Server, API key and project come from `RUNYARD_SERVER`, `RUNYARD_API_KEY`, `RUNYARD_PROJECT`. The client
>   sends from a background thread and never raises into the training loop; run status (finished, failed,
>   crashed), stdout/stderr and GPU usage are recorded by the client itself.
>
> The same callback has shipped in the runyard package as `runyard.integrations.hf.RunyardCallback` and is
> used with Trainer and TRL's `SFTTrainer`.
>
> **Tests:** two unit tests in `tests/trainer/test_trainer_callback.py` with a mocked `runyard` module (metrics
> by step with `train/`/`eval/` keys, summary scalars, config and run name; non-main processes start no run).
> Also tested end to end: a 1-layer GPT-2 `Trainer` with `report_to="runyard"` against a local runyard server,
> where the run, its config, metrics at the expected steps and summary arrived and the run ended `finished`.

**What maintainers usually ask for**

- Whether the integration should live upstream at all. transformers already lists many trackers, and
  maintainers sometimes prefer `callbacks=[ThirdPartyCallback()]` from the third-party package, which runyard
  already offers. Expect this question; the case for merging is `report_to="runyard"` working from configs and
  CLIs (TRL, Axolotl, LLaMA-Factory) that only pass a string.
- `make style` and `make check-repo` (ruff, docstring and export checks) green; the CI "repo consistency" job
  runs these. Not run locally.
- A unit test without network, which the patch has; possibly a `@require_runyard` test that runs a real
  `Trainer` for a few steps.
- Docs: the patch adds the autodoc entry. They may also want a line in `docs/source/en/trainer_callbacks.md`.
- `TrainingArguments.project` is documented as "only used by Trackio". The callback uses `RUNYARD_PROJECT`
  instead; using `args.project` is a one-line change if they prefer it, but its default `"huggingface"` would
  then override `RUNYARD_PROJECT`.

---

## accelerate

**Title:** `Add RunyardTracker: log_with="runyard"`

**Description:**

> [runyard](https://github.com/cobanov/runyard) is a lightweight, self-hosted training tracker (Apache-2.0):
> one Go binary plus PostgreSQL, a web UI with live charts, samples and logs, phone notifications when a run
> finishes, fails or crashes, and an MCP endpoint so coding agents can read runs. The Python client is
> `pip install runyard`.
>
> This PR adds `RunyardTracker`, registered the way the Trackio and SwanLab trackers are:
> `LoggerType.RUNYARD`, `is_runyard_available()`, `_available_trackers`, `LOGGER_TYPE_TO_CLASS`, the
> `log_with` docstrings, `require_runyard`, docs (package reference and usage guide) and the `test_trackers`
> extra.
>
> - `init_trackers(project_name, config=..., init_kwargs={"runyard": {...}})` starts a runyard run in that
>   project; `init_kwargs` go to `runyard.init` (`name`, `tags`, `notes`, ...).
> - `store_init_configuration` writes the config, `log` the metrics at `step`, `log_images` image samples
>   (numpy, PIL or torch), `finish` closes the run with status `finished`.
> - Server and API key come from `RUNYARD_SERVER` and `RUNYARD_API_KEY`.
>
> The same tracker has shipped in the runyard package as `runyard.integrations.accelerate.RunyardTracker`.
>
> **Tests:** `RunyardTrackingTest` (mocked `runyard`: init with project and kwargs, config, log with step,
> finish through `Accelerator`) and `test_runyard_deferred_init` in `TrackerDeferredInitializationTest`, which
> needs runyard installed. Also tested end to end against a local runyard server: config, five steps of
> `train/loss`, an image sample and the `finished` status arrived.

**What maintainers usually ask for**

- `make quality` and `make style` green (ruff 0.13.1 passes on the changed files).
- The deferred-init test, which they ask of every tracker; it is in the patch and needs `runyard` from PyPI,
  hence the `test_trackers` extra entry.
- Docs: the patch adds the reference section and the usage-guide list (and changes "eight trackers" there to
  "nine").
