# runyard for iOS

A native iPhone app for a runyard server, the way the Jellyfin app works: type the server's address, sign in
if the server has `RUNYARD_AUTH` on, and read your runs.

- **Projects:** what is running now, with each run's main metric, then every project and its runs.
- **A run:** status, duration, heartbeat and alerts, then tabs for charts (touch a chart for a value), system load
  (CPU, RAM, and a row per GPU: utilisation, memory, temperature, power), samples (images, videos, text tables,
  with a slider through the steps), logs, config and summary, and artifacts.
- **Alerts:** the server's alerts, newest first.

It only reads; runs are written by the Python client. Screens refresh on their own while a run is running
(every 3–10 seconds), and on pull. Notifications still come from the server's channels (ntfy, Slack, Telegram,
web push).

## Connecting

The address can be an IP or a host name with a port (`192.168.1.10:8080`, `toji.example.ts.net`). Without a
scheme the app tries https, then http. Plain http is allowed, since a home server usually has no certificate.
The address and the session (the same `runyard_session` the web UI gets from `POST /api/v1/login`) are kept in
the keychain. On a server without `RUNYARD_AUTH` there is no sign-in.

## Build and install

Needs Xcode 27 and [XcodeGen](https://github.com/yonaskolb/XcodeGen); the Xcode project is generated, not
committed.

```sh
cd ios
xcodegen generate
open Runyard.xcodeproj   # pick your team under Signing, then Run on your iPhone
```

`DEVELOPMENT_TEAM` in `project.yml` is the author's; change it to yours (or pick yours in Xcode).

## Tests

`UITests/TourTests.swift` walks every screen against a real server and keeps a screenshot of each in the
result bundle:

```sh
TEST_RUNNER_RUNYARD_TEST_SERVER=localhost:8080 \
TEST_RUNNER_RUNYARD_TEST_USER=me TEST_RUNNER_RUNYARD_TEST_PASSWORD=... \
TEST_RUNNER_RUNYARD_TEST_PROJECT=vision TEST_RUNNER_RUNYARD_TEST_RUN=resnet50 \
xcodebuild -project Runyard.xcodeproj -scheme Runyard \
  -destination 'platform=iOS Simulator,name=iPhone 18 Pro' -resultBundlePath tour.xcresult test
xcrun xcresulttool export attachments --path tour.xcresult --output-path shots
```

Leave out the user and password for a server without `RUNYARD_AUTH`, and the project and run to take the last
project and the newest run.
