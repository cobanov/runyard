import Charts
import SwiftUI

/// Home, the overview: the counts that matter, what needs a look, what is training now and how it is going, the
/// machines, what ended lately, then every project.
struct HomeView: View {
    @Environment(Store.self) private var store
    @State private var agents: [Agent] = []
    @State private var running: [Run] = []
    @State private var spark: [String: [Double]] = [:] // run id -> recent values of its main metric
    @State private var thumb: [String: String] = [:] // run id -> its latest sample image
    @State private var ended: [Run] = [] // the newest runs that are not running; the first 8 are listed
    @State private var projects: [Project] = []
    @State private var machines: [Machine] = [] // from runyard beacon; none on a server without one
    @State private var loaded = false
    @State private var okAt = Date.now // the last load that worked
    @State private var error: String?
    @State private var deleting: Project?
    @State private var failure: String?

    var body: some View {
        let recent = Array(ended.prefix(8))
        let today = ended.filter { Calendar.current.isDateInToday($0.endedAt ?? $0.lastHeartbeatAt ?? $0.startedAt) }
        let bad = today.count { $0.status == "failed" || $0.status == "crashed" }
        List {
            if loaded {
                HStack(spacing: 10) {
                    Tile(count: running.count, label: "Running", color: .running)
                    Tile(count: today.count { $0.status == "finished" }, label: "Done today", color: .finished)
                    Tile(count: bad, label: "Failed today", color: .failed)
                    Button { store.tab = 1 } label: { Tile(count: store.active.count, label: "Alerts", color: .crashed) }.buttonStyle(.plain)
                }
                .bareRow(above: 4)
                if error != nil { NotUpdated(since: okAt).bareRow(above: 10) }
            } else if let error {
                Empty("Can't load runs: \(error)").bareRow()
            }
            if let a = store.active.first {
                Button { store.tab = 1 } label: { AlertLine(alert: a, more: store.active.count - 1) }
                    .buttonStyle(.plain).panelRow(above: 12)
            }
            if loaded, running.isEmpty {
                Heading("Running")
                Empty("Nothing is training. A run shows up here the moment a script calls runyard.init().").bareRow()
            }
            if !running.isEmpty {
                Heading("Running")
                ForEach(Array(running.enumerated()), id: \.element.id) { i, run in
                    NavigationLink(value: Route.run(run.id)) {
                        LiveRunCard(run: run, spark: spark[run.id] ?? [], thumb: thumb[run.id], stale: error != nil)
                    }
                    .navigationLinkIndicatorVisibility(.hidden)
                    .panelRow(above: i == 0 ? 0 : 12)
                }
            }
            if !machines.isEmpty {
                Heading("Machines")
                ForEach(Array(machines.enumerated()), id: \.element.id) { i, m in
                    MachineRow(machine: m, procs: false).panelRow(i, of: machines.count)
                }
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
        .navigationTitle("Overview")
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
            async let h: [Machine]? = try? store.get("/api/v1/hosts")
            let (runs, newest, ps) = try await (r, n, p)
            if let cards = await a { agents = cards.sorted(by: Agent.urgentFirst) }
            if let hosts = await h { machines = hosts }
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

/// One count of the overview: big and in its colour when there is something to count, quiet at zero.
struct Tile: View {
    let count: Int
    let label: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("\(count)").font(.title.bold()).foregroundStyle(count > 0 ? color : Color(uiColor: .tertiaryLabel)).contentTransition(.numericText())
            Text(label).font(.caption2.weight(.medium)).foregroundStyle(.muted).lineLimit(1).minimumScaleFactor(0.8)
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.panel, in: .rect(cornerRadius: 16, style: .continuous))
        .accessibilityElement(children: .combine)
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
            AlertIcon(level: alert.level)
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
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Image(systemName: "heart.fill").font(.caption)
                        .foregroundStyle(quiet == nil ? Color.running : .crashed)
                        .symbolEffect(.bounce, value: run.lastHeartbeatAt) // it beats when the run does
                        .accessibilityHidden(true)
                    Text(run.name).font(.headline).lineLimit(1)
                    Spacer(minLength: 8)
                    TimelineView(.periodic(from: .now, by: 1)) { t in
                        Text(duration(run.duration(now: t.date))).font(.subheadline.monospacedDigit()).foregroundStyle(.muted)
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
                    if spark.count > 1 { Sparkline(values: spark).frame(maxWidth: .infinity).frame(height: 52) } else { Spacer(minLength: 0) }
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
            if let load = SysLoad(run.metrics) { MachineLoad(load: load) }
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
                    Text(agent.title).font(.headline).lineLimit(1)
                    Spacer(minLength: 8)
                    Text(agent.word).font(.subheadline.bold()).foregroundStyle(agentColor(agent.state)).layoutPriority(1)
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

/// The machine at a glance: a bar for the CPU and for each GPU, teal until it is full, then orange.
struct MachineLoad: View {
    let load: SysLoad
    var boxed = true // off where the strip is a card of its own

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            if let cpu = load.cpu {
                row("CPU", cpu, load.ramGB.map { String(format: "RAM %.0f GB", $0) } ?? "")
            }
            ForEach(load.gpus) { g in
                row("GPU \(g.id)", g.util, [g.memGB.map { String(format: "%.0f GB", $0) }, g.tempC.map { "\(Int($0.rounded()))°" }]
                    .compactMap { $0 }.joined(separator: " · "))
            }
        }
        .font(.caption.monospacedDigit())
        .padding(boxed ? 12 : 0)
        .background(boxed ? Color.soft : .clear, in: .rect(cornerRadius: 12, style: .continuous))
    }

    private func row(_ name: String, _ util: Double?, _ details: String) -> some View {
        HStack(spacing: 8) {
            Text(name).frame(width: 44, alignment: .leading)
            Bar(value: (util ?? 0) / 100, fill: loadColor(util ?? 0))
            Text(util.map { "\(Int($0.rounded()))%" } ?? "–").bold().frame(width: 38, alignment: .trailing)
            Text(details).foregroundStyle(.muted).lineLimit(1).frame(width: 86, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
    }
}

struct Sparkline: View {
    let values: [Double]

    var body: some View {
        let low = values.min() ?? 0
        Chart(values.indices, id: \.self) { i in
            AreaMark(x: .value("i", i), yStart: .value("low", low), yEnd: .value("v", values[i]))
                .foregroundStyle(LinearGradient(colors: [Color.series.opacity(0.25), Color.series.opacity(0)], startPoint: .top, endPoint: .bottom))
            LineMark(x: .value("i", i), y: .value("v", values[i])).lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartYScale(domain: .automatic(includesZero: false))
        .foregroundStyle(.series)
        .accessibilityHidden(true)
    }
}

struct ProjectRow: View {
    let project: Project

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(project.name).font(.headline).lineLimit(1)
                if project.running > 0 { Pill(text: "\(project.running) running", color: .running) }
            }
            Text([project.runs == 1 ? "1 run" : "\(fmt(Double(project.runs))) runs", project.lastRunAt.map { "last " + ago($0) }]
                .compactMap { $0 }.joined(separator: " · "))
                .font(.subheadline).foregroundStyle(.muted).lineLimit(1)
        }
    }
}

/// A state as a word on a tint of its colour.
struct Pill: View {
    let text: String
    let color: Color

    var body: some View {
        Text(text).font(.caption.bold()).padding(.horizontal, 8).padding(.vertical, 3)
            .background(color.opacity(0.15), in: .capsule).foregroundStyle(color).fixedSize()
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
                Text(run.name).font(.headline).lineLimit(1)
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
    @State private var showOld = false // the ones put away stay out of sight until asked for

    var body: some View {
        let open = alerts.filter { $0.dismissedAt == nil }
        let old = alerts.filter { $0.dismissedAt != nil }
        List {
            if let error {
                Text(loaded ? "That did not work: \(error)" : "Can't load alerts: \(error)").font(.footnote).foregroundStyle(.failed).bareRow(above: 8)
            }
            if loaded, open.isEmpty {
                Empty(old.isEmpty ? "No alerts. A script raises one with run.alert(), the server when a loss turns NaN or a run stalls."
                      : "Nothing needs a look.").bareRow(above: 8)
            }
            ForEach(Array(open.enumerated()), id: \.element.id) { i, a in
                NavigationLink(value: Route.run(a.run)) { AlertRow(alert: a, showRun: true) }
                    .navigationLinkIndicatorVisibility(.hidden)
                    .swipeActions { Button("Dismiss") { Task { await dismiss([a.id]) } }.tint(.gray) }
                    .panelRow(i, of: open.count, above: i == 0 ? 8 : 0)
            }
            if !old.isEmpty {
                Button(showOld ? "Hide dismissed" : "Show \(old.count) dismissed") { withAnimation { showOld.toggle() } }
                    .buttonStyle(.borderless).font(.subheadline).frame(maxWidth: .infinity).bareRow(above: 16, below: 8)
                if showOld {
                    ForEach(Array(old.enumerated()), id: \.element.id) { i, a in
                        NavigationLink(value: Route.run(a.run)) { AlertRow(alert: a, showRun: true).opacity(0.55) }
                            .navigationLinkIndicatorVisibility(.hidden)
                            .panelRow(i, of: old.count)
                    }
                }
            }
            Color.clear.frame(height: 16).bareRow()
        }
        .paperList()
        .toolbar {
            if !open.isEmpty {
                Button("Dismiss all") { Task { await dismiss([]) } }
            }
        }
        .navigationTitle("Alerts")
        .refreshable { await load() }
        .poll(every: { 15 }) { await load() }
    }

    /// Puts alerts away (all of them for no ids): they leave the list, and stay behind "Show dismissed".
    private func dismiss(_ ids: [Int]) async {
        do {
            try await store.post("/api/v1/alerts/dismiss", json: ["ids": ids])
            await load()
        } catch {
            self.error = error.localizedDescription
        }
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
            AlertIcon(level: alert.level)
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

struct AlertIcon: View {
    let level: String

    var body: some View {
        Image(systemName: level == "error" ? "exclamationmark.octagon.fill" : level == "warn" ? "exclamationmark.triangle.fill" : "info.circle.fill")
            .foregroundStyle(levelColor(level)).accessibilityHidden(true)
    }
}
