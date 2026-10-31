// A small App Store Connect API client for shipping runyard to TestFlight.
// It signs in with an API key (.p8), so no Apple ID or 2FA is needed.
//
//   swift scripts/asc.swift probe           what exists: app record, bundle id, distribution certificates
//   swift scripts/asc.swift ensure-app      register the bundle id; create the app record if the API allows
//   swift scripts/asc.swift profile <sha1>  App Store profile for the distribution certificate with that SHA-1
//   swift scripts/asc.swift builds          the latest builds and their processing state
//   swift scripts/asc.swift tester <email>  add a team member to the internal group that gets every build
//   swift scripts/asc.swift public <build>  open TestFlight to anyone: test information, the external group
//                                           "Public" with a public link, that build in it, sent to beta review
//
// `public` also needs CONTACT_FIRST, CONTACT_LAST, CONTACT_EMAIL and CONTACT_PHONE for Apple's reviewer;
// they stay out of this public repository.
// Environment: ASC_KEY_ID, ASC_ISSUER_ID, APPLE_TEAM_ID (the key is read from
// ~/.appstoreconnect/private_keys/AuthKey_<ASC_KEY_ID>.p8). Adapted from PocketVibe's.
import CryptoKit
import Foundation

let bundleID = "dev.cobanov.runyard"
let appName = "runyard"
let profileName = "runyard App Store"
let env = ProcessInfo.processInfo.environment
let keyID = env["ASC_KEY_ID"] ?? ""
let issuerID = env["ASC_ISSUER_ID"] ?? ""
let teamID = env["APPLE_TEAM_ID"] ?? ""

func die(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}

if keyID.isEmpty || issuerID.isEmpty || teamID.isEmpty { die("set ASC_KEY_ID, ASC_ISSUER_ID and APPLE_TEAM_ID") }

// ---------- Requests ----------

func base64url(_ data: Data) -> String {
    data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}

