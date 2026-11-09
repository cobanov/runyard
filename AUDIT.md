# runyard audit

Audited 2026-11-09 at commit `c253e8f`. This file is the list of what is wrong or missing and what happened to
it; it is kept current. "Fixed" means the change is committed and was seen working (how is in the last section).

**How.** A fresh install from the README on an empty machine-local Compose project, a new virtualenv with the
wheel, a real PyTorch training, ai-toolkit's own `toolkit/logging_aitk.py` driven through the W&B bridge, the
commonly used wandb surface called one by one, a 200,000-step run for timings, the server killed and unreachable
on purpose, and the web UI walked in Chrome. The server, the web UI and both apps were also read in full by
three independent reviews; their findings are marked *read* until reproduced.

**Impact** is for a team that deploys one server and watches many long runs. **Effort**: S under about 30
lines, M a few files, L more.

## What already worked

Worth saying, because the list below is long: the README's two commands gave a working server; three lines of
Python logged a run with images, logs, CPU and RAM; `log()` cost 2.7 µs a call and never blocked; a clean exit,
an exception and `sys.exit(3)` became finished, failed and failed; ai-toolkit's logger, unchanged, wrote loss,
learning rate and its sample images through the bridge; a 200,000-step run charted from a 141 kB response
because the server downsamples; SQL is parameterized everywhere, passwords and keys are hashed, and team scoping
holds for canonical run ids.

