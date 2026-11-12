import SwiftUI
import WatchKit

// MARK: - Lançar do pulso

/// Passos da lista completa: computador → agente → repo → papel → tarefa.
enum LaunchStep: Hashable {
    case servers
    case agents(LaunchServer)
    case repos(LaunchServer, LaunchAgent)
    case roles(LaunchServer, LaunchAgent, LaunchRepo)
    case task(LaunchShortcut)
}

/// Atalhos dos últimos combos e a lista completa. O relógio lança sempre no modo default:
/// cada edição e cada comando pedem aprovação aqui no pulso.
struct WatchLaunchView: View {
    @ObservedObject var viewModel: WatchViewModel
    var onLaunched: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var path: [LaunchStep] = []

    var body: some View {
        NavigationStack(path: $path) {
            List {
                if !viewModel.launchRecent.isEmpty {
                    Section("Atalhos") {
                        ForEach(viewModel.launchRecent) { combo in
                            NavigationLink(value: LaunchStep.task(combo)) {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(combo.label).font(.footnote.weight(.semibold)).lineLimit(1)
                                    Text("\(combo.serverName) · \(combo.agentName)")
                                        .font(.caption2).foregroundStyle(Tower.soft).lineLimit(1)
                                }
                            }
                        }
                    }
                }
                NavigationLink(value: LaunchStep.servers) {
                    Label("Lista completa", systemImage: "list.bullet")
                }
            }
            .navigationTitle("Lançar")
            .navigationDestination(for: LaunchStep.self) { step in
                switch step {
                case .servers:
                    List(viewModel.launchServers) { server in
                        NavigationLink(server.name, value: LaunchStep.agents(server))
                    }
                    .overlay { if viewModel.launchServers.isEmpty { Text("Nenhum computador aceita lançamentos.").font(.footnote) } }
                    .navigationTitle("Computador")
                case .agents(let server):
                    CatalogStep(viewModel: viewModel, server: server, title: "Agente") { cat in
                        ForEach(cat.agents) { NavigationLink($0.name, value: LaunchStep.repos(server, $0)) }
                    }
                case .repos(let server, let agent):
                    CatalogStep(viewModel: viewModel, server: server, title: "Repo") { cat in
                        // Sem papéis, o passo do papel só teria «Sem papel»: salta-se para a tarefa.
                        ForEach(cat.repos) { repo in
                            NavigationLink(repo.name, value: repo.roles.isEmpty
                                ? LaunchStep.task(combo(server, agent, repo, ""))
                                : LaunchStep.roles(server, agent, repo))
                        }
                    }
                case .roles(let server, let agent, let repo):
                    List {
                        NavigationLink("Sem papel", value: LaunchStep.task(combo(server, agent, repo, "")))
                        ForEach(repo.roles) { NavigationLink($0.name, value: LaunchStep.task(combo(server, agent, repo, $0.name))) }
                    }
                    .navigationTitle("Papel")
                case .task(let combo):
                    WatchLaunchTask(viewModel: viewModel, combo: combo) { id in
                        dismiss()
                        if let id { onLaunched(id) }
                    }
                }
            }
        }
    }

    private func combo(_ s: LaunchServer, _ a: LaunchAgent, _ r: LaunchRepo, _ role: String) -> LaunchShortcut {
        LaunchShortcut(serverId: s.id, serverName: s.name, agent: a.id, agentName: a.name, root: r.root, repoName: r.name, role: role)
    }
}

/// Um passo que precisa do catálogo do computador (lido pelo iPhone uma vez por abertura).
struct CatalogStep<Rows: View>: View {
    @ObservedObject var viewModel: WatchViewModel
    let server: LaunchServer
    let title: String
    @ViewBuilder var rows: (LaunchCatalog) -> Rows
    @State private var error: String?

    var body: some View {
        List {
            if let cat = viewModel.catalogs[server.id] {
                rows(cat)
            } else if let error {
                Text(error).font(.footnote).foregroundStyle(Tower.red)
                Button("Tentar de novo", action: load)
            } else {
                ProgressView()
            }
        }
        .navigationTitle(title)
        .onAppear { if viewModel.catalogs[server.id] == nil { load() } }
    }

    private func load() {
        error = nil
        viewModel.loadCatalog(serverId: server.id) { error = $0 }
    }
}

/// Ditado da tarefa e confirmação.
struct WatchLaunchTask: View {
    @ObservedObject var viewModel: WatchViewModel
    let combo: LaunchShortcut
    var onDone: (String?) -> Void
    @State private var task = ""
    @State private var busy = false
    @State private var note: String?

    private var text: String { task.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                Text("\(combo.serverName) · \(combo.agentName)".uppercased())
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(Tower.soft)
                Text(combo.label).font(.footnote.weight(.semibold))
                // No relógio, o campo abre o ditado, o rabisco ou o teclado.
                TextField("Ditar a tarefa…", text: $task)
                if !text.isEmpty {
                    Text("Cada edição e cada comando vão pedir aprovação no pulso.")
                        .font(.caption2)
                        .foregroundStyle(Tower.soft)
                    Button(busy ? "A lançar…" : "Lançar", action: launch)
                        .disabled(busy)
                }
                if let note {
                    Text(note).font(.caption2).foregroundStyle(Tower.red)
                }
            }
        }
        .navigationTitle("Tarefa")
    }

    private func launch() {
        guard !text.isEmpty, !busy else { return }
        busy = true
        note = nil
        viewModel.launch(combo, task: text) { ok, msg, id in
            busy = false
            if ok {
                viewModel.requestStateFromPhone()
                onDone(id)
            } else {
                note = msg
            }
        }
    }
}
