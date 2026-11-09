import Charts
import SwiftUI

/// Home: the coding agents, what is training now, each at a glance, what ended lately, then every project.
struct HomeView: View {
    @Environment(Store.self) private var store
    @State private var agents: [Agent] = []
    @State private var running: [Run] = []
    @State private var spark: [String: [Double]] = [:] // run id -> recent values of its main metric
    @State private var recent: [Run] = [] // the newest runs that are not running
    @State private var projects: [Project] = []
    @State private var loaded = false
    @State private var okAt = Date.now // the last load that worked
    @State private var error: String?
    @State private var deleting: Project?
    @State private var failure: String?

    var body: some View {
        List {
            if loaded, error != nil {
                TimelineView(.periodic(from: .now, by: 1)) { t in
                    Text("Not updated for \(duration(t.date.timeIntervalSince(okAt))): can't reach the server")
                        .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                }
                .listRowBackground(Color.clear)
            }
            if !agents.isEmpty {
                Section("Agents") {
                    ForEach(agents) { a in
                        let card = AgentCard(agent: a, score: running.first { $0.id == a.lastRun?.id }?.score(a.metric))
                        if let run = a.lastRun {
                            NavigationLink(value: Route.run(run.id)) { card }
                        } else {
                            card
                        }
                    }
                }
            }
            if !running.isEmpty {
                Section("Running") {
                    ForEach(running) { run in
                        NavigationLink(value: Route.run(run.id)) { LiveRunCard(run: run, spark: spark[run.id] ?? [], stale: error != nil) }
                    }
                }
            }
            if !recent.isEmpty {
                Section("Recent") {
                    ForEach(recent) { run in
                        NavigationLink(value: Route.run(run.id)) { RunRow(run: run, showProject: true) }
                    }
                }
            }
            Section("All projects") {
                ForEach(projects) { p in
                    NavigationLink(value: Route.project(p.name)) { ProjectRow(project: p) }
                        .swipeActions { if store.canDelete { Button("Delete", systemImage: "trash") { deleting = p }.tint(.red) } }
                }
            }
        }
        .confirmationDialog("Delete \(deleting?.name ?? "")?", isPresented: Binding(present: $deleting), titleVisibility: .visible, presenting: deleting) { p in
            Button("Delete project", role: .destructive) {
                Task {
                    do { try await store.delete("/api/v1/projects/\(q(p.name))"); await load() } catch { failure = error.localizedDescription }
                }
            }
        } message: { p in
            Text("Its \(p.runs == 1 ? "1 run" : "\(p.runs) runs"), with their metrics, logs, samples and artifacts, are deleted for good.")
        }
        .alert("Can't delete", isPresented: Binding(present: $failure), presenting: failure) { _ in } message: { Text($0) }
        .overlay {
            if loaded && projects.isEmpty && agents.isEmpty {
                ContentUnavailableView("No projects yet", systemImage: "chart.xyaxis.line",
                                       description: Text("Runs show up here once a training script calls runyard.init()."))
            } else if !loaded, let error {
                ContentUnavailableView("Can't load projects", systemImage: "wifi.exclamationmark", description: Text(error))
            }
        }
        .navigationTitle("Projects")
        .refreshable { await load() }
        .poll(every: { running.isEmpty ? 10 : 5 }) { await load() }
    }

    private func load() async {
        do {
            // Two lists: a long run must not drop off the screen because twenty newer ones started after it.
            async let r: [Run] = store.get("/api/v1/runs?status=running&limit=50&metrics=1")
            async let n: [Run] = store.get("/api/v1/runs?limit=20&metrics=1")
            async let p: [Project] = store.get("/api/v1/projects")
            async let a: [Agent]? = try? store.get("/api/v1/agents?hours=24") // a server before 0.5.0 answers 404: no section
            let (runs, newest, ps) = try await (r, n, p)
            if let cards = await a { agents = cards.sorted(by: Agent.urgentFirst) }
            (running, recent, projects) = (runs, Array(newest.filter { !$0.running }.prefix(8)), ps)
            (loaded, okAt, error) = (true, .now, nil)
        } catch {
            self.error = error.localizedDescription
            return
        }
        // The cards are on screen; their sparklines fill in as each answers.
        await withTaskGroup(of: (String, [Double]?).self) { group in
            for run in running {
                guard let k = run.mainMetric?.key else { continue }
                group.addTask {
                    let s: [String: Series]? = try? await store.get("/api/v1/runs/\(run.id)/metrics?points=40&keys=\(q(k))")
                    return (run.id, s?[k]?.last.map(\.value).filter(\.isFinite))
                }
            }
            for await (id, values) in group { spark[id] = values }
        }
    }
}

