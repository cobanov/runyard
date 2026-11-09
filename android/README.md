# runyard for Android

A native Android app for a runyard server, the counterpart of the iPhone app (`ios/`): type the server's address,
sign in if the server has `RUNYARD_AUTH` on, and read your runs. Kotlin and Jetpack Compose, themed with
`DESIGN.md`'s tokens in light and dark.

Get it: [runyard-0.4.0.apk](https://github.com/cobanov/runyard/releases/download/v0.4.0/runyard-0.4.0.apk) from the
v0.4.0 release (allow installs from your browser when Android asks).

- **Projects:** a card per coding agent session from the last day, most urgent first, as the kiosk shows them
  (`docs/agents.md`; a server before 0.5.0 has none, and the section stays away); tap one for its last run. Then a
  card per running run (its main metric with a sparkline, heartbeat, host, and the machine's CPU and per-GPU load),
  then **Recent**, the latest runs that are not running, with status and project, so a run that ends stays in
  sight, then every project with its run count. While the server cannot be reached one line says how old the
  screen is ("Not updated for 3m"), and no run is shown as silent because of it.
- **A project:** its runs, newest first, with status, duration, start and main metric.
- **A run:** status, duration, heartbeat and alerts, then tabs for charts (touch a chart for a value), system load
  (CPU, RAM, and a row per GPU), samples, logs, and config and summary.
- **Samples:** every image of a step, one or two a row, and text tables, with a slider through the steps that
  follows the newest step while it is left at the end. Tap an image for the full screen: swipe between that
  step's images, pinch to zoom. A video is still a line pointing at the web UI.
- **Alerts:** the server's alerts, newest first; tap one for its run. The tab carries the number of alerts not
  dismissed yet.
- **Server:** the machines running `runyard beacon`, the address, notifications, sign out and change server.

It reads, and deletes: long-press a project on the home screen or a run in a project's list, then confirm.
Deleting needs an admin (the server says 403 otherwise), so on a server without `RUNYARD_AUTH`, where only an
API key may delete, the app offers none. Screens refresh on their own while the app is in front (every 3–10
seconds while a run is running), and on pull. Rotating the phone and going back keep the place.

## Notifications

The switch on the Server tab makes the phone tell you when a run that was running finished, failed or crashed,
and when a new alert fired; tap one for its run. There are no Google services in this: a `JobScheduler` job
(`Notify.kt`) asks the server for its runs and alerts and compares them with what it saw last time. Android runs
such a job about every 15 minutes at best, and less often while the phone sleeps, so a notification can be that
late, and a run that started and ended between two checks is not reported. For instant ones use the server's own
channels (ntfy, Slack, Telegram).

**Try the demo** on the connect screen opens the app on made-up runs with no server: an in-app fake of the same
API (`Demo.kt`) telling `web/demo/mock.js`'s story. Runs keep training while you watch; one full fine-tune
breaks with a NaN loss two and a half minutes in, and a ResNet finishes after about a minute.

## Connecting

The address can be an IP or a host name, with or without a port (`192.168.1.10:8080`, `toji.example.ts.net`).
Without a scheme the app tries https, then http, and without a port it also tries 8080. When nothing answers it
says why for each address it tried: no such host, timed out, connection refused, a certificate that is not
trusted, or something that answered but is not a runyard server. Plain http is allowed
(`network_security_config.xml`), since a home server usually has no certificate; the sign-in and Server screens
then say "http, not encrypted". The address and the session (the same `runyard_session` the web UI gets from
`POST /api/v1/login`) are kept in the app's private storage. On a server without `RUNYARD_AUTH` there is no
sign-in. While the server cannot be reached at launch, the app keeps trying on its own (after 2 seconds, then
twice as long each time, up to 30).

Two links open the app:

- `runyard://connect?url=<percent-encoded server address>` connects to that server, as if the address was typed;
  an app already connected to another server asks first. The web UI can show it as a QR.
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
address is looked for, and plays the demo's story through the same parsers.
