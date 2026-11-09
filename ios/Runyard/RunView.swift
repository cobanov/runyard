import AVKit
import Charts
import SwiftUI

/// One run, with the web UI's tabs.
struct RunView: View {
    @Environment(Store.self) private var store
    let id: String

    enum Section: String, CaseIterable, Identifiable {
        case charts = "Charts", system = "System", samples = "Samples", logs = "Logs", config = "Config", files = "Files"
        var id: Self { self }
    }

    @State private var run: Run?
    @State private var series: [String: Series] = [:]
    @State private var alerts: [Alert] = []
    @State private var artifacts: [Artifact] = []
    @AppStorage("runSection") private var lastSection = Section.charts // the tab people open, on every run
    @State private var error: String?
    @State private var okAt = Date.now // the last load that worked

    private var trainKeys: [String] { series.keys.filter { !$0.hasPrefix("sys/") }.sorted() }
    private var sysKeys: [String] { series.keys.filter { $0.hasPrefix("sys/") }.sorted() }
    private var sections: [Section] {
        Section.allCases.filter { s in
            switch s {
            case .system: !sysKeys.isEmpty
            case .files: !artifacts.isEmpty
            default: true
            }
        }
    }
    /// The remembered tab, when this run has it.
    private var section: Section { sections.contains(lastSection) ? lastSection : .charts }

    var body: some View {
        Group {
            if let run {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        if error != nil { NotUpdated(since: okAt) }
                        header(run)
                        if !alerts.isEmpty {
                            VStack(alignment: .leading, spacing: 10) {
                                ForEach(alerts) { AlertRow(alert: $0) }
                            }
                            .padding(12)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(.orange.opacity(0.08), in: .rect(cornerRadius: 12))
                        }
                        Picker("Section", selection: Binding(get: { section }, set: { lastSection = $0 })) {
                            ForEach(sections) { Text($0.rawValue).tag($0) }
                        }
                        .pickerStyle(.segmented)
                        content(run)
                    }
                    .padding()
                }
                .refreshable { await load() }
            } else if let error {
                ContentUnavailableView("Can't load this run", systemImage: "exclamationmark.triangle", description: Text(error))
            } else {
                ProgressView()
            }
        }
        .navigationTitle(run?.name ?? "Run")
        .navigationBarTitleDisplayMode(.inline)
        .poll(every: { run?.running == false ? 60 : 5 }) { await load() }
    }

    private func load() async {
        do {
            async let r: Run = store.get("/api/v1/runs/\(id)")
            async let s: [String: Series] = store.get("/api/v1/runs/\(id)/metrics?points=300")
            async let a: [Alert] = store.get("/api/v1/alerts?run=\(id)&limit=10")
            async let f: [Artifact] = store.get("/api/v1/runs/\(id)/artifacts")
            (run, series, alerts, artifacts) = try await (r, s, a, f)
            (okAt, error) = (.now, nil)
        } catch {
            self.error = error.localizedDescription
        }
    }

    @ViewBuilder private func header(_ run: Run) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(run.project).font(.subheadline).foregroundStyle(.secondary)
            Text(run.name).font(.title2.bold())
            HStack(spacing: 8) {
                Text(run.status)
                    .font(.caption.bold())
                    .padding(.horizontal, 8).padding(.vertical, 3)
                    .background(statusColor(run.status).opacity(0.15), in: .capsule)
                    .foregroundStyle(statusColor(run.status))
                Text("for \(duration(run.duration())), started \(run.startedAt.formatted(.relative(presentation: .named)))")
                    .font(.subheadline).foregroundStyle(.secondary)
            }
            // Not while the refresh fails: the heartbeat is old because the phone could not ask.
            if run.running, error == nil, let beat = run.lastHeartbeatAt {
                Text("Last heartbeat \(beat.formatted(.relative(presentation: .named)))\(run.host.map { " from \($0)" } ?? "")")
                    .font(.caption).foregroundStyle(.secondary)
            } else if let host = run.host, !host.isEmpty {
                Text("on \(host)").font(.caption).foregroundStyle(.secondary)
            }
            RunProgress(run: run)
            if let notes = run.notes, !notes.isEmpty { Text(notes).font(.subheadline) }
        }
    }

    @ViewBuilder private func content(_ run: Run) -> some View {
        switch section {
        case .charts:
            if trainKeys.isEmpty {
                empty("No metrics yet", "They appear as soon as the script calls run.log().")
            } else {
                charts(trainKeys, xLabel: "step")
            }
        case .system:
            if let load = SysLoad(run.metrics) { SystemNow(load: load) }
            charts(sysKeys, xLabel: "second")
        case .samples:
            SamplesView(id: id, running: run.running)
        case .logs:
            LogsView(id: id, running: run.running)
        case .config:
            ConfigView(run: run)
        case .files:
            VStack(alignment: .leading, spacing: 0) {
                ForEach(artifacts) { a in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(a.name).font(.subheadline.bold())
                        Text([a.kind, a.step.map { "step \($0)" }, a.size.map { ByteCountFormatter.string(fromByteCount: Int64($0), countStyle: .file) }]
                            .compactMap { $0 }.joined(separator: " · "))
                            .font(.caption).foregroundStyle(.secondary)
                        if a.url == nil { Text(a.uri).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled) }
                    }
                    .padding(.vertical, 8)
                    Divider()
                }
            }
        }
    }

    private func charts(_ keys: [String], xLabel: String) -> some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 320), spacing: 12)], spacing: 12) {
            ForEach(keys, id: \.self) { k in
                MetricChart(title: k.hasPrefix("sys/") ? String(k.dropFirst(4)) : k, series: series[k]!, xLabel: xLabel)
            }
        }
    }
}

