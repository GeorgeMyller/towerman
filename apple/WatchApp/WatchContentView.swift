import SwiftUI
import WatchKit

public struct WatchContentView: View {
    @StateObject private var viewModel = WatchViewModel()
    @State private var path: [String] = []
    @State private var refreshing = false
    @State private var refreshNote: String?
    /// Resultado de uma ação feita por gesto na lista (ex.: focar).
    @State private var actionNote: String?
    /// Sessão a quem mandar uma resposta rápida (arrastar para a esquerda).
    @State private var quickTarget: Agent?
    @State private var showLaunch = false

    public init() {}

    // A mesma regra do iPhone: todo o agente que aguarda você sobe às faixas de pedido.
    private var waiting: [Agent] { viewModel.agents.filter { AgentState($0) == .waiting } }
    private var others: [Agent] { viewModel.agents.filter { AgentState($0) != .waiting } }

    public var body: some View {
        NavigationStack(path: $path) {
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    if let note = actionNote ?? refreshNote {
                        Text(note)
                            .font(.footnote)
                            .foregroundStyle(Tower.red)
                    }
                    if viewModel.agents.isEmpty {
                        emptyState
                    } else {
                        // O radar é o primeiro ecrã do pulso.
                        RadarView(agents: viewModel.agents, showLabels: false) { path.append($0.id) }
                            .frame(height: 130)
                            .frame(maxWidth: .infinity)
                        HStack(spacing: 10) {
                            count(waiting.count, "espera", Tower.amber)
                            count(others.filter { AgentState($0) == .running }.count, "em curso", Tower.green)
                        }
                        if !waiting.isEmpty {
                            ForEach(waiting) { agent in
                                WatchRequestStrip(agent: agent, viewModel: viewModel,
                                                  onOpen: { path.append(agent.id) },
                                                  onQuick: { quickTarget = agent })
                            }
                        }
                        if !others.isEmpty {
                            TowerLabel("Partidas")
                                .padding(.top, 4)
                            ForEach(others) { agent in
                                NavigationLink(value: agent.id) { WatchDepartureRow(agent: agent) }
                                    .buttonStyle(.plain)
                                    .towerSwipe(
                                        leading: agent.can("focus")
                                            ? SwipeAction("Focar", icon: "macwindow", tint: Tower.blue, ink: .white) {
                                                focus(agent)
                                            }
                                            : nil,
                                        trailing: agent.can("input") && AgentState(agent) != .exited
                                            ? SwipeAction("Resposta", icon: "text.bubble", tint: Tower.blue, ink: .white) {
                                                quickTarget = agent
                                            }
                                            : nil,
                                        threshold: 55)
                            }
                        }
                    }
                }
                .scrollTargetLayout()
            }
            .background(Tower.ground)
            .navigationTitle("Towerman")
            .navigationDestination(for: String.self) { id in
                WatchAgentDetail(agentId: id, viewModel: viewModel)
            }
            .toolbar {
                if !viewModel.launchServers.isEmpty {
                    ToolbarItem(placement: .topBarLeading) {
                        Button { showLaunch = true } label: {
                            Image(systemName: "plus")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(Tower.ground)
                        }
                        .accessibilityLabel("Lançar sessão")
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button(action: refresh) {
                        if refreshing {
                            ProgressView()
                                .tint(Tower.ground)
                        } else {
                            // O botão da toolbar já é azul; a seta azul desaparecia nele.
                            Image(systemName: "arrow.clockwise")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(Tower.ground)
                        }
                    }
                    .disabled(refreshing)
                    .accessibilityLabel("Atualizar")
                }
            }
            .onAppear { viewModel.requestStateFromPhone() }
            .sheet(isPresented: $showLaunch) {
                WatchLaunchView(viewModel: viewModel) { id in path = [id] }
            }
            .confirmationDialog("Resposta rápida", isPresented: Binding(get: { quickTarget != nil },
                                                                     set: { if !$0 { quickTarget = nil } }),
                                titleVisibility: .visible, presenting: quickTarget) { agent in
                ForEach(QuickReplies.load(), id: \.self) { text in
                    Button(text) { sendQuick(text, to: agent) }
                }
                Button("Ditar…") { path.append(agent.id) }
                Button("Cancelar", role: .cancel) {}
            } message: { agent in
                Text(agent.replyTail(120) ?? agent.title)
            }
        }
        // Widget e Smart Stack: towerman://agent/<id> abre direto no pedido.
        .onOpenURL { url in
            if let id = TowerLink.agentId(from: url) { path = [id] }
        }
        // Âmbar fica reservado a "precisa de si"; os controlos usam o azul da torre.
        .tint(Tower.blue)
    }

    private func sendQuick(_ text: String, to agent: Agent) {
        viewModel.act("input", agentId: agent.id, text: text) { ok, msg in
            WKInterfaceDevice.current().play(ok ? .success : .failure)
            actionNote = msg
        }
    }

    private func focus(_ agent: Agent) {
        viewModel.act("focus", agentId: agent.id) { ok, msg in
            WKInterfaceDevice.current().play(ok ? .success : .failure)
            actionNote = ok ? nil : msg
        }
    }

    private func refresh() {
        refreshing = true
        refreshNote = nil
        viewModel.refresh { _, note in
            refreshing = false
            refreshNote = note
        }
    }

    private func count(_ value: Int, _ label: String, _ color: Color) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 3) {
            Text("\(value)")
                .font(.headline.monospacedDigit())
                .foregroundStyle(value == 0 ? Tower.soft : color)
            Text(label.uppercased())
                .font(.caption2.weight(.semibold))
                .foregroundStyle(Tower.soft)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(value) \(label)")
    }

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(viewModel.offlineNote != nil ? "Sem contato" : viewModel.isConnectedToPhone ? "Céu limpo" : "Sem iPhone")
                .font(.headline)
                .foregroundStyle(viewModel.offlineNote != nil ? Tower.red : Tower.text)
            Text(viewModel.offlineNote
                 ?? (viewModel.isConnectedToPhone
                     ? "Nenhum agente ativo."
                     : "Abra o Towerman no iPhone para conectar o relógio."))
                .font(.footnote)
                .foregroundStyle(Tower.soft)
            // Sem cartões, o toque duplo fica livre: atualiza, como o botão.
            Button("Atualizar", action: refresh)
                .disabled(refreshing)
                .handGestureShortcut(.primaryAction, isEnabled: !refreshing)
                .padding(.top, 4)
        }
        .padding(.top, 4)
    }
}

