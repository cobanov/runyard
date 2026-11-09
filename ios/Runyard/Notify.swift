import BackgroundTasks
import Foundation
import UserNotifications

// Notifications without a relay: the app asks the server when iOS lets it run in the background, and tells
// what changed since it last looked. Late by design; the server's own channels (ntfy, web push) are instant.

/// What the app saw at its last look: every run's status, the newest alert, and when it looked.
nonisolated struct Seen: Codable, Equatable, Sendable {
    var runs: [String: String]
    var alert: Int
    var at: Date? // none in a look kept by an older version
}

nonisolated struct Note: Equatable, Sendable {
    let title, body, run: String
}

/// What is news since `old`: a run that has ended, and was running then or started after it, and an alert newer
/// than the last one seen. The first look (no `old`) tells nothing: everything on the server is new to it.
nonisolated func news(since old: Seen?, runs: [Run], alerts: [Alert], now: Date = .now) -> (seen: Seen, notes: [Note]) {
    let seen = Seen(runs: Dictionary(runs.map { ($0.id, $0.status) }) { a, _ in a },
                    alert: max(old?.alert ?? 0, alerts.map(\.id).max() ?? 0), at: now)
    guard let old else { return (seen, []) }
    let ended = runs.filter { r in
        guard ["finished", "failed", "crashed"].contains(r.status) else { return false }
        if let was = old.runs[r.id] { return was == "running" }
        // Not there at the last look: it started and ended since. One synced later from an offline cache ended
        // before that look, so it stays quiet. shortcut: the phone's clock against the server's, so a phone
        // minutes ahead can miss one; compare with the response's Date header if that ever matters.
        guard let at = old.at, let end = r.endedAt else { return false }
        return end > at
    }
        .map { Note(title: "\($0.name) \($0.status)", body: "\($0.project), after \(duration($0.duration()))", run: $0.id) }
    let raised = alerts.filter { $0.id > old.alert }.sorted { $0.id < $1.id }
        .map { Note(title: $0.title, body: "\($0.project)/\($0.runName)" + ($0.body.split(separator: "\n").first.map { ": \($0)" } ?? ""), run: $0.run) }
    return (seen, ended + raised)
}

enum Notify {
    nonisolated static let task = "dev.cobanov.runyard.refresh" // also in project.yml, BGTaskSchedulerPermittedIdentifiers
    nonisolated static let onKey = "notify"
    nonisolated static let seenKey = "seen"

    /// Asks iOS for a background refresh. It picks the time, a quarter of an hour away at the earliest.
    static func schedule() {
        guard UserDefaults.standard.bool(forKey: onKey) else { return }
        let request = BGAppRefreshTaskRequest(identifier: task)
        request.earliestBeginDate = .now + 15 * 60
        try? BGTaskScheduler.shared.submit(request)
    }

    /// Looks at the server and remembers it; with `tell`, posts a notification for each piece of news.
    static func check(_ api: API, tell: Bool) async {
        let d = UserDefaults.standard
        guard d.bool(forKey: onKey), !api.base.isEmpty, api.base != DemoServer.base,
              let runs: [Run] = try? await api.get("/api/v1/runs?limit=50"),
              let alerts: [Alert] = try? await api.get("/api/v1/alerts?limit=20") else { return }
        let old = d.data(forKey: seenKey).flatMap { try? JSONDecoder().decode(Seen.self, from: $0) }
        let (seen, notes) = news(since: old, runs: runs, alerts: alerts)
        d.set(try? JSONEncoder().encode(seen), forKey: seenKey)
        guard tell else { return }
        for n in notes {
            let c = UNMutableNotificationContent()
            (c.title, c.body, c.sound, c.userInfo) = (n.title, n.body, .default, ["run": n.run])
            try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: UUID().uuidString, content: c, trigger: nil))
        }
    }
}

/// A tap on a notification opens its run.
final class NotifyTaps: NSObject, UNUserNotificationCenterDelegate {
    let store: Store
    init(store: Store) { self.store = store }

    // The completion-handler form, not the async one: that one answers UIKit from a background thread, and the
    // app dies in an assertion on every tap. iOS calls this on the main thread.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler done: @escaping () -> Void) {
        let run = response.notification.request.content.userInfo["run"] as? String
        MainActor.assumeIsolated { if let run { store.openRun = run } }
        done()
    }
}