func empty(_ title: String, _ text: String) -> some View {
    ContentUnavailableView(title, systemImage: "chart.xyaxis.line", description: Text(text)).padding(.top, 24)
}

/// A metric's line, with the min–max band of each bucket when the server thinned it out. Touch shows a value.
struct MetricChart: View {
    let title: String
    let series: Series
    let xLabel: String
    @State private var selected: Int?

    private struct Point: Identifiable {
        let id: Int
        let step: Int
        let last, min, max: Double
    }

    private var points: [Point] {
        series.step.indices.compactMap { i in
            let p = Point(id: i, step: series.step[i], last: series.last[i].value, min: series.min[i].value, max: series.max[i].value)
            return p.last.isFinite ? p : nil
        }
    }

    var body: some View {
        let pts = points
        let band = pts.contains { $0.min != $0.max && $0.min.isFinite && $0.max.isFinite }
        let pick = selected.flatMap { s in pts.min { abs($0.step - s) < abs($1.step - s) } }
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(title).font(.subheadline.bold()).lineLimit(1)
                Spacer()
                if let p = pick ?? pts.last {
                    Text("\(fmt(p.last))  \(xLabel) \(String(p.step))").font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                }
            }
            Chart {
                ForEach(pts) { p in
                    if band {
                        AreaMark(x: .value(xLabel, p.step), yStart: .value("min", p.min), yEnd: .value("max", p.max))
                            .foregroundStyle(.tint.opacity(0.15))
                    }
                    LineMark(x: .value(xLabel, p.step), y: .value(title, p.last))
                        .foregroundStyle(.tint)
                        .interpolationMethod(.linear)
                }
                if let pick {
                    RuleMark(x: .value(xLabel, pick.step)).foregroundStyle(.secondary.opacity(0.5))
                    PointMark(x: .value(xLabel, pick.step), y: .value(title, pick.last)).foregroundStyle(.tint)
                }
            }
            .chartYScale(domain: .automatic(includesZero: false))
            .chartXSelection(value: $selected)
            .frame(height: 150)
            if pts.count < series.step.count {
                Text("\(series.step.count - pts.count) NaN or infinite values not drawn").font(.caption2).foregroundStyle(.orange)
            }
        }
        .padding(12)
        .background(.background.secondary, in: .rect(cornerRadius: 12))
    }
}

/// The latest readings of the SDK's system monitor (sys/cpu, sys/ram_gb, sys/gpu{i}.util|mem_gb|temp_c|power_w):
/// the machine's load at a glance, one row per GPU.
struct SystemNow: View {
    let load: SysLoad

    var body: some View {
        VStack(spacing: 8) {
            HStack(spacing: 8) {
                if let cpu = load.cpu { tile("CPU", "\(Int(cpu.rounded()))%", load: cpu) }
                if let ram = load.ramGB { tile("RAM", String(format: "%.1f GB", ram)) }
            }
            ForEach(load.gpus) { g in
                VStack(alignment: .leading, spacing: 6) {
                    HStack(alignment: .firstTextBaseline) {
                        Text("GPU \(g.id)").font(.subheadline.bold())
                        Spacer()
                        if let u = g.util { Text("\(Int(u.rounded()))%").font(.title3.bold().monospacedDigit()) }
                    }
                    if let u = g.util { ProgressView(value: min(max(u, 0), 100), total: 100).tint(loadColor(u)) }
                    Text(g.details).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                }
                .padding(10)
                .background(.background.secondary, in: .rect(cornerRadius: 10))
            }
        }
    }

    private func tile(_ title: String, _ value: String, load: Double? = nil) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption).foregroundStyle(.secondary)
            Text(value).font(.title3.bold().monospacedDigit())
            if let load { ProgressView(value: min(max(load, 0), 100), total: 100).tint(loadColor(load)) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(.background.secondary, in: .rect(cornerRadius: 10))
    }
}

