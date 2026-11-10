import AVKit
import Charts
import SwiftUI

/// One run, with the web UI's tabs.
struct RunView: View {
    @Environment(Store.self) private var store
    let id: String

    enum Section: String, CaseIterable, Identifiable {
        case charts = "Charts", samples = "Samples", logs = "Logs", system = "System", config = "Config", files = "Files"
        var id: Self { self }
    }

    @State private var run: Run?
    @State private var series: [String: Series] = [:]
    @State private var alerts: [Alert] = []
    @State private var artifacts: [Artifact] = []
    @AppStorage("runSection") private var lastSection = Section.charts // the tab people open, on every run
    @State private var error: String?
    @State private var okAt = Date.now // the last load that worked

    @State private var above = 0.0 // the height of what sits over the tab's content
    /// The main metric (a loss) first, the rest by name.
    private var trainKeys: [String] {
        let main = run?.mainMetric?.key
        return series.keys.filter { !$0.hasPrefix("sys/") }.sorted { $0 == main || ($1 != main && $0 < $1) }
    }
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
                GeometryReader { screen in
                    ScrollView {
                        VStack(alignment: .leading, spacing: 16) {
                            VStack(alignment: .leading, spacing: 16) {
                                if error != nil { NotUpdated(since: okAt) }
                                header(run)
                                if !alerts.isEmpty {
                                    VStack(alignment: .leading, spacing: 12) {
                                        ForEach(alerts) { AlertRow(alert: $0) }
                                    }
                                    .card()
                                }
                                Picker("Section", selection: Binding(get: { section }, set: { lastSection = $0 })) {
                                    ForEach(sections) { Text($0.rawValue).tag($0) }
                                }
                                .pickerStyle(.segmented)
                            }
                            .onGeometryChange(for: Double.self) { $0.size.height } action: { above = $0 }
                            // The log is its own scroller, as tall as what is left of the screen.
                            content(run, rest: max(280, screen.size.height - above - 48))
                        }
                        .padding(16)
                    }
                    .refreshable { await load() }
                }
            } else if let error {
                Empty("Can't load this run: \(error)").padding(16).frame(maxHeight: .infinity, alignment: .top)
            } else {
                Color.clear
            }
        }
        .background(.paper)
        // The name is the headline below; the bar says only where the run lives.
        .navigationTitle(run?.project ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .principal) { Text(run?.project ?? "").font(.subheadline).foregroundStyle(.muted) } }
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
            Text(run.name).font(.title2.bold()).kerning(-0.4)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Pill(text: run.status, color: statusColor(run.status))
                TimelineView(.periodic(from: .now, by: 1)) { t in
                    Text((run.running ? "for " : "ran ") + duration(run.duration(now: t.date))).font(.subheadline).foregroundStyle(.muted)
                }
            }
            // No heartbeat's age while the refresh fails: it is old because the phone could not ask.
            let beat = run.running && error == nil ? run.lastHeartbeatAt.map { "heartbeat \(ago($0))" } : run.running ? nil : "started \(ago(run.startedAt))"
            Text([run.host ?? "", beat ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")).font(.footnote).foregroundStyle(.muted)
            if run.progress != nil { RunProgress(run: run).padding(.top, 6) }
            if let notes = run.notes, !notes.isEmpty { Text(notes).font(.subheadline).padding(.top, 4) }
        }
    }

    @ViewBuilder private func content(_ run: Run, rest: Double) -> some View {
        switch section {
        case .charts:
            if trainKeys.isEmpty {
                Empty("No metrics yet. They appear as soon as the script calls run.log().")
            } else {
                charts(trainKeys, xLabel: "step")
            }
        case .system:
            if let load = SysLoad(run.metrics) { MachineLoad(load: load, boxed: false).card() }
            charts(sysKeys, xLabel: "second")
        case .samples:
            SamplesView(id: id, running: run.running)
        case .logs:
            LogsView(id: id, running: run.running).frame(height: rest)
        case .config:
            ConfigView(run: run)
        case .files:
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(artifacts.enumerated()), id: \.element.id) { i, a in
                    if i > 0 { Color.rule.frame(height: 1) }
                    VStack(alignment: .leading, spacing: 3) {
                        Text(a.name).font(.subheadline.weight(.semibold))
                        Text([a.kind, a.step.map { "step \(fmt(Double($0)))" }, a.size.map { ByteCountFormatter.string(fromByteCount: Int64($0), countStyle: .file) }]
                            .compactMap { $0 }.joined(separator: " · "))
                            .font(.footnote).foregroundStyle(.muted)
                        if a.url == nil { Text(a.uri).font(.caption.monospaced()).foregroundStyle(.muted).textSelection(.enabled) }
                    }
                    .padding(.vertical, 10)
                }
            }
            .padding(.horizontal, 14).card(0)
        }
    }

    private func charts(_ keys: [String], xLabel: String) -> some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 320), spacing: 14)], spacing: 14) {
            ForEach(keys, id: \.self) { k in
                MetricChart(title: k.hasPrefix("sys/") ? String(k.dropFirst(4)) : k, series: series[k]!, xLabel: xLabel)
            }
        }
    }
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
        let (from, to) = (pts.first?.step ?? 0, pts.last?.step ?? 0)
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline) {
                Text(title).font(.subheadline.weight(.semibold)).lineLimit(1)
                Spacer(minLength: 8)
                if let p = pick ?? pts.last { Text(fmt(p.last)).font(.headline) }
            }
            // Always there, so the plot does not jump when a finger lands.
            Text("\(xLabel) \(fmt(Double(pick?.step ?? to)))").font(.caption).foregroundStyle(.muted).opacity(pick == nil ? 0 : 1)
            Chart {
                ForEach(pts) { p in
                    if band {
                        AreaMark(x: .value(xLabel, p.step), yStart: .value("min", p.min), yEnd: .value("max", p.max))
                            .foregroundStyle(Color.series.opacity(0.14))
                    }
                    LineMark(x: .value(xLabel, p.step), y: .value(title, p.last))
                        .foregroundStyle(.series)
                        .lineStyle(StrokeStyle(lineWidth: 2, lineJoin: .round))
                }
                if let pick {
                    RuleMark(x: .value(xLabel, pick.step)).foregroundStyle(.muted).lineStyle(StrokeStyle(lineWidth: 1))
                    PointMark(x: .value(xLabel, pick.step), y: .value(title, pick.last)).foregroundStyle(.series).symbolSize(40)
                }
            }
            .chartYScale(domain: .automatic(includesZero: false))
            .chartXScale(domain: from...max(to, from + 1))
            // Hairlines across and nothing else: no frame, no vertical lines, no dashes.
            .chartYAxis {
                AxisMarks(position: .trailing, values: .automatic(desiredCount: 4)) { v in
                    AxisGridLine(stroke: StrokeStyle(lineWidth: 1)).foregroundStyle(.rule)
                    AxisValueLabel { if let d = v.as(Double.self) { Text(fmt(d)).font(.caption).foregroundStyle(.muted) } }
                }
            }
            .chartXAxis {
                AxisMarks(values: from == to ? [from] : [from, (from + to) / 2, to]) { v in
                    let s = v.as(Int.self) ?? 0
                    AxisValueLabel(anchor: s == from ? .topLeading : s == to ? .topTrailing : .top) {
                        Text(fmt(Double(s))).font(.caption).foregroundStyle(.muted)
                    }
                }
            }
            .chartXSelection(value: $selected)
            .frame(height: 170)
            if pts.count < series.step.count {
                Text("\(series.step.count - pts.count) NaN or infinite values not drawn").font(.caption).foregroundStyle(.crashed).padding(.top, 6)
            }
        }
        .card()
    }
}

