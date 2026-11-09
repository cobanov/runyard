import Foundation
import Security

// The runyard REST API (server/api.go): the app reads, and deletes runs and projects (server/admin.go); it never writes runs.

nonisolated struct Me: Decodable, Sendable {
    let auth: Bool
    let user: String
}

nonisolated struct Project: Decodable, Identifiable, Hashable, Sendable {
    let name: String
    let runs: Int
    let running: Int
    let lastRunAt: Date?
    var id: String { name }
    enum CodingKeys: String, CodingKey { case name, runs, running, lastRunAt = "last_run_at" }
}

nonisolated struct Run: Decodable, Identifiable, Hashable, Sendable {
    let id: String
    let project: String
    let name: String
    let status: String
    let config: JSONValue?
    let summary: JSONValue?
    let tags: [String]?
    let notes: String?
    let host: String?
    let git: JSONValue?
    let startedAt: Date
    let endedAt: Date?
    let lastHeartbeatAt: Date?
    let metrics: [String: MetricSummary]? // only from GET /runs/{id}
    enum CodingKeys: String, CodingKey {
        case id, project, name, status, config, summary, tags, notes, host, git, metrics
        case startedAt = "started_at", endedAt = "ended_at", lastHeartbeatAt = "last_heartbeat_at"
    }

    var running: Bool { status == "running" }

    /// What a row shows, as the web UI picks it: the shortest key with "loss" in it, else the most logged key.
    var mainMetric: (key: String, value: Double)? {
        guard let m = metrics?.filter({ !$0.key.hasPrefix("sys/") }), !m.isEmpty else { return nil }
        let key = m.keys.filter { $0.contains("loss") }.min { ($0.count, $0) < ($1.count, $1) }
            ?? m.keys.sorted().max { m[$0]!.count < m[$1]!.count }!
        return (key, m[key]!.last.value)
    }
    /// The number on an agent's card: the project's main metric, else what a row shows.
    func score(_ metric: String) -> (key: String, value: Double)? {
        metric.isEmpty ? mainMetric : metrics?[metric].map { (metric, $0.last.value) }
    }
    /// How long it ran: until it ended, until now while running, else until its last heartbeat (a crash).
    func duration(now: Date = .now) -> TimeInterval {
        (endedAt ?? (running ? now : lastHeartbeatAt ?? startedAt)).timeIntervalSince(startedAt)
    }

    /// How far it is, when the config names a total: steps (HF max_steps and the like) against the latest
    /// logged step, else epochs against an epoch metric. Nested config keys count by their last part.
    var progress: (done: Double, total: Double, unit: String)? {
        var cfg: [String: Double] = [:]
        for (k, v) in config?.flat ?? [] {
            guard let d = Double(v) else { continue }
            let name = String(k.split(separator: ".").last ?? "")
            cfg[name] = Swift.max(cfg[name] ?? d, d) // sampler.steps must not hide train.steps
        }
        let total = { (names: [String]) in names.lazy.compactMap { cfg[$0] }.first { $0 > 0 } }
        let train = (metrics ?? [:]).filter { !$0.key.hasPrefix("sys/") }
        let done = Double(train.values.map(\.step).max() ?? 0)
        // A plain "steps" may be a sampler's, so one the run has passed is not its total.
        if let t = total(["max_steps", "total_steps", "num_steps", "train_steps", "steps"]), done <= t {
            return (done, t, "step")
        }
        if let t = total(["num_train_epochs", "epochs", "max_epochs", "num_epochs"]),
           let e = train.first(where: { $0.key == "epoch" || $0.key.hasSuffix("/epoch") })?.value.last.value, e.isFinite {
            return (e, t, "epoch")
        }
        return nil
    }
}

