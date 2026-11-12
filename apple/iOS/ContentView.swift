import SwiftUI
import VisionKit

// MARK: - Torre

public struct ContentView: View {
    @StateObject private var bridge = AgentDeckBridgeManager()
    @State private var pairingNote: String?
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dynamicTypeSize) private var typeSize
    @State private var hostFilter = "Todos"
    @State private var showSetup = false
    @State private var showLaunch = false
    @State private var path: [String] = []
    /// Resultado de uma ação feita por gesto (ex.: focar), mostrado alguns segundos.
    @State private var toast: String?
    /// Sessão a quem mandar uma resposta rápida (arrastar para a esquerda).
    @State private var quickTarget: Agent?

    public init() {}

    private func show(_ message: String) {
        toast = message
        Task {
            try? await Task.sleep(nanoseconds: 3_500_000_000)
            await MainActor.run { if toast == message { toast = nil } }
        }
    }

    private func sendQuick(_ text: String, to agent: Agent) {
        Task {
            let r = await bridge.sendInput(agentId: agent.id, text: text)
            await MainActor.run {
                UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
                show(r.message)
            }
        }
    }

    /// Arrastar para a esquerda: resposta rápida, quando a sessão aceita texto.
    private func quickSwipe(_ agent: Agent) -> SwipeAction? {
        guard agent.can("input"), AgentState(agent) != .exited else { return nil }
        return SwipeAction("Resposta rápida", icon: "text.bubble", tint: Tower.blue, ink: .white) { quickTarget = agent }
    }

    private func focus(_ agent: Agent) {
        Task {
            let r = await bridge.focus(agentId: agent.id)
            await MainActor.run {
                UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
                toast = r.message.isEmpty ? (r.ok ? "Focado no computador." : "Não foi possível focar.") : r.message
            }
            try? await Task.sleep(nanoseconds: 3_500_000_000)
            await MainActor.run { toast = nil }
        }
    }

    private var agents: [Agent] { bridge.currentState?.agents ?? [] }
    private var hosts: [String] { Array(Set(agents.map(\.host).filter { !$0.isEmpty })).sorted() }
    private var visible: [Agent] { hostFilter == "Todos" ? agents : agents.filter { $0.host == hostFilter } }
    // Todo o agente que aguarda você sobe às faixas de pedido, tenha ou não botões de resposta.
    private var waiting: [Agent] { visible.filter { AgentState($0) == .waiting } }
    private var board: [Agent] { visible.filter { AgentState($0) != .waiting } }

    public var body: some View {
        NavigationStack(path: $path) {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    if bridge.currentState == nil {
                        disconnected
                    } else {
                        counters
                        if hosts.count > 1 {
                            Picker("Computador", selection: $hostFilter) {
                                Text("Todos").tag("Todos")
                                ForEach(hosts, id: \.self) { Text($0).tag($0) }
                            }
                            .pickerStyle(.segmented)
                            .padding(.bottom, 14)
                        }
                        // Com letra grande, os pedidos sobem acima do radar: quem precisa de si
                        // tem de caber no primeiro ecrã.
                        if !typeSize.isAccessibilitySize { radar }
                        if visible.isEmpty {
                            empty
                        } else {
                            if !waiting.isEmpty {
                                TowerLabel("Aguardando você", color: Tower.amber)
                                    .padding(.top, typeSize.isAccessibilitySize ? 4 : 22)
                                    .padding(.bottom, 8)
                                VStack(spacing: 12) {
                                    ForEach(waiting) { agent in
                                        RequestStrip(agent: agent, bridge: bridge, onQuick: { quickTarget = agent })
                                    }
                                }
                            }
                            if typeSize.isAccessibilitySize { radar.padding(.top, 22) }
                            if !board.isEmpty {
                                TowerLabel("Partidas")
                                    .padding(.top, 26)
                                    .padding(.bottom, 8)
                                departures
                            }
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 32)
                .animation(.spring(duration: 0.35), value: waiting.map(\.id))
            }
            .background(Tower.ground.ignoresSafeArea())
            .overlay(alignment: .bottom) {
                if let toast {
                    Text(toast)
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Tower.text)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 10)
                        .background(Tower.panel, in: Capsule())
                        .overlay(Capsule().strokeBorder(Tower.line, lineWidth: 1))
                        .padding(.bottom, 12)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .animation(.spring(duration: 0.3), value: toast)
            .confirmationDialog("Resposta rápida", isPresented: Binding(get: { quickTarget != nil },
                                                                     set: { if !$0 { quickTarget = nil } }),
                                titleVisibility: .visible, presenting: quickTarget) { agent in
                ForEach(QuickReplies.load(), id: \.self) { text in
                    Button(text) { sendQuick(text, to: agent) }
                }
                Button("Escrever…") { path.append(agent.id) }
                Button("Cancelar", role: .cancel) {}
            } message: { agent in
                Text(agent.replyTail(220).map { "\(agent.title): \($0)" } ?? "Para \(agent.title)")
            }
            .navigationTitle("Towerman")
            .navigationDestination(for: String.self) { id in
                AgentDetail(agentId: id, bridge: bridge)
            }
            .toolbar {
                if !bridge.launchServers.isEmpty {
                    ToolbarItem(placement: .topBarLeading) {
                        Button { showLaunch = true } label: { Image(systemName: "plus") }
                            .accessibilityLabel("Nova tarefa")
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showSetup = true } label: {
                        Image(systemName: bridge.isConnected ? "antenna.radiowaves.left.and.right" : "antenna.radiowaves.left.and.right.slash")
                            .foregroundStyle(bridge.isConnected ? Tower.text : Tower.red)
                    }
                    .accessibilityLabel(bridge.isConnected ? "Servidor conectado. Configurar" : "Desconectado. Configurar servidor")
                }
            }
            .refreshable { await bridge.fetchInitialState() }
            .sheet(isPresented: $showSetup) {
                SetupSheet(bridge: bridge)
            }
            .sheet(isPresented: $showLaunch) {
                NovaTarefaView(bridge: bridge) { id in path.append(id) }
            }
        }
        .tint(Tower.blue)
        .preferredColorScheme(.dark)
        .onAppear {
            // A torre abre já ligada aos computadores pareados.
            if !bridge.isConnected {
                bridge.startListening()
            }
            #if DEBUG
            // Testes no simulador: parear sem tocar no alerta "Abrir no Towerman?".
            if let link = ProcessInfo.processInfo.environment["TOWERMAN_PAIR_URL"], let url = URL(string: link) {
                Task { pairingNote = await bridge.pair(with: url).message }
            }
            #endif
        }
        .onReceive(NotificationCenter.default.publisher(for: .towerOpenAgent)) { note in
            if let id = note.object as? String { path = [id] }
        }
        // QR lido pela câmara do sistema: towerman://pair?… abre o app e pareia.
        .onOpenURL { url in
            // towerman://agent/<id> vem da Live Activity: basta abrir o app.
            guard TowerLink.agentId(from: url) == nil else { return }
            Task {
                let r = await bridge.pair(with: url)
                pairingNote = r.message
            }
        }
        .alert("Pareamento", isPresented: Binding(get: { pairingNote != nil }, set: { if !$0 { pairingNote = nil } })) {
            Button("OK") { pairingNote = nil }
        } message: {
            Text(pairingNote ?? "")
        }
        .onChange(of: scenePhase) { _, phase in
            // Enquanto bridge.wantsConnection for verdadeiro, o áudio de fundo mantém o processo
            // vivo para receber eventos em tempo real com a tela bloqueada e alertar o Apple Watch.
            // Ao voltar (.active), religa imediatamente se a ligação foi solicitada.
            if phase == .active && bridge.wantsConnection {
                bridge.startListening()
            } else if phase == .background {
                if !bridge.wantsConnection {
                    bridge.suspendForBackground()
                }
            }
        }
        .onChange(of: hosts) { _, list in
            if hostFilter != "Todos" && !list.contains(hostFilter) { hostFilter = "Todos" }
        }
    }

    // MARK: Contadores LED

    private var counters: some View {
        let states = visible.map(AgentState.init)
        // Com letra grande, duas colunas de largura igual em vez de uma pilha irregular.
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(GridLayout(alignment: .leading, horizontalSpacing: 8, verticalSpacing: 8))
            : AnyLayout(HStackLayout(spacing: 8))
        return VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Blip(state: bridge.isConnected ? .running : .exited, size: 7)
                Text(bridge.isConnected ? "\(max(hosts.count, 1)) \(hosts.count > 1 ? "computadores" : "computador") em linha" : "Reconectando…")
                    .font(.subheadline)
                    .foregroundStyle(Tower.soft)
            }
            layout {
                if typeSize.isAccessibilitySize {
                    GridRow {
                        LEDCounter(value: states.filter { $0 == .waiting }.count, label: "À espera", color: Tower.amber, alert: true, fill: true)
                        LEDCounter(value: states.filter { $0 == .running }.count, label: "Em curso", color: Tower.green, fill: true)
                    }
                    GridRow {
                        LEDCounter(value: states.filter { $0 != .waiting && $0 != .running }.count, label: "Parados", color: Tower.text, fill: true)
                    }
                } else {
                    LEDCounter(value: states.filter { $0 == .waiting }.count, label: "À espera", color: Tower.amber, alert: true)
                    LEDCounter(value: states.filter { $0 == .running }.count, label: "Em curso", color: Tower.green)
                    LEDCounter(value: states.filter { $0 != .waiting && $0 != .running }.count, label: "Parados", color: Tower.text)
                }
            }
        }
        .padding(.bottom, 14)
    }

    // MARK: Radar

    private var radar: some View {
        RadarView(agents: visible) { path.append($0.id) }
            .frame(height: typeSize.isAccessibilitySize ? 220 : 250)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity)
            .background {
                RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Tower.panel)
                RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Tower.line, lineWidth: 1)
            }
            .overlay(alignment: .topLeading) {
                TowerLabel("Radar").padding(12)
            }
    }

    // MARK: Quadro de partidas

    private var departures: some View {
        VStack(spacing: 0) {
            ForEach(Array(board.enumerated()), id: \.element.id) { index, agent in
                NavigationLink(value: agent.id) { DepartureRow(agent: agent) }
                    .buttonStyle(.plain)
                    .background(Tower.panel)
                    .towerSwipe(
                        leading: agent.can("focus")
                            ? SwipeAction("Focar", icon: "macwindow", tint: Tower.blue, ink: .white) { focus(agent) }
                            : nil,
                        trailing: quickSwipe(agent))
                if index < board.count - 1 {
                    Rectangle().fill(Tower.line).frame(height: 1).padding(.leading, 14)
                }
            }
        }
        .background {
            RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Tower.panel)
            RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Tower.line, lineWidth: 1)
        }
    }

    // MARK: Estados vazios

    private var disconnected: some View {
        VStack(alignment: .leading, spacing: 12) {
            TowerLabel(bridge.servers.isEmpty ? "Sem computadores" : "Sem contato", color: Tower.red)
            Text(bridge.servers.isEmpty ? "Pareie um computador" : "A torre não está falando com os seus computadores")
                .font(.title3.weight(.semibold))
                .foregroundStyle(Tower.text)
            Text(bridge.servers.isEmpty
                 ? "No Mac ou no Linux, rode «towerman pair» e leia o QR com a câmera do iPhone."
                 : (bridge.errorMessage ?? "Verifique se o computador está ligado e com internet."))
                .font(.body)
                .foregroundStyle(Tower.soft)
            Button(bridge.servers.isEmpty ? "Adicionar computador" : "Ver computadores") { showSetup = true }
                .buttonStyle(.borderedProminent)
                .tint(Tower.blue)
                .controlSize(.large)
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Tower.panel)
            RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Tower.line, lineWidth: 1)
        }
        .padding(.top, 8)
    }

    private var empty: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Céu limpo")
                .font(.title3.weight(.semibold))
                .foregroundStyle(Tower.text)
            Text("Nenhum agente ativo \(hostFilter == "Todos" ? "nos seus computadores" : "em \(hostFilter)"). Abra uma sessão do Claude Code e ela aparece no radar.")
                .font(.body)
                .foregroundStyle(Tower.soft)
        }
        .padding(.top, 20)
    }
}