struct ConfigView: View {
    let run: Run

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            if let s = run.summary?.flat, !s.isEmpty { table("Summary", s) }
            table("Config", run.config?.flat ?? [])
            table("Run", [("id", run.id), ("host", run.host ?? ""), ("tags", (run.tags ?? []).joined(separator: ", "))].filter { !$0.1.isEmpty }
                + (run.git?.flat.map { ("git." + $0.key, $0.value) } ?? []))
        }
    }

    private func table(_ title: String, _ rows: [(key: String, value: String)]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.footnote).foregroundStyle(.muted).accessibilityAddTraits(.isHeader)
            if rows.isEmpty {
                Empty("Nothing recorded.")
            } else {
                VStack(spacing: 0) {
                    ForEach(rows.indices, id: \.self) { i in
                        if i > 0 { Color.rule.frame(height: 1) }
                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            Text(rows[i].key).font(.footnote.monospaced()).foregroundStyle(.muted)
                            Spacer(minLength: 8)
                            Text(rows[i].value).font(.subheadline).multilineTextAlignment(.trailing).textSelection(.enabled)
                        }
                        .padding(.vertical, 10)
                    }
                }
                .padding(.horizontal, 14)
                .card(0)
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
            LazyVStack(alignment: .leading, spacing: 2) {
                ForEach(lines) { l in
                    Text(l.line)
                        .font(.caption.monospaced())
                        .foregroundStyle(Self.failure(l.line) ? .failed : .ink)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .textSelection(.enabled)
            .padding(12)
        }
        // Opens at the newest line and follows new ones, lets go when the reader scrolls up; a short log starts at the top.
        .defaultScrollAnchor(.bottom, for: .initialOffset)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        .defaultScrollAnchor(.top, for: .alignment)
        .background(.panel, in: .rect(cornerRadius: radius, style: .continuous))
        .overlay { if loaded && lines.isEmpty { Text("No output yet. What the script prints shows up here.").font(.footnote).foregroundStyle(.muted) } }
        .poll(every: { running ? 3 : 60 }) { await load() }
    }

    /// A line that names a failure. stderr alone does not: progress bars are written there.
    private static func failure(_ line: String) -> Bool {
        ["error", "exception", "traceback"].contains { line.localizedCaseInsensitiveContains($0) }
    }

    private func load() async {
        let path = lines.last.map { "/api/v1/runs/\(id)/logs?after=\($0.seq)&limit=2000" } ?? "/api/v1/runs/\(id)/logs?tail&limit=1000"
        guard let next: [LogLine] = try? await store.get(path) else { return }
        lines = Array((lines + next).suffix(3000))
        loaded = true
    }
}