/// A machine's latest readings, from the sys/ metrics the client logs (sys/cpu, sys/ram_gb, sys/gpu{i}.util ...).
nonisolated struct SysLoad: Sendable {
    struct GPU: Identifiable, Sendable {
        let id: Int
        let util, memGB, tempC, powerW: Double?
        var details: String {
            [memGB.map { String(format: "%.1f GB", $0) }, tempC.map { "\(Int($0.rounded())) °C" }, powerW.map { "\(Int($0.rounded())) W" }]
                .compactMap { $0 }.joined(separator: " · ")
        }
    }
    let cpu: Double?
    let ramGB: Double?
    let gpus: [GPU]

    init(cpu: Double?, ramGB: Double?, gpus: [GPU]) {
        (self.cpu, self.ramGB, self.gpus) = (cpu, ramGB, gpus)
    }

    init?(_ metrics: [String: MetricSummary]?) {
        // Finite only: a script can log NaN under sys/, and Int(NaN) traps where the bars round it.
        let v = (metrics ?? [:]).filter { $0.key.hasPrefix("sys/") }.mapValues(\.last.value).filter(\.value.isFinite)
        guard !v.isEmpty else { return nil }
        cpu = v["sys/cpu"]
        ramGB = v["sys/ram_gb"]
        gpus = Set(v.keys.compactMap { k -> Int? in
            guard k.hasPrefix("sys/gpu"), let dot = k.firstIndex(of: ".") else { return nil }
            return Int(k[k.index(k.startIndex, offsetBy: 7)..<dot])
        }).sorted().map { i in
            let g = { (field: String) in v["sys/gpu\(i).\(field)"] }
            return GPU(id: i, util: g("util"), memGB: g("mem_gb"), tempC: g("temp_c"), powerW: g("power_w"))
        }
    }
}

nonisolated struct MetricSummary: Decodable, Hashable, Sendable {
    let count: Int
    let step: Int
    let last: Num
    let min: Num
    let max: Num
}

/// One metric's history in buckets: the last value of each bucket, and its min and max.
nonisolated struct Series: Decodable, Sendable {
    let step: [Int]
    let min: [Num]
    let max: [Num]
    let last: [Num]
}

nonisolated struct LogLine: Decodable, Identifiable, Sendable {
    let seq: Int
    let ts: Date
    let stream: Int // 1 stdout, 2 stderr
    let line: String
    var id: Int { seq }
}

nonisolated struct Media: Decodable, Identifiable, Sendable {
    let id: String
    let key: String
    let step: Int
    let kind: String // image | video | text
    let url: String
    let meta: JSONValue?
    var caption: String? { meta?["caption"]?.string }
    /// Drawn as a still image (a GIF too), so it can open full screen.
    var isImage: Bool { kind != "text" && (kind != "video" || url.hasSuffix(".gif")) }
    /// For the grid: the server answers ?w= with a JPEG no wider than that (an older one ignores it and
    /// sends the original). Only its own paths: a signed S3 URL breaks when a parameter is added.
    var thumb: String { url.hasPrefix("/media/") ? url + "?w=640" : url }
}

nonisolated struct Artifact: Decodable, Identifiable, Sendable {
    let id: String
    let name: String
    let kind: String
    let step: Int?
    let uri: String
    let size: Int?
    let url: String?
}

nonisolated struct Alert: Decodable, Identifiable, Sendable {
    let id: Int
    let run: String
    let runName: String
    let project: String
    let ts: Date
    let level: String // info | warn | error
    let title: String
    let body: String
    let dismissedAt: Date? // put away by someone; the web Overview leaves it out
    enum CodingKeys: String, CodingKey { case id, run, project, ts, level, title, body, runName = "run_name", dismissedAt = "dismissed_at" }
}

/// A machine running `runyard beacon` (server/hosts.go): its latest reading, every process on its GPUs, and the runs on it.
nonisolated struct Machine: Decodable, Identifiable, Sendable {
    struct Proc: Decodable, Sendable {
        let pid: Int
        let name: String
        let user: String?
        let memGB: Double
        enum CodingKeys: String, CodingKey { case pid, name, user, memGB = "mem_gb" }
    }
    struct GPU: Decodable, Sendable {
        let i: Int
        let util, memGB, tempC, powerW: Double?
        let procs: [Proc]?
        enum CodingKeys: String, CodingKey { case i, util, procs, memGB = "mem_gb", tempC = "temp_c", powerW = "power_w" }
    }
    struct Stats: Decodable, Sendable {
        let cpu, ramGB: Double?
        let gpus: [GPU]?
        enum CodingKeys: String, CodingKey { case cpu, gpus, ramGB = "ram_gb" }
    }
    struct OnIt: Decodable, Sendable {
        let id, name, project: String
    }
    let name: String
    let seenAt: Date
    let silent: Bool
    let stats: Stats
    let runs: [OnIt]
    var id: String { name }
    enum CodingKeys: String, CodingKey { case name, silent, stats, runs, seenAt = "seen_at" }

    var load: SysLoad {
        SysLoad(cpu: stats.cpu, ramGB: stats.ramGB, gpus: (stats.gpus ?? []).map {
            SysLoad.GPU(id: $0.i, util: $0.util, memGB: $0.memGB, tempC: $0.tempC, powerW: $0.powerW)
        })
    }
    /// Every GPU process, with the GPU it is on.
    var procs: [(gpu: Int, proc: Proc)] { (stats.gpus ?? []).flatMap { g in (g.procs ?? []).map { (g.i, $0) } } }
}

