import SwiftUI

/// First launch: the server's address, the way Jellyfin asks for it.
struct ConnectView: View {
    @Environment(Store.self) private var store
    @State private var address = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        List {
            VStack(spacing: 8) {
                LogoMark().frame(width: 56, height: 56)
                Text("runyard").font(.title.bold()).kerning(-0.5)
                Text("Your training runs, from your own server").font(.subheadline).foregroundStyle(.muted)
            }
            .frame(maxWidth: .infinity)
            .bareRow(above: 40, below: 8)

            if !store.servers.isEmpty {
                Heading("Your servers")
                ForEach(Array(store.servers.enumerated()), id: \.element) { i, s in
                    Button { store.use(s) } label: {
                        HStack {
                            Text(s).lineLimit(1).truncationMode(.middle)
                            Spacer(minLength: 8)
                            Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.muted)
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain).font(.subheadline)
                    .swipeActions { Button("Forget", systemImage: "trash") { store.forget(s) }.tint(.failed) }
                    .panelRow(i, of: store.servers.count)
                }
            }

            VStack(alignment: .leading, spacing: 8) {
                TextField("Server address", text: $address)
                    .keyboardType(.URL)
                    .textContentType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.go)
                    .onSubmit(connect)
                    .accessibilityIdentifier("server")
                    .modifier(Field())
                if let error {
                    // The sentence that names the failure, then each address tried and why.
                    let lines = error.split(separator: "\n", maxSplits: 1)
                    Text(lines[0]).font(.footnote).foregroundStyle(.failed)
                    if lines.count > 1 { Text(lines[1]).font(.footnote).foregroundStyle(.muted) }
                } else {
                    Text("IP or host name, as in 192.168.1.10:8080").font(.footnote).foregroundStyle(.muted)
                }
                Button(action: connect) {
                    if busy { ProgressView().tint(.paper) } else { Text("Connect") }
                }
                .buttonStyle(Primary())
                .disabled(busy || address.trimmingCharacters(in: .whitespaces).isEmpty)
                .padding(.top, 6)
            }
            .bareRow(above: 24)

            VStack(spacing: 2) {
                Text("No server yet? Look around on made-up runs that keep training while you watch.")
                    .font(.footnote).foregroundStyle(.muted).multilineTextAlignment(.center)
                Button("Try the demo", action: store.demo).buttonStyle(.borderless).font(.subheadline.weight(.semibold)).frame(minHeight: 44)
            }
            .frame(maxWidth: .infinity)
            .bareRow(above: 20, below: 24)
        }
        .paperList()
        .scrollDismissesKeyboard(.interactively)
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
        List {
            VStack(spacing: 6) {
                Text("Sign in").font(.title.bold()).kerning(-0.5)
                Text([URL(string: store.server)?.host() ?? store.server, store.plainHTTP ? "http, not encrypted" : nil].compactMap { $0 }.joined(separator: " · "))
                    .font(.subheadline).foregroundStyle(.muted)
            }
            .frame(maxWidth: .infinity)
            .bareRow(above: 40, below: 24)

            VStack(alignment: .leading, spacing: 10) {
                TextField("Name", text: $name)
                    .textContentType(.username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("name")
                    .modifier(Field())
                SecureField("Password", text: $password)
                    .textContentType(.password)
                    .submitLabel(.go)
                    .onSubmit(login)
                    .accessibilityIdentifier("password")
                    .modifier(Field())
                if let error { Text(error).font(.footnote).foregroundStyle(.failed) }
                Button(action: login) {
                    if busy { ProgressView().tint(.paper) } else { Text("Sign in") }
                }
                .buttonStyle(Primary())
                .disabled(busy || name.isEmpty || password.isEmpty)
                .padding(.top, 6)
                Button("Change server") { store.leave() }.buttonStyle(.borderless).font(.subheadline.weight(.semibold))
                    .frame(maxWidth: .infinity, minHeight: 44)
            }
            .bareRow()
        }
        .paperList()
        .scrollDismissesKeyboard(.interactively)
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

/// The runyard mark: two running-track lanes forming an r, the inside one in the running teal.
struct LogoMark: View {
    var body: some View {
        ZStack {
            Lane(outer: 108, inner: 72).fill(.ink)
            Lane(outer: 48, inner: 12).fill(.running)
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