func token() -> String {
    let path = ("~/.appstoreconnect/private_keys/AuthKey_\(keyID).p8" as NSString).expandingTildeInPath
    guard let pem = try? String(contentsOfFile: path, encoding: .utf8),
          let key = try? P256.Signing.PrivateKey(pemRepresentation: pem) else { die("cannot read the API key at \(path)") }
    let now = Int(Date().timeIntervalSince1970)
    let head = base64url(Data(#"{"alg":"ES256","kid":"\#(keyID)","typ":"JWT"}"#.utf8))
    let body = base64url(Data(#"{"iss":"\#(issuerID)","iat":\#(now),"exp":\#(now + 900),"aud":"appstoreconnect-v1"}"#.utf8))
    let signature = try! key.signature(for: Data("\(head).\(body)".utf8))
    return "\(head).\(body).\(base64url(signature.rawRepresentation))"
}

struct APIError: Error { let status: Int; let body: String }

@discardableResult
func call(_ method: String, _ path: String, _ body: [String: Any]? = nil) throws -> [String: Any] {
    var request = URLRequest(url: URL(string: "https://api.appstoreconnect.apple.com\(path)")!)
    request.httpMethod = method
    request.setValue("Bearer \(token())", forHTTPHeaderField: "Authorization")
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    if let body { request.httpBody = try JSONSerialization.data(withJSONObject: body) }
    let done = DispatchSemaphore(value: 0)
    var result: (Data?, URLResponse?, Error?)
    URLSession.shared.dataTask(with: request) { result = ($0, $1, $2); done.signal() }.resume()
    done.wait()
    if let error = result.2 { throw error }
    let status = (result.1 as? HTTPURLResponse)?.statusCode ?? 0
    let data = result.0 ?? Data()
    if !(200..<300).contains(status) { throw APIError(status: status, body: String(decoding: data, as: UTF8.self)) }
    return (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
}

func list(_ path: String) -> [[String: Any]] {
    do { return try call("GET", path)["data"] as? [[String: Any]] ?? [] } catch { die("GET \(path): \(error)") }
}

func attributes(_ item: [String: Any]) -> [String: Any] { item["attributes"] as? [String: Any] ?? [:] }

func app() -> [String: Any]? { list("/v1/apps?filter[bundleId]=\(bundleID)").first }

func bundle() -> [String: Any]? {
    list("/v1/bundleIds?filter[identifier]=\(bundleID)&limit=200").first { attributes($0)["identifier"] as? String == bundleID }
}

// ---------- Commands ----------

func probe() {
    if let app = app() { print("app record: \(attributes(app)["name"] ?? "?") (id \(app["id"] ?? "?"))") } else { print("app record: none") }
    print("bundle id: \(bundle().map { "registered (id \($0["id"] ?? "?"))" } ?? "not registered")")
    for cert in list("/v1/certificates?limit=200") {
        let a = attributes(cert)
        guard let type = a["certificateType"] as? String, type.contains("DISTRIBUTION") else { continue }
        print("certificate: \(type) \(a["displayName"] ?? "") expires \(a["expirationDate"] ?? "?") sha1 \(sha1(of: cert))")
    }
}

func sha1(of cert: [String: Any]) -> String {
    guard let content = attributes(cert)["certificateContent"] as? String,
          let der = Data(base64Encoded: content, options: .ignoreUnknownCharacters) else { return "?" }
    return Insecure.SHA1.hash(data: der).map { String(format: "%02X", $0) }.joined()
}

func ensureApp() {
    if bundle() == nil {
        do {
            try call("POST", "/v1/bundleIds", ["data": ["type": "bundleIds", "attributes": [
                "identifier": bundleID, "name": appName, "platform": "IOS", "seedId": teamID,
            ]]])
            print("bundle id registered")
        } catch { die("registering the bundle id failed: \(error)") }
    } else {
        print("bundle id already registered")
    }
    if app() != nil { return print("app record already exists") }
    do {
        try call("POST", "/v1/apps", ["data": ["type": "apps", "attributes": [
            "name": appName, "bundleId": bundleID, "primaryLocale": "en-US", "sku": "runyard",
        ]]])
        print("app record created")
    } catch {
        die("""
        creating the app record failed: \(error)
        Create it once in App Store Connect: Apps > + > New App, iOS, name \(appName),
        bundle id \(bundleID), SKU runyard, primary language English (U.S.).
        """)
    }
}

func profile(certSHA1: String) {
    guard let bundle = bundle(), let bid = bundle["id"] as? String else { die("register the bundle id first (ensure-app)") }
    guard let cert = list("/v1/certificates?limit=200").first(where: { sha1(of: $0) == certSHA1.uppercased() }),
          let certID = cert["id"] as? String else { die("no certificate with SHA-1 \(certSHA1)") }
    // The old profile goes first: two of the same name are refused.
    for old in list("/v1/profiles?limit=200") where attributes(old)["name"] as? String == profileName {
        if let id = old["id"] as? String { _ = try? call("DELETE", "/v1/profiles/\(id)") }
    }
    do {
        let created = try call("POST", "/v1/profiles", ["data": [
            "type": "profiles",
            "attributes": ["name": profileName, "profileType": "IOS_APP_STORE"],
            "relationships": [
                "bundleId": ["data": ["type": "bundleIds", "id": bid]],
                "certificates": ["data": [["type": "certificates", "id": certID]]],
            ],
        ]])
        let data = created["data"] as? [String: Any] ?? [:]
        guard let content = attributes(data)["profileContent"] as? String,
              let file = Data(base64Encoded: content, options: .ignoreUnknownCharacters) else { die("the profile came back empty") }
        let dir = ("~/Library/MobileDevice/Provisioning Profiles" as NSString).expandingTildeInPath
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let path = "\(dir)/runyard_AppStore.mobileprovision"
        try file.write(to: URL(fileURLWithPath: path))
        print("profile \"\(profileName)\" written to \(path)")
    } catch { die("creating the profile failed: \(error)") }
}

func builds() {
    guard let id = app()?["id"] as? String else { die("no app record") }
    for build in list("/v1/builds?filter[app]=\(id)&sort=-uploadedDate&limit=5") {
        let a = attributes(build)
        print("build \(a["version"] ?? "?"): \(a["processingState"] ?? "?"), uploaded \(a["uploadedDate"] ?? "?")")
    }
}

func tester(_ email: String) {
    guard let id = app()?["id"] as? String else { die("no app record") }
    let groups = list("/v1/apps/\(id)/betaGroups?limit=200")
    var group = groups.first { attributes($0)["isInternalGroup"] as? Bool == true }
    if group == nil {
        do {
            group = try call("POST", "/v1/betaGroups", ["data": [
                "type": "betaGroups",
                "attributes": ["name": "Team", "isInternalGroup": true, "hasAccessToAllBuilds": true],
                "relationships": ["app": ["data": ["type": "apps", "id": id]]],
            ]])["data"] as? [String: Any]
            print("internal group \"Team\" created")
        } catch { die("creating the internal group failed: \(error)") }
    }
    guard let gid = group?["id"] as? String else { die("no internal group") }
    // Only the one asked for: everyone on the team would get an invitation.
    guard let user = list("/v1/users?limit=200").first(where: { (attributes($0)["username"] as? String)?.lowercased() == email.lowercased() })
    else { die("\(email) is not on the App Store Connect team") }
    let a = attributes(user)
    do {
        try call("POST", "/v1/betaTesters", ["data": [
            "type": "betaTesters",
            "attributes": ["email": email, "firstName": a["firstName"] ?? "", "lastName": a["lastName"] ?? ""],
            "relationships": ["betaGroups": ["data": [["type": "betaGroups", "id": gid]]]],
        ]])
        print("tester added: \(email)")
    } catch let error as APIError where error.status == 409 {
        print("tester already there: \(email)")
    } catch {
        die("could not add \(email): \(error)")
    }
}

// ---------- Public TestFlight ----------

let testerInfo = """
runyard shows your ML training runs live: metrics, logs, samples, GPU load and alerts, from your own runyard \
server (github.com/cobanov/runyard). No server yet? Tap "Try the demo" on the first screen.
"""
let reviewNotes = """
runyard is the companion app for a self-hosted server (https://github.com/cobanov/runyard), so there is no account \
to sign in with. To review it, tap "Try the demo" on the first screen: the whole app opens on built-in sample \
training runs, with no server and no network. "Leave demo" goes back to the first screen.
"""

/// Creates or updates the one item at `listPath` (a to-one or a list) with these attributes.
func upsert(_ listPath: String, type: String, attributes: [String: Any], create: [String: Any], fixed: Set<String> = []) {
    let found: [String: Any]?
    do {
        let data = try call("GET", listPath)["data"]
        found = (data as? [[String: Any]])?.first ?? (data as? [String: Any])
    } catch { die("GET \(listPath): \(error)") }
    do {
        if let id = found?["id"] as? String {
            try call("PATCH", "/v1/\(type)/\(id)", ["data": ["type": type, "id": id, "attributes": attributes.filter { !fixed.contains($0.key) }]])
        } else {
            try call("POST", "/v1/\(type)", ["data": ["type": type, "attributes": attributes].merging(create) { a, _ in a }])
        }
    } catch { die("\(type): \(error)") }
}

func publicBeta(_ number: String) {
    guard let id = app()?["id"] as? String else { die("no app record") }
    let contact = ["CONTACT_FIRST", "CONTACT_LAST", "CONTACT_EMAIL", "CONTACT_PHONE"].map { env[$0] ?? "" }
    if contact.contains(where: \.isEmpty) { die("set CONTACT_FIRST, CONTACT_LAST, CONTACT_EMAIL and CONTACT_PHONE") }
    guard let build = list("/v1/builds?filter[app]=\(id)&filter[version]=\(number)").first, let bid = build["id"] as? String
    else { die("no build \(number)") }
    guard attributes(build)["processingState"] as? String == "VALID" else { die("build \(number) is not processed yet") }
    let toApp = ["relationships": ["app": ["data": ["type": "apps", "id": id]]]]

    upsert("/v1/apps/\(id)/betaAppLocalizations", type: "betaAppLocalizations", attributes: [
        "locale": "en-US", "description": testerInfo, "feedbackEmail": contact[2],
        "marketingUrl": "https://runyard.cobanov.dev", "privacyPolicyUrl": "https://runyard.cobanov.dev/privacy",
    ], create: toApp, fixed: ["locale"])
    upsert("/v1/apps/\(id)/betaAppReviewDetail", type: "betaAppReviewDetails", attributes: [
        "contactFirstName": contact[0], "contactLastName": contact[1], "contactEmail": contact[2], "contactPhone": contact[3],
        "demoAccountRequired": false, "notes": reviewNotes,
    ], create: toApp)
    upsert("/v1/builds/\(bid)/betaBuildLocalizations", type: "betaBuildLocalizations", attributes: [
        "locale": "en-US", "whatsNew": "Try the demo: the whole app on sample runs, no server needed.",
    ], create: ["relationships": ["build": ["data": ["type": "builds", "id": bid]]]], fixed: ["locale"])
    print("test information, review contact and what to test set")

    var group = list("/v1/apps/\(id)/betaGroups?limit=200").first { attributes($0)["name"] as? String == "Public" }
    if group == nil {
        do {
            group = try call("POST", "/v1/betaGroups", ["data": [
                "type": "betaGroups",
                "attributes": ["name": "Public", "publicLinkEnabled": true, "feedbackEnabled": true],
            ].merging(toApp) { a, _ in a }])["data"] as? [String: Any]
            print("external group \"Public\" created")
        } catch { die("creating the external group failed: \(error)") }
    }
    guard let gid = group?["id"] as? String else { die("no external group") }
    do {
        try call("POST", "/v1/betaGroups/\(gid)/relationships/builds", ["data": [["type": "builds", "id": bid]]])
        print("build \(number) added to Public")
    } catch { die("adding the build failed: \(error)") }
    do {
        try call("POST", "/v1/betaAppReviewSubmissions", ["data": [
            "type": "betaAppReviewSubmissions", "relationships": ["build": ["data": ["type": "builds", "id": bid]]],
        ]])
        print("build \(number) sent to beta review")
    } catch let error as APIError where error.status == 409 {
        print("build \(number) already in beta review: \(error.body.prefix(200))")
    } catch { die("submitting for review failed: \(error)") }
    if let link = list("/v1/apps/\(id)/betaGroups?limit=200").first(where: { $0["id"] as? String == gid }).flatMap({ attributes($0)["publicLink"] as? String }) {
        print("public link: \(link)")
    }
}

let args = CommandLine.arguments.dropFirst()
switch args.first {
case "probe": probe()
case "ensure-app": ensureApp()
case "profile":
    guard args.count == 2 else { die("usage: profile <certificate sha1>") }
    profile(certSHA1: args[args.startIndex + 1])
case "builds": builds()
case "tester":
    guard args.count == 2 else { die("usage: tester <email>") }
    tester(args[args.startIndex + 1])
case "public":
    guard args.count == 2 else { die("usage: public <build number>") }
    publicBeta(args[args.startIndex + 1])
default: die("usage: swift scripts/asc.swift probe|ensure-app|profile <sha1>|builds|tester <email>|public <build>")
}
