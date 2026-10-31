import SwiftUI

/// Home: what is training now, then every project.
struct HomeView: View {
    @Environment(Store.self) private var store
    @State private var running: [Run] = []
    @State private var projects: [Project] = []
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        List {
            if !running.isEmpty {
                Section("Running") {
                    ForEach(running) { run in
                        NavigationLink(value: Route.run(run.id)) { RunRow(run: run, showProject: true) }
                    }
                }
            }
            Section("All projects") {
                ForEach(projects) { p in
                    NavigationLink(value: Route.project(p.name)) { ProjectRow(project: p) }
                }
            }
        }
        .overlay {
            if loaded && projects.isEmpty {
                ContentUnavailableView("No projects yet", systemImage: "chart.xyaxis.line",
                                       description: Text("Runs show up here once a training script calls runyard.init()."))
            } else if !loaded, let error {
                ContentUnavailableView("Can't load projects", systemImage: "wifi.exclamationmark", description: Text(error))
            }
        }
        .navigationTitle("Projects")
        .refreshable { await load() }
        .poll(every: { 10 }) { await load() }
    }

    private func load() async {
        do {
            async let r: [Run] = store.get("/api/v1/runs?status=running&limit=50&metrics=1")
            async let p: [Project] = store.get("/api/v1/projects")
            (running, projects) = try await (r, p)
            loaded = true
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct ProjectRow: View {
    let project: Project

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(project.name).font(.headline)
                if project.running > 0 {
                    Text("\(project.running) running")
                        .font(.caption.bold())
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(.blue.opacity(0.15), in: .capsule)
                        .foregroundStyle(.blue)
                }
            }
            Text([project.runs == 1 ? "1 run" : "\(project.runs) runs",
                  project.lastRunAt.map { "last " + $0.formatted(.relative(presentation: .named)) }]
                .compactMap { $0 }.joined(separator: " · "))
                .font(.subheadline).foregroundStyle(.secondary)
        }
    }
}

/// One project's runs, newest first.
struct RunsView: View {
    @Environment(Store.self) private var store
    let project: String
    @State private var runs: [Run] = []
    @State private var loaded = false
    @State private var error: String?
    @State private var search = ""

    private var shown: [Run] {
        search.isEmpty ? runs : runs.filter { r in
            r.name.localizedCaseInsensitiveContains(search) || (r.tags ?? []).contains { $0.localizedCaseInsensitiveContains(search) }
        }
    }

    var body: some View {
        List(shown) { run in
            NavigationLink(value: Route.run(run.id)) { RunRow(run: run) }
        }
        .overlay {
            if loaded && runs.isEmpty {
                ContentUnavailableView("No runs", systemImage: "tray")
            } else if !loaded, let error {
                ContentUnavailableView("Can't load runs", systemImage: "wifi.exclamationmark", description: Text(error))
            }
        }
        .searchable(text: $search, prompt: "Name or tag")
        .navigationTitle(project)
        .refreshable { await load() }
        .poll(every: { runs.contains(where: \.running) ? 10 : 30 }) { await load() }
    }

    private func load() async {
        do {
            runs = try await store.get("/api/v1/runs?project=\(q(project))&limit=200&metrics=1")
            loaded = true
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct RunRow: View {
    let run: Run
    var showProject = false

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Circle().fill(statusColor(run.status)).frame(width: 9, height: 9)
            VStack(alignment: .leading, spacing: 3) {
                Text(run.name).font(.headline).lineLimit(1)
                Text([showProject ? run.project : nil,
                      run.status,
                      duration(run.duration()),
                      run.running ? nil : run.startedAt.formatted(.relative(presentation: .named))]
                    .compactMap { $0 }.joined(separator: " · "))
                    .font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                if let tags = run.tags, !tags.isEmpty {
                    Text(tags.joined(separator: "  ")).font(.caption).foregroundStyle(.tint).lineLimit(1)
                }
            }
            Spacer(minLength: 4)
            if let m = run.mainMetric {
                VStack(alignment: .trailing, spacing: 2) {
                    Text(fmt(m.value)).font(.headline.monospacedDigit())
                    Text(m.key).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
    }
}

struct AlertsView: View {
    @Environment(Store.self) private var store
    @State private var alerts: [Alert] = []
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        List(alerts) { a in
            NavigationLink(value: Route.run(a.run)) { AlertRow(alert: a, showRun: true) }
        }
        .overlay {
            if loaded && alerts.isEmpty {
                ContentUnavailableView("No alerts", systemImage: "bell.slash",
                                       description: Text("Alerts from run.alert() and the server's checks (NaN loss, stalls) land here."))
            } else if !loaded, let error {
                ContentUnavailableView("Can't load alerts", systemImage: "wifi.exclamationmark", description: Text(error))
            }
        }
        .navigationTitle("Alerts")
        .refreshable { await load() }
        .poll(every: { 15 }) { await load() }
    }

    private func load() async {
        do {
            alerts = try await store.get("/api/v1/alerts?limit=100")
            loaded = true
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct AlertRow: View {
    let alert: Alert
    var showRun = false

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: alert.level == "error" ? "exclamationmark.octagon.fill" : alert.level == "warn" ? "exclamationmark.triangle.fill" : "info.circle.fill")
                .foregroundStyle(levelColor(alert.level))
            VStack(alignment: .leading, spacing: 3) {
                Text(alert.title).font(.subheadline.bold())
                if let first = alert.body.split(separator: "\n").first {
                    Text(first).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }
                Text([showRun ? "\(alert.project)/\(alert.runName)" : nil, alert.ts.formatted(.relative(presentation: .named))]
                    .compactMap { $0 }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

func statusColor(_ status: String) -> Color {
    switch status {
    case "running": .blue
    case "finished": .green
    case "failed": .red
    case "crashed": .orange
    default: .secondary
    }
}

func levelColor(_ level: String) -> Color {
    level == "error" ? .red : level == "warn" ? .orange : .blue
}

func duration(_ seconds: TimeInterval) -> String {
    Duration.seconds(max(0, seconds)).formatted(.units(allowed: [.days, .hours, .minutes, .seconds], width: .narrow, maximumUnitCount: 2))
}
