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
| L20 | `config=vars(args)` sent `--wandb_api_key` and `--hf_token` values to the server, onto the Config tab (found running kohya's pattern) | High | S | Fixed: masked, in the config and the recorded command line |
| L15 | Resuming: `wandb.init(id=..., resume=...)` and `WANDB_RUN_ID` are ignored (a restart is a new run), and `ry.init(id=<existing>)` restarts log line numbers at 1, so the server drops the new lines as duplicates | Medium | M | Fixed: `ry.init(id=..., resume=True)`; in the bridge `wandb.init(id=...)` and `WANDB_RUN_ID` continue the run |
| L16 | ai-toolkit still needs its own `logging: use_wandb: true`; nothing outside ai-toolkit can flip that | Low | – | Documented |
| L17 | No wrapper for a tool that logs nowhere (`runyard run -- python train.py`: output, exit status, GPU, a watched samples directory) | Medium | M | Fixed: `runyard run [--samples DIR] -- COMMAND` |
| L18 | Installed packages (`pip freeze`) are not recorded | Low | S | Fixed: on the Config tab |
| L19 | kohya-ss/sd-scripts and ai-toolkit were not run on a GPU. What ran here: ai-toolkit's own logger module, Accelerate's real wandb tracker with kohya's sample-image calls, transformers' real `WandbCallback` | Medium | S | Open: one real LoRA run on a GPU box |

## Server: security

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| S1 | Stored XSS: an uploaded `.svg` or `.html` "sample", or an MLflow artifact, is served inline from the app's origin with no `nosniff` or CSP. With `RUNYARD_AUTH`, a team member's upload opened by an admin can mint a server key | High | S | Fixed: sandboxed and never sniffed; test uploads an .svg |
| S2 | Team scoping bypass: the guard resolves `/runs/<ref>/...` by name when `<ref>` is not a canonical UUID, the handler casts the same text to a UUID. An uppercase UUID plus a run of your own named like the target's id reads and writes a hidden run | High | S | Fixed: reproduced in a test (200 with the other team's logs), then closed |
| S3 | No body limit on `POST /api/v1/login` and the MLflow JSON endpoints; 4 GB on JSON writes | Medium | S | Fixed: 1 MB, 64 MB, 64 MB |
| S4 | Push subscribe and unsubscribe skip the cross-origin check the other cookie-authenticated changes have | Medium | S | Fixed |
| S5 | With auth on, media is `Cache-Control: public, immutable`: a shared cache may serve it to anyone with the URL | Medium | S | Fixed: `private` with auth on |
| S6 | `limit` and `points` are not clamped; `GET /runs?limit=10000000&metrics=1` needs no key on an open server | Low | S | Fixed: 1,000 runs, 10,000 points |
| S7 | `POST /api/v1/runs` with an id shorter than 8 characters panics (recovered: a reset connection, not a 400) | Low | S | Fixed: 400 |
| S8 | A failing channel logs its full URL, which holds the Telegram bot token or the Slack webhook secret | Low | S | Fixed |
| S9 | Login has no rate limit (each try costs 0.2 s of CPU) | Low | S | Fixed: ten wrong passwords for a name from one address, then 429 for five minutes |
| S10 | API keys have one scope. A key for a training box can also delete runs and make keys (no auth) or act as its whole user (auth) | Medium | M | Fixed: a key can be made for logging only (`scope: log`, `key create -log`); it gets 403 for deleting and for keys and channels. Old keys keep everything |
| S11 | Agent cards with no project are visible to every user with auth on | Low | M | Fixed: admins only |

## Server: speed and growth

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| P1 | Every run's metric summary (last, min, max per key) is computed by scanning and sorting all its metric rows: 0.4 s for one 200,000-step run, on every page poll, for 200 runs at once in lists, in every phone poll | High | M | Fixed: a summary row per run and metric, written with the metrics; 408 ms to 1.2 ms |
| P2 | Nothing is compressed; a run page's chart data is 141 kB every 4 seconds, the apps fetch it on cellular | Medium | S | Fixed: gzip; 84.7 kB to 13.8 kB |
| P3 | Runs lists stop at the newest 200 with no way to see the rest | High | M | Fixed: `limit`, `before=`, `q=`; the list has Show more and searches on the server |
| P4 | Deleting a project scans the whole metrics table once per metric key (the `metrics.key_id` foreign key has no index) | Medium | S | Fixed: the foreign key is dropped |
| P5 | One notification channel that hangs uses up the 60 seconds all channels share | Medium | S | Fixed: 15 s per channel |
| P6 | A check's key pattern is compiled again for every value of every batch | Low | S | Fixed |
| P7 | Logs, metrics, alerts and agent sessions are never trimmed; 100 lines a second is 8.6 million rows a day | Medium | M | Fixed for output: a run keeps its newest million lines (`RUNYARD_LOG_LINES`); stale agent sessions go after a month. Metrics and alerts go with their runs |
| P8 | No graceful shutdown: an upgrade can lose a "finished" notification that was being sent | Low | S | Fixed: ten seconds on SIGTERM |
| P9 | Downsampling a long series still reads all of it (1 s for four keys of 200,000 steps) | Medium | L | Fixed: a 100-step rollup kept as metrics arrive; a chart of a long metric reads that |

