import SwiftUI

// MARK: - Deck

/// O deck da Torre no Mac: contadores, radar, pedidos e partidas. `compact` é o painel da barra
/// de menus; sem ele, a janela larga com o radar à esquerda.
struct DeckView: View {
    @ObservedObject var store: DeckStore
    let compact: Bool
    @Environment(\.openWindow) private var openWindow
    @EnvironmentObject private var server: ServerManager
    @State private var selected: String?
    /// Sessão a quem mandar uma resposta rápida (dois dedos para a esquerda).
    @State private var quickTarget: Agent?
    /// Resultado de uma ação feita por gesto, mostrado alguns segundos.
    @State private var toast: String?

    private var waiting: [Agent] { store.agents.filter { AgentState($0) == .waiting } }
    private var board: [Agent] { store.agents.filter { AgentState($0) != .waiting } }

    var body: some View {
        VStack(spacing: 0) {
            header
            Rectangle().fill(Tower.line).frame(height: 1)
            if store.servers.isEmpty || (!store.isConnected && store.agents.isEmpty) {
                disconnected
            } else if compact {
                ScrollView { column.padding(14) }
            } else {
                HStack(alignment: .top, spacing: 18) {
                    radar.frame(maxWidth: .infinity, maxHeight: .infinity)
                    ScrollView { column }.frame(width: 440)
                }
                .padding(18)
            }
        }
        .background(Tower.ground)
        .foregroundStyle(Tower.text)
        .tint(Tower.blue)
        .preferredColorScheme(.dark)
        .overlay(alignment: .bottom) {
            if let toast {
                Text(toast)
                    .font(.callout.weight(.semibold))
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .background(Tower.panelRaised, in: Capsule())
                    .overlay(Capsule().strokeBorder(Tower.line, lineWidth: 1))
                    .padding(.bottom, 12)
                    .transition(.opacity)
            }
        }
        .animation(.easeOut(duration: 0.2), value: toast)
        .confirmationDialog("Resposta rápida", isPresented: Binding(get: { quickTarget != nil },
                                                                 set: { if !$0 { quickTarget = nil } }),
                            titleVisibility: .visible, presenting: quickTarget) { agent in
            ForEach(QuickReplies.load(), id: \.self) { text in
                Button(text) { act { await store.send(agent, text: text) } }
            }
            Button("Escrever…") { selected = agent.id }
            Button("Cancelar", role: .cancel) {}
        } message: { agent in
            Text(agent.replyTail(220).map { "\(agent.title): \($0)" } ?? "Para \(agent.title)")
        }
    }

    /// Corre uma ação no computador do agente e mostra o resultado.
    private func act(_ run: @escaping () async -> DeckStore.Result) {
        Task {
            let r = await run()
            show(r.message)
        }
    }

    private func show(_ message: String) {
        toast = message
        DispatchQueue.main.asyncAfter(deadline: .now() + 3.5) { if toast == message { toast = nil } }
    }

    private func focusSwipe(_ agent: Agent) -> SwipeAction? {
        guard agent.can("focus") else { return nil }
        return SwipeAction("Focar", icon: "macwindow", tint: Tower.blue, ink: .white) { act { await store.focus(agent) } }
    }

    private func quickSwipe(_ agent: Agent) -> SwipeAction? {
        guard agent.can("input"), AgentState(agent) != .exited else { return nil }
        return SwipeAction("Resposta rápida", icon: "text.bubble", tint: Tower.blue, ink: .white) { quickTarget = agent }
    }