/// A coding agent's session (server/agents.go), as the web's kiosk shows it (web/src/Kiosk.svelte).
nonisolated struct Agent: Decodable, Identifiable, Sendable {
    struct LastRun: Decodable, Sendable {
        let id, name: String
    }
    let id, tool, label, host, cwd, metric, state, said, report: String
    let project: String?
    let runs: Int
    let lastRun: LastRun?
    let activeAt: Date
    enum CodingKeys: String, CodingKey {
        case id, tool, label, host, cwd, metric, state, said, report, project, runs
        case lastRun = "last_run", activeAt = "active_at"
    }

    var title: String { !label.isEmpty ? label : cwd.split(separator: "/").last.map(String.init) ?? tool }
    var word: String {
        ["waiting": "Waiting for you", "error": "Error", "done": "Done", "training": "Training", "working": "Working",
         "idle": "Idle", "stale": "Quiet", "ended": "Ended"][state] ?? state
    }
    private var rank: Int { ["waiting": 0, "error": 1, "done": 2, "training": 3, "working": 3, "idle": 4, "stale": 5, "ended": 6][state] ?? 7 }

    /// Whatever needs a person comes first, then the newest activity.
    static func urgentFirst(_ a: Agent, _ b: Agent) -> Bool { (a.rank, b.activeAt) < (b.rank, a.activeAt) }
}

/// A metric value: the server sends NaN and ±Infinity as strings, which JSON numbers cannot hold.
nonisolated struct Num: Decodable, Hashable, Sendable {
    let value: Double
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if let d = try? c.decode(Double.self) {
            value = d
        } else {
            value = Double(try c.decode(String.self)) ?? .nan
        }
    }
}

