import Charts
import SwiftUI

/// Home: what needs a look, what is training now and how it is going, what ended lately, then every project.
struct HomeView: View {
    @Environment(Store.self) private var store
    @State private var agents: [Agent] = []
    @State private var running: [Run] = []
    @State private var spark: [String: [Double]] = [:] // run id -> recent values of its main metric
    @State private var thumb: [String: String] = [:] // run id -> its latest sample image
    @State private var ended: [Run] = [] // the newest runs that are not running; the first 8 are listed
    @State private var projects: [Project] = []
    @State private var loaded = false
    @State private var okAt = Date.now // the last load that worked
    @State private var error: String?
    @State private var deleting: Project?
    @State private var failure: String?

    /// "2 running · 1 failed today"
    private var summary: String {
        let today = ended.filter { Calendar.current.isDateInToday($0.endedAt ?? $0.lastHeartbeatAt ?? $0.startedAt) }
        return ([running.isEmpty ? "Nothing is training" : "\(running.count) running"] + ["failed", "crashed"].compactMap { s in
            let n = today.count { $0.status == s }
            return n == 0 ? nil : "\(n) \(s) today"
        }).joined(separator: " · ")
    }

    var body: some View {
        let recent = Array(ended.prefix(8))
        List {
            if loaded {
                Group { if error != nil { NotUpdated(since: okAt) } else { Text(summary) } }
                    .font(.subheadline).foregroundStyle(.muted).bareRow()
            } else if let error {
                Empty("Can't load runs: \(error)").bareRow()
            }
            if let a = store.active.first {
                Button { store.tab = 1 } label: { AlertLine(alert: a, more: store.active.count - 1) }
                    .buttonStyle(.plain).panelRow(above: 14)
            }
            if !agents.isEmpty {
                Heading("Agents")
                ForEach(Array(agents.enumerated()), id: \.element.id) { i, a in
                    let card = AgentCard(agent: a, score: running.first { $0.id == a.lastRun?.id }?.score(a.metric))
                    Group {
                        if let run = a.lastRun {
                            NavigationLink(value: Route.run(run.id)) { card }.navigationLinkIndicatorVisibility(.hidden)
                        } else {
                            card
                        }
                    }
                    .panelRow(i, of: agents.count)
                }
            }
            if !running.isEmpty {
                Heading("Running")
                ForEach(Array(running.enumerated()), id: \.element.id) { i, run in
                    NavigationLink(value: Route.run(run.id)) {
                        LiveRunCard(run: run, spark: spark[run.id] ?? [], thumb: thumb[run.id], stale: error != nil)
                    }
                    .navigationLinkIndicatorVisibility(.hidden)
                    .panelRow(below: i == running.count - 1 ? 0 : 14)
                }
            }
            if !recent.isEmpty {
                Heading("Recent")
                ForEach(Array(recent.enumerated()), id: \.element.id) { i, run in
                    NavigationLink(value: Route.run(run.id)) { RunRow(run: run, showProject: true) }
                        .navigationLinkIndicatorVisibility(.hidden)
                        .panelRow(i, of: recent.count)
                }
            }
            if loaded {
                Heading("Projects")
                if projects.isEmpty { Empty("Runs show up here once a training script calls runyard.init().").bareRow() }
                ForEach(Array(projects.enumerated()), id: \.element.id) { i, p in
                    NavigationLink(value: Route.project(p.name)) { ProjectRow(project: p) }
                        .swipeActions { if store.canDelete { Button("Delete", systemImage: "trash") { deleting = p }.tint(.failed) } }
                        .panelRow(i, of: projects.count)
                }
            }
            Color.clear.frame(height: 16).bareRow()
        }
        .paperList()
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
        .navigationTitle("Runs")
        .refreshable { await load() }
        .poll(every: { running.isEmpty ? 10 : 5 }) { await load() }
    }

    // Two lists: a long run must not drop off the screen because twenty newer ones started after it.
    private static let runningPath = "/api/v1/runs?status=running&limit=50&metrics=1"
    private static let newestPath = "/api/v1/runs?limit=20&metrics=1"
    private static let projectsPath = "/api/v1/projects"
    private static let agentsPath = "/api/v1/agents?hours=24"