// MARK: - Pedido no pulso

struct WatchRequestStrip: View {
    let agent: Agent
    @ObservedObject var viewModel: WatchViewModel
    /// Toque duplo (indicador e polegar) aprova; só no detalhe. No painel, sem ação principal, o
    /// toque duplo percorre os cartões como na Smart Stack, e um toque no cartão abre o agente.
    var doubleTap = false
    /// Abre o detalhe (responder por ditado); nil quando a faixa já está no detalhe.
    var onOpen: (() -> Void)? = nil
    /// Abre as respostas rápidas (arrastar para a esquerda).
    var onQuick: (() -> Void)? = nil
    @State private var sending: String?
    @State private var failed = false
    @State private var note: String?

    var body: some View {
        let prompt = agent.waiting
        VStack(alignment: .leading, spacing: 6) {
            // O texto do pedido abre o agente; os botões por baixo continuam a responder.
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 5) {
                    Blip(state: .waiting, size: 8, tint: Tower.onAmber, kind: AgentKind(agent))
                    Text(agent.title)
                        .font(.headline)
                        .lineLimit(1)
                }
                Text([agent.originLine.uppercased(), PanelTime.since(agent.statusSince)].compactMap { $0 }
                    .filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption2.weight(.bold))
                    .opacity(0.8)
                    .lineLimit(1)
                if let prompt {
                    Text(prompt.detail)
                        .font(prompt.isQuestion ? .footnote : .system(.footnote, design: .monospaced))
                        .lineLimit(5)
                } else if let reply = agent.lastReply, !reply.isEmpty {
                    // Espera sem pedido: o fim da última resposta costuma ser a pergunta.
                    Text(reply)
                        .font(.footnote)
                        .lineLimit(6)
                } else if !agent.askedText.isEmpty {
                    Text(agent.askedText)
                        .font(.footnote)
                        .lineLimit(4)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
            .onTapGesture { onOpen?() }

            if let prompt, agent.can("answer") && !prompt.options.isEmpty {
                let primary = prompt.options.first(where: \.primary) ?? prompt.options[0]
                answer(primary, prompt: prompt, filled: true)
                    .handGestureShortcut(.primaryAction, isEnabled: doubleTap && sending == nil)
                ForEach(prompt.options.filter { $0.key != primary.key }) {
                    answer($0, prompt: prompt, filled: false)
                }
                .padding(.top, 14) // afastado do primário, para não ser tocado por engano
                if failed {
                    Text("Não foi possível responder. Veja no iPhone.")
                        .font(.caption2.weight(.medium))
                }
                if prompt.isDecision {
                    decisionExtras(prompt)
                }
            } else {
                // Sem botões de resposta: responder por ditado no detalhe, ou ir ao computador.
                if let onOpen, agent.can("input") {
                    plain("Responder", icon: "mic", filled: true, action: onOpen)
                        .handGestureShortcut(.primaryAction, isEnabled: doubleTap)
                }
                if let prompt, prompt.isDecision {
                    plain("Não é pergunta", icon: "xmark", filled: false) { send(.dismiss, prompt: prompt) }
                }
                // No detalhe (onOpen nil), o foco já tem botão próprio por baixo da faixa.
                if agent.can("focus"), onOpen != nil {
                    plain("Focar no computador", icon: "macwindow", filled: !agent.can("input"), action: focus)
                }
                if let note {
                    Text(note)
                        .font(.caption2.weight(.medium))
                }
            }
        }
        .foregroundStyle(Tower.onAmber)
        .padding(10)
        .background(Tower.amber, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .towerSwipe(leading: swipeRight, trailing: swipeLeft, threshold: 55)
    }

    /// Decisão em texto livre: além das opções, responder outra coisa por ditado, descartar um
    /// falso alarme e, num terminal que não aceita texto, o motivo e o foco no computador.
    @ViewBuilder
    private func decisionExtras(_ prompt: Prompt) -> some View {
        if !prompt.context.isEmpty {
            Text(prompt.context)
                .font(.caption2.weight(.medium))
        }
        if let onOpen, agent.can("input") {
            plain("Responder", icon: "mic", filled: false, action: onOpen)
        }
        if prompt.options.contains(where: \.disabled), agent.can("focus"), onOpen != nil {
            plain("Focar no computador", icon: "macwindow", filled: false, action: focus)
        }
        plain("Não é pergunta", icon: "xmark", filled: false) { send(.dismiss, prompt: prompt) }
        if let note {
            Text(note)
                .font(.caption2.weight(.medium))
        }
    }

    private func focus() {
        note = nil
        viewModel.act("focus", agentId: agent.id) { ok, msg in
            WKInterfaceDevice.current().play(ok ? .success : .failure)
            note = msg
        }
    }

    // MARK: Gestos (como no iPhone)

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
        return SwipeAction("Focar", icon: "macwindow", tint: Tower.blue, ink: .white, run: focus)
    }