/// Free-form JSON: run config, summary, git, media meta.
nonisolated enum JSONValue: Decodable, Hashable, Sendable {
    case null, bool(Bool), number(Double), string(String), array([JSONValue]), object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(Double.self) { self = .number(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else if let v = try? c.decode([JSONValue].self) { self = .array(v) }
        else { self = .object(try c.decode([String: JSONValue].self)) }
    }

    subscript(key: String) -> JSONValue? {
        if case .object(let o) = self { return o[key] }
        return nil
    }

    var string: String? {
        if case .string(let s) = self { return s }
        return nil
    }

    var text: String {
        switch self {
        case .null: "null"
        case .bool(let b): b ? "true" : "false"
        case .number(let d): fmt(d)
        case .string(let s): s
        case .array(let a): "[" + a.map(\.text).joined(separator: ", ") + "]"
        case .object: "{…}"
        }
    }

    /// Nested objects as dotted keys, sorted, as the web UI's Config tab shows them.
    var flat: [(key: String, value: String)] {
        guard case .object(let o) = self else { return [] }
        return o.keys.sorted().flatMap { k -> [(key: String, value: String)] in
            let v = o[k]!
            if case .object = v { return v.flat.map { (k + "." + $0.key, $0.value) } }
            return [(k, v.text)]
        }
    }
}

/// Numbers short and the same in every locale (a dot for decimals, no grouping), with an exponent only when tiny or huge.
nonisolated func fmt(_ v: Double) -> String {
    if v.isNaN { return "NaN" }
    if v.isInfinite { return v > 0 ? "∞" : "-∞" }
    if v == v.rounded() && abs(v) < 1e15 { return String(Int64(v)) }
    return String(format: "%.4g", v)
}

nonisolated struct APIError: LocalizedError {
    let status: Int
    let message: String
    var errorDescription: String? { status == 0 ? message : "\(message) (\(status))" }
}

nonisolated struct API: Sendable {
    /// Scheme, host and port, no trailing slash: https://toji.example.ts.net or http://192.168.1.10:8080.
    let base: String
    /// The web UI's session cookie value, kept in the keychain instead of a cookie jar.
    let session: String?

    static let http = session()
    /// Samples never change, so they stay on the device. The default cache skips anything over 500 KB, which is every one.
    static let media = session(cache: URLCache(memoryCapacity: 32 << 20, diskCapacity: 1 << 30,
                                               directory: URL.cachesDirectory.appending(path: "samples")))

    private static func session(cache: URLCache? = nil) -> URLSession {
        let c = URLSessionConfiguration.default
        c.httpShouldSetCookies = false
        c.httpCookieStorage = nil
        c.timeoutIntervalForRequest = 20
        c.protocolClasses = [DemoServer.self] + (c.protocolClasses ?? [])
        if let cache { c.urlCache = cache }
        return URLSession(configuration: c)
    }

    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        nonisolated(unsafe) let iso = ISO8601DateFormatter()
        d.dateDecodingStrategy = .custom { dec in
            var s = try dec.singleValueContainer().decode(String.self)
            // Go sends up to nine fraction digits, which ISO8601DateFormatter refuses; seconds are enough here.
            if let dot = s.firstIndex(of: ".") {
                let end = s[s.index(after: dot)...].firstIndex { !$0.isNumber } ?? s.endIndex
                s.removeSubrange(dot..<end)
            }
            guard let date = iso.date(from: s) else {
                throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: "not a date: \(s)"))
            }
            return date
        }
        return d
    }()

    /// A server path (/api/v1/...) or a URL the server handed out (media may live on S3) as a request.
    func request(_ path: String, method: String = "GET", timeout: TimeInterval = 20) throws -> URLRequest {
        guard let url = URL(string: path.contains("://") ? path : base + path) else {
            throw APIError(status: 0, message: "Bad address: \(base + path)")
        }
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = method
        if let session, !path.contains("://") { req.setValue("runyard_session=\(session)", forHTTPHeaderField: "Cookie") }
        return req
    }

    func send(_ req: URLRequest, via session: URLSession = API.http) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: req)
        guard let http = response as? HTTPURLResponse else { throw APIError(status: 0, message: "No HTTP response") }
        guard (200..<300).contains(http.statusCode) else {
            struct Body: Decodable { let error: String? }
            let msg = (try? JSONDecoder().decode(Body.self, from: data))?.error
            throw APIError(status: http.statusCode, message: msg ?? HTTPURLResponse.localizedString(forStatusCode: http.statusCode))
        }
        return (data, http)
    }

    func data(_ path: String) async throws -> Data {
        try await send(request(path), via: path.contains("/media/") ? API.media : API.http).0
    }

    /// With `keep`, the answer is also written to the caches directory, for when the server cannot be reached.
    func get<T: Decodable>(_ path: String, keep: Bool = false) async throws -> T {
        let data = try await data(path)
        let value = try API.decoder.decode(T.self, from: data)
        if keep {
            try? FileManager.default.createDirectory(at: API.offline, withIntermediateDirectories: true)
            try? data.write(to: file(path), options: .atomic)
        }
        return value
    }

    static let offline = URL.cachesDirectory.appending(path: "offline")
    private func file(_ path: String) -> URL { API.offline.appending(path: q(base + path)) }

    /// The last answer kept for this server and path, and when it came.
    func kept<T: Decodable>(_ path: String) -> (value: T, at: Date)? {
        guard let data = try? Data(contentsOf: file(path)), let value = try? API.decoder.decode(T.self, from: data),
              let at = try? file(path).resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate else { return nil }
        return (value, at)
    }

    /// Headers for players that fetch a URL themselves (AVPlayer).
    var headers: [String: String] {
        session.map { ["Cookie": "runyard_session=\($0)"] } ?? [:]
    }
}

/// Encodes a value for a URL query.
nonisolated func q(_ s: String) -> String {
    s.addingPercentEncoding(withAllowedCharacters: .alphanumerics.union(.init(charactersIn: "-._~"))) ?? s
}

nonisolated enum Keychain {
    private static func query(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: "dev.cobanov.runyard",
         kSecAttrAccount as String: account]
    }

    static func read(_ account: String) -> String? {
        var q = query(account)
        q[kSecReturnData as String] = true
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func write(_ account: String, _ value: String?) {
        SecItemDelete(query(account) as CFDictionary)
        guard let value else { return }
        var q = query(account)
        q[kSecValueData as String] = Data(value.utf8)
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(q as CFDictionary, nil)
    }
}
