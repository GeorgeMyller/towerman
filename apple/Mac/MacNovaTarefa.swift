import SwiftUI

// MARK: - Nova tarefa (Mac)

/// O mesmo formulário do iPhone: computador, agente, repo, papel, tarefa e permissões.
struct MacNovaTarefaView: View {
    @ObservedObject var store: DeckStore
    @Environment(\.dismissWindow) private var dismissWindow

    @State private var serverId = ""
    @State private var catalog: LaunchCatalog?
    @State private var loading = false
    @State private var error: String?
    @State private var agentId = ""
    @State private var root = ""
    @State private var role = ""
    @State private var task = ""
    @State private var mode = ""
    @State private var sending = false
    @State private var note: DeckStore.Result?

    private static let modeLabels = ["acceptEdits": "Editar sem pedir", "default": "Pedir tudo", "plan": "Só planear"]

    private var agent: LaunchAgent? { catalog?.agents.first { $0.id == agentId } }
    private var repo: LaunchRepo? { catalog?.repos.first { $0.root == root } }
    private var trimmedTask: String { task.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var full: Bool { (catalog?.headless.open ?? 0) >= (catalog?.headless.max ?? 3) }
    private var canSend: Bool { agent != nil && repo != nil && !trimmedTask.isEmpty && !sending && !full }

    var body: some View {
        Form {
            if store.launchServers.isEmpty {
                Text("Nenhum computador aceita lançamentos. Atualize o Towerman no computador.")
                    .foregroundStyle(Tower.soft)
            } else {
                Picker("Computador", selection: $serverId) {
                    ForEach(store.launchServers) { Text($0.name).tag($0.id) }
                }
            }
            if loading {
                ProgressView("A ler o computador…")
            } else if let error {
                Text(error).foregroundStyle(Tower.red)
                Button("Tentar de novo") { Task { await load() } }
            } else if let catalog {
                Picker("Agente", selection: $agentId) {
                    ForEach(catalog.agents) { Text($0.name).tag($0.id) }
                }
                if let n = agent?.note { Text(n).font(.footnote).foregroundStyle(Tower.soft) }
                Picker("Repo", selection: $root) {
                    ForEach(catalog.repos) { Text($0.name).tag($0.root) }
                }
                // Sem papéis em .claude/agents, o seletor só teria «Sem papel»: esconde-se.
                if let roles = repo?.roles, !roles.isEmpty {
                    Picker("Papel", selection: $role) {
                        Text("Sem papel").tag("")
                        ForEach(roles) { Text($0.name).tag($0.name) }
                    }
                }
                TextField("Tarefa", text: $task, prompt: Text("O que o agente deve fazer"), axis: .vertical)
                    .lineLimit(3...8)
                if !trimmedTask.isEmpty {
                    Text("Branch: feat/\(LaunchBranch.slug(trimmedTask, 40))").font(.footnote).foregroundStyle(Tower.soft)
                }
                if let agent, agent.modes.count > 1 {
                    Picker("Permissões", selection: $mode) {
                        ForEach(agent.modes, id: \.self) { Text(Self.modeLabels[$0] ?? $0).tag($0) }
                    }
                    .pickerStyle(.segmented)
                }
                Text(full ? "Já há \(catalog.headless.max) sessões abertas pelo Towerman neste computador. Termine uma primeiro."
                          : "\(catalog.headless.open) de \(catalog.headless.max) sessões do Towerman a correr.")
                    .font(.footnote)
                    .foregroundStyle(full ? Tower.amber : Tower.soft)
            }
            if let note {
                Text(note.message).foregroundStyle(note.ok ? Tower.green : Tower.red)
            }
            HStack {
                Spacer()
                Button("Cancelar") { dismissWindow(id: "launch") }
                Button(sending ? "A enviar…" : "Enviar", action: send)
                    .keyboardShortcut(.defaultAction)
                    .disabled(!canSend)
            }
        }
        .formStyle(.grouped)
        .padding(8)
        .task { if serverId.isEmpty, let first = store.launchServers.first { serverId = first.id } }
        .onChange(of: serverId) { _, _ in Task { await load() } }
        .onChange(of: agentId) { _, _ in pickMode() }
        .onChange(of: root) { _, _ in if repo?.roles.contains(where: { $0.name == role }) != true { role = "" } }
    }

    private func load() async {
        guard !serverId.isEmpty else { return }
        loading = true
        error = nil
        let (cat, erro) = await store.catalog(serverId: serverId)
        loading = false
        catalog = cat
        error = cat == nil ? (erro ?? "Não consegui ler o computador.") : nil
        guard let cat else { return }
        let last = LaunchRecents.load().first { $0.serverId == serverId }
        agentId = cat.agents.first { $0.id == last?.agent }?.id ?? cat.agents.first?.id ?? ""
        root = cat.repos.first { $0.root == last?.root }?.root ?? cat.repos.first?.root ?? ""
        role = repo?.roles.first { $0.name == last?.role }?.name ?? ""
        pickMode()
    }

    private func pickMode() {
        guard let agent else { return }
        if !agent.modes.contains(mode) {
            mode = agent.modes.contains("acceptEdits") ? "acceptEdits" : (agent.modes.first ?? "")
        }
    }

    private func send() {
        guard let agent, let repo, let server = store.launchServers.first(where: { $0.id == serverId }) else { return }
        let combo = LaunchShortcut(serverId: server.id, serverName: server.name, agent: agent.id, agentName: agent.name,
                                   root: repo.root, repoName: repo.name, role: role)
        sending = true
        note = nil
        Task {
            let r = await store.launch(combo, task: trimmedTask, mode: mode.isEmpty ? nil : mode)
            sending = false
            note = r
            if r.ok {
                task = ""
                await load()
            }
        }
    }
}