## Server: behaviour

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| B1 | A heartbeat tells no open page, so "Heartbeat 14m ago" stays on a live quiet run and a revived run stays "crashed" until reload | High | S | Fixed |
| B2 | "No progress" exists only as an alert: a stuck run keeps its healthy running dot everywhere | Medium | S | Fixed: `progress_at` on runs; lists, cards and the run page say "no output for 14m" after ten minutes |
| B3 | A run name with an uppercase letter can never be found by name (CLI, MCP); a name that looks like hex picks another run by id prefix | Medium | S | Fixed |
| B4 | On an open server, making the first API key needs a shell on the server; the web UI can only make keys once it has one | Medium | S | Fixed: a server without users and without a key lets the Settings page make the first one, once |
| B5 | "Recently ended" lists the newest by start time: a five-day run that just ended is missing after 40 newer starts | Low | S | Fixed: `sort=ended` |
| B6 | Smaller: a metric value of JSON `null` is stored as 0; a non-object `config` patch corrupts the column; an MLflow overwrite deletes the old file before the new upload lands | Low | S | Fixed: both are 400; the MLflow overwrite removes the old file after the new one landed |

## Web UI

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| W1 | One failed poll replaces the run page with an error until a manual reload | High | S | Fixed |
| W2 | The event stream does not reconnect after an HTTP error (a restart behind a proxy, an expired session): the tab stays "Reconnecting" for good *(read)* | High | S | Fixed: reproduced behind a proxy answering 502 for twelve seconds; the stream came back three seconds after it stopped |
| W3 | Server down at page load is a blank page | High | S | Fixed |
| W4 | Loading and failing both render as "nothing here" ("No metrics yet", "No alerts", "No machines yet") | High | M | Fixed on the Overview and the run page; one line under the top bar says the server is not answering |
| W5 | Metric columns of running runs in the list never update | High | S | Fixed |
| W6 | Every metrics event refetches every series and rebuilds every chart; a zoom is lost within seconds on a live run; hidden tabs keep polling | High | M | Fixed: charts take new points in place (same canvas, zoom kept, seen in Chrome), only the open tab's series are fetched, hidden tabs ask nothing |
| W7 | No progress or time left in the runs list or on the Overview cards (only on the run's own page) | Medium | S | Fixed |
| W8 | Comparing runs: hovering shows no values, no smoothing or log scale, no samples side by side | High | M | Fixed: values on hover, smoothing, log scale, samples side by side with one step slider |
| W9 | Samples: the viewer's arrows walk every image instead of one prompt across steps; thumbnails are the full files | Medium | M | Fixed: the viewer follows one image through the steps; grids load 640 px copies (`?w=`) |
| W10 | Logs: only the last 1,000 lines can be reached, no search, all of stderr (tqdm, `logging`) is red | Medium | S | Fixed: find, Earlier lines, red only for lines that name a failure |
| W11 | No time axis for charts (step only) | Medium | M | Fixed: Time axis on the run page and when comparing |
| W12 | Config values were rounded to four digits, in the diff too | Medium | S | Fixed |
| W13 | Every tab is titled "runyard"; filters and search are lost on Back | Medium | S | Fixed: tabs are named; Back finds the list's filter, search, order and picks, and a run opens on the last tab used |
| W14 | First visit says "Nothing is training right now" and no more; no way to hand the address to the phone | Medium | S | Fixed: a first-run pointer to Settings (not seen on a server with runs), a QR for the phone apps, the beacon line |
| W15 | Light mode: the running and crashed words are under 4.5:1 on a panel *(read)* | Low | S | Fixed: 5.47:1 and 5.02:1 |
| W16 | An open tab keeps the old UI after a server upgrade until reloaded | Low | S | Fixed: the page says the server was upgraded and offers to reload |

## Phone apps

Found by reading; the fixes were then run on the iPhone 18 Pro simulator and an Android 16 emulator against a local server.

| # | Finding | Where | Impact | Effort | Status |
|---|---|---|---|---|---|
| A1 | No native notifications of any kind | both | High | M | Fixed as far as it goes without a relay (decision 3): both apps look in the background and post local notifications, also for a run that started and ended between two looks. Android about every 15 minutes, seen on the emulator; iOS when it allows, the background task itself not seen |
| A2 | A run that ends vanishes from the home screen; alerts show only on their own tab, with no badge | both | High | S | Fixed: Recent under Running, a badge on Alerts |
| A3 | A failed refresh is silent, and after 90 seconds offline the app shows the run as silent when it is the phone that lost contact | both | High | S | Fixed on the home, run and list screens of both |
| A4 | At most four samples per key, each a quarter of the screen wide | Android | High | S | Fixed |
| A5 | No full-screen, zoomable sample; scrubbing blanks the image; the slider stops following new steps | both | High | M | Fixed: a full-screen viewer; pinch zoom ran but no screenshot shows it |
| A6 | Nothing is shown offline, and the error screen does not retry | both | High | M | Fixed: the home screen opens from the last answers when the server is out of reach, and the unreachable screen retries |
| A7 | The home screen waits for one request per running run, in a row, before it draws | both | Medium | S | Fixed |
| A8 | The address is typed by hand; no QR, no link | both | Medium | S | Fixed: `runyard://connect?url=` from the web UI's QR, `runyard://run/<id>`. Opened by link on both; the camera path was not tried |
| A9 | Rotating or going back loses your place; polling continues in the background | Android | Medium | S | Fixed |
| A10 | A NaN under a `sys/` key crashes the home screen | both | Medium | S | Fixed: unit tests on both |
| A11 | Every connect failure reads "No runyard server answered"; a silent fallback to http | both | Medium | S | Fixed: the reason is shown, `:8080` is tried, http is labelled |
| A12 | Delete can never work on an open server, and says "sign in as an admin" where there is no sign-in | both | Medium | S | Fixed: hidden there |
| A13 | One server at a time | both | Medium | M | Fixed: both apps keep a list of servers with their sessions; tap to switch |
| A14 | Neither app's tests run in CI | both | Low | S | Partly: Android's run in CI; iOS's need an Xcode hosted runners may not have |
| A15 | A tap on an iOS notification did not open the run in the one automated try (the same code path opens it from a link) | iOS | Medium | S | Fixed: the tap crashed the app (the delegate returned off the main thread); a UI test taps the banner, warm and cold |
| A16 | A run that starts and ends between two background looks is never notified | both | Low | S | Fixed: unit tests on both |
| A17 | Android: the heartbeat mark renders as a red emoji, so its state colour never shows; video is still a line of text | Android | Low | S | Fixed: the heart takes the state colour, videos play inline and GIFs move; heartbeat age is measured on the server's clock |

## Design (2026-11-10)

The first two passes fixed what was broken and added what was missing, and left the screens looking as they had,
with the new parts bolted on. The owner said so. This pass applied `DESIGN.md` screen by screen, looking at each
in light and dark.

| # | Finding | Where | Status |
|---|---|---|---|
| U1 | Colour was not state only: teal marked links, buttons and the selected tab, a busy CPU bar turned amber, a finished run was green | apps | Fixed: ink for anything you can press, teal, red and amber only for a state, grey for finished |
| U2 | The phone's home screen was titled "Projects" and a running card was five bands with a box inside a box and no sample | apps | Fixed: "Runs" with a line that sums up, the active alert first, a card that reads number, curve, newest sample, progress, machine |
| U3 | A run's name was said twice, its charts had frames and dashed lines with the latest value small and grey, axis labels used the device's decimal comma | apps | Fixed |
| U4 | Each prompt had its own slider and a lone image; on the web a 290 px image sat beside an empty page | web, apps | Fixed: one scrubber for every key, the images side by side, two across on a phone |
| U5 | The web's running card had no samples and no progress bar; "recently ended" said "0s, 1m 35s ago" | web | Fixed |
| U6 | The runs list's search floated above its title and the table scrolled sideways, on a phone too | web | Fixed: one toolbar, fewer columns, a list below 640px |
| U7 | The connect screen's button looked like a field; the Server tab repeated its title and explained notifications in a paragraph | apps | Fixed |
| U8 | Android set type with Material's wide tracking and tall lines | Android | Fixed in the theme |
| U9 | The demo on the website showed "the server was upgraded" for no reason and broken sample pictures (introduced on 2026-11-09) | web | Fixed |
| U10 | The apps use the system font, the web Atkinson Hyperlegible | apps | Open: bundling the font adds a megabyte to each app |
| U11 | Not seen on screen after the redesign: "no output for", a crashed run's card, the apps' empty and error states in dark, a video or a text table in the new sample grid, the chart's touch readout | apps | Open: needs a hand on a device |

## Deployment and docs

| # | Finding | Impact | Effort | Status |
|---|---|---|---|---|
| D1 | Compose builds from source; there is no pinned image line to upgrade by, and no documented upgrade or restore | Medium | S | Fixed: compose runs `ghcr.io/cobanov/runyard:${RUNYARD_VERSION:-latest}` with a healthcheck, `--build` builds the checkout; backup, restore and upgrade are documented |
| D2 | The database password is the fixed `runyard` (not published outside the Compose network) | Low | S | Fixed: `POSTGRES_PASSWORD`, default unchanged |
| D3 | The README says the bridge needs `RUNYARD_WANDB=1` and has no ai-toolkit or kohya recipe | Medium | S | Fixed |

## Decisions, as made on 2026-11-09

The owner asked for these to be decided rather than left open.

1. **First API key from the browser: yes.** Only on a server without users and without a single key, and the
   door shuts with the first key. The port is bound to localhost by default; a server exposed before anyone
   opened it is the window, as with every self-hosted tool that starts this way.
2. **Scoped keys: one new scope.** A key is either full or for logging. Finer scopes (per project, read-only)
   wait for someone to need them.
3. **Instant push on iPhone: no relay.** Apple only delivers pushes signed with the app's key, so it would mean
   running a service every self-hoster depends on and that sees their notification titles. The apps look in the
   background and notify late; ntfy and web push from the server stay the instant path.
4. **Retention: output only.** A run keeps its newest million lines by default. Nothing else is deleted on its
   own: metrics and samples are what people come back for.
5. **Compose runs the released image.** `docker compose up -d` pulls it and `--build` builds the checkout, which
   is what the lab and the deploy on toji already pass.
6. **A default stall check: yes, at an hour.** No metric and no output line for sixty minutes with heartbeats
   arriving is a warning, once per run. MLflow runs are exempt; `check rm` removes it.

## How the fixes were checked

- `make test`: the Go tests and the Python tests against a real server and PostgreSQL, 30 passed. New tests cover
  the scoping bypass (it returned the other team's logs before the fix), an uploaded `.svg`, list paging, search and
  end order, earlier log lines, gzip, the bridge's new surface and when it is on, a process killed while the server
  is away and the next run sending its file, the recorded environment and masked secrets.
- A copy of a real 0.6 database (migrations 001 to 009, 31 runs) was started with the new server: migrations 010
  and 011 applied, and the summary table matched the old computed summary for all 62 run and metric pairs.
- Timings on the 200,000-step run, before and after: run detail 408 ms to 1.2 ms, chart data 0.89 s to 0.23 s and
  84.7 kB to 13.8 kB on the wire.
- In Chrome against the rebuilt server: a chart kept its canvas and a dragged zoom through two refreshes of a live
  run; the server was stopped for ten seconds and the page kept its content, said so, and recovered by itself;
  a hidden tab made no requests; comparing runs showed values on hover and samples side by side; a comparison
  with a deleted run showed the rest; log search and the QR code rendered.
- The apps: screenshots of the home screen with Recent, the sample viewer, the stale line after 100 seconds
  offline, the unreachable screen coming back by itself, connecting by link, and a "finished" notification on
  both; on Android the notification's tap opened the run. Unit tests: 9 on iOS, 13 on Android.
- Through the bridge with no `wandb` installed and only the two settings: ai-toolkit's `toolkit/logging_aitk.py`
  (loss, learning rate, eight sample images at their steps), Accelerate's wandb tracker used the way kohya does,
  and transformers' `WandbCallback` on a small `Trainer`.

## Not checked

A real GPU training with ai-toolkit or kohya (the Mac has no CUDA); the server at tens of millions of metric
rows; `RUNYARD_AUTH` with several teams beyond the existing tests; S3 media; the Slack, Discord and Telegram
channels; the MCP tools.
