import SwiftUI

/// First launch: the server's address, the way Jellyfin asks for it.
struct ConnectView: View {
    @Environment(Store.self) private var store
    @State private var address = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        Form {
            Section {
                VStack(spacing: 8) {
                    LogoMark().frame(width: 72, height: 72)
                    Text("runyard").font(.largeTitle.bold())
                    Text("Connect to your training server").foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 24)
                .listRowBackground(Color.clear)
            }
            ServerList()
            Section {
                TextField("192.168.1.10:8080", text: $address)
                    .keyboardType(.URL)
                    .textContentType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.go)
                    .onSubmit(connect)
                    .accessibilityIdentifier("server")
            } header: {
                Text("Server address")
            } footer: {
                Text(error ?? "An IP address or host name, with the port if it is not 80, 443 or 8080. Without http:// the app tries https first.")
                    .foregroundStyle(error == nil ? Color.secondary : .red)
            }
            Section {
                Button(action: connect) {
                    HStack {
                        Spacer()
                        if busy { ProgressView() } else { Text("Connect").bold() }
                        Spacer()
                    }
                }
                .disabled(busy || address.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            Section {
                Button(action: store.demo) {
                    HStack {
                        Spacer()
                        Text("Try the demo")
                        Spacer()
                    }
                }
            } footer: {
                Text("No server yet? Look around on made-up runs that keep training while you watch.")
            }
        }
    }

    private func connect() {
        busy = true
        error = nil
        Task {
            do { try await store.connect(address) } catch { self.error = error.localizedDescription }
            busy = false
        }
    }
}

/// The server has RUNYARD_AUTH on: a user made with `runyard-server user add`.
struct LoginView: View {
    @Environment(Store.self) private var store
    @State private var name = ""
    @State private var password = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        Form {
            Section {
                VStack(spacing: 6) {
                    Text("Sign in").font(.largeTitle.bold())
                    Text(URL(string: store.server)?.host() ?? store.server).foregroundStyle(.secondary)
                    if store.plainHTTP { Text("http, not encrypted").font(.footnote).foregroundStyle(.orange) }
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 24)
                .listRowBackground(Color.clear)
            }
            Section {
                TextField("Name", text: $name)
                    .textContentType(.username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("name")
                SecureField("Password", text: $password)
                    .textContentType(.password)
                    .submitLabel(.go)
                    .onSubmit(login)
                    .accessibilityIdentifier("password")
            } footer: {
                if let error { Text(error).foregroundStyle(.red) }
            }
            Section {
                Button(action: login) {
                    HStack {
                        Spacer()
                        if busy { ProgressView() } else { Text("Sign in").bold() }
                        Spacer()
                    }
                }
                .disabled(busy || name.isEmpty || password.isEmpty)
                Button("Change server") { store.leave() }
            }
        }
    }

    private func login() {
        busy = true
        error = nil
        Task {
            do { try await store.login(name: name, password: password) } catch { self.error = error.localizedDescription }
            busy = false
        }
    }
}

/// The runyard mark: two running-track lanes forming an r, the inside one in the running teal (the accent).
struct LogoMark: View {
    var body: some View {
        ZStack {
            Lane(outer: 108, inner: 72).fill(.primary)
            Lane(outer: 48, inner: 12).fill(.tint)
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }
}

/// One lane, in the 256-unit box of the logo's SVG: centred on (144, 144), ends at 220, the right end cut at 45°.
nonisolated private struct Lane: Shape {
    let outer, inner: CGFloat

    func path(in rect: CGRect) -> Path {
        let c: CGFloat = 144, a = c - outer, b = c - inner
        var p = Path()
        p.move(to: CGPoint(x: a, y: 220))
        p.addArc(tangent1End: CGPoint(x: a, y: a), tangent2End: CGPoint(x: c, y: a), radius: outer)
        p.addLine(to: CGPoint(x: 220, y: a))
        p.addLine(to: CGPoint(x: 220 - (outer - inner), y: b))
        p.addLine(to: CGPoint(x: c, y: b))
        p.addArc(tangent1End: CGPoint(x: b, y: b), tangent2End: CGPoint(x: b, y: c), radius: inner)
        p.addLine(to: CGPoint(x: b, y: 220))
        p.closeSubpath()
        let s = min(rect.width, rect.height) / 256
        return p.applying(CGAffineTransform(translationX: rect.minX, y: rect.minY).scaledBy(x: s, y: s))
    }
}
