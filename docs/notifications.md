# Notifications and checks

## Channels

```sh
NTFY_TOKEN=tk_... runyard-server channel add ntfy https://ntfy.sh/my-topic
runyard-server channel add -project sft -events failed,crashed webhook https://example.com/hook
runyard-server channel add slack https://hooks.slack.com/services/...
runyard-server channel add discord https://discord.com/api/webhooks/...
TELEGRAM_TOKEN=123:abc runyard-server channel add telegram CHAT_ID
runyard-server channel ls
runyard-server channel test 1
runyard-server channel rm 1
```

The web UI's Settings page does the same for admins: add a channel, test it, remove it.

Events: `finished`, `failed`, `crashed`, `alert`; `-events` picks some, `-project` limits a channel to one project.
Messages carry the last metrics (or the last output lines of a failed run), the latest sample image where the
channel can show it, and a link to the run when `RUNYARD_URL` is set. Tokens are read from the environment so
they stay out of the process list. A webhook gets the whole event as JSON.

Channels that hear `alert` for every project also hear about machines: "toji went silent" when its
[`runyard beacon`](server.md#machines) has sent nothing for 2 minutes, and "toji is back" when it sends again (event
`host` in a webhook's JSON). They also hear "gba is waiting for you" when a [coding agent](agents.md) has been stuck at
a permission prompt for 2 minutes, once per prompt (event `agent`). That takes its hooks; a prompt answered within
the 2 minutes stays quiet. Claude Code only says a prompt was answered when the command exits, so a long command you
approved can notify too, unless it is a run: a run starting ends the wait.

## Web push

Tap the bell in the web UI's header to get notifications on that device, from the browser or the installed app
(on iOS, add it to the home screen first). It needs HTTPS or localhost. Each device is a channel of kind `webpush`
for every event; the server signs with its own VAPID key, made on first start. A subscription the browser drops
is deleted. With `RUNYARD_AUTH` on, a device gets only the runs its user may see.

## Checks

Checks raise alerts from the data itself, once per run, through the channels that listen to `alert`. One is built
in: any `*loss*` metric going NaN or ±Inf. Add your own per project:

```sh
runyard-server check add -project sft -after 100 above train/loss 10   # diverged, after warmup
runyard-server check add -project sft below eval/accuracy 0.2
runyard-server check add -project gba stall 30m   # heartbeats, but no metric or output: stuck
runyard-server check ls
runyard-server check rm 2
```

`*` in a metric name matches anything. Data older than five minutes (a `runyard sync` of an old run) is not
checked. A script raises its own with `run.alert(title, body, level)`.
