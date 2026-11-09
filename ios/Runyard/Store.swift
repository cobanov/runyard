import Foundation
import Observation

/// Which server the app talks to and who it is signed in as. The addresses and the sessions live in the keychain.
@Observable
final class Store {
    var server: String = Keychain.read("server") ?? "" {
        didSet {
            Keychain.write("server", server.isEmpty ? nil : server)
            UserDefaults.standard.removeObject(forKey: Notify.seenKey) // another server's runs are not news
            session = Keychain.read("session " + server) // each server keeps its own
        }
    }
    var session: String? = Keychain.read("session " + (Keychain.read("server") ?? "")) {
        didSet { Keychain.write("session " + server, session) }
    }
    /// Every server the app has connected to, the current one too, oldest first. The demo is not one.
    var servers: [String] = (Keychain.read("servers") ?? "").split(separator: "\n").map(String.init) {
        didSet { Keychain.write("servers", servers.isEmpty ? nil : servers.joined(separator: "\n")) }
    }
    /// nil until /api/v1/me answered for the current server.
    var me: Me?
    var loadError: String?
    /// A run a link or a notification asked for; MainView opens it and clears this.
    var openRun: String?
    /// Alerts nobody put away yet, for the tab's badge.
    var activeAlerts = 0

    init() {
        // A version with one server kept its session under "session".
        if let old = Keychain.read("session") {
            Keychain.write("session", nil)
            if session == nil, !server.isEmpty {
                Keychain.write("session " + server, old) // by hand: an observer may not run for a write made in init
                session = old
            }
        }
        if !server.isEmpty, !isDemo, !servers.contains(server) { servers.append(server) }
        #if DEBUG
        // UI tests start from a clean slate with `-resetState YES`.
        if UserDefaults.standard.bool(forKey: "resetState") {
            servers.forEach(forget)
            leave()
            UserDefaults.standard.removePersistentDomain(forName: Bundle.main.bundleIdentifier ?? "")
        }
        #endif
    }

    var api: API { API(base: server, session: session) }
    var needsLogin: Bool { me.map { $0.auth && $0.user.isEmpty } ?? false }
    /// Without RUNYARD_AUTH the server wants an API key to delete, and the app has no place for one.
    var canDelete: Bool { isDemo || me?.auth == true }
    var plainHTTP: Bool { server.hasPrefix("http://") }

    /// Finds a runyard server at what the user typed: a full URL, or host[:port] tried over https, then http,
    /// then on 8080 (the server's default) when no port was given.
    func connect(_ address: String) async throws {
        var a = address.trimmingCharacters(in: .whitespacesAndNewlines)
        while a.hasSuffix("/") { a.removeLast() }
        guard !a.isEmpty else { throw APIError(status: 0, message: "Enter the server's address.") }
        struct Health: Decodable { let ok: Bool }
        var bases = a.contains("://") ? [a] : ["https://" + a, "http://" + a]
        if URLComponents(string: bases[0])?.port == nil { bases.append((a.contains("://") ? a : "http://" + a) + ":8080") }
        var why: [String] = []
        for base in bases {
            let api = API(base: base, session: nil)
            do {
                let res = try await api.send(api.request("/api/v1/health", timeout: 8))
                guard (try? JSONDecoder().decode(Health.self, from: res.0))?.ok == true else {
                    throw APIError(status: 0, message: "answered, but is not a runyard server")
                }
            } catch {
                why.append("\(base): \(Self.reason(error))")
                continue
            }
            if !servers.contains(base) { servers.append(base) }
            use(base)
            await loadMe()
            return
        }
        throw APIError(status: 0, message: "No runyard server found.\n" + why.joined(separator: "\n"))
    }