    private var header: some View {
        let states = store.agents.map(AgentState.init)
        return HStack(spacing: 8) {
            Text("Towerman").font(.title3.weight(.bold))
            Blip(state: store.isConnected ? .running : .exited, size: 7)
            Spacer(minLength: 8)
            LEDCounter(value: states.filter { $0 == .waiting }.count, label: "À espera", color: Tower.amber, alert: true)
            LEDCounter(value: states.filter { $0 == .running }.count, label: "Em curso", color: Tower.green)
            if !compact {
                LEDCounter(value: states.filter { $0 != .waiting && $0 != .running }.count, label: "Parados", color: Tower.text)
            }
            Menu {
                Text(server.running ? "Servidor: a correr" : "Servidor: parado")
                if let detail = server.detail { Text(detail) }
                Button("Reiniciar servidor") { Task { await server.restart(); store.reconnect() } }
                Toggle("Abrir ao iniciar sessão", isOn: Binding(get: { server.startAtLogin },
                                                               set: { server.setStartAtLogin($0) }))
                Divider()
                Button("Abrir janela do deck") { openWindow(id: "deck"); NSApp.activate(ignoringOtherApps: true) }
                Button("Nova tarefa…") { openWindow(id: "launch"); NSApp.activate(ignoringOtherApps: true) }
                    .disabled(store.launchServers.isEmpty)
                Button("Métricas…") { openWindow(id: "metrics"); NSApp.activate(ignoringOtherApps: true) }
                Button("Computadores e iPhone…") { openWindow(id: "computers"); NSApp.activate(ignoringOtherApps: true) }
                Button("Reconectar") { store.reconnect() }
                Divider()
                Button("Sair do Towerman") { NSApp.terminate(nil) }
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .fixedSize()
            .accessibilityLabel("Mais opções")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }

    private var radar: some View {
        RadarView(agents: store.agents) { selected = $0.id }
            .padding(12)
            .background {
                RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Tower.panel)
                RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Tower.line, lineWidth: 1)
            }
            .overlay(alignment: .topLeading) { TowerLabel("Radar").padding(12) }
    }