func loadColor(_ percent: Double) -> Color { percent > 90 ? .orange : .accentColor }

struct ConfigView: View {
    let run: Run

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            if let s = run.summary?.flat, !s.isEmpty { table("Summary", s) }
            table("Config", run.config?.flat ?? [])
            table("Run", [("id", run.id), ("host", run.host ?? ""), ("tags", (run.tags ?? []).joined(separator: ", "))]
                + (run.git?.flat.map { ("git." + $0.key, $0.value) } ?? []))
        }
    }

    private func table(_ title: String, _ rows: [(key: String, value: String)]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(title).font(.headline).padding(.bottom, 6)
            if rows.isEmpty { Text("Nothing recorded.").font(.subheadline).foregroundStyle(.secondary) }
            ForEach(rows.indices, id: \.self) { i in
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Text(rows[i].key).font(.subheadline).foregroundStyle(.secondary)
                    Spacer(minLength: 8)
                    Text(rows[i].value).font(.subheadline.monospacedDigit()).multilineTextAlignment(.trailing).textSelection(.enabled)
                }
                .padding(.vertical, 7)
                Divider()
            }
        }
    }
}

/// stdout and stderr, newest at the bottom; follows the run while it is running.
struct LogsView: View {
    @Environment(Store.self) private var store
    let id: String
    let running: Bool
    @State private var lines: [LogLine] = []
    @State private var loaded = false

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 1) {
                ForEach(lines) { l in
                    Text(l.line)
                        .font(.caption.monospaced())
                        .foregroundStyle(l.stream == 2 ? .red : .primary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .textSelection(.enabled)
            .padding(10)
        }
        .defaultScrollAnchor(.bottom)
        .frame(height: 520)
        .background(.background.secondary, in: .rect(cornerRadius: 12))
        .overlay { if loaded && lines.isEmpty { Text("No output yet.").foregroundStyle(.secondary) } }
        .poll(every: { running ? 3 : 60 }) { await load() }
    }

    private func load() async {
        let path = lines.last.map { "/api/v1/runs/\(id)/logs?after=\($0.seq)&limit=2000" } ?? "/api/v1/runs/\(id)/logs?tail&limit=1000"
        guard let next: [LogLine] = try? await store.get(path) else { return }
        lines = Array((lines + next).suffix(3000))
        loaded = true
    }
}

/// Images, videos and text tables by key, with a slider through the steps; the latest step by default.
struct SamplesView: View {
    @Environment(Store.self) private var store
    let id: String
    let running: Bool
    @State private var media: [Media] = []
    @State private var loaded = false
    @State private var picked: [String: Int] = [:] // key -> index into its steps; none follows the newest
    @State private var viewing: Viewing?

    private struct Viewing: Identifiable {
        let id: String // the image tapped
        let images: [Media]
    }

    var body: some View {
        let groups = Dictionary(grouping: media, by: \.key)
        VStack(alignment: .leading, spacing: 20) {
            if loaded && media.isEmpty { empty("No samples yet", "run.image(), run.video() and run.text() show up here.") }
            ForEach(groups.keys.sorted(), id: \.self) { key in
                let items = groups[key]!
                let (steps, index) = place(key, items)
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Text(key).font(.headline)
                        Spacer()
                        Text("step \(String(steps[index]))").font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                    if steps.count > 1 {
                        // Left at the end, it goes on following the newest step.
                        Slider(value: Binding(get: { Double(index) }, set: { picked[key] = Int($0.rounded()) == steps.count - 1 ? nil : Int($0.rounded()) }),
                               in: 0...Double(steps.count - 1), step: 1)
                    }
                    // By position, not by id: the view stays, so its image stays until the next step's has loaded.
                    let shown = items.filter { $0.step == steps[index] }
                    ForEach(Array(shown.enumerated()), id: \.offset) { _, m in
                        if m.isImage {
                            Button { viewing = Viewing(id: m.id, images: onScreen(groups)) } label: { MediaItem(media: m) }
                                .buttonStyle(.plain)
                                .accessibilityHint("Opens full screen")
                        } else {
                            MediaItem(media: m)
                        }
                    }
                }
            }
        }
        .fullScreenCover(item: $viewing) { v in SampleViewer(images: v.images, at: v.id) }
        .poll(every: { running ? 10 : 120 }) {
            if let m: [Media] = try? await store.get("/api/v1/runs/\(id)/media") { media = m }
            loaded = true
        }
    }

    /// A key's steps, and the one its slider is on.
    private func place(_ key: String, _ items: [Media]) -> (steps: [Int], index: Int) {
        let steps = Array(Set(items.map(\.step))).sorted()
        return (steps, min(picked[key] ?? steps.count - 1, steps.count - 1))
    }

    /// Every image showing now, in the order of the page: what the full-screen viewer swipes through.
    private func onScreen(_ groups: [String: [Media]]) -> [Media] {
        groups.keys.sorted().flatMap { key in
            let (steps, index) = place(key, groups[key]!)
            return groups[key]!.filter { $0.step == steps[index] && $0.isImage }
        }
    }
}