    private var swipeLeft: SwipeAction? {
        if let pair = permissionPair, let p = agent.waiting {
            return SwipeAction(pair.deny.label, icon: "xmark", tint: Tower.red, ink: .white) { send(pair.deny, prompt: p) }
        }
        guard let onQuick, agent.can("input") else { return nil }
        return SwipeAction("Resposta", icon: "text.bubble", tint: Tower.blue, ink: .white, run: onQuick)
    }

    private func send(_ option: PromptOption, prompt: Prompt) {
        sending = option.key
        failed = false
        viewModel.sendAnswer(agentId: agent.id, promptId: prompt.id, key: option.key) { ok in
            sending = nil
            failed = !ok
        }
    }

    private func plain(_ label: String, icon: String, filled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Label(label, systemImage: icon)
                .font(.footnote.weight(.semibold))
                .lineLimit(1)
                .frame(maxWidth: .infinity, minHeight: 36)
                .foregroundStyle(filled ? Tower.amber : Tower.onAmber)
                .background {
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .fill(filled ? Tower.onAmber : Color.clear)
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .strokeBorder(Tower.onAmber, lineWidth: filled ? 0 : 2)
                }
        }
        .buttonStyle(.plain)
    }

    private func answer(_ option: PromptOption, prompt: Prompt, filled: Bool) -> some View {
        Button {
            send(option, prompt: prompt)
        } label: {
            HStack(spacing: 4) {
                if sending == option.key { ProgressView().tint(filled ? Tower.amber : Tower.onAmber) }
                Text(option.label)
            }
                .font(.headline)
                .lineLimit(2)
                .frame(maxWidth: .infinity, minHeight: 40)
                .foregroundStyle(filled ? Tower.amber : Tower.onAmber)
                .background {
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .fill(filled ? Tower.onAmber : Color.clear)
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .strokeBorder(Tower.onAmber, lineWidth: filled ? 0 : 2)
                }
        }
        .buttonStyle(.plain)
        .disabled(sending != nil || option.disabled)
        .opacity(option.disabled ? 0.45 : 1)
        .accessibilityLabel("\(option.label): \(prompt.detail)")
    }
}

// MARK: - Detalhe no pulso

