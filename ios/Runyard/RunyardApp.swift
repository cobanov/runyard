import SwiftUI
import UserNotifications

@main
struct RunyardApp: App {
    @State private var store: Store
    private let taps: NotifyTaps
    @Environment(\.scenePhase) private var phase

    init() {
        let store = Store()
        _store = State(initialValue: store)
        taps = NotifyTaps(store: store)
        UNUserNotificationCenter.current().delegate = taps // before launch ends, or a tap that starts the app is lost
    }

    var body: some Scene {
        WindowGroup {
            RootView().environment(store)
        }
        // What was on screen is not news: every time the app comes or goes, the next comparison starts from now.
        .onChange(of: phase) {
            guard phase != .inactive else { return }
            if phase == .background { Notify.schedule() }
            Task { await Notify.check(store.api, tell: false) }
        }
        .backgroundTask(.appRefresh(Notify.task)) {
            await Notify.schedule()
            let server = Keychain.read("server") ?? ""
            await Notify.check(API(base: server, session: Keychain.read("session " + server)), tell: true)
        }
    }
}

/// Connect, then sign in if the server wants it, then the app.
struct RootView: View {
    @Environment(Store.self) private var store
    @State private var trying = false
    @State private var switching: String? // a link's server, while another one is connected
    @State private var failure: String?

    var body: some View {
        Group {
            if store.server.isEmpty {
                ConnectView()
            } else if store.me == nil, let error = store.loadError {
                ContentUnavailableView {
                    Label("Can't reach the server", systemImage: "wifi.exclamationmark")
                } description: {
                    Text("\(store.server)\n\(error)\nTrying again on its own.")
                } actions: {
                    Button { Task { await retry() } } label: {
                        if trying { ProgressView() } else { Text("Try again") }
                    }
                    .buttonStyle(.borderedProminent).disabled(trying)
                    Button("Change server") { store.leave() }
                }
                .task {
                    // A server that restarts is back in seconds; one that is off should not be asked all night.
                    var wait = 2.0
                    while (try? await Task.sleep(for: .seconds(wait))) != nil {
                        await retry()
                        wait = min(wait * 2, 30)
                    }
                }
            } else if store.me == nil {
                ProgressView().task { await store.loadMe() }
            } else if store.needsLogin {
                LoginView()
            } else {
                MainView()
            }
        }
        .onOpenURL(perform: open)
        .alert("Switch server?", isPresented: Binding(present: $switching), presenting: switching) { a in
            Button("Switch") { connect(a) }
            Button("Cancel", role: .cancel) {}
        } message: { a in
            Text("This link is for \(a). The app is connected to \(store.isDemo ? "the demo" : store.server).")
        }
        .alert("Can't connect", isPresented: Binding(present: $failure), presenting: failure) { _ in } message: { Text($0) }
    }

    private func retry() async {
        trying = true
        await store.loadMe()
        trying = false
    }

    /// runyard://connect?url=<server address> (the web UI's QR code) and runyard://run/<run id>.
    private func open(_ url: URL) {
        switch url.host() {
        case "connect":
            guard let a = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "url" })?.value,
                  !a.isEmpty else { return }
            let same = { (s: String) in s == a || s.hasSuffix("://" + a) || s + "/" == a }
            // A server the app knows needs no question: nothing is lost by going there and back.
            if let known = store.servers.first(where: same) {
                if known != store.server { store.use(known) }
            } else if store.server.isEmpty { connect(a) } else { switching = a }
        case "run":
            store.openRun = url.lastPathComponent
        default:
            break
        }
    }

    private func connect(_ address: String) {
        Task {
            do { try await store.connect(address) } catch { failure = error.localizedDescription }
        }
    }
}

enum Route: Hashable {
    case project(String)
    case run(String)
}

extension View {
    func routes() -> some View {
        navigationDestination(for: Route.self) { route in
            switch route {
            case .project(let name): RunsView(project: name)
            case .run(let id): RunView(id: id)
            }
        }
    }

    /// Runs load now and then again every `every` seconds while the view is on screen.
    func poll(every: @escaping () -> Double, _ load: @escaping () async -> Void) -> some View {
        task {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(every()))
            }
        }
    }
}

extension Binding where Value == Bool {
    /// True while `item` is set; setting false clears it. For a dialog about an optional item.
    init<T: Sendable>(present item: Binding<T?>) {
        self.init(get: { item.wrappedValue != nil }, set: { if !$0 { item.wrappedValue = nil } })
    }
}

struct MainView: View {
    @Environment(Store.self) private var store
    @State private var tab = 0
    @State private var path: [Route] = []

    var body: some View {
        if store.isDemo {
            tabs.tabViewBottomAccessory {
                HStack {
                    Text("Demo: made-up runs").font(.subheadline).foregroundStyle(.secondary)
                    Spacer()
                    Button("Leave demo") { store.leave() }.font(.subheadline.weight(.medium))
                }
                .padding(.horizontal, 16)
            }
        } else {
            tabs
        }
    }