struct MediaItem: View {
    @Environment(Store.self) private var store
    let media: Media

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            switch media.kind {
            case "video" where !media.url.hasSuffix(".gif"):
                if let url = try? store.api.request(media.url).url {
                    let asset = AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": store.api.headers])
                    VideoPlayer(player: AVPlayer(playerItem: AVPlayerItem(asset: asset)))
                        .aspectRatio(16 / 9, contentMode: .fit)
                        .clipShape(.rect(cornerRadius: 10))
                }
            case "text":
                TextTable(url: media.url)
            default:
                RemoteImage(url: media.thumb)
            }
            if let c = media.caption, !c.isEmpty { Text(c).font(.caption).foregroundStyle(.secondary) }
        }
    }
}

/// An image fetched with the session, which AsyncImage cannot send. Pixel art stays sharp.
struct RemoteImage: View {
    @Environment(Store.self) private var store
    let url: String
    @State private var image: UIImage?

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .interpolation(image.size.width < 128 ? .none : .medium)
                    .aspectRatio(contentMode: .fit)
                    .clipShape(.rect(cornerRadius: 10))
            } else {
                RoundedRectangle(cornerRadius: 10).fill(.background.secondary).frame(height: 160).overlay(ProgressView())
            }
        }
        .task(id: url) { image = (try? await store.api.data(url)).flatMap(UIImage.init(data:)) }
    }
}

/// The images on the Samples tab full screen: swipe between them, pinch or double-tap to zoom, share.
struct SampleViewer: View {
    @Environment(\.dismiss) private var dismiss
    let images: [Media]
    @State var at: String

    var body: some View {
        NavigationStack {
            TabView(selection: $at) {
                ForEach(images) { m in ZoomImage(media: m).tag(m.id) }
            }
            .tabViewStyle(.page)
            .navigationTitle(images.first { $0.id == at }.map { "\($0.key) · step \(String($0.step))" } ?? "")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } } }
        }
    }
}

struct ZoomImage: View {
    @Environment(Store.self) private var store
    let media: Media
    @State private var image: UIImage?
    @State private var scale = 1.0
    @State private var pinch = 1.0
    @State private var offset = CGSize.zero
    @State private var drag = CGSize.zero

    var body: some View {
        VStack(spacing: 12) {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .interpolation(image.size.width < 128 ? .none : .medium)
                    .aspectRatio(contentMode: .fit)
                    .scaleEffect(scale * pinch)
                    .offset(x: offset.width + drag.width, y: offset.height + drag.height)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .contentShape(.rect)
                    .gesture(MagnifyGesture().onChanged { pinch = $0.magnification }.onEnded { _ in
                        scale = min(max(scale * pinch, 1), 8)
                        pinch = 1
                        if scale == 1 { offset = .zero }
                    })
                    // Only while zoomed: at rest a drag is the swipe to the next image.
                    .gesture(scale > 1 ? DragGesture().onChanged { drag = $0.translation }.onEnded { _ in
                        offset = CGSize(width: offset.width + drag.width, height: offset.height + drag.height)
                        drag = .zero
                    } : nil)
                    .onTapGesture(count: 2) { withAnimation { (scale, offset) = (scale > 1 ? 1 : 3, .zero) } }
                    .accessibilityLabel(media.caption ?? "Sample image")
                if let c = media.caption, !c.isEmpty { Text(c).font(.caption).foregroundStyle(.secondary) }
                ShareLink(item: Image(uiImage: image), preview: SharePreview(media.caption ?? media.key, image: Image(uiImage: image)))
                    .padding(.bottom, 36) // clear of the page dots
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .clipped()
        .task { image = (try? await store.api.data(media.url)).flatMap(UIImage.init(data:)) }
    }
}

/// run.text(): a JSON list of rows, each shown as key–value lines.
struct TextTable: View {
    @Environment(Store.self) private var store
    let url: String
    @State private var rows: [[String: JSONValue]] = []

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(rows.indices, id: \.self) { i in
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(rows[i].keys.sorted(), id: \.self) { k in
                        VStack(alignment: .leading, spacing: 1) {
                            Text(k).font(.caption).foregroundStyle(.secondary)
                            Text(rows[i][k]!.text).font(.subheadline).textSelection(.enabled)
                        }
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(.background.secondary, in: .rect(cornerRadius: 10))
            }
        }
        .task(id: url) { rows = (try? await store.get(url)) ?? [] }
    }
}
