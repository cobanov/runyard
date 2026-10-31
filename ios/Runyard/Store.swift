import Foundation
import Observation

/// Which server the app talks to and who it is signed in as. The address and the session live in the keychain.
@Observable
final class Store {
    var server: String = Keychain.read("server") ?? "" { didSet { Keychain.write("server", server.isEmpty ? nil : server) } }
    var session: String? = Keychain.read("session") { didSet { Keychain.write("session", session) } }
    /// nil until /api/v1/me answered for the current server.
    var me: Me?
    var loadError: String?

    init() {
        #if DEBUG
        // UI tests start from a clean slate with `-resetState YES`.
        if UserDefaults.standard.bool(forKey: "resetState") { server = ""; session = nil }
        #endif
    }

    var api: API { API(base: server, session: session) }
    var needsLogin: Bool { me.map { $0.auth && $0.user.isEmpty } ?? false }

    /// Finds a runyard server at what the user typed: a full URL, or host[:port] tried over https, then http.
    func connect(_ address: String) async throws {
        var a = address.trimmingCharacters(in: .whitespacesAndNewlines)
        while a.hasSuffix("/") { a.removeLast() }
        guard !a.isEmpty else { throw APIError(status: 0, message: "Enter the server's address.") }
        struct Health: Decodable { let ok: Bool }
        for base in a.contains("://") ? [a] : ["https://" + a, "http://" + a] {
            let api = API(base: base, session: nil)
            guard let req = try? api.request("/api/v1/health", timeout: 8),
                  let res = try? await api.send(req),
                  (try? JSONDecoder().decode(Health.self, from: res.0))?.ok == true else { continue }
            session = nil
            me = nil
            server = base
            await loadMe()
            return
        }
        throw APIError(status: 0, message: "No runyard server answered at \(a).")
    }

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
        await loadMe()
    }

    func forget() {
        server = ""
        session = nil
        me = nil
        loadError = nil
    }

    /// DELETE for the swipe actions. Only an admin may, so a 401 or 403 is answered with a sentence saying so.
    func delete(_ path: String) async throws {
        do {
            _ = try await api.send(api.request(path, method: "DELETE"))
        } catch let e as APIError where e.status == 401 || e.status == 403 {
            throw APIError(status: 0, message: "Only an admin can delete. Sign in as one.")
        }
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
