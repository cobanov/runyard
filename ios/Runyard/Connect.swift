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
                    Image(systemName: "chart.xyaxis.line").font(.system(size: 44)).foregroundStyle(.tint)
                    Text("runyard").font(.largeTitle.bold())
                    Text("Connect to your training server").foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 24)
                .listRowBackground(Color.clear)
            }
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
                Text(error ?? "An IP address or host name, with the port if it is not 80 or 443. Without http:// the app tries https first.")
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
                Button("Change server") { store.forget() }
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
