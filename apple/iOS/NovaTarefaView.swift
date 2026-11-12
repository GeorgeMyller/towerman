import SwiftUI

// MARK: - Nova tarefa

/// Lança uma sessão nova num computador pareado: o computador cria a worktree e arranca o agente.
struct NovaTarefaView: View {
    @ObservedObject var bridge: AgentDeckBridgeManager
    /// Chamado com o id da sessão criada, para abrir o cartão dela.
    var onLaunched: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    @State private var serverId = ""
    @State private var catalog: LaunchCatalog?
    @State private var loading = false
    @State private var loadError: String?
    @State private var agentId = ""
    @State private var root = ""
    @State private var role = ""
    @State private var task = ""
    @State private var mode = ""
    @State private var sending = false
    @State private var sendError: String?

    private static let modeLabels = ["acceptEdits": "Editar sem pedir", "default": "Pedir tudo", "plan": "Só planear"]
    private static let modeHelp = [
        "acceptEdits": "Edita ficheiros sem pedir; comandos pedem aprovação no iPhone e no relógio.",
        "default": "Cada edição e cada comando pedem aprovação no iPhone e no relógio.",
        "plan": "O agente só planeia: não edita nem corre comandos.",
    ]

    /// O codex exec não pede aprovações: o modo escolhe o sandbox.
    private static let codexHelp = [
        "default": "Sem aprovações: edita a worktree dentro do sandbox, sem rede.",
        "plan": "Sem aprovações: só lê, não edita nem corre comandos que escrevam.",
    ]

    private var agent: LaunchAgent? { catalog?.agents.first { $0.id == agentId } }
    private var repo: LaunchRepo? { catalog?.repos.first { $0.root == root } }
    private var trimmedTask: String { task.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var full: Bool { (catalog?.headless.open ?? 0) >= (catalog?.headless.max ?? 3) }
    private var canSend: Bool { agent != nil && repo != nil && !trimmedTask.isEmpty && !sending && !full }

    var body: some View {
        NavigationStack {
            Form {
                Section("Computador") {
                    if bridge.launchServers.isEmpty {
                        Text("Nenhum computador aceita lançamentos. Atualize o Towerman no computador.")
                            .foregroundStyle(Tower.soft)
                    } else {
                        Picker("Computador", selection: $serverId) {
                            ForEach(bridge.launchServers) { Text($0.name).tag($0.id) }
                        }
                    }
                }
                .listRowBackground(Tower.panel)

                if loading {
                    Section { HStack { ProgressView(); Text("A ler o computador…").foregroundStyle(Tower.soft) } }
                        .listRowBackground(Tower.panel)
                } else if let loadError {
                    Section {
                        Text(loadError).foregroundStyle(Tower.red)
                        Button("Tentar de novo") { Task { await load() } }
                    }
                    .listRowBackground(Tower.panel)
                } else if let catalog {
                    form(catalog)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Tower.ground)
            .navigationTitle("Nova tarefa")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancelar") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if sending { ProgressView() } else { Button("Enviar", action: send).disabled(!canSend) }
                }
            }
            .task {
                if serverId.isEmpty, let first = bridge.launchServers.first { serverId = first.id }
            }
            .onChange(of: serverId) { _, _ in Task { await load() } }
            .onChange(of: agentId) { _, _ in pickMode() }
            .onChange(of: root) { _, _ in if repo?.roles.contains(where: { $0.name == role }) != true { role = "" } }
        }
        .tint(Tower.blue)
        .preferredColorScheme(.dark)
    }

    @ViewBuilder
    private func form(_ catalog: LaunchCatalog) -> some View {
        Section {
            Picker("Agente", selection: $agentId) {
                ForEach(catalog.agents) { Text($0.name).tag($0.id) }
            }
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
        } footer: {
            if let note = agent?.note { Text(note) }
        }
        .listRowBackground(Tower.panel)

        Section {
            TextField("O que o agente deve fazer", text: $task, axis: .vertical)
                .lineLimit(3...8)
        } header: {
            Text("Tarefa")
        } footer: {
            if !trimmedTask.isEmpty { Text("Branch: feat/\(LaunchBranch.slug(trimmedTask, 40))") }
        }
        .listRowBackground(Tower.panel)

        if let agent, agent.modes.count > 1 {
            Section {
                Picker("Permissões", selection: $mode) {
                    ForEach(agent.modes, id: \.self) { Text(Self.modeLabels[$0] ?? $0).tag($0) }
                }
                .pickerStyle(.segmented)
            } header: {
                Text("Permissões")
            } footer: {
                Text((agent.id == "codex" ? Self.codexHelp[mode] : Self.modeHelp[mode]) ?? "")
            }
            .listRowBackground(Tower.panel)
        }

        Section {
            if let sendError { Text(sendError).foregroundStyle(Tower.red) }
            Text(full ? "Já há \(catalog.headless.max) sessões abertas pelo Towerman neste computador. Termine uma primeiro."
                      : "\(catalog.headless.open) de \(catalog.headless.max) sessões do Towerman a correr neste computador.")
                .font(.footnote)
                .foregroundStyle(full ? Tower.amber : Tower.soft)
        }
        .listRowBackground(Tower.panel)
    }

    private func load() async {
        guard !serverId.isEmpty else { return }
        loading = true
        loadError = nil
        let (cat, erro) = await bridge.catalog(serverId: serverId)
        loading = false
        catalog = cat
        loadError = cat == nil ? (erro ?? "Não consegui ler o computador.") : nil
        guard let cat else { return }
        // Começa no último combo usado neste computador, se ainda existir.
        let last = bridge.launchRecent.first { $0.serverId == serverId }
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
        guard let agent, let repo, let server = bridge.launchServers.first(where: { $0.id == serverId }) else { return }
        let combo = LaunchShortcut(serverId: server.id, serverName: server.name, agent: agent.id, agentName: agent.name,
                                   root: repo.root, repoName: repo.name, role: role)
        sending = true
        sendError = nil
        Task {
            let r = await bridge.launch(combo, task: trimmedTask, mode: mode.isEmpty ? nil : mode, origin: "phone")
            sending = false
            UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
            if r.ok {
                dismiss()
                if let id = r.sessionId { onLaunched(id) }
            } else {
                sendError = r.message
                await load()
            }
        }
    }
}