/// Images and videos of every key on one scrubber, side by side, so each prompt is seen at the same step; the
/// latest step by default. Text tables keep a slider each, below.
struct SamplesView: View {
    @Environment(Store.self) private var store
    let id: String
    let running: Bool
    @State private var media: [Media] = []
    @State private var loaded = false
    @State private var step: Int? // where the scrubber is; none follows the newest
    @State private var picked: [String: Int] = [:] // a text key -> index into its steps; none follows the newest
    @State private var viewing: Viewing?

    private struct Viewing: Identifiable {
        let id: String // the image tapped
        let images: [Media]
    }

    var body: some View {
        let seen = media.filter { $0.kind != "text" }
        let steps = Array(Set(seen.map(\.step))).sorted()
        let index = step.flatMap { s in steps.lastIndex { $0 <= s } } ?? steps.count - 1
        // Each key at the chosen step, or at its last one before it.
        let shown = steps.isEmpty ? [] : Dictionary(grouping: seen, by: \.key).sorted { $0.key < $1.key }.flatMap { _, items -> [Media] in
            let at = items.map(\.step).filter { $0 <= steps[index] }.max()
            return items.filter { $0.step == at }
        }
        let tables = Dictionary(grouping: media.filter { $0.kind == "text" }, by: \.key)
        VStack(alignment: .leading, spacing: 24) {
            if loaded && media.isEmpty { Empty("No samples yet. run.image(), run.video() and run.text() put them here.") }
            if !steps.isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    HStack(spacing: 12) {
                        if steps.count > 1 {
                            // Left at the end, it goes on following the newest step.
                            Slider(value: Binding(get: { Double(index) }, set: { step = Int($0.rounded()) == steps.count - 1 ? nil : steps[Int($0.rounded())] }),
                                   in: 0...Double(steps.count - 1), step: 1)
                                .accessibilityLabel("Step")
                        }
                        Text("step \(fmt(Double(steps[index]))) · \(index + 1) of \(steps.count)").font(.footnote).foregroundStyle(.muted).fixedSize()
                    }
                    // By position, not by id: the view stays, so its image stays until the next step's has loaded.
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 10, alignment: .top), count: shown.count < 2 ? 1 : 2), spacing: 14) {
                        ForEach(Array(shown.enumerated()), id: \.offset) { _, m in
                            let item = MediaItem(media: m, key: m.key, from: m.step == steps[index] ? nil : m.step)
                            if m.isImage {
                                Button { viewing = Viewing(id: m.id, images: shown.filter(\.isImage)) } label: { item }
                                    .buttonStyle(.plain)
                                    .accessibilityHint("Opens full screen")
                            } else {
                                item
                            }
                        }
                    }
                }
            }
            ForEach(tables.keys.sorted(), id: \.self) { key in
                let items = tables[key]!
                let steps = Array(Set(items.map(\.step))).sorted()
                let index = min(picked[key] ?? steps.count - 1, steps.count - 1)
                VStack(alignment: .leading, spacing: 8) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(key).font(.subheadline.weight(.semibold)).lineLimit(1)
                        Spacer(minLength: 8)
                        Text("step \(fmt(Double(steps[index])))").font(.footnote).foregroundStyle(.muted)
                    }
                    if steps.count > 1 {
                        Slider(value: Binding(get: { Double(index) }, set: { picked[key] = Int($0.rounded()) == steps.count - 1 ? nil : Int($0.rounded()) }),
                               in: 0...Double(steps.count - 1), step: 1)
                    }
                    ForEach(Array(items.filter { $0.step == steps[index] }.enumerated()), id: \.offset) { _, m in MediaItem(media: m) }
                }
            }
        }
        .fullScreenCover(item: $viewing) { v in SampleViewer(images: v.images, at: v.id) }
        .poll(every: { running ? 10 : 120 }) {
            if let m: [Media] = try? await store.get("/api/v1/runs/\(id)/media") { media = m }
            loaded = true
        }
    }
}

