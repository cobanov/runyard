import Foundation
import Observation

/// Which server the app talks to and who it is signed in as. The address and the session live in the keychain.
@Observable
final class Store {
    var server: String = Keychain.read("server") ?? "" {
        didSet {
            Keychain.write("server", server.isEmpty ? nil : server)
            UserDefaults.standard.removeObject(forKey: Notify.seenKey) // another server's runs are not news
        }
    }
    var session: String? = Keychain.read("session") { didSet { Keychain.write("session", session) } }
    /// nil until /api/v1/me answered for the current server.
    var me: Me?
    var loadError: String?
    /// A run a link or a notification asked for; MainView opens it and clears this.
    var openRun: String?
    /// Alerts nobody put away yet, for the tab's badge.
    var activeAlerts = 0

    init() {
        #if DEBUG
        // UI tests start from a clean slate with `-resetState YES`.
        if UserDefaults.standard.bool(forKey: "resetState") { server = ""; session = nil }
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
            session = nil
            me = nil
            server = base
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

    /// Made-up runs answered inside the app (Demo.swift). It is kept like a server, so a relaunch opens the demo again.
    func demo() {
        session = nil
        me = nil
        loadError = nil
        server = DemoServer.base
    }

    var isDemo: Bool { server == DemoServer.base }

    func loadMe() async {
        do {
            me = try await api.get("/api/v1/me")
            loadError = nil
        } catch {
            loadError = error.localizedDescription
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
        API.media.configuration.urlCache?.removeAllCachedResponses()
        await loadMe()
    }

    func forget() {
        server = ""
        session = nil
        me = nil
        loadError = nil
        API.media.configuration.urlCache?.removeAllCachedResponses()
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
    func get<T: Decodable>(_ path: String) async throws -> T {
        do {
            return try await api.get(path)
        } catch let e as APIError where e.status == 401 {
            session = nil
            me = Me(auth: true, user: "")
            throw e
        }
    }
}