## Logging library and integrations

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| L1 | `with wandb.init() as run:` raised `TypeError` in the bridge | High | S | Fixed |
| L2 | `wandb.init(mode="disabled")`, `WANDB_MODE=disabled`, `WANDB_DISABLED` still created a run (in project `default`) | Medium | S | Fixed |
| L3 | `except wandb.errors.CommError:` raised `TypeError`: the name was not a class | Medium | S | Fixed |
| L4 | `wandb.Artifact`, `log_artifact`, `wandb.save` were dropped, so a migrated script lost its checkpoint list | Medium | S | Fixed: recorded where they are, not uploaded |
| L5 | `wandb.run.name = ...` and `.tags = ...` never reached the server | Low | S | Fixed |
| L6 | The bridge needed `RUNYARD_WANDB=1` even when wandb was not installed and the import would fail anyway | High | S | Fixed: on with `RUNYARD_API_KEY` set and no wandb; `RUNYARD_WANDB=0` turns it off |
| L7 | No way to use the bridge from code you can change without the environment switch | Medium | S | Fixed: `import runyard.wandb as wandb` |
| L8 | A process killed while the server was away lost everything queued: the queue lived in memory until a clean exit | High | S | Fixed: written to the cache every 30 s while the server is away |
| L9 | Pending runs were only sent by `runyard sync`, which nobody remembers | High | S | Fixed: the next run on the machine sends them in the background |
| L10 | The queue grew without bound while the server was away | Low | S | Fixed: stops at a million calls, with one warning |
| L11 | A run synced later showed the sync time as its end | Low | S | Fixed |
| L12 | Not recorded: command line, Python, OS, hardware, uncommitted diff | Medium | S | Fixed: on the run's Config tab; values of secret-looking arguments are left out |
| L13 | `runyard sync` could only send to the address the run was started with | Low | S | Fixed: `runyard sync --server` |
| L14 | A test passed only with `RUNYARD_API_KEY` in the environment | Low | S | Fixed |
| L15 | Resuming: `wandb.init(id=..., resume=...)` and `WANDB_RUN_ID` are ignored (a restart is a new run), and `ry.init(id=<existing>)` restarts log line numbers at 1, so the server drops the new lines as duplicates | Medium | M | Open |
| L16 | ai-toolkit still needs its own `logging: use_wandb: true`; nothing outside ai-toolkit can flip that | Low | – | Documented |
| L17 | No wrapper for a tool that logs nowhere (`runyard run -- python train.py`: output, exit status, GPU, a watched samples directory) | Medium | M | Open |
| L18 | Installed packages (`pip freeze`) are not recorded | Low | S | Open |
| L19 | kohya-ss/sd-scripts not run for real; its call pattern (Accelerate's wandb tracker plus `wandb.Image`) is covered by the bridge tests | Medium | S | Open: needs a GPU box |

## Server: security

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| S1 | Stored XSS: an uploaded `.svg` or `.html` "sample", or an MLflow artifact, is served inline from the app's origin with no `nosniff` or CSP. With `RUNYARD_AUTH`, a team member's upload opened by an admin can mint a server key | High | S | Open |
| S2 | Team scoping bypass: the guard resolves `/runs/<ref>/...` by name when `<ref>` is not a canonical UUID, the handler casts the same text to a UUID. An uppercase UUID plus a run of your own named like the target's id reads and writes a hidden run | High | S | Open |
| S3 | No body limit on `POST /api/v1/login` and the MLflow JSON endpoints; 4 GB on JSON writes | Medium | S | Open |
| S4 | Push subscribe and unsubscribe skip the cross-origin check the other cookie-authenticated changes have | Medium | S | Open |
| S5 | With auth on, media is `Cache-Control: public, immutable`: a shared cache may serve it to anyone with the URL | Medium | S | Open |
| S6 | `limit` and `points` are not clamped; `GET /runs?limit=10000000&metrics=1` needs no key on an open server | Low | S | Open |
| S7 | `POST /api/v1/runs` with an id shorter than 8 characters panics (recovered: a reset connection, not a 400) | Low | S | Open |
| S8 | A failing channel logs its full URL, which holds the Telegram bot token or the Slack webhook secret | Low | S | Open |
| S9 | Login has no rate limit (each try costs 0.2 s of CPU) | Low | S | Open |
| S10 | API keys have one scope. A key for a training box can also delete runs and make keys (no auth) or act as its whole user (auth) | Medium | M | Open: decision 2 |
| S11 | Agent cards with no project are visible to every user with auth on | Low | M | Open |

## Server: speed and growth

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| P1 | Every run's metric summary (last, min, max per key) is computed by scanning and sorting all its metric rows: 0.4 s for one 200,000-step run, on every page poll, for 200 runs at once in lists, in every phone poll | High | M | Open |
| P2 | Nothing is compressed; a run page's chart data is 141 kB every 4 seconds, the apps fetch it on cellular | Medium | S | Open |
| P3 | Runs lists stop at the newest 200 with no way to see the rest | High | M | Open |
| P4 | Deleting a project scans the whole metrics table once per metric key (the `metrics.key_id` foreign key has no index) | Medium | S | Open |
| P5 | One notification channel that hangs uses up the 60 seconds all channels share | Medium | S | Open |
| P6 | A check's key pattern is compiled again for every value of every batch | Low | S | Open |
| P7 | Logs, metrics, alerts and agent sessions are never trimmed; 100 lines a second is 8.6 million rows a day | Medium | M | Open |
| P8 | No graceful shutdown: an upgrade can lose a "finished" notification that was being sent | Low | S | Open |
| P9 | Downsampling a long series still reads all of it (1 s for four keys of 200,000 steps) | Medium | L | Open |

## Server: behaviour

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| B1 | A heartbeat tells no open page, so "Heartbeat 14m ago" stays on a live quiet run and a revived run stays "crashed" until reload | High | S | Open |
| B2 | "No progress" exists only as an alert: a stuck run keeps its healthy running dot everywhere | Medium | S | Open |
| B3 | A run name with an uppercase letter can never be found by name (CLI, MCP); a name that looks like hex picks another run by id prefix | Medium | S | Open |
| B4 | On an open server, making the first API key needs a shell on the server; the web UI can only make keys once it has one | Medium | S | Open: decision 1 |
| B5 | "Recently ended" lists the newest by start time: a five-day run that just ended is missing after 40 newer starts | Low | S | Open |
| B6 | Smaller: a metric value of JSON `null` is stored as 0; a non-object `config` patch corrupts the column; an MLflow overwrite deletes the old file before the new upload lands | Low | S | Open |

## Web UI

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| W1 | One failed poll replaces the run page with an error until a manual reload | High | S | Open |
| W2 | The event stream does not reconnect after an HTTP error (a restart behind a proxy, an expired session): the tab stays "Reconnecting" for good *(read)* | High | S | Open |
| W3 | Server down at page load is a blank page | High | S | Open |
| W4 | Loading and failing both render as "nothing here" ("No metrics yet", "No alerts", "No machines yet") | High | M | Open |
| W5 | Metric columns of running runs in the list never update | High | S | Open |
| W6 | Every metrics event refetches every series and rebuilds every chart; a zoom is lost within seconds on a live run; hidden tabs keep polling | High | M | Open |
| W7 | No progress or time left in the runs list or on the Overview cards (only on the run's own page) | Medium | S | Open |
| W8 | Comparing runs: hovering shows no values, no smoothing or log scale, no samples side by side | High | M | Open |
| W9 | Samples: the viewer's arrows walk every image instead of one prompt across steps; thumbnails are the full files | Medium | M | Open |
| W10 | Logs: only the last 1,000 lines can be reached, no search, all of stderr (tqdm, `logging`) is red | Medium | S | Open |
| W11 | No time axis for charts (step only) | Medium | M | Open |
| W12 | Config values were rounded to four digits, in the diff too | Medium | S | Fixed |
| W13 | Every tab is titled "runyard"; filters and search are lost on Back | Medium | S | Open |
| W14 | First visit says "Nothing is training right now" and no more; no way to hand the address to the phone | Medium | S | Open |
| W15 | Light mode: the running and crashed words are under 4.5:1 on a panel *(read)* | Low | S | Open |
| W16 | An open tab keeps the old UI after a server upgrade until reloaded | Low | S | Open |

## Phone apps

All *read*: neither app was changed before this pass.

| # | Finding | Where | Impact | Effort | Status |
|---|---|---|---|---|---|
| A1 | No native notifications of any kind | both | High | M | Open: decision 3 |
| A2 | A run that ends vanishes from the home screen; alerts show only on their own tab, with no badge | both | High | S | Open |
| A3 | A failed refresh is silent, and after 90 seconds offline the app shows the run as silent when it is the phone that lost contact | both | High | S | Open |
| A4 | At most four samples per key, each a quarter of the screen wide | Android | High | S | Open |
| A5 | No full-screen, zoomable sample; scrubbing blanks the image; the slider stops following new steps | both | High | M | Open |
| A6 | Nothing is shown offline, and the error screen does not retry | both | High | M | Open |
| A7 | The home screen waits for one request per running run, in a row, before it draws | both | Medium | S | Open |
| A8 | The address is typed by hand; no QR, no link | both | Medium | S | Open |
| A9 | Rotating or going back loses your place; polling continues in the background | Android | Medium | S | Open |
| A10 | A NaN under a `sys/` key crashes the home screen | both | Medium | S | Open |
| A11 | Every connect failure reads "No runyard server answered"; a silent fallback to http | both | Medium | S | Open |
| A12 | Delete can never work on an open server, and says "sign in as an admin" where there is no sign-in | both | Medium | S | Open |
| A13 | One server at a time | both | Medium | M | Open |
| A14 | Neither app's tests run in CI | both | Low | S | Open |

## Deployment and docs

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| D1 | Compose builds from source; there is no pinned image line to upgrade by, and no documented upgrade or restore | Medium | S | Open |
| D2 | The database password is the fixed `runyard` (not published outside the Compose network) | Low | S | Open |
| D3 | The README says the bridge needs `RUNYARD_WANDB=1` and has no ai-toolkit or kohya recipe | Medium | S | Open |

## Decisions that are yours

1. **First API key from the browser (B4).** The smallest fix lets whoever first opens a fresh server without
   users make its first key. That is how most self-hosted tools start, and the port is bound to localhost by
   default, but it is a window on a server exposed before you open it.
2. **Scoped keys (S10).** A write-only key for training machines changes what existing keys mean, or adds a
   kind. Not started.
3. **Instant push on iPhone (A1).** Apple only delivers pushes signed with the app's own key, which a
   self-hosted server cannot hold. Either you run a small relay for every self-hoster (uptime, abuse, and it
   sees notification titles unless they are encrypted end to end), or the app polls in the background and
   notifies late, or people keep using ntfy.
4. **Retention (P7).** What to trim by default, if anything: log lines per run, runs older than N days.

## Not checked

A real GPU training with ai-toolkit or kohya (the Mac has no CUDA); the server at tens of millions of metric
rows; `RUNYARD_AUTH` with several teams beyond the existing tests; S3 media; the Slack, Discord and Telegram
channels; the MCP tools.