// MARK: - Faixa de pedido

struct RequestStrip: View {
    let agent: Agent
    @ObservedObject var bridge: AgentDeckBridgeManager
    /// Abre as respostas rápidas (arrastar para a esquerda); nil quando a faixa já está no detalhe.
    var onQuick: (() -> Void)? = nil
    @State private var sending: String?
    @State private var failure: String?
    @State private var focusing = false
    @Environment(\.dynamicTypeSize) private var typeSize

    private func focus() {
        focusing = true
        failure = nil
        Task {
            let r = await bridge.focus(agentId: agent.id)
            await MainActor.run {
                focusing = false
                UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
                failure = r.message
            }
        }
    }

    var body: some View {
        let prompt = agent.waiting
        TimelineView(.periodic(from: .now, by: 30)) { ctx in
            VStack(alignment: .leading, spacing: 10) {
                let origin = HStack(spacing: 6) {
                    ProviderBadge(AgentKind(agent), onAmber: true)
                    Text([agent.host, PanelTime.since(agent.statusSince, now: ctx.date)]
                        .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ").uppercased())
                }
                    .font(.caption2.weight(.bold))
                    .tracking(0.8)
                    .opacity(0.8)
                // Com letra grande, a origem desce para baixo do nome em vez de o cortar.
                let header = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
                    : AnyLayout(HStackLayout(spacing: 8))
                header {
                    HStack(spacing: 8) {
                        Blip(state: .waiting, size: 10, tint: Tower.onAmber, kind: AgentKind(agent))
                        Text(agent.title)
                            .font(.headline)
                            .lineLimit(typeSize.isAccessibilitySize ? 2 : 1)
                    }
                    if !typeSize.isAccessibilitySize { Spacer(minLength: 8) }
                    origin.lineLimit(typeSize.isAccessibilitySize ? 2 : 1)
                }

                // O que o agente disse por último: é o contexto para decidir a resposta.
                if prompt != nil, let reply = agent.lastReply, !reply.isEmpty {
                    Text(reply)
                        .font(.footnote)
                        .lineLimit(typeSize.isAccessibilitySize ? nil : 3)
                        .opacity(0.85)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .accessibilityLabel("O agente disse: \(reply)")
                }
                if let prompt {
                    if !prompt.title.isEmpty {
                        Text(prompt.title)
                            .font(.subheadline.weight(.semibold))
                    }
                    Text(prompt.detail)
                        .font(prompt.isQuestion ? .body : .system(.callout, design: .monospaced))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(prompt.isQuestion ? 0 : 10)
                        .background(prompt.isQuestion ? Color.clear : Tower.onAmber.opacity(0.1),
                                    in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                        .textSelection(.enabled)
                } else if let reply = agent.lastReply, !reply.isEmpty {
                    // Espera sem pedido: o fim da última resposta costuma ser a pergunta.
                    Text(reply)
                        .font(.body)
                        .lineLimit(typeSize.isAccessibilitySize ? nil : 6)
                        .textSelection(.enabled)
                } else if !agent.askedText.isEmpty {
                    Text(agent.askedText)
                        .font(.body)
                        .lineLimit(typeSize.isAccessibilitySize ? nil : 4)
                }

                if let prompt, agent.can("answer") && !prompt.options.isEmpty {
                    answers(prompt)
                    if prompt.isDecision {
                        decisionExtras(prompt)
                    }
                } else {
                    // Sem botões: o agente espera texto. Responde-se daqui, ou foca-se o terminal.
                    if agent.can("input") {
                        ReplyField(agent: agent, bridge: bridge, onAmber: true)
                    }
                    HStack {
                        Text(agent.can("input") ? "Ou responda no computador." : "Responda no computador.")
                            .font(.footnote.weight(.medium))
                        Spacer()
                        if let prompt, prompt.isDecision {
                            Button("Não é pergunta") { send(.dismiss, prompt: prompt) }
                                .buttonStyle(.bordered)
                                .tint(Tower.onAmber)
                        }
                        Button(focusing ? "A focar…" : "Focar") { focus() }
                            .buttonStyle(.bordered)
                            .tint(Tower.onAmber)
                            .disabled(focusing)
                    }
                }
                if let failure {
                    Text(failure)
                        .font(.footnote.weight(.medium))
                }
            }
            .foregroundStyle(Tower.onAmber)
            .padding(14)
            .background(Tower.amber, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .towerSwipe(leading: swipeRight, trailing: swipeLeft)
            .accessibilityElement(children: .contain)
            .accessibilityLabel("\(agent.title), aguardando você")
        }
    }

    // MARK: Gestos

    /// Permissão com duas opções: direita permite, esquerda nega.
    private var permissionPair: (allow: PromptOption, deny: PromptOption)? {
        guard let p = agent.waiting, agent.can("answer"), !p.isQuestion, p.options.count == 2 else { return nil }
        let allow = p.options.first(where: \.primary) ?? p.options[0]
        guard let deny = p.options.first(where: { $0.key != allow.key }) else { return nil }
        return (allow, deny)
    }

    private var swipeRight: SwipeAction? {
        if let pair = permissionPair, let p = agent.waiting {
            return SwipeAction(pair.allow.label, icon: "checkmark", tint: Tower.green) { send(pair.allow, prompt: p) }
        }
        guard agent.can("focus") else { return nil }
        return SwipeAction("Focar", icon: "macwindow", tint: Tower.blue, ink: .white) { focus() }
    }

    private var swipeLeft: SwipeAction? {
        if let pair = permissionPair, let p = agent.waiting {
            return SwipeAction(pair.deny.label, icon: "xmark", tint: Tower.red, ink: .white) { send(pair.deny, prompt: p) }
        }
        guard let onQuick, agent.can("input") else { return nil }
        return SwipeAction("Resposta rápida", icon: "text.bubble", tint: Tower.blue, ink: .white, run: onQuick)
    }

    /// Decisão em texto livre: o motivo dos botões desativados, a caixa para responder outra coisa,
    /// descartar um falso alarme e focar o terminal.
    @ViewBuilder
    private func decisionExtras(_ prompt: Prompt) -> some View {
        if !prompt.context.isEmpty {
            Text("Botões desativados: \(prompt.context). Foque o terminal e responda lá.")
                .font(.footnote.weight(.medium))
        }
        if agent.can("input") {
            ReplyField(agent: agent, bridge: bridge, onAmber: true)
        }
        HStack {
            Button("Não é pergunta") { send(.dismiss, prompt: prompt) }
                .buttonStyle(.bordered)
                .tint(Tower.onAmber)
                .disabled(sending != nil)
            Spacer()
            Button(focusing ? "A focar…" : "Focar") { focus() }
                .buttonStyle(.bordered)
                .tint(Tower.onAmber)
                .disabled(focusing)
        }
    }

    @ViewBuilder
    private func answers(_ prompt: Prompt) -> some View {
        let primary = prompt.options.first(where: \.primary) ?? prompt.options[0]
        let rest = prompt.options.filter { $0.key != primary.key }
        if prompt.isQuestion || prompt.options.count > 2 || typeSize.isAccessibilitySize {
            // Perguntas: uma opção por linha. Numa permissão empilhada, Negar continua afastado.
            VStack(spacing: prompt.isQuestion ? 8 : 28) {
                answerButton(primary, prompt: prompt, filled: true)
                ForEach(rest) { answerButton($0, prompt: prompt, filled: false) }
            }
        } else {
            // Permissão: Permitir largo; Negar afastado, para não ser tocado por engano.
            HStack(spacing: 28) {
                answerButton(primary, prompt: prompt, filled: true)
                ForEach(rest) { answerButton($0, prompt: prompt, filled: false).frame(width: 104) }
            }
        }
    }

    private func answerButton(_ option: PromptOption, prompt: Prompt, filled: Bool) -> some View {
        Button {
            send(option, prompt: prompt)
        } label: {
            HStack(spacing: 6) {
                if sending == option.key { ProgressView().tint(filled ? Tower.amber : Tower.onAmber) }
                Text(option.label).lineLimit(2)
            }
            .font(.headline)
            .frame(maxWidth: .infinity, minHeight: 48)
            .foregroundStyle(filled ? Tower.amber : Tower.onAmber)
            .background {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(filled ? Tower.onAmber : Color.clear)
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(Tower.onAmber, lineWidth: filled ? 0 : 2)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(sending != nil || option.disabled)
        .opacity(option.disabled ? 0.45 : 1)
        .accessibilityLabel("\(option.label): \(prompt.detail)")
    }

    private func send(_ option: PromptOption, prompt: Prompt) {
        sending = option.key
        failure = nil
        Task {
            let ok = await bridge.sendAnswer(agentId: agent.id, promptId: prompt.id, key: option.key)
            await MainActor.run {
                sending = nil
                if ok {
                    UINotificationFeedbackGenerator().notificationOccurred(.success)
                } else {
                    UINotificationFeedbackGenerator().notificationOccurred(.error)
                    failure = "Não foi possível responder. O pedido pode já ter sido respondido no terminal."
                }
            }
        }
    }
}

// MARK: - Responder com texto

/// Campo para mandar texto à sessão, como se fosse escrito no terminal. Mostra o que aconteceu:
/// entregue no terminal, ou na fila até o passo atual terminar.
struct ReplyField: View {
    let agent: Agent
    @ObservedObject var bridge: AgentDeckBridgeManager
    let onAmber: Bool
    @State private var text = ""
    @State private var sending = false
    @State private var result: AgentDeckBridgeManager.ActionResult?
    @FocusState private var focused: Bool

    var body: some View {
        let ink = onAmber ? Tower.onAmber : Tower.text
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                TextField("Responder ao agente…", text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .focused($focused)
                    .submitLabel(.send)
                    .onSubmit(send)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 8)
                    .foregroundStyle(ink)
                    .background(onAmber ? Tower.onAmber.opacity(0.1) : Tower.ground,
                                in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                Button(action: send) {
                    Group {
                        if sending { ProgressView() } else { Image(systemName: "arrow.up.circle.fill").font(.title2) }
                    }
                    .frame(width: 44, height: 44)
                }
                .foregroundStyle(onAmber ? Tower.onAmber : Tower.blue)
                .disabled(sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .accessibilityLabel("Enviar")
            }
            if let result {
                Text(result.message)
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(onAmber ? Tower.onAmber : (result.ok ? Tower.green : Tower.red))
            }
        }
    }

    private func send() {
        let msg = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !msg.isEmpty, !sending else { return }
        sending = true
        Task {
            let r = await bridge.sendInput(agentId: agent.id, text: msg)
            await MainActor.run {
                sending = false
                result = r
                UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
                if r.ok {
                    text = ""
                    focused = false
                }
            }
        }
    }
}

// MARK: - Linha do quadro de partidas

struct DepartureRow: View {
    let agent: Agent

    var body: some View {
        let state = AgentState(agent)
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .center, spacing: 12) {
                Blip(state: state, kind: AgentKind(agent))
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text(agent.title)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(Tower.text)
                            .lineLimit(1)
                        ProviderBadge(AgentKind(agent))
                    }
                    Text(agent.activityLine)
                        .font(.caption)
                        .foregroundStyle(Tower.soft)
                        .lineLimit(1)
                }
                Spacer(minLength: 8)
                VStack(alignment: .trailing, spacing: 4) {
                    StateTag(state: state)
                    Text(agent.host.uppercased())
                        .font(.caption2.weight(.semibold))
                        .tracking(1)
                        .foregroundStyle(Tower.soft)
                }
            }
            // A última coisa que o agente disse (a pergunta a que se responde), a toda a largura.
            if let tail = agent.replyTail(96) {
                Text(tail)
                    .font(.caption)
                    .foregroundStyle(Tower.text.opacity(0.85))
                    .lineLimit(2)
                    .padding(.leading, 22)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(agent.title), \(AgentKind(agent).name), \(agent.host), \(state.label). \(agent.activityLine). \(agent.replyTail(96) ?? "")")
        .accessibilityHint("Abre os detalhes do agente")
    }
}

// MARK: - Detalhe do agente

struct AgentDetail: View {
    let agentId: String
    @ObservedObject var bridge: AgentDeckBridgeManager
    @State private var note: String?

    private var agent: Agent? { bridge.currentState?.agents.first { $0.id == agentId } }

    var body: some View {
        List {
            if let agent {
                let state = AgentState(agent)
                Section {
                    HStack(spacing: 10) {
                        Blip(state: state, size: 12, kind: AgentKind(agent))
                        VStack(alignment: .leading, spacing: 2) {
                            Text(state.label).font(.headline)
                            if let since = PanelTime.since(agent.statusSince) {
                                Text("há \(since)").font(.footnote).foregroundStyle(Tower.soft)
                            }
                        }
                        Spacer()
                        StateTag(state: state)
                    }
                    if state == .waiting {
                        RequestStrip(agent: agent, bridge: bridge)
                            .listRowInsets(EdgeInsets(top: 8, leading: 8, bottom: 8, trailing: 8))
                    }
                }
                .listRowBackground(Tower.panel)
                if !agent.askedText.isEmpty || agent.lastReply?.isEmpty == false {
                    Section("Conversa") {
                        if !agent.askedText.isEmpty {
                            VStack(alignment: .leading, spacing: 4) {
                                TowerLabel("Você pediu")
                                Text(agent.askedText).font(.body).textSelection(.enabled)
                            }
                        }
                        if let reply = agent.lastReply, !reply.isEmpty {
                            VStack(alignment: .leading, spacing: 4) {
                                TowerLabel("\(AgentKind(agent).name) respondeu")
                                Text(reply).font(.body).textSelection(.enabled)
                            }
                        }
                    }
                    .listRowBackground(Tower.panel)
                }
                Section("Voo") {
                    row("Computador", agent.host)
                    LabeledContent("Agente") {
                        HStack(spacing: 6) {
                            Text(AgentKind(agent).name)
                            ProviderBadge(AgentKind(agent))
                        }
                    }
                    row("Pasta", agent.cwd)
                    row("Branch", agent.branch)
                    row("Modelo", agent.modelLabel)
                    if let ctx = agent.context {
                        row("Contexto", "\(Int(ctx.fraction * 100)) %")
                    }
                    if let cost = agent.costUsd {
                        row("Custo estimado", String(format: "US$ %.2f", cost))
                    }
                }
                .listRowBackground(Tower.panel)
                if agent.can("input") && state != .exited {
                    Section {
                        ReplyField(agent: agent, bridge: bridge, onAmber: false)
                    } header: {
                        Text("Responder ao agente")
                    } footer: {
                        Text("Entra já no terminal se o agente estiver parado; senão, quando terminar o passo atual.")
                    }
                    .listRowBackground(Tower.panel)
                }
                Section {
                    if agent.can("focus") {
                        Button {
                            Task {
                                let r = await bridge.focus(agentId: agent.id)
                                note = r.message
                                UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
                            }
                        } label: { Label("Focar no computador", systemImage: "macwindow.on.rectangle") }
                    }
                    if agent.isRunning && agent.can("interrupt") {
                        Button(role: .destructive) {
                            Task {
                                let ok = await bridge.interruptAgent(agentId: agent.id)
                                note = ok ? "Turno interrompido." : "Não foi possível interromper."
                            }
                        } label: { Label("Interromper turno", systemImage: "stop.circle") }
                    }
                    if agent.canTerminate {
                        Button(role: .destructive) {
                            Task {
                                let r = await bridge.terminate(agentId: agent.id)
                                note = r.message
                                UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
                            }
                        } label: { Label("Terminar sessão", systemImage: "xmark.octagon") }
                    }
                } footer: {
                    if let note { Text(note) }
                }
                .listRowBackground(Tower.panel)
            } else {
                Text("Este agente saiu do radar.")
                    .foregroundStyle(Tower.soft)
                    .listRowBackground(Tower.panel)
            }
        }
        .scrollContentBackground(.hidden)
        .background(Tower.ground)
        .navigationTitle(agent?.title ?? "Agente")
        .navigationBarTitleDisplayMode(.inline)
    }

    @ViewBuilder
    private func row(_ label: String, _ value: String) -> some View {
        if !value.isEmpty {
            LabeledContent(label) {
                Text(value).multilineTextAlignment(.trailing).textSelection(.enabled)
            }
        }
    }
}

// MARK: - Computadores

struct SetupSheet: View {
    @ObservedObject var bridge: AgentDeckBridgeManager
    @Environment(\.dismiss) private var dismiss
    @State private var scanning = false
    @State private var note: AgentDeckBridgeManager.ActionResult?
    @State private var pairing = false
    @State private var manualURL = ""
    @State private var manualToken = ""
    @State private var replies = QuickReplies.load()
    @State private var newReply = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    if bridge.servers.isEmpty {
                        Text("Nenhum computador pareado.")
                            .foregroundStyle(Tower.soft)
                    }
                    ForEach(bridge.servers) { server in
                        serverRow(server)
                    }
                    .onDelete { idx in
                        idx.map { bridge.servers[$0] }.forEach { bridge.remove($0) }
                    }
                } header: {
                    Text("Computadores")
                } footer: {
                    Text("Deslize para a esquerda para esquecer um computador.")
                }
                .listRowBackground(Tower.panel)

                Section {
                    Button {
                        scanning = true
                    } label: {
                        Label("Adicionar computador", systemImage: "qrcode.viewfinder")
                    }
                    .disabled(pairing)
                    Button {
                        if let text = UIPasteboard.general.string, let url = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)) {
                            pair(url)
                        } else {
                            note = .init(ok: false, message: "A área de transferência não tem um link do Towerman.")
                        }
                    } label: {
                        Label("Colar link de pareamento", systemImage: "doc.on.clipboard")
                    }
                    .disabled(pairing)
                    if pairing {
                        HStack { ProgressView(); Text("Pareando…").foregroundStyle(Tower.soft) }
                    }
                    if let note {
                        Text(note.message)
                            .font(.footnote)
                            .foregroundStyle(note.ok ? Tower.green : Tower.red)
                    }
                } footer: {
                    Text("No computador, rode «towerman pair» (ou abra o painel em /parear) e leia o QR. O código vale 2 minutos.")
                }
                .listRowBackground(Tower.panel)

