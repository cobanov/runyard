# runyard for Android

A native Android app for a runyard server, the counterpart of the iPhone app (`ios/`): type the server's address,
sign in if the server has `RUNYARD_AUTH` on, and read your runs. Kotlin and Jetpack Compose, themed with
`DESIGN.md`'s tokens in light and dark.

Get it: [runyard-0.5.0.apk](https://github.com/cobanov/runyard/releases/download/v0.7.0/runyard-0.5.0.apk) from the
v0.7.0 release (allow installs from your browser when Android asks).

- **Runs** (home): what is training, without a tap. Under the title one line sums up ("2 running · 1 failed
  today", or "Nothing is training"); while the server cannot be reached it says how old the screen is instead
  ("Not updated for 3m"), and no run is shown as silent because of it; a project's runs, a run and the alerts say
  the same. Then, in this order: the alerts nobody dismissed, in one row (the newest, "+2 more", tap for the
  Alerts tab); a card per running run; a card per coding agent session from the last day, most urgent first, as
  the kiosk shows them (`docs/agents.md`; a server before 0.5.0 has none); **Recent**, the last 8 runs that are
  not running, each with its project, when it ended, the word "failed" or "crashed" in its colour, and its main
  metric; then every project.
  A running card has the run's name and elapsed time, project and host, the main metric's value with a sparkline
  and, when the run has logged images, its latest one as a thumbnail (`GET /runs/{id}/media?kind=image&latest`,
  loaded 320 pixels wide; tap it for the run's samples), the progress bar with "step 519 / 1,500 · 35%" and the
  time left, and the machine in one line ("GPU 97% · 31.4 GB · 68 °C", "+1" when there are more GPUs, or the CPU
  and RAM). A run that missed three heartbeats, or has logged nothing for ten minutes (`progress_at`), says so in
  amber in place of the breathing mark.
  The last answers are kept on the phone, so the app opens on them when the server is not there at launch (the
  home screen only; signing out or leaving the server drops them). Heartbeats and durations are measured on the
  server's clock (the `Date` header of its answers), so a phone a few minutes off shows no run as silent.
- **A project:** its runs, newest first, with state, duration, when it ended, tags and main metric, and a search
  by name or tag.
- **A run:** the name as the headline (the bar above it names the project), the state and how long it ran, host
  and heartbeat, progress, alerts, then tabs in this order: **Charts** (the main metric first, the rest by name;
  the metric's name and latest value on one line, touch the plot for the value at a step), **Samples**, **Logs**
  (in what is left of the screen; it follows the end until you scroll up; lines that name an error, an exception
  or a traceback are red, whatever stream they came on), **System** (CPU, RAM, a row per GPU, and their charts)
  and **Config** (keys in monospace, values with every digit, never rounded; summary, and the run's id, host,
  tags, start and git).
- **Samples:** every image and video key moves on one slider, over every step any of them logged ("step 1,720 ·
  9 of 13"), and it follows the newest step while it is left at the end. Under it one grid, two a row (a lone
  sample takes the width): each key's images at that step, with the key in monospace and the caption (the prompt)
  under them; a key that logged nothing at that step shows its last one before it and says "from step N". Text
  tables come below, a key and a slider each. An image under 256 pixels is scaled to the nearest pixel so it stays
  crisp. Tap an image for the full screen: swipe between the grid's images, pinch to zoom. The grid asks the
  server for images 640 pixels wide (`?w=640`, which an older server ignores), the full screen for the original.
  A video plays in place when tapped, looping and muted; a GIF moves.
- **Alerts:** the server's alerts, newest first, each with its level as a dot, its title, run, project and age,
  and the first line of what it said; tap one for its run. The tab carries the number of alerts not dismissed yet.
- **Server:** the address the app is on, whether it is plain http and who is signed in (with sign out); every
  server the app has connected to, a check mark on the current one, and **Add a server**; the notifications
  switch; the machines running `runyard beacon` (CPU and RAM in a line, then each GPU with its memory, temperature
  and power, its load and one bar, and under it the processes on it; a silent machine says since when, in amber,
  over its faded last readings); the app's version.

Colour means state and nothing else (`DESIGN.md`): teal, red and amber are a run's or a machine's state, a chart's
line is the chart blue, a load bar is grey whatever its value, and buttons, links and the selected tab are ink.
Numbers use a dot for decimals and tabular figures in every locale.

It reads, and deletes: long-press a project on the home screen or a run in a project's list, then confirm.
Deleting needs an admin (the server says 403 otherwise), so on a server without `RUNYARD_AUTH`, where only an
API key may delete, the app offers none. Screens refresh on their own while the app is in front (every 3–10
seconds while a run is running), and on pull. Rotating the phone and going back keep the place.

## Notifications

The switch on the Server tab makes the phone tell you when a run that was running finished, failed or crashed,
and when a new alert fired; tap one for its run. There are no Google services in this: a `JobScheduler` job
(`Notify.kt`) asks the server for its runs and alerts and compares them with what it saw last time. Android runs
such a job about every 15 minutes at best, and less often while the phone sleeps, so a notification can be that
late. A run that started and ended between two checks is reported too, when it ended after the last check; one
that ended before it (synced later from a machine's offline cache) is not. For instant ones use the server's own
channels (ntfy, Slack, Telegram).

**Try the demo** on the connect screen opens the app on made-up runs with no server: an in-app fake of the same
API (`Demo.kt`) telling `web/demo/mock.js`'s story. Runs keep training while you watch; one full fine-tune
breaks with a NaN loss two and a half minutes in, and a ResNet finishes after about a minute.

## Connecting

The address can be an IP or a host name, with or without a port (`192.168.1.10:8080`, `toji.example.ts.net`).
Without a scheme the app tries https, then http, and without a port it also tries 8080. When nothing answers it
says why for each address it tried: no such host, timed out, could not connect (nothing listens on that port, or
the network cannot reach it: Android's HTTP client does not say which), a certificate that is not trusted, or
something that answered but is not a runyard server. Plain http is allowed
(`network_security_config.xml`), since a home server usually has no certificate; the sign-in and Server screens
then say "http, not encrypted". The address and the session (the same `runyard_session` the web UI gets from
`POST /api/v1/login`) are kept in the app's private storage, for every server the app has connected to: the
Server tab and the connect screen list them, a tap switches to one as it was left (no typing, no sign-in), a
long-press forgets it. **Add a server** on the Server tab goes to the connect screen and keeps the list. On a server without
`RUNYARD_AUTH` there is no sign-in. While the server cannot be reached at launch and nothing was kept of it, the
app keeps trying on its own (after 2 seconds, then twice as long each time, up to 30).

Two links open the app:

- `runyard://connect?url=<percent-encoded server address>` connects to that server, as if the address was typed;
  an app already connected to another server asks first, unless the address is on its list, which it switches
  to. The web UI can show it as a QR.
- `runyard://run/<run id>` opens that run. Notifications use it.

## Build

Needs JDK 17–21 and the Android SDK (platform 36). The Gradle wrapper fetches Gradle 9.8.2.

```sh
cd android
./gradlew testDebugUnitTest assembleDebug
adb install app/build/outputs/apk/debug/app-debug.apk
```

## Release

```sh
android/release.sh   # android/build/dist/runyard-<version>.apk, signed, shrunk with R8
```

The key is `~/.config/runyard/android-release.keystore` (alias `runyard`, or `$RUNYARD_KEYSTORE`), its password in
`~/.config/runyard/android-keystore-password` (or `$RUNYARD_KEYSTORE_PASSWORD`). Neither is in the repository;
without them `assembleRelease` makes an unsigned APK. Keep a copy of both: an app signed with another key cannot
update one already installed. The version is `appVersion` in `app/build.gradle.kts`.

## Tests

`app/src/test/ApiTest.kt` parses the server's JSON shapes (Go's nine-digit dates, NaN as a string), checks the
main-metric rule, the agent cards' order and the number format, what the notification check reports, where a typed
address is looked for, which sample URLs are asked for small, what the screens write (counts, gridlines, the
machine's line, the home summary, config values, the latest sample, the samples grid at a step), and plays the demo's story through the same
parsers.