    private var tabs: some View {
        TabView(selection: $tab) {
            Tab("Runs", systemImage: "chart.xyaxis.line", value: 0) {
                NavigationStack(path: $path) { HomeView().routes() }
            }
            Tab("Alerts", systemImage: "bell", value: 1) {
                NavigationStack { AlertsView().routes() }
            }
            .badge(store.alertsBadge.map { Text($0) })
            Tab("Server", systemImage: "server.rack", value: 2) {
                NavigationStack { ServerView() }
            }
        }
        .poll(every: { 15 }) { await store.countAlerts() }
        .onChange(of: store.openRun, initial: true) {
            guard let id = store.openRun else { return }
            tab = 0
            path = [.run(id)]
            store.openRun = nil
        }
    }
}

struct ServerView: View {
    @Environment(Store.self) private var store
    @State private var machines: [Machine] = []
    @AppStorage(Notify.onKey) private var notify = false
    @State private var refused = false

    var body: some View {
        List {
            if !machines.isEmpty {
                Section {
                    ForEach(machines) { MachineRow(machine: $0) }
                } header: {
                    Text("Machines")
                } footer: {
                    Text("From runyard beacon on each machine: every process on its GPUs, whoever started it.")
                }
            }
            Section("Server") {
                LabeledContent("Address", value: store.isDemo ? "Demo, made-up runs" : store.server)
                if store.plainHTTP { LabeledContent("Connection", value: "http, not encrypted") }
                if let me = store.me {
                    LabeledContent("Signed in as", value: me.auth ? me.user : "Open server, no sign-in")
                }
            }
            ServerList()
            Section {
                if store.me?.auth == true {
                    Button("Sign out") { Task { await store.logout() } }
                }
                Button(store.isDemo ? "Leave demo" : "Change server") { store.leave() }
            }
            if !store.isDemo {
                Section {
                    Toggle("Notify when a run ends or alerts", isOn: $notify)
                } footer: {
                    Text(refused ? "Notifications are off for runyard in the Settings app. Allow them there, then turn this on."
                         : "The app asks the server in the background, and iOS decides when that happens: these can be minutes or hours late, and stop if you close the app from the app switcher. For instant ones use the server's channels (ntfy, Slack, Telegram or web push), set up on the web UI's Settings page.")
                }
            }
            Section {
                LabeledContent("App version", value: Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "")
            }
        }
        .onChange(of: notify) {
            guard notify else { return }
            Task {
                refused = (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])) != true
                if refused { notify = false; return }
                // Start from now: what already ended is not news.
                UserDefaults.standard.removeObject(forKey: Notify.seenKey)
                await Notify.check(store.api, tell: false)
            }
        }
        .navigationTitle("Server")
        .refreshable { await loadMachines() }
        .poll(every: { 10 }) { await loadMachines() }
    }

    private func loadMachines() async {
        if let m: [Machine] = try? await store.get("/api/v1/hosts") { machines = m }
    }
}

/// The servers the app has connected to, but for the one it is on: tap to switch, swipe to forget.
struct ServerList: View {
    @Environment(Store.self) private var store

    var body: some View {
        let others = store.servers.filter { $0 != store.server }
        if !others.isEmpty {
            Section {
                ForEach(others, id: \.self) { s in
                    Button(s) { store.use(s) }
                        .swipeActions { Button("Forget", systemImage: "trash") { store.forget(s) }.tint(.red) }
                }
            } header: {
                Text(store.server.isEmpty ? "Your servers" : "Other servers")
            } footer: {
                Text("Tap to switch, with no sign-in again. Swipe to forget one.")
            }
        }
    }
}

struct MachineRow: View {
    let machine: Machine

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(machine.name).font(.headline)
                Spacer()
                if machine.silent {
                    Text("silent since \(machine.seenAt.formatted(date: .omitted, time: .shortened))").font(.caption).foregroundStyle(.orange)
                }
            }
            MachineStrip(load: machine.load).opacity(machine.silent ? 0.5 : 1)
            ForEach(Array(machine.procs.enumerated()), id: \.offset) { _, p in
                Text("GPU \(p.gpu) · \(p.proc.name.isEmpty ? "pid \(p.proc.pid)" : p.proc.name)\(p.proc.user.map { " · \($0)" } ?? "")\(p.proc.memGB > 0 ? String(format: " · %.1f GB", p.proc.memGB) : "")")
                    .font(.caption.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
            }
            if !machine.runs.isEmpty {
                Text("Training " + machine.runs.map(\.name).joined(separator: ", ")).font(.caption).foregroundStyle(.tint)
            }
        }
        .padding(.vertical, 4)
    }
}