    @ViewBuilder private var column: some View {
        VStack(alignment: .leading, spacing: 0) {
            if compact {
                radar.frame(height: 220).padding(.bottom, 16)
            }
            if !waiting.isEmpty {
                TowerLabel("Aguardando você", color: Tower.amber).padding(.bottom, 8)
                VStack(spacing: 10) {
                    ForEach(waiting) { agent in
                        MacRequestStrip(agent: agent, store: store,
                                        swipeRight: focusSwipe(agent), swipeLeft: quickSwipe(agent))
                    }
                }
                .padding(.bottom, 20)
            }
            if !board.isEmpty {
                TowerLabel("Partidas").padding(.bottom, 8)
                VStack(spacing: 0) {
                    ForEach(Array(board.enumerated()), id: \.element.id) { i, agent in
                        MacDepartureRow(agent: agent, store: store, expanded: selected == agent.id) {
                            selected = selected == agent.id ? nil : agent.id
                        }
                        .towerSwipe(leading: focusSwipe(agent), trailing: quickSwipe(agent))
                        if i < board.count - 1 { Rectangle().fill(Tower.line).frame(height: 1) }
                    }
                }
                .background {
                    RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Tower.panel)
                    RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Tower.line, lineWidth: 1)
                }
            }
            if store.agents.isEmpty {
                Text("Céu limpo. Abra uma sessão do Claude Code e ela aparece no radar.")
                    .foregroundStyle(Tower.soft)
            }
        }
        .animation(.spring(duration: 0.35), value: waiting.map(\.id))
    }

    private var disconnected: some View {
        VStack(alignment: .leading, spacing: 10) {
            TowerLabel(store.servers.isEmpty ? "A parear" : "Sem contato", color: Tower.red)
            Text(store.servers.isEmpty ? "A ligar ao Towerman deste Mac…" : "A torre não está falando com os seus computadores")
                .font(.title3.weight(.semibold))
            Text(store.servers.isEmpty
                 ? "Se não ligar, confirme que o serviço está a correr: towerman serve (ou towerman install)."
                 : (store.status.values.compactMap(\.problem).first ?? "A tentar de novo…"))
                .foregroundStyle(Tower.soft)
            HStack {
                Button("Computadores…") { openWindow(id: "computers"); NSApp.activate(ignoringOtherApps: true) }
                Button("Tentar de novo") {
                    store.reconnect()
                    Task { await store.pairLocalIfNeeded() }
                }
            }
            .padding(.top, 4)
        }
        .padding(18)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

// MARK: - Faixa de pedido

struct MacRequestStrip: View {
    let agent: Agent
    @ObservedObject var store: DeckStore
    /// Gestos quando não é uma permissão (focar / resposta rápida), vindos do deck.
    var swipeRight: SwipeAction? = nil
    var swipeLeft: SwipeAction? = nil
    @State private var sending: String?
    @State private var note: String?

    var body: some View {
        let prompt = agent.waiting
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Blip(state: .waiting, size: 9, tint: Tower.onAmber, kind: AgentKind(agent))
                Text(agent.title).font(.headline).lineLimit(1)
                ProviderBadge(AgentKind(agent), onAmber: true)
                Spacer(minLength: 6)
                Text([agent.host, PanelTime.since(agent.statusSince)].compactMap { $0 }
                    .filter { !$0.isEmpty }.joined(separator: " · ").uppercased())
                    .font(.caption2.weight(.bold))
                    .opacity(0.8)
                    .lineLimit(1)
            }
            if let prompt {
                if !prompt.title.isEmpty { Text(prompt.title).font(.subheadline.weight(.semibold)) }
                Text(prompt.detail)
                    .font(prompt.isQuestion ? .body : .system(.callout, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(prompt.isQuestion ? 0 : 8)
                    .background(prompt.isQuestion ? Color.clear : Tower.onAmber.opacity(0.1),
                                in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            } else if let reply = agent.lastReply, !reply.isEmpty {
                // Espera sem pedido: o fim da última resposta costuma ser a pergunta.
                Text(reply).lineLimit(8).textSelection(.enabled)
            } else if !agent.askedText.isEmpty {
                Text(agent.askedText).lineLimit(4)
            }
            if prompt != nil, let reply = agent.lastReply, !reply.isEmpty {
                DisclosureGroup("Contexto") {
                    MacConversation(agent: agent, ink: Tower.onAmber)
                }
                .font(.footnote.weight(.semibold))
            }
            if let prompt, agent.can("answer"), !prompt.options.isEmpty {
                answers(prompt)
                if prompt.isDecision {
                    // Decisão em texto livre: motivo dos botões desativados, outra resposta e descarte.
                    if !prompt.context.isEmpty {
                        Text("Botões desativados: \(prompt.context). Foque o terminal e responda lá.")
                            .font(.footnote.weight(.medium))
                    }
                    MacReplyField(agent: agent, store: store, onAmber: true)
                    dismissButton(prompt)
                }
            } else {
                MacReplyField(agent: agent, store: store, onAmber: true)
                if let prompt, prompt.isDecision {
                    dismissButton(prompt)
                }
            }
            if let note { Text(note).font(.footnote.weight(.medium)) }
        }
        .foregroundStyle(Tower.onAmber)
        .padding(12)
        .background(Tower.amber, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .towerSwipe(leading: permissionSwipe(primary: true) ?? swipeRight,
                    trailing: permissionSwipe(primary: false) ?? swipeLeft)
    }

    /// Permissão com duas opções: dois dedos para a direita permitem, para a esquerda negam.
    private func permissionSwipe(primary: Bool) -> SwipeAction? {
        guard let p = agent.waiting, agent.can("answer"), !p.isQuestion, p.options.count == 2 else { return nil }
        let allow = p.options.first(where: \.primary) ?? p.options[0]
        guard let deny = p.options.first(where: { $0.key != allow.key }) else { return nil }
        let option = primary ? allow : deny
        return SwipeAction(option.label, icon: primary ? "checkmark" : "xmark",
                           tint: primary ? Tower.green : Tower.red, ink: primary ? Tower.ground : .white) {
            Task {
                sending = option.key
                let r = await store.answer(agent, prompt: p, key: option.key)
                sending = nil
                note = r.ok ? nil : r.message
            }
        }
    }

    private func dismissButton(_ prompt: Prompt) -> some View {
        Button("Não é pergunta") {
            Task {
                let r = await store.answer(agent, prompt: prompt, key: PromptOption.dismiss.key)
                note = r.ok ? nil : r.message
            }
        }
        .buttonStyle(.bordered)
        .tint(Tower.onAmber)
    }

    @ViewBuilder private func answers(_ prompt: Prompt) -> some View {
        let primary = prompt.options.first(where: \.primary) ?? prompt.options[0]
        let rest = prompt.options.filter { $0.key != primary.key }
        if prompt.isQuestion || prompt.options.count > 2 {
            VStack(spacing: 6) {
                button(primary, prompt, filled: true)
                ForEach(rest) { button($0, prompt, filled: false) }
            }
        } else {
            // Negar afastado de Permitir, para não ser clicado por engano.
            HStack(spacing: 28) {
                button(primary, prompt, filled: true)
                ForEach(rest) { button($0, prompt, filled: false).frame(width: 100) }
            }
        }
    }

    private func button(_ option: PromptOption, _ prompt: Prompt, filled: Bool) -> some View {
        Button {
            sending = option.key
            Task {
                let r = await store.answer(agent, prompt: prompt, key: option.key)
                sending = nil
                note = r.ok ? nil : r.message
            }
        } label: {
            HStack(spacing: 6) {
                if sending == option.key { ProgressView().controlSize(.small) }
                Text(option.label).lineLimit(2)
            }
            .font(.headline)
            .frame(maxWidth: .infinity, minHeight: 36)
            .foregroundStyle(filled ? Tower.amber : Tower.onAmber)
            .background {
                RoundedRectangle(cornerRadius: 10, style: .continuous).fill(filled ? Tower.onAmber : Color.clear)
                RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Tower.onAmber, lineWidth: filled ? 0 : 2)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(sending != nil || option.disabled)
        .opacity(option.disabled ? 0.45 : 1)
        .keyboardShortcut(filled && !prompt.isQuestion ? .defaultAction : nil)
    }
}

// MARK: - Responder com texto

struct MacReplyField: View {
    let agent: Agent
    @ObservedObject var store: DeckStore
    let onAmber: Bool
    @State private var text = ""
    @State private var sending = false
    @State private var note: String?
    @State private var focusing = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                TextField("Responder ao agente…", text: $text, axis: .vertical)
                    .textFieldStyle(.plain)
                    .lineLimit(1...4)
                    .onSubmit(send)
                    .padding(8)
                    .background(onAmber ? Tower.onAmber.opacity(0.1) : Tower.ground,
                                in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                Button("Enviar", action: send)
                    .disabled(sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                Button(focusing ? "A focar…" : "Focar") {
                    focusing = true
                    Task {
                        let r = await store.focus(agent)
                        focusing = false
                        note = r.message
                    }
                }
                .disabled(focusing)
                if agent.canTerminate {
                    Button("Terminar", role: .destructive) {
                        Task { note = await store.terminate(agent).message }
                    }
                }
            }
            if let note { Text(note).font(.footnote.weight(.medium)).foregroundStyle(onAmber ? Tower.onAmber : Tower.soft) }
        }
    }

    private func send() {
        let msg = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !msg.isEmpty, !sending else { return }
        sending = true
        Task {
            let r = await store.send(agent, text: msg)
            sending = false
            note = r.message
            if r.ok { text = "" }
        }
    }
}

// MARK: - Conversa

/// O último pedido da pessoa e o fim da última resposta do agente.
struct MacConversation: View {
    let agent: Agent
    let ink: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !agent.askedText.isEmpty {
                TowerLabel("Você pediu", color: ink.opacity(0.7))
                Text(agent.askedText).font(.callout).lineLimit(6).textSelection(.enabled)
            }
            if let reply = agent.lastReply, !reply.isEmpty {
                TowerLabel("\(AgentKind(agent).name) respondeu", color: ink.opacity(0.7))
                Text(reply).font(.callout).lineLimit(14).textSelection(.enabled)
            }
        }
        .foregroundStyle(ink)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Partida

struct MacDepartureRow: View {
    let agent: Agent
    @ObservedObject var store: DeckStore
    let expanded: Bool
    let toggle: () -> Void

    var body: some View {
        let state = AgentState(agent)
        VStack(alignment: .leading, spacing: 8) {
            Button(action: toggle) {
                HStack(spacing: 10) {
                    Blip(state: state, kind: AgentKind(agent))
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            Text(agent.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                            ProviderBadge(AgentKind(agent))
                        }
                        Text(agent.activityLine).font(.caption).foregroundStyle(Tower.soft).lineLimit(1)
                    }
                    Spacer(minLength: 6)
                    VStack(alignment: .trailing, spacing: 3) {
                        StateTag(state: state)
                        Text(agent.host.uppercased()).font(.caption2.weight(.semibold)).foregroundStyle(Tower.soft).lineLimit(1)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if expanded {
                Text(agent.cwd).font(.caption.monospaced()).foregroundStyle(Tower.soft).textSelection(.enabled)
                MacConversation(agent: agent, ink: Tower.text)
                MacReplyField(agent: agent, store: store, onAmber: false)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(expanded ? Tower.panelRaised : Color.clear)
    }
}

// MARK: - Computadores e iPhone

struct ComputersView: View {
    @ObservedObject var store: DeckStore
    @State private var link = ""
    @State private var note: String?
    @State private var qr: NSImage?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                TowerLabel("Computadores")
                VStack(spacing: 0) {
                    ForEach(store.servers) { server in
                        let st = store.status[server.id]
                        HStack(spacing: 10) {
                            Blip(state: st?.connected == true ? .running : .exited, size: 9)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(server.name).font(.headline)
                                Text(st?.connected == true ? "Conectado por \(st?.address ?? "") · \(st?.agents ?? 0) agentes"
                                     : (st?.problem ?? "Procurando…"))
                                    .font(.caption)
                                    .foregroundStyle(st?.connected == true ? Tower.soft : Tower.amber)
                            }
                            Spacer()
                            Button("Esquecer") { store.remove(server) }
                        }
                        .padding(12)
                        Rectangle().fill(Tower.line).frame(height: 1)
                    }
                    if store.servers.isEmpty {
                        Text("Nenhum computador pareado.").foregroundStyle(Tower.soft).padding(12)
                    }
                }
                .background(Tower.panel, in: RoundedRectangle(cornerRadius: 12, style: .continuous))

                TowerLabel("Adicionar outro computador (ex.: o Linux)")
                Text("No outro computador, rode «towerman pair --link» e cole aqui o link towerman://.")
                    .font(.callout)
                    .foregroundStyle(Tower.soft)
                HStack {
                    TextField("towerman://pair?…", text: $link)
                        .textFieldStyle(.roundedBorder)
                    Button("Parear") {
                        guard let url = URL(string: link.trimmingCharacters(in: .whitespacesAndNewlines)) else { return }
                        Task {
                            note = await store.pair(with: url)
                            link = ""
                        }
                    }
                    .disabled(link.isEmpty)
                }
                if let note { Text(note).font(.callout).foregroundStyle(Tower.soft) }

                TowerLabel("Parear o iPhone")
                HStack(alignment: .top, spacing: 16) {
                    Group {
                        if let qr {
                            Image(nsImage: qr).interpolation(.none).resizable().frame(width: 200, height: 200)
                        } else {
                            RoundedRectangle(cornerRadius: 8).fill(Tower.panel).frame(width: 200, height: 200)
                                .overlay(Text("Sem QR").foregroundStyle(Tower.soft))
                        }
                    }
                    .padding(8)
                    .background(Color.white, in: RoundedRectangle(cornerRadius: 12))
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Aponte a câmera do iPhone para o código e toque em «Abrir no Towerman».")
                            .foregroundStyle(Tower.soft)
                        Text("Vale 2 minutos e só serve uma vez.").font(.caption).foregroundStyle(Tower.soft)
                        Button("Gerar outro código") { Task { qr = await store.iPhoneQR() } }
                    }
                }
            }
            .padding(20)
        }
        .background(Tower.ground)
        .foregroundStyle(Tower.text)
        .preferredColorScheme(.dark)
        .task { qr = await store.iPhoneQR() }
    }
}