/// A running run on the home screen: enough to see how it is going without opening it.
struct LiveRunCard: View {
    let run: Run
    let spark: [Double]
    var stale = false // the last poll failed: the heartbeat is old because the phone could not ask

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                TimelineView(.periodic(from: .now, by: 1)) { t in
                    let age = t.date.timeIntervalSince(run.lastHeartbeatAt ?? run.startedAt)
                    let silent = !stale && age > Self.silentAfter
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Image(systemName: "heart.fill").font(.caption)
                            .foregroundStyle(silent ? .orange : statusColor(run.status))
                            .symbolEffect(.bounce, value: run.lastHeartbeatAt)
                            .accessibilityLabel("Last heartbeat \(duration(age)) ago")
                        Text(run.name).font(.headline).lineLimit(1)
                        Spacer(minLength: 4)
                        if silent { Text(duration(age)).font(.caption.monospacedDigit()).foregroundStyle(.orange) }
                        Text(duration(run.duration(now: t.date))).font(.subheadline.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                Text([run.project, run.host.map { "on \($0)" }].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            if let m = run.mainMetric {
                HStack(alignment: .bottom, spacing: 12) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(fmt(m.value)).font(.title2.bold().monospacedDigit())
                        Text(run.progress == nil ? "\(m.key) · step \(String(run.metrics?[m.key]?.step ?? 0))" : m.key)
                            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                    Spacer(minLength: 0)
                    if spark.count > 1 { Sparkline(values: spark).frame(width: 110, height: 34) }
                }
            }
            RunProgress(run: run)
            if let load = SysLoad(run.metrics) { MachineStrip(load: load) }
        }
        .padding(.vertical, 4)
    }

    /// The library beats every 30 seconds: three missed beats is worth a look, long before the server's 5 minutes.
    private static let silentAfter: TimeInterval = 90
}

/// How far a run is, when its config names a total. Keep it outside a ticking view: the step only moves with a
/// poll, so an elapsed time that ticks would push the estimate up between polls.
struct RunProgress: View {
    let run: Run