    private func load() async {
        do {
            async let r: [Run] = store.get(Self.runningPath, keep: true)
            async let n: [Run] = store.get(Self.newestPath, keep: true)
            async let p: [Project] = store.get(Self.projectsPath, keep: true)
            async let a: [Agent]? = try? store.get(Self.agentsPath, keep: true) // a server before 0.5.0 answers 404: no section
            let (runs, newest, ps) = try await (r, n, p)
            if let cards = await a { agents = cards.sorted(by: Agent.urgentFirst) }
            (running, ended, projects) = (runs, newest.filter { !$0.running }, ps)
            (loaded, okAt, error) = (true, .now, nil)
        } catch {
            self.error = error.localizedDescription
            // Nothing on screen yet: what the server said last time, under the line that says how old it is.
            let api = store.api
            if !loaded, let r: (value: [Run], at: Date) = api.kept(Self.runningPath), let n: (value: [Run], at: Date) = api.kept(Self.newestPath),
               let p: (value: [Project], at: Date) = api.kept(Self.projectsPath) {
                let a: (value: [Agent], at: Date)? = api.kept(Self.agentsPath)
                agents = (a?.value ?? []).sorted(by: Agent.urgentFirst)
                (running, ended, projects) = (r.value, n.value.filter { !$0.running }, p.value)
                (loaded, okAt) = (true, min(r.at, n.at, p.at))
            }
            return
        }
        // The cards are on screen; their sparklines and latest samples fill in as each answers.
        await withTaskGroup(of: (String, [Double]?, String?).self) { group in
            for run in running {
                let k = run.mainMetric?.key
                group.addTask {
                    async let s: [String: Series]? = try? store.get("/api/v1/runs/\(run.id)/metrics?points=40&keys=\(q(k ?? ""))")
                    // A server before 0.7.1 ignores `latest` and sends every step: the newest is still in there.
                    async let m: [Media]? = try? store.get("/api/v1/runs/\(run.id)/media?kind=image&latest")
                    let newest = (await m)?.filter { $0.kind == "image" }
                        .max { ($0.createdAt ?? .distantPast, $0.step) < ($1.createdAt ?? .distantPast, $1.step) }
                    let values = await s?[k ?? ""]?.last.map(\.value).filter(\.isFinite)
                    return (run.id, values,
                            newest.map { $0.url.hasPrefix("/media/") ? $0.url + "?w=320" : $0.url })
                }
            }
            for await (id, values, image) in group {
                spark[id] = values
                if let image { thumb[id] = image }
            }
        }
    }
}

/// The one line a screen shows while its refresh fails: what is on it is this old.
struct NotUpdated: View {
    let since: Date

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { t in
            Text("Not updated for \(duration(t.date.timeIntervalSince(since))): can't reach the server")
                .font(.subheadline).foregroundStyle(.muted)
        }
    }
}

/// The newest alert nobody put away, on the home screen: "loss is NaN · qwen3-8b-full · 4m ago".
struct AlertLine: View {
    let alert: Alert
    let more: Int

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Dot(color: levelColor(alert.level))
            // The name gives way first: what happened and how long ago stay whole.
            (Text(alert.title).fontWeight(.semibold) + Text(" · \(alert.runName)").foregroundStyle(.muted)).lineLimit(1)
            Text("· \(ago(alert.ts))").foregroundStyle(.muted).fixedSize().padding(.leading, -6)
            Spacer(minLength: 8)
            if more > 0 { Text("+\(more) more").foregroundStyle(.muted).fixedSize() }
        }
        .font(.subheadline)
        .contentShape(.rect)
        .accessibilityHint("Opens the alerts")
    }
}

/// A running run on the home screen: enough to see how it is going without opening it.
struct LiveRunCard: View {
    @Environment(Store.self) private var store
    let run: Run
    let spark: [Double]
    var thumb: String?
    var stale = false // the last poll failed: nothing is known to be late