    /// Why a try failed, in a few words: the system's sentences are long and say "the Internet" for a LAN.
    private static func reason(_ error: Error) -> String {
        switch (error as? URLError)?.code {
        case .cannotFindHost, .dnsLookupFailed: "no such host name (DNS)"
        case .timedOut: "no answer (timed out)"
        case .cannotConnectToHost: "nothing listens on that port"
        case .notConnectedToInternet, .networkConnectionLost: "this phone is offline"
        case .secureConnectionFailed: "no https there"
        case .serverCertificateUntrusted, .serverCertificateHasBadDate, .serverCertificateHasUnknownRoot,
             .serverCertificateNotYetValid: "its certificate is not trusted"
        case nil where error is APIError && (error as! APIError).status != 0: "answered, but is not a runyard server (\((error as! APIError).status))"
        default: error.localizedDescription
        }
    }

    func countAlerts() async {
        if let a: [Alert] = try? await get("/api/v1/alerts?active&limit=20") { activeAlerts = a.count }
    }
    /// The request stops at 20, so 20 means "20 or more".
    var alertsBadge: String? { activeAlerts == 0 ? nil : activeAlerts < 20 ? "\(activeAlerts)" : "20+" }

    /// Made-up runs answered inside the app (Demo.swift). It is kept like a server, so a relaunch opens the demo again.
    func demo() { use(DemoServer.base) }

    /// Switches to a server in the list. Its session is still in the keychain, so there is no sign-in.
    func use(_ base: String) {
        me = nil
        loadError = nil
        server = base
    }

    var isDemo: Bool { server == DemoServer.base }

    func loadMe() async {
        do {
            me = try await api.get("/api/v1/me", keep: true)
            loadError = nil
        } catch {
            loadError = error.localizedDescription
            // Out of reach at launch: the home screen shows what the server said last, and says how old it is.
            if me == nil, let kept: Me = api.kept("/api/v1/me")?.value, !kept.auth || !kept.user.isEmpty { me = kept }
        }
    }

    func login(name: String, password: String) async throws {
        var req = try api.request("/api/v1/login", method: "POST")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["name": name, "password": password])
        let resp: HTTPURLResponse
        do {
            resp = try await api.send(req).1
        } catch let e as APIError where e.status == 401 {
            throw APIError(status: 0, message: "Wrong name or password.")
        }
        let fields = resp.allHeaderFields as? [String: String] ?? [:]
        guard let url = resp.url,
              let cookie = HTTPCookie.cookies(withResponseHeaderFields: fields, for: url).first(where: { $0.name == "runyard_session" }) else {
            throw APIError(status: 0, message: "The server did not start a session.")
        }
        session = cookie.value
        await loadMe()
    }

    func logout() async {
        if let req = try? api.request("/api/v1/logout", method: "POST") { _ = try? await api.send(req) }
        session = nil
        me = nil
        clearCaches()
        await loadMe()
    }

    /// Back to the connect screen. Every server stays in the list, signed in.
    func leave() {
        use("")
        clearCaches()
    }

    /// Drops a server from the list, and its session.
    func forget(_ base: String) {
        Keychain.write("session " + base, nil)
        servers.removeAll { $0 == base }
        if base == server { leave() }
    }

    private func clearCaches() {
        API.media.configuration.urlCache?.removeAllCachedResponses()
        try? FileManager.default.removeItem(at: API.offline)
    }

    /// DELETE for the swipe actions. Only an admin may, so a 401 or 403 is answered with a sentence saying so.
    func delete(_ path: String) async throws {
        do {
            _ = try await api.send(api.request(path, method: "DELETE"))
        } catch let e as APIError where e.status == 401 || e.status == 403 {
            throw APIError(status: 0, message: "Only an admin can delete. Sign in as one.")
        }
    }

    /// POST with a JSON body, for putting alerts away.
    func post(_ path: String, json: [String: any Sendable]) async throws {
        var req = try api.request(path, method: "POST")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: json)
        _ = try await api.send(req)
    }

    /// GET for the screens: a 401 means the session ended, so the app goes back to the login form.
    func get<T: Decodable>(_ path: String, keep: Bool = false) async throws -> T {
        do {
            return try await api.get(path, keep: keep)
        } catch let e as APIError where e.status == 401 {
            session = nil
            me = Me(auth: true, user: "")
            throw e
        }
    }
}