struct WatchAgentDetail: View {
    let agentId: String
    @ObservedObject var viewModel: WatchViewModel
    @State private var reply = ""
    @State private var note: String?
    @State private var busy = false

    private var agent: Agent? { viewModel.agents.first { $0.id == agentId } }

    /// O toque duplo é da faixa quando ela tem botões de opção. Num pedido sem botões, fica com
    /// «Responder» (ou com o foco, se o agente não aceita texto), em vez de rolar a página.
    private func ownsDoubleTap(_ agent: Agent) -> Bool {
        guard AgentState(agent) == .waiting else { return false }
        guard let prompt = agent.waiting, agent.can("answer") else { return true }
        return prompt.options.isEmpty
    }

    var body: some View {
        ScrollView {
            if let agent {
                let state = AgentState(agent)
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 6) {
                        Blip(state: state, size: 9, kind: AgentKind(agent))
                        Text(state.label).font(.footnote.weight(.semibold))
                    }
                    Text(agent.originLine.uppercased())
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(Tower.soft)
                    if state == .waiting {
                        WatchRequestStrip(agent: agent, viewModel: viewModel, doubleTap: true)
                    } else {
                        Text(agent.activityLine)
                            .font(.footnote)
                            .foregroundStyle(Tower.text)
                            .lineLimit(4)
                    }
                    if !agent.askedText.isEmpty {
                        TowerLabel("Você pediu").padding(.top, 2)
                        Text(agent.askedText)
                            .font(.footnote)
                            .foregroundStyle(Tower.text)
                            .lineLimit(5)
                    }
                    if let reply = agent.lastReply, !reply.isEmpty, !(state == .waiting && agent.waiting == nil) {
                        TowerLabel("\(AgentKind(agent).badge) respondeu").padding(.top, 2)
                        Text(reply)
                            .font(.footnote)
                            .foregroundStyle(Tower.text)
                            .lineLimit(10)
                    }
                    if agent.can("input") && state != .exited {
                        // Abre o ditado, o rabisco ou o teclado; a resposta segue ao terminar.
                        TextFieldLink(busy ? "A enviar…" : "Responder", prompt: Text("Resposta")) { text in
                            reply = text
                            send()
                        }
                        .disabled(busy)
                        .handGestureShortcut(.primaryAction, isEnabled: ownsDoubleTap(agent) && !busy)
                    }
                    if agent.can("focus") {
                        Button {
                            busy = true
                            viewModel.act("focus", agentId: agent.id) { _, msg in
                                busy = false
                                note = msg
                            }
                        } label: {
                            Label("Focar no computador", systemImage: "macwindow")
                        }
                        .disabled(busy)
                        .handGestureShortcut(.primaryAction, isEnabled: ownsDoubleTap(agent) && !agent.can("input") && !busy)
                    }
                    if agent.canTerminate {
                        Button(role: .destructive) {
                            busy = true
                            viewModel.act("terminate", agentId: agent.id) { _, msg in
                                busy = false
                                note = msg
                            }
                        } label: {
                            Label("Terminar sessão", systemImage: "xmark.octagon")
                        }
                        .disabled(busy)
                    }
                    if let note {
                        Text(note)
                            .font(.caption2)
                            .foregroundStyle(Tower.soft)
                    }
                }
            } else {
                Text("Este agente saiu do radar.")
                    .font(.footnote)
                    .foregroundStyle(Tower.soft)
            }
        }
        .background(Tower.ground)
        .navigationTitle(agent?.title ?? "Agente")
    }

    private func send() {
        let text = reply.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !busy else { return }
        busy = true
        viewModel.act("input", agentId: agentId, text: text) { ok, msg in
            busy = false
            note = msg
            if ok { reply = "" }
        }
    }
}

// MARK: - Linha de partida

struct WatchDepartureRow: View {
    let agent: Agent

    var body: some View {
        let state = AgentState(agent)
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Blip(state: state, size: 8, kind: AgentKind(agent))
                Text(agent.title)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Tower.text)
                    .lineLimit(1)
                Spacer(minLength: 4)
                StateTag(state: state)
            }
            Text("\(AgentKind(agent).badge.uppercased()) · \(agent.activityLine)")
                .font(.caption2)
                .foregroundStyle(Tower.soft)
                .lineLimit(1)
            if let tail = agent.replyTail(56) {
                Text(tail)
                    .font(.caption2)
                    .foregroundStyle(Tower.text.opacity(0.85))
                    .lineLimit(2)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Tower.panel)
            RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Tower.line, lineWidth: 1)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(agent.title), \(AgentKind(agent).name), \(state.label). \(agent.activityLine)")
    }
}
