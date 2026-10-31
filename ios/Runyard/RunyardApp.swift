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

    var body: some View {
        List {
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
    }
}