struct MediaItem: View {
    @Environment(Store.self) private var store
    let media: Media
    var key: String? // in the grid of every key: which one this is
    var from: Int? // an older step standing in, when the key has nothing at the chosen one

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            switch media.kind {
            case "video" where !media.url.hasSuffix(".gif"):
                if let url = try? store.api.request(media.url).url {
                    let asset = AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": store.api.headers])
                    VideoPlayer(player: AVPlayer(playerItem: AVPlayerItem(asset: asset)))
                        .aspectRatio(16 / 9, contentMode: .fit)
                        .clipShape(.rect(cornerRadius: radiusSmall))
                }
            case "text":
                TextTable(url: media.url)
            default:
                RemoteImage(url: media.thumb)
            }
            if let key { Text(key).font(.caption.monospaced()).lineLimit(1).truncationMode(.middle).padding(.top, 2) }
            if let c = media.caption, !c.isEmpty { Text(c).font(.footnote).foregroundStyle(.muted).multilineTextAlignment(.leading) }
            if let from { Text("from step \(fmt(Double(from)))").font(.footnote).foregroundStyle(.muted) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// An image fetched with the session, which AsyncImage cannot send. A small source (pixel art, a 64 px
/// sample) is scaled by its nearest pixels, so it stays crisp. With `side`, a square thumbnail that fills.
struct RemoteImage: View {
    @Environment(Store.self) private var store
    let url: String
    var side: CGFloat?
    @State private var image: UIImage?

    var body: some View {
        let r = radiusSmall
        Group {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .interpolation(image.size.width * image.scale < 256 ? .none : .medium)
                    .aspectRatio(contentMode: side == nil ? .fit : .fill)
            } else {
                // The space a square image will take: most samples are, so little moves when it arrives.
                Rectangle().fill(.soft).aspectRatio(1, contentMode: .fit)
            }
        }
        .frame(width: side, height: side)
        .clipShape(.rect(cornerRadius: r))
        .overlay(RoundedRectangle(cornerRadius: r).strokeBorder(.rule))
        .task(id: url) { if let i = (try? await store.api.data(url)).flatMap(UIImage.init(data:)) { image = i } }
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
            .background(.paper)
            .navigationTitle(images.first { $0.id == at }.map { "\($0.key) · step \(fmt(Double($0.step)))" } ?? "")
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
                    .interpolation(image.size.width * image.scale < 256 ? .none : .medium)
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
                if let c = media.caption, !c.isEmpty { Text(c).font(.footnote).foregroundStyle(.muted).padding(.horizontal, 16) }
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
                            Text(k).font(.footnote).foregroundStyle(.muted)
                            Text(rows[i][k]!.text).font(.subheadline).textSelection(.enabled)
                        }
                    }
                }
                .card()
            }
        }
        .task(id: url) { rows = (try? await store.get(url)) ?? [] }
    }
}
