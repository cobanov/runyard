# runyard for Android

A native Android app for a runyard server, the counterpart of the iPhone app (`ios/`): type the server's address,
sign in if the server has `RUNYARD_AUTH` on, and read your runs. Kotlin and Jetpack Compose, themed with
`DESIGN.md`'s tokens in light and dark.

Get it: [runyard-0.4.0.apk](https://github.com/cobanov/runyard/releases/download/v0.4.0/runyard-0.4.0.apk) from the
v0.4.0 release (allow installs from your browser when Android asks).

- **Projects:** a card per running run (its main metric with a sparkline, heartbeat, host, and the machine's CPU
  and per-GPU load), then every project with its run count.
- **A project:** its runs, newest first, with status, duration, start and main metric.
- **A run:** status, duration, heartbeat and alerts, then tabs for charts (touch a chart for a value), system load
  (CPU, RAM, and a row per GPU), samples (images and text tables, with a slider through the steps), logs, and
  config and summary.
- **Alerts:** the server's alerts, newest first; tap one for its run.
- **Server:** the machines running `runyard agent`, the address, sign out and change server.

It reads, and deletes: long-press a project on the home screen or a run in a project's list, then confirm.
Deleting needs an admin (the server says 403 otherwise). Screens refresh on their own while a run is running
(every 3–10 seconds), and on pull.

**Try the demo** on the connect screen opens the app on made-up runs with no server: an in-app fake of the same
API (`Demo.kt`) telling `web/demo/mock.js`'s story. Runs keep training while you watch; one full fine-tune
breaks with a NaN loss two and a half minutes in, and a ResNet finishes after about a minute.

## Connecting

The address can be an IP or a host name with a port (`192.168.1.10:8080`, `toji.example.ts.net`). Without a
scheme the app tries https, then http. Plain http is allowed (`network_security_config.xml`), since a home server
usually has no certificate. The address and the session (the same `runyard_session` the web UI gets from
`POST /api/v1/login`) are kept in the app's private storage. On a server without `RUNYARD_AUTH` there is no
sign-in.

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
main-metric rule and the number format, and plays the demo's story through the same parsers.
