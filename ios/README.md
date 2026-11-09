# runyard for iOS

A native iPhone app for a runyard server, the way the Jellyfin app works: type the server's address, sign in
if the server has `RUNYARD_AUTH` on, and read your runs.

- **Runs** (home): one line that sums up ("2 running · 1 failed today", or "Nothing is training"), then the
  newest alert nobody put away as a row that opens the Alerts tab ("+2 more" when there are several), a card per
  coding agent session from the last day, most urgent first, as on the web's kiosk (none on a server before 0.5.0;
  a tap opens its last run), and a card per running run: its name and how long it has run, project and host, the
  main metric's value with a sparkline, the latest sample image as a thumbnail (a tap opens the run on Samples),
  progress and a time-left guess when the config names a total such as `max_steps` or `num_train_epochs`, and the
  machine in one line ("GPU 97% · 31.4 GB · 68 °C", or the CPU and memory without a GPU). A run that has logged
  nothing for ten minutes (`progress_at`) says "no output for 14m" in the crashed colour. Then the eight newest
  runs that are not running (so one that just ended is still there, with how it ended and its project), then every
  project. The cards show as soon as the lists answer; the sparklines and thumbnails fill in after. When a refresh
  fails, one line says for how long ("Not updated for 3m: can't reach the server") in place of the summary, and no
  run is marked quiet. The last answers are kept on the phone (the caches directory, per server): when the server
  is out of reach at launch, this screen opens on them under the same line. Only this screen: a run opened then
  says it cannot load.
- **A run:** its name, state and duration, host and heartbeat, progress, and alerts, then tabs in this order:
  charts (the main metric first; touch a chart for the value at a step), samples, logs (they fill the rest of the
  screen and follow new lines; lines that name an error, an exception or a traceback are in the failed colour),
  system load (CPU, RAM, and a row per GPU: utilisation, memory, temperature, power), config and summary (values
  as written), and artifacts. The tab you chose is the one the next run opens on. A failed refresh shows the same
  "Not updated for ..." line, and the heartbeat's age is left out until one works.
- **Samples:** images and videos of every key on one slider, over every step any of them has ("step 1,720 · 9
  of 13"), side by side in two columns, so each prompt is seen at the same step. Under an image its key and its
  caption; a key with nothing at the chosen step shows its last one before it and says "from step N". Left at the
  end, the slider follows the newest step; an image stays until the next step's has loaded. Text tables come
  below, each key with its own slider. An image under 256 pixels wide is scaled by its nearest pixels, so it
  stays crisp. Tap an image for full screen: swipe through the images showing, pinch or double-tap to zoom,
  share. The page asks for copies at most 640 pixels wide (`/media/...?w=640`; a server that does not know the
  parameter sends the original), the home screen's thumbnail for 320 (from
  `/runs/{id}/media?kind=image&latest`; a server before 0.7.1 sends every step and the app takes the newest), the
  full-screen viewer for the original.
- **Alerts:** the server's alerts, newest first. The tab's badge counts the ones nobody put away, "20+" from
  twenty on.
- **Server:** the server the app is on and who is signed in, every server it knows (a check mark on the current
  one, "Add a server"), the notifications switch, and the machines running `runyard beacon`.

The look follows `DESIGN.md` (`Runyard/Theme.swift` holds its tokens): colour means a run's or a machine's state
and nothing else, so links, buttons and the selected tab are ink; numbers use a dot for decimals, a comma for
thousands and tabular figures in every locale.

It reads, and deletes: swipe a project on the home screen or a run in a project's list, then confirm. Deleting
takes the runs' metrics, logs, samples and artifacts with them, and needs an admin (the server says 403
otherwise). A server without `RUNYARD_AUTH` wants an API key to delete, which the app has no place for, so there
is no delete there. Runs are written by the Python client. Screens refresh on their own while a run is running
(every 3–10 seconds), and on pull.

## Notifications

The switch on the Server tab makes the app tell you when a run ends (finished, failed or crashed) and when an
alert is raised, with no relay server: in a background refresh (`BGAppRefreshTask`, `Runyard/Notify.swift`) it
asks for `/api/v1/runs?limit=50` and `/api/v1/alerts?limit=20`, compares them with what it saw last time, and
posts a local notification for each change; a tap opens the run. A run that started and ended between two looks
counts too, when it ended after the earlier look (by the phone's clock); one that shows up later with an older
end, synced from a training machine's offline cache, does not. iOS decides when a background refresh runs:
these can be minutes or hours late, and stop once the app is closed from the app switcher. Whatever happened
while the app was open is not news, and neither is anything from before the switch was turned on. Only the
server the app is on is watched. For instant
notifications use the server's channels (ntfy, Slack, Telegram, web push), which an admin manages on the web
UI's Settings page.

## Connecting

The address can be an IP or a host name with a port (`192.168.1.10:8080`, `toji.example.ts.net`). Without a
scheme the app tries https, then http, and without a port also http on 8080. When none answers, the error names
each address tried and why it failed (no such host name, timed out, certificate not trusted, answered but is not
a runyard server). Plain http is allowed, since a home server usually has no certificate; the sign-in and Server
screens then say "http, not encrypted". The address and the session (the same `runyard_session` the web UI gets
from `POST /api/v1/login`) are kept in the keychain. On a server without `RUNYARD_AUTH` there is no sign-in.
When the server does not answer at launch, the home screen opens on what it said last time; when nothing was
kept, the app says so and keeps trying on its own (after 2, 4, 8 ... at most 30 seconds).

The app remembers every server it has connected to, each with its own session. They are listed on the Server tab
and on the connect screen: tap one to switch, with no address to type and no sign-in again, swipe to forget it
and its session. "Add a server" goes back to the connect screen and forgets nothing; it and "Sign out" empty what
was kept for offline.

### Links

The app opens `runyard://` links:

- `runyard://connect?url=<percent-encoded address>` connects to that server, as if the address were typed. When
  another server is connected, the app asks before switching, unless the link's server is one it already knows:
  then it switches. This is what a QR code should hold.
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

`UITests/NotifyTests.swift` taps a notification with the app in the background, and again after closing it, and
expects the run's page both times. It is skipped unless it gets a server without sign-in and a run's id, and the
Mac has to post the notifications while it runs:

```sh
echo '{"aps":{"alert":{"title":"tap-test"}},"run":"<run id>"}' > note.json
(for i in $(seq 40); do sleep 4; xcrun simctl push booted dev.cobanov.runyard note.json; done &)
TEST_RUNNER_RUNYARD_TEST_SERVER=http://localhost:8080 TEST_RUNNER_RUNYARD_TEST_RUN=<run id> \
xcodebuild -project Runyard.xcodeproj -scheme Runyard \
  -destination 'platform=iOS Simulator,name=iPhone 18 Pro' test -only-testing:RunyardUITests/NotifyTests
```