                Section {
                    LabeledContent("Relógio") {
                        Text(bridge.watchReachable ? "Conectado" : "Em espera")
                    }
                }
                .listRowBackground(Tower.panel)

                Section {
                    ForEach(replies.indices, id: \.self) { i in
                        TextField("Frase", text: Binding(get: { replies[i] }, set: { replies[i] = $0 }))
                    }
                    .onDelete { replies.remove(atOffsets: $0) }
                    .onMove { replies.move(fromOffsets: $0, toOffset: $1) }
                    HStack {
                        TextField("Nova frase", text: $newReply)
                            .onSubmit(addReply)
                        Button("Juntar", action: addReply)
                            .disabled(newReply.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                } header: {
                    Text("Respostas rápidas")
                } footer: {
                    Text("Arraste uma sessão para a esquerda, no iPhone ou no relógio, para mandar uma destas frases ao agente.")
                }
                .listRowBackground(Tower.panel)
                .onChange(of: replies) { _, list in
                    QuickReplies.save(list)
                    bridge.syncQuickReplies()
                }

                Section {
                    DisclosureGroup("Ligação manual") {
                        TextField("http://192.168.1.10:7351", text: $manualURL)
                            .keyboardType(.URL)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                        SecureField("Token", text: $manualToken)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                        Button("Adicionar") {
                            bridge.addManual(url: manualURL, token: manualToken)
                            manualURL = ""
                            manualToken = ""
                        }
                        .disabled(manualURL.isEmpty)
                    }
                } footer: {
                    Text("Para gateways antigos, sem QR.")
                }
                .listRowBackground(Tower.panel)
            }
            .scrollContentBackground(.hidden)
            .background(Tower.ground)
            .navigationTitle("Computadores")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("OK") { dismiss() }
                }
            }
            .sheet(isPresented: $scanning) {
                QRScannerSheet { url in
                    scanning = false
                    pair(url)
                }
            }
        }
        .tint(Tower.blue)
        .preferredColorScheme(.dark)
        .presentationDetents([.large])
    }

    private func addReply() {
        let t = newReply.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        replies.append(t)
        newReply = ""
    }

    private func serverRow(_ server: PairedServer) -> some View {
        let st = bridge.serverStatus[server.id]
        return VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Blip(state: st?.connected == true ? .running : .exited, size: 9)
                Text(server.name)
                    .font(.headline)
                    .foregroundStyle(Tower.text)
                Spacer()
                if let n = st?.agents, st?.connected == true {
                    Text("\(n) \(n == 1 ? "agente" : "agentes")")
                        .font(.caption)
                        .foregroundStyle(Tower.soft)
                }
            }
            Text(st?.connected == true ? "Conectado por \(st?.address ?? "")" : (st?.problem ?? "Procurando…"))
                .font(.caption)
                .foregroundStyle(st?.connected == true ? Tower.soft : Tower.amber)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    private func pair(_ url: URL) {
        pairing = true
        note = nil
        Task {
            let r = await bridge.pair(with: url)
            pairing = false
            note = r
            UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .error)
        }
    }
}

