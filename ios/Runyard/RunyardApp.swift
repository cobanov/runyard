import SwiftUI

@main
struct RunyardApp: App {
    @State private var store = Store()

    var body: some Scene {
        WindowGroup {
            RootView().environment(store)
        }
    }
}

/// Connect, then sign in if the server wants it, then the app.
struct RootView: View {
    @Environment(Store.self) private var store

    var body: some View {
        if store.server.isEmpty {
            ConnectView()
        } else if store.me == nil, let error = store.loadError {
            ContentUnavailableView {
                Label("Can't reach the server", systemImage: "wifi.exclamationmark")
            } description: {
                Text("\(store.server)\n\(error)")
            } actions: {
                Button("Try again") { Task { await store.loadMe() } }.buttonStyle(.borderedProminent)
                Button("Change server") { store.forget() }
            }
        } else if store.me == nil {
            ProgressView().task { await store.loadMe() }
        } else if store.needsLogin {
            LoginView()
        } else {
            MainView()
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
    var body: some View {
        TabView {
            Tab("Runs", systemImage: "chart.xyaxis.line") {
                NavigationStack { HomeView().routes() }
            }
            Tab("Alerts", systemImage: "bell") {
                NavigationStack { AlertsView().routes() }
            }
            Tab("Server", systemImage: "server.rack") {
                NavigationStack { ServerView() }
            }
        }
    }
}

struct ServerView: View {
    @Environment(Store.self) private var store
    @State private var machines: [Machine] = []

    var body: some View {
        List {
            if !machines.isEmpty {
                Section {
                    ForEach(machines) { MachineRow(machine: $0) }
                } header: {
                    Text("Machines")
                } footer: {
                    Text("From runyard agent on each machine: every process on its GPUs, whoever started it.")
                }
            }
            Section("Server") {
                LabeledContent("Address", value: store.server)
                if let me = store.me {
                    LabeledContent("Signed in as", value: me.auth ? me.user : "Open server, no sign-in")
                }
            }
            Section {
                if store.me?.auth == true {
                    Button("Sign out") { Task { await store.logout() } }
                }
                Button("Change server", role: .destructive) { store.forget() }
            }
            Section {
                LabeledContent("App version", value: Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "")
            } footer: {
                Text("Notifications come from the server's channels: ntfy, Slack, Telegram or web push.")
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
                Text(machine.name).font(.headline)
                Spacer()
                if machine.silent {
                    Text("silent since \(machine.seenAt.formatted(date: .omitted, time: .shortened))").font(.caption).foregroundStyle(.orange)
                }
            }
            MachineStrip(load: machine.load).opacity(machine.silent ? 0.5 : 1)
            ForEach(Array(machine.procs.enumerated()), id: \.offset) { _, p in
                Text("GPU \(p.gpu) · \(p.proc.name.isEmpty ? "pid \(p.proc.pid)" : p.proc.name)\(p.proc.user.map { " · \($0)" } ?? "") · \(String(format: "%.1f GB", p.proc.memGB))")
                    .font(.caption.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
            }
            if !machine.runs.isEmpty {
                Text("Training " + machine.runs.map(\.name).joined(separator: ", ")).font(.caption).foregroundStyle(.tint)
            }
        }
        .padding(.vertical, 4)
    }
}
