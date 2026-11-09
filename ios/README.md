# runyard for iOS

A native iPhone app for a runyard server, the way the Jellyfin app works: type the server's address, sign in
if the server has `RUNYARD_AUTH` on, and read your runs.

- **Projects:** a card per coding agent session from the last day, most urgent first, as on the web's kiosk (none
  on a server before 0.5.0; a tap opens its last run), a card per running run (its main metric with a sparkline,
  progress and a time-left guess when the config names a total such as `max_steps` or `num_train_epochs`, heartbeat,
  and the machine's CPU and per-GPU load), the eight newest runs that are not running (so one that just ended is
  still there, with its status and project), then every project and its runs. The cards show as soon as the lists
  answer; the sparklines fill in after. When a refresh fails, one line says for how long ("Not updated for 3m:
  can't reach the server") and no run is marked silent, since its heartbeat is only old on the phone.
- **A run:** status, duration, heartbeat and alerts, then tabs for charts (touch a chart for a value), system load
  (CPU, RAM, and a row per GPU: utilisation, memory, temperature, power), samples, logs, config and summary, and
  artifacts. The tab you chose is the one the next run opens on.
- **Samples:** images, videos and text tables by key, with a slider through the steps. Left at the end, the
  slider follows the newest step; the image stays until the next step's has loaded. Tap an image for full screen:
  swipe through the images showing (every key, at the step its slider is on), pinch or double-tap to zoom, share.
- **Alerts:** the server's alerts, newest first. The tab's badge counts the ones nobody put away (up to 20).

It reads, and deletes: swipe a project on the home screen or a run in a project's list, then confirm. Deleting
takes the runs' metrics, logs, samples and artifacts with them, and needs an admin (the server says 403
otherwise). A server without `RUNYARD_AUTH` wants an API key to delete, which the app has no place for, so there
is no delete there. Runs are written by the Python client. Screens refresh on their own while a run is running
(every 3–10 seconds), and on pull.

## Notifications

The switch on the Server tab makes the app tell you when a run ends (finished, failed or crashed) and when an
alert is raised, with no relay server: in a background refresh (`BGAppRefreshTask`, `Runyard/Notify.swift`) it
asks for `/api/v1/runs?limit=50` and `/api/v1/alerts?limit=20`, compares them with what it saw last time, and
posts a local notification for each change; a tap opens the run. iOS decides when a background refresh runs:
these can be minutes or hours late, and stop once the app is closed from the app switcher. Whatever happened
while the app was open is not news, and neither is anything from before the switch was turned on. For instant
notifications use the server's channels (ntfy, Slack, Telegram, web push), which an admin manages on the web
UI's Settings page.

## Connecting

The address can be an IP or a host name with a port (`192.168.1.10:8080`, `toji.example.ts.net`). Without a
scheme the app tries https, then http, and without a port also http on 8080. When none answers, the error names
each address tried and why it failed (no such host name, timed out, certificate not trusted, answered but is not
a runyard server). Plain http is allowed, since a home server usually has no certificate; the sign-in and Server
screens then say "http, not encrypted". The address and the session (the same `runyard_session` the web UI gets
from `POST /api/v1/login`) are kept in the keychain. On a server without `RUNYARD_AUTH` there is no sign-in.
When the server stops answering at launch, the app keeps trying on its own (after 2, 4, 8 ... at most 30 seconds).

### Links

The app opens `runyard://` links:

- `runyard://connect?url=<percent-encoded address>` connects to that server, as if the address were typed. When
  another server is connected, the app asks before switching. This is what a QR code should hold.
- `runyard://run/<run id>` opens that run.

### Demo

"Try the demo" on the connect screen opens the whole app on made-up runs, with no server: the web demo's story
(`web/demo/mock.js`), answered inside the app by a `URLProtocol` (`Runyard/Demo.swift`) that `API.http` routes
`https://demo.runyard.invalid` to. Runs keep training while you watch, `qwen3-8b-full-lr2e-5` goes NaN about 2.5
minutes in, and a relaunch starts the story over. It is kept like a server, so the app reopens in the demo until
"Leave demo". Videos are left out, since AVPlayer does not go through the session.

## Build and install

Needs Xcode 27 and [XcodeGen](https://github.com/yonaskolb/XcodeGen); the Xcode project is generated, not
committed.

```sh
cd ios
xcodegen generate
open Runyard.xcodeproj   # pick your team under Signing, then Run on your iPhone
```

`DEVELOPMENT_TEAM` in `project.yml` is the author's; change it to yours (or pick yours in Xcode).

### TestFlight

`scripts/testflight.sh` archives, signs for the App Store and uploads with an App Store Connect API key;
`scripts/asc.swift` registers the bundle id, makes the App Store profile, and adds a tester to an internal group;
`asc.swift public <build>` opens a build to anyone through the [public link](https://testflight.apple.com/join/A9jhtURf)
and sends it to Apple's beta review.
Once: create the app record in App Store Connect (the API cannot), then `swift scripts/asc.swift profile <sha1>`.
Each upload needs a new commit, since the build number counts them. The variables are in the script's header.

## Tests

`Tests/` holds unit tests for what the screens derive from a run (progress, machine load) and for what counts as
news for a notification.

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