    var body: some View {
        // Ten minutes with nothing logged while the server still calls it running: stuck, or one very slow step.
        let quiet = stale ? nil : run.progressAt.map { Date.now.timeIntervalSince($0) }.flatMap { $0 > 600 ? $0 : nil }
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Dot(color: quiet == nil ? .running : .crashed, breathes: quiet == nil)
                    Text(run.name).font(.callout.weight(.semibold)).lineLimit(1)
                    Spacer(minLength: 8)
                    TimelineView(.periodic(from: .now, by: 1)) { t in
                        Text(duration(run.duration(now: t.date))).font(.footnote).foregroundStyle(.muted)
                    }
                }
                (Text(quiet.map { "no output for \(duration($0, units: 1)) · " } ?? "").foregroundStyle(.crashed)
                    + Text([run.project, run.host ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")).foregroundStyle(.muted))
                    .font(.footnote).lineLimit(1)
            }
            if run.mainMetric != nil || thumb != nil {
                HStack(spacing: 12) {
                    if let m = run.mainMetric {
                        VStack(alignment: .leading, spacing: 0) {
                            Text(fmt(m.value)).font(.title.bold()).kerning(-0.5).lineLimit(1).minimumScaleFactor(0.7)
                            Text(run.progress == nil ? "\(m.key) · step \(fmt(Double(run.metrics?[m.key]?.step ?? 0)))" : m.key)
                                .font(.footnote).foregroundStyle(.muted).lineLimit(1)
                        }
                        .layoutPriority(1)
                    }
                    if spark.count > 1 { Sparkline(values: spark).frame(maxWidth: .infinity).frame(height: 48) } else { Spacer(minLength: 0) }
                    if let thumb {
                        Button {
                            UserDefaults.standard.set(RunView.Section.samples.rawValue, forKey: "runSection")
                            store.openRun = run.id
                        } label: {
                            RemoteImage(url: thumb, side: 64)
                        }
                        .buttonStyle(.borderless)
                        .accessibilityLabel("Latest sample")
                    }
                }
            }
            RunProgress(run: run)
            if let line = SysLoad(run.metrics)?.line, !line.isEmpty {
                Text(line).font(.footnote).foregroundStyle(.muted).lineLimit(1)
            }
        }
        .padding(.vertical, 2)
    }
}

/// How far a run is, when its config names a total. Keep it outside a ticking view: the step only moves with a
/// poll, so an elapsed time that ticks would push the estimate up between polls.
struct RunProgress: View {
    let run: Run

    var body: some View {
        if let p = run.progress {
            let f = min(max(p.done / p.total, 0), 1)
            VStack(alignment: .leading, spacing: 6) {
                Bar(value: f, fill: statusColor(run.status))
                HStack {
                    Text("\(p.unit) \(fmt(p.done)) / \(fmt(p.total)) · \(Int((f * 100).rounded()))%")
                    Spacer(minLength: 8)
                    // Elapsed time includes setup, so this is a rough guess.
                    if run.running, f > 0.01, f < 1 { Text("~\(duration(run.duration() * (1 - f) / f, units: 1)) left") }
                }
                .font(.footnote).foregroundStyle(.muted).lineLimit(1)
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
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(agent.title).font(.callout.weight(.semibold)).lineLimit(1)
                    Spacer(minLength: 8)
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Dot(color: agentColor(agent.state), breathes: ["training", "working"].contains(agent.state))
                        Text(agent.word).font(.footnote.weight(.medium)).foregroundStyle(agentColor(agent.state))
                    }
                    .layoutPriority(1)
                }
                Text([agent.tool, agent.project ?? "", agent.host].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.footnote).foregroundStyle(.muted).lineLimit(1)
            }
            if let score {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(fmt(score.value)).font(.title2.bold())
                    Text(score.key).font(.footnote).foregroundStyle(.muted).lineLimit(1)
                }
            }
            let text = agent.report.isEmpty ? agent.said : agent.report
            if !text.isEmpty { Text(text).font(.subheadline).lineLimit(3) }
            Text([agent.lastRun?.name, agent.runs == 1 ? "1 run" : "\(agent.runs) runs", ago(agent.activeAt)]
                .compactMap { $0 }.joined(separator: " · "))
                .font(.footnote).foregroundStyle(.muted).lineLimit(1).truncationMode(.middle)
        }
        .padding(.vertical, 2)
        .opacity(["stale", "ended"].contains(agent.state) ? 0.5 : 1)
    }
}

/// A machine's load as text, and one neutral bar per GPU.
struct MachineLoad: View {
    let load: SysLoad

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            let cpu = [load.cpu.map { "CPU \(Int($0.rounded()))%" }, load.ramGB.map { "RAM " + String(format: "%.1f GB", $0) }].compactMap { $0 }
            if !cpu.isEmpty { Text(cpu.joined(separator: " · ")).foregroundStyle(.muted) }
            ForEach(load.gpus) { g in
                VStack(alignment: .leading, spacing: 5) {
                    HStack(alignment: .firstTextBaseline) {
                        Text("GPU \(g.id)")
                        Spacer(minLength: 8)
                        Text(([g.util.map { "\(Int($0.rounded()))%" }, g.details.isEmpty ? nil : g.details] as [String?]).compactMap { $0 }.joined(separator: " · "))
                            .foregroundStyle(.muted).lineLimit(1)
                    }
                    Bar(value: (g.util ?? 0) / 100, fill: .muted)
                }
            }
        }
        .font(.footnote)
    }
}

struct Sparkline: View {
    let values: [Double]

