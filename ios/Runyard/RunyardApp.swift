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
                VStack(alignment: .leading, spacing: 10) {
                    Text("Can't reach the server").font(.title2.bold())
                    Text("\(store.server)\n\(error)\nTrying again on its own.").font(.subheadline).foregroundStyle(.muted)
                    Button { Task { await retry() } } label: {
                        if trying { ProgressView().tint(.paper) } else { Text("Try again") }
                    }
                    .buttonStyle(Primary()).disabled(trying).padding(.top, 10)
                    Button("Change server") { store.leave() }.font(.subheadline.weight(.medium)).frame(maxWidth: .infinity, minHeight: 44)
                }
                .padding(16)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .task {
                    // A server that restarts is back in seconds; one that is off should not be asked all night.
                    var wait = 2.0
                    while (try? await Task.sleep(for: .seconds(wait))) != nil {
                        await retry()
                        wait = min(wait * 2, 30)
                    }
                }
            } else if store.me == nil {
                Color.clear.task { await store.loadMe() } // an answer takes less than a spinner needs to be read
            } else if store.needsLogin {
                LoginView()
            } else {
                MainView()
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.paper)
        .monospacedDigit() // tabular figures everywhere
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
    @State private var path: [Route] = []

    var body: some View {
        if store.isDemo {
            tabs.tabViewBottomAccessory {
                HStack {
                    Text("Demo: made-up runs").font(.subheadline).foregroundStyle(.muted)
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
        @Bindable var store = store
        return TabView(selection: $store.tab) {
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
            store.tab = 0
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
        let signedIn = store.me?.auth == true
        let about = store.isDemo ? "made-up runs, answered inside the app"
            : [store.plainHTTP ? "http, not encrypted" : nil, store.me.map { $0.auth ? "signed in as \($0.user)" : "open server" }].compactMap { $0 }.joined(separator: " · ")
        List {
            VStack(alignment: .leading, spacing: 3) {
                Text(store.isDemo ? "Demo" : store.server).font(.callout.weight(.semibold)).lineLimit(1).truncationMode(.middle)
                if !about.isEmpty { Text(about).font(.footnote).foregroundStyle(.muted) }
            }
            .panelRow(0, of: signedIn ? 2 : 1, above: 8)
            if signedIn {
                Button("Sign out") { Task { await store.logout() } }.font(.subheadline).panelRow(1, of: 2)
            }

            // In the demo the bar over the tabs already leaves it: only the servers to go back to are listed.
            let rows = store.servers.count + (store.isDemo ? 0 : 1)
            if rows > 0 { Heading("Servers") }
            ForEach(Array(store.servers.enumerated()), id: \.element) { i, s in
                Button { if s != store.server { store.use(s) } } label: {
                    HStack {
                        Text(s).lineLimit(1).truncationMode(.middle)
                        Spacer(minLength: 8)
                        if s == store.server { Image(systemName: "checkmark").font(.footnote.weight(.semibold)).accessibilityLabel("Connected") }
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain).font(.subheadline)
                .swipeActions { Button("Forget", systemImage: "trash") { store.forget(s) }.tint(.failed) }
                .panelRow(i, of: rows)
            }
            if !store.isDemo {
                Button { store.leave() } label: {
                    HStack(spacing: 8) {
                        Image(systemName: "plus").font(.footnote.weight(.semibold)).foregroundStyle(.muted)
                        Text("Add a server")
                        Spacer(minLength: 0)
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain).font(.subheadline)
                .panelRow(rows - 1, of: rows)
            }
            if !store.servers.isEmpty {
                Text("Tap one to switch, with no sign-in again. Swipe to forget it.").font(.footnote).foregroundStyle(.muted).bareRow(above: 8)
            }

            if !store.isDemo {
                Heading("Notifications")
                Toggle(isOn: $notify) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("When a run ends or alerts").font(.subheadline)
                        Text(refused ? "Notifications are off for runyard in the Settings app. Allow them there, then turn this on."
                             : "Checked in the background, so they can arrive late. For instant ones, use ntfy or web push from the server.")
                            .font(.footnote).foregroundStyle(.muted)
                    }
                }
                .panelRow()
            }

            if !machines.isEmpty {
                Heading("Machines")
                ForEach(Array(machines.enumerated()), id: \.element.id) { i, m in
                    MachineRow(machine: m).panelRow(i, of: machines.count)
                }
                Text("From runyard beacon on each machine: every process on its GPUs, whoever started it.")
                    .font(.footnote).foregroundStyle(.muted).bareRow(above: 8)
            }

            Text("runyard \(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "")")
                .font(.footnote).foregroundStyle(.muted).frame(maxWidth: .infinity).bareRow(above: 24, below: 24)
        }
        .paperList()
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

struct MachineRow: View {
    let machine: Machine

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(machine.name).font(.callout.weight(.semibold)).lineLimit(1)
                Spacer(minLength: 8)
                if machine.silent { Text("silent for \(duration(Date.now.timeIntervalSince(machine.seenAt), units: 1))").font(.footnote).foregroundStyle(.crashed) }
            }
            MachineLoad(load: machine.load).opacity(machine.silent ? 0.5 : 1)
            ForEach(Array(machine.procs.enumerated()), id: \.offset) { _, p in
                Text("GPU \(p.gpu) · \(p.proc.name.isEmpty ? "pid \(p.proc.pid)" : p.proc.name)\(p.proc.user.map { " · \($0)" } ?? "")\(p.proc.memGB > 0 ? String(format: " · %.1f GB", p.proc.memGB) : "")")
                    .font(.footnote).foregroundStyle(.muted).lineLimit(1)
            }
            if !machine.runs.isEmpty {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Dot(color: .running, breathes: true)
                    Text("training " + machine.runs.map(\.name).joined(separator: ", ")).font(.footnote).foregroundStyle(.running).lineLimit(2)
                }
            }
        }
        .padding(.vertical, 2)
    }
}