    var body: some View {
        if let p = run.progress {
            let f = min(max(p.done / p.total, 0), 1)
            VStack(alignment: .leading, spacing: 4) {
                ProgressView(value: f)
                HStack {
                    Text("\(p.unit) \(fmt(p.done)) / \(fmt(p.total)) · \(Int((f * 100).rounded()))%")
                    Spacer(minLength: 4)
                    // Elapsed time includes setup, so this is a rough guess.
                    if run.running, f > 0.01, f < 1 { Text("~\(duration(run.duration() * (1 - f) / f)) left") }
                }
                .font(.caption.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

/// A coding agent on the home screen: what state it is in and what it last said.
struct AgentCard: View {
    let agent: Agent
    let score: (key: String, value: Double)?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(agent.title).font(.headline).lineLimit(1)
                Spacer(minLength: 4)
                Text(agent.word).font(.subheadline.bold()).foregroundStyle(agentColor(agent.state)).layoutPriority(1)
            }
            Text([agent.tool, agent.project ?? "", agent.host].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if let score {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(fmt(score.value)).font(.title2.bold().monospacedDigit())
                    Text(score.key).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            let text = agent.report.isEmpty ? agent.said : agent.report
            if !text.isEmpty { Text(text).font(.subheadline).lineLimit(3) }
            Text([agent.lastRun?.name, agent.runs == 1 ? "1 run" : "\(agent.runs) runs",
                  "\(duration(Date.now.timeIntervalSince(agent.activeAt))) ago"]
                .compactMap { $0 }.joined(separator: " · "))
                .font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
        }
        .padding(.vertical, 4)
        .opacity(["stale", "ended"].contains(agent.state) ? 0.5 : 1)
    }
}

/// The machine at a glance: a bar for the CPU and for each GPU.
struct MachineStrip: View {
    let load: SysLoad

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            if let cpu = load.cpu {
                row("CPU", cpu, load.ramGB.map { String(format: "RAM %.0f GB", $0) } ?? "")
            }
            ForEach(load.gpus) { g in
                row("GPU \(g.id)", g.util, [g.memGB.map { String(format: "%.0f GB", $0) }, g.tempC.map { "\(Int($0.rounded()))°" }]
                    .compactMap { $0 }.joined(separator: " · "))
            }
        }
        .font(.caption.monospacedDigit())
        .padding(10)
        .background(.background.secondary, in: .rect(cornerRadius: 10))
    }

    private func row(_ name: String, _ util: Double?, _ details: String) -> some View {
        HStack(spacing: 8) {
            Text(name).frame(width: 44, alignment: .leading)
            ProgressView(value: min(max(util ?? 0, 0), 100), total: 100).tint(loadColor(util ?? 0))
            Text(util.map { "\(Int($0.rounded()))%" } ?? "–").bold().frame(width: 38, alignment: .trailing)
            Text(details).foregroundStyle(.secondary).lineLimit(1).frame(width: 84, alignment: .leading)
        }
    }
}

struct Sparkline: View {
    let values: [Double]

    var body: some View {
        Chart(values.indices, id: \.self) { i in
            LineMark(x: .value("i", i), y: .value("v", values[i])).interpolationMethod(.monotone)
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartYScale(domain: .automatic(includesZero: false))
        .foregroundStyle(.tint)
        .accessibilityHidden(true)
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
                        .background(Color.accentColor.opacity(0.15), in: .capsule)
                        .foregroundStyle(Color.accentColor)
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
    @State private var deleting: Run?
    @State private var failure: String?

    private var shown: [Run] {
        search.isEmpty ? runs : runs.filter { r in
            r.name.localizedCaseInsensitiveContains(search) || (r.tags ?? []).contains { $0.localizedCaseInsensitiveContains(search) }
        }
    }

    var body: some View {
        List(shown) { run in
            NavigationLink(value: Route.run(run.id)) { RunRow(run: run) }
                .swipeActions { if store.canDelete { Button("Delete", systemImage: "trash") { deleting = run }.tint(.red) } }
        }
        .confirmationDialog("Delete \(deleting?.name ?? "")?", isPresented: Binding(present: $deleting), titleVisibility: .visible, presenting: deleting) { run in
            Button("Delete run", role: .destructive) {
                Task {
                    do { try await store.delete("/api/v1/runs/\(run.id)"); await load() } catch { failure = error.localizedDescription }
                }
            }
        } message: { run in
            Text("Its metrics, logs, samples and artifacts are deleted for good." + (run.running ? " It is still running; what it sends from now on is refused." : ""))
        }
        .alert("Can't delete", isPresented: Binding(present: $failure), presenting: failure) { _ in } message: { Text($0) }
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
                .opacity(a.dismissedAt == nil ? 1 : 0.5)
                .swipeActions {
                    if a.dismissedAt == nil {
                        Button("Dismiss") { Task { await dismiss([a.id]) } }.tint(.gray)
                    }
                }
        }
        .toolbar {
            if alerts.contains(where: { $0.dismissedAt == nil }) {
                Button("Dismiss all") { Task { await dismiss([]) } }
            }
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

    /// Puts alerts away (all of them for no ids): they stay here, dimmed.
    private func dismiss(_ ids: [Int]) async {
        do {
            try await store.post("/api/v1/alerts/dismiss", json: ["ids": ids])
        } catch {
            self.error = error.localizedDescription
        }
        await load()
        await store.countAlerts()
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
    case "running": .accentColor  // the running teal, as in DESIGN.md
    case "finished": .green
    case "failed": .red
    case "crashed": .orange
    default: .secondary
    }
}

func agentColor(_ state: String) -> Color {
    switch state {
    case "waiting": .orange
    case "error": .red
    case "done": .green
    case "training": .accentColor
    case "working": .blue
    default: .secondary
    }
}

func levelColor(_ level: String) -> Color {
    level == "error" ? .red : level == "warn" ? .orange : .blue
}

nonisolated func duration(_ seconds: TimeInterval) -> String {
    Duration.seconds(max(0, seconds)).formatted(.units(allowed: [.days, .hours, .minutes, .seconds], width: .narrow, maximumUnitCount: 2))
}