    var body: some View {
        Chart(values.indices, id: \.self) { i in
            LineMark(x: .value("i", i), y: .value("v", values[i])).lineStyle(StrokeStyle(lineWidth: 1.5, lineJoin: .round))
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartYScale(domain: .automatic(includesZero: false))
        .foregroundStyle(.series) // the chart blue: teal stays a state
        .accessibilityHidden(true)
    }
}

struct ProjectRow: View {
    let project: Project

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(project.name).font(.callout.weight(.semibold)).lineLimit(1)
            (Text(project.running > 0 ? "\(project.running) running · " : "").foregroundStyle(.running)
                + Text([project.runs == 1 ? "1 run" : "\(fmt(Double(project.runs))) runs", project.lastRunAt.map { "last " + ago($0) }]
                    .compactMap { $0 }.joined(separator: " · ")).foregroundStyle(.muted))
                .font(.footnote).lineLimit(1)
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
        let shown = shown
        List {
            if !loaded, let error {
                Empty("Can't load runs: \(error)").bareRow(above: 8)
            } else if loaded, shown.isEmpty {
                Empty(runs.isEmpty ? "No runs here yet. One shows up when a script calls runyard.init(project: \"\(project)\")." : "No run here has \"\(search)\" in its name or tags.")
                    .bareRow(above: 8)
            }
            ForEach(Array(shown.enumerated()), id: \.element.id) { i, run in
                NavigationLink(value: Route.run(run.id)) { RunRow(run: run) }
                    .navigationLinkIndicatorVisibility(.hidden)
                    .swipeActions { if store.canDelete { Button("Delete", systemImage: "trash") { deleting = run }.tint(.failed) } }
                    .panelRow(i, of: shown.count, above: i == 0 ? 8 : 0)
            }
            Color.clear.frame(height: 16).bareRow()
        }
        .paperList()
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
        // How it ended, when that is not "finished"; then where and when.
        let word = run.status == "finished" ? "" : run.status + " · "
        let end = run.endedAt ?? run.lastHeartbeatAt ?? run.startedAt
        let rest = run.running ? [showProject ? run.project : nil, duration(run.duration())]
            : showProject ? [run.project, ago(end)] : [ago(end), "ran " + duration(run.duration())]
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Dot(color: statusColor(run.status), breathes: run.running)
            VStack(alignment: .leading, spacing: 3) {
                Text(run.name).font(.callout.weight(.semibold)).lineLimit(1)
                (Text(word).foregroundStyle(statusColor(run.status)) + Text(rest.compactMap { $0 }.joined(separator: " · ")).foregroundStyle(.muted))
                    .font(.footnote).lineLimit(1)
                if let tags = run.tags, !tags.isEmpty, !showProject {
                    Text(tags.joined(separator: "  ")).font(.caption).foregroundStyle(.muted).lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            if let m = run.mainMetric {
                VStack(alignment: .trailing, spacing: 2) {
                    Text(fmt(m.value)).font(.subheadline.weight(.semibold))
                    Text(m.key).font(.caption).foregroundStyle(.muted).lineLimit(1)
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
        List {
            if !loaded, let error {
                Empty("Can't load alerts: \(error)").bareRow(above: 8)
            } else if loaded, alerts.isEmpty {
                Empty("No alerts. A script raises one with run.alert(), the server when a loss turns NaN or a run stalls.").bareRow(above: 8)
            }
            ForEach(Array(alerts.enumerated()), id: \.element.id) { i, a in
                NavigationLink(value: Route.run(a.run)) { AlertRow(alert: a, showRun: true).opacity(a.dismissedAt == nil ? 1 : 0.5) }
                    .navigationLinkIndicatorVisibility(.hidden)
                    .swipeActions {
                        if a.dismissedAt == nil {
                            Button("Dismiss") { Task { await dismiss([a.id]) } }.tint(.finished)
                        }
                    }
                    .panelRow(i, of: alerts.count, above: i == 0 ? 8 : 0)
            }
            Color.clear.frame(height: 16).bareRow()
        }
        .paperList()
        .toolbar {
            if alerts.contains(where: { $0.dismissedAt == nil }) {
                Button("Dismiss all") { Task { await dismiss([]) } }
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
            Dot(color: levelColor(alert.level))
            VStack(alignment: .leading, spacing: 3) {
                Text(alert.title).font(.subheadline.weight(.semibold)).accessibilityLabel("\(alert.level): \(alert.title)")
                Text(((showRun ? [alert.runName, alert.project] : []) + [ago(alert.ts)]).joined(separator: " · "))
                    .font(.footnote).foregroundStyle(.muted).lineLimit(1)
                if let first = alert.body.split(separator: "\n").first {
                    Text(first).font(.footnote).lineLimit(2)
                }
            }
        }
    }
}