// MARK: - Leitor de QR

/// Câmara com o leitor de códigos do sistema; devolve o primeiro link towerman:// que vir.
struct QRScannerSheet: View {
    let onFound: (URL) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if DataScannerViewController.isSupported && DataScannerViewController.isAvailable {
                    QRScanner(onFound: onFound)
                        .ignoresSafeArea()
                } else {
                    VStack(spacing: 12) {
                        Text("A câmera não está disponível.")
                            .font(.headline)
                        Text("Leia o QR com o app Câmera do iPhone, ou copie o link e use «Colar link de pareamento».")
                            .multilineTextAlignment(.center)
                            .foregroundStyle(Tower.soft)
                    }
                    .padding(24)
                }
            }
            .background(Tower.ground)
            .navigationTitle("Ler QR do computador")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancelar") { dismiss() }
                }
            }
        }
        .preferredColorScheme(.dark)
    }
}

struct QRScanner: UIViewControllerRepresentable {
    let onFound: (URL) -> Void

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let vc = DataScannerViewController(recognizedDataTypes: [.barcode(symbologies: [.qr])],
                                           qualityLevel: .balanced, isHighlightingEnabled: true)
        vc.delegate = context.coordinator
        try? vc.startScanning()
        return vc
    }

    func updateUIViewController(_ vc: DataScannerViewController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(onFound: onFound) }

    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        let onFound: (URL) -> Void
        private var done = false

        init(onFound: @escaping (URL) -> Void) { self.onFound = onFound }

        func dataScanner(_ scanner: DataScannerViewController, didAdd items: [RecognizedItem], allItems: [RecognizedItem]) {
            for item in items {
                if case let .barcode(code) = item, let text = code.payloadStringValue,
                   text.hasPrefix("towerman://"), let url = URL(string: text), !done {
                    done = true
                    scanner.stopScanning()
                    UINotificationFeedbackGenerator().notificationOccurred(.success)
                    onFound(url)
                }
            }
        }
    }
}
