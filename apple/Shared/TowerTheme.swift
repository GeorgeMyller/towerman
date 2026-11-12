import SwiftUI

// MARK: - Torre de controlo
// O mesmo mundo da vista "Torre" do painel web do Towerman: radar, quadro de partidas e
// contadores LED, adaptado ao telemóvel e ao pulso. Azul-marinho sem brilhos, SF Pro na
// interface e letra mono só no comando a aprovar. Sempre escuro: a torre à noite.

public enum Tower {
    public static let ground = Color(hex: 0x0B1626)
    public static let panel = Color(hex: 0x12223A)
    public static let panelRaised = Color(hex: 0x1A2E4A)
    public static let line = Color(hex: 0x26405F)
    public static let text = Color(hex: 0xE3EBF5)
    public static let soft = Color(hex: 0x8EA3C0)

    /// Precisa de si. Um pedido acende a faixa inteira desta cor.
    public static let amber = Color(hex: 0xFFB547)
    /// Tinta sobre âmbar (contraste ≈ 11:1).
    public static let onAmber = Color(hex: 0x1A1205)
    public static let green = Color(hex: 0x5BBF8A)
    public static let red = Color(hex: 0xF0645A)
    public static let blue = Color(hex: 0x5AA9FF)
    public static let parked = Color(hex: 0x6F7F99)
}

extension Color {
    init(hex: UInt32) {
        self.init(red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255,
                  blue: Double(hex & 0xFF) / 255)
    }
}

// MARK: - Estado de cada agente

public enum AgentState: Equatable {
    case waiting, running, fault, parked, unseen, exited

    public init(_ agent: Agent) {
        switch agent.status {
        case "waiting": self = .waiting
        case "running", "starting": self = .running
        case "error": self = .fault
        case "exited": self = .exited
        default: self = agent.unseen ? .unseen : .parked
        }
    }

    public var color: Color {
        switch self {
        case .waiting: return Tower.amber
        case .running: return Tower.green
        case .fault: return Tower.red
        case .parked, .unseen, .exited: return Tower.parked
        }
    }

    /// Ponto oco: livre (parado, terminado ou por ver). O estado nunca vive só na cor.
    public var hollow: Bool {
        switch self {
        case .parked, .exited, .unseen: return true
        default: return false
        }
    }

    /// Etiqueta do quadro de partidas.
    public var tag: String {
        switch self {
        case .waiting: return "Espera"
        case .running: return "Em curso"
        case .fault: return "Erro"
        case .parked: return "Parado"
        case .unseen: return "Por ver"
        case .exited: return "Saiu"
        }
    }

    public var label: String {
        switch self {
        case .waiting: return "Aguardando você"
        case .running: return "Em curso"
        case .fault: return "Erro"
        case .parked: return "Parado"
        case .unseen: return "Concluído · por ver"
        case .exited: return "Encerrado"
        }
    }

    /// Anel do radar: o que precisa de si fica mais perto do centro.
    var ring: Double {
        switch self {
        case .waiting: return 0.34
        case .running, .fault: return 0.64
        case .parked, .unseen, .exited: return 0.84
        }
    }
}

// MARK: - Peças

/// Rótulo em versaletes, como os cabeçalhos da Torre.
public struct TowerLabel: View {
    let text: String
    var color: Color = Tower.soft

    public init(_ text: String, color: Color = Tower.soft) {
        self.text = text
        self.color = color
    }

    public var body: some View {
        Text(text.uppercased())
            .font(.caption2.weight(.semibold))
            .tracking(1.6)
            .foregroundStyle(color)
    }
}

// MARK: - Origem do agente

/// Que agente corre a sessão. A forma do ponto diz o agente (círculo, quadrado, losango); a cor
/// continua a dizer o estado e o cheio/oco, ocupado/livre.
public enum AgentKind: Equatable {
    case claude, codex, agy, antigravityIDE, other

    public init(_ agent: Agent) {
        switch (agent.provider.isEmpty ? agent.tool : agent.provider).lowercased() {
        case "claude": self = .claude
        case "codex": self = .codex
        case "agy": self = .agy
        case "antigravity": self = .antigravityIDE
        default: self = .other
        }
    }

    /// Nome curto do selo.
    public var badge: String {
        switch self {
        case .claude: return "Claude"
        case .codex: return "Codex"
        case .agy: return "agy"
        case .antigravityIDE: return "AG·IDE"
        case .other: return "Agente"
        }
    }

    /// Nome por extenso, para o VoiceOver e o detalhe.
    public var name: String {
        switch self {
        case .claude: return "Claude Code"
        case .codex: return "Codex"
        case .agy: return "Antigravity CLI"
        case .antigravityIDE: return "Antigravity IDE"
        case .other: return "Agente"
        }
    }
}

/// A forma de cada agente: círculo (Claude), quadrado (Codex), losango (Antigravity).
public struct AgentShape: Shape {
    let kind: AgentKind

    public func path(in r: CGRect) -> Path {
        switch kind {
        case .codex:
            let inset = r.insetBy(dx: r.width * 0.06, dy: r.height * 0.06)
            return Path(roundedRect: inset, cornerRadius: r.width * 0.18)
        case .agy, .antigravityIDE:
            var p = Path()
            p.move(to: CGPoint(x: r.midX, y: r.minY))
            p.addLine(to: CGPoint(x: r.maxX, y: r.midY))
            p.addLine(to: CGPoint(x: r.midX, y: r.maxY))
            p.addLine(to: CGPoint(x: r.minX, y: r.midY))
            p.closeSubpath()
            return p
        case .claude, .other:
            return Path(ellipseIn: r)
        }
    }
}

extension AgentShape: InsettableShape {
    public func inset(by amount: CGFloat) -> InsetAgentShape { InsetAgentShape(kind: kind, amount: amount) }
}

/// AgentShape com margem, para strokeBorder não sair da caixa.
public struct InsetAgentShape: InsettableShape {
    let kind: AgentKind
    var amount: CGFloat

    public func path(in r: CGRect) -> Path { AgentShape(kind: kind).path(in: r.insetBy(dx: amount, dy: amount)) }
    public func inset(by more: CGFloat) -> InsetAgentShape { InsetAgentShape(kind: kind, amount: amount + more) }
}

/// Ponto de estado: a forma diz o agente, a cor o estado, cheio = ocupado e oco = livre.
public struct Blip: View {
    let state: AgentState
    var size: CGFloat = 9
    var tint: Color?
    var kind: AgentKind = .claude

    public var body: some View {
        let c = tint ?? state.color
        // O losango e o quadrado parecem menores que o círculo com o mesmo lado: compensar.
        let lado = kind == .claude || kind == .other ? size : size * 1.12
        AgentShape(kind: kind)
            .strokeBorder(c, lineWidth: state.hollow ? 1.8 : 0)
            .background(AgentShape(kind: kind).fill(state.hollow ? Color.clear : c))
            .frame(width: lado, height: lado)
            .accessibilityHidden(true)
    }
}

/// Selo da origem: a forma do agente e o nome curto, ao lado do nome da sessão.
public struct ProviderBadge: View {
    let kind: AgentKind
    var onAmber = false

    public init(_ kind: AgentKind, onAmber: Bool = false) {
        self.kind = kind
        self.onAmber = onAmber
    }

    public var body: some View {
        let ink = onAmber ? Tower.onAmber : Tower.soft
        HStack(spacing: 4) {
            AgentShape(kind: kind).fill(ink).frame(width: 6, height: 6)
            Text(kind.badge.uppercased())
                .font(.caption2.weight(.bold))
                .tracking(0.8)
                .lineLimit(1)
        }
        .foregroundStyle(ink)
        .padding(.horizontal, 5)
        .padding(.vertical, 2)
        .overlay(Capsule().strokeBorder(ink.opacity(0.6), lineWidth: 1))
        .fixedSize()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(kind.name)
    }
}

/// Etiqueta de estado do quadro de partidas.
public struct StateTag: View {
    let state: AgentState

    public var body: some View {
        Text(state.tag.uppercased())
            .font(.caption2.weight(.bold))
            .tracking(1)
            .foregroundStyle(state == .waiting ? Tower.onAmber : state.color)
            .padding(.horizontal, 6)
            .padding(.vertical, 3)
            .background {
                RoundedRectangle(cornerRadius: 4, style: .continuous)
                    .fill(state == .waiting ? Tower.amber : Tower.ground)
                RoundedRectangle(cornerRadius: 4, style: .continuous)
                    .strokeBorder(state == .waiting ? Color.clear : Tower.line, lineWidth: 1)
            }
            .fixedSize()
    }
}

/// Contador LED da Torre: número grande, rótulo pequeno.
public struct LEDCounter: View {
    let value: Int
    let label: String
    let color: Color
    var alert = false
    /// Ocupa a largura da célula (grelha de duas colunas com letra grande).
    var fill = false

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text("\(value)")
                .font(.title3.weight(.bold).monospacedDigit())
                .foregroundStyle(value == 0 ? Tower.soft : color)
            Text(label.uppercased())
                .font(.caption2.weight(.semibold))
                .tracking(1)
                .foregroundStyle(Tower.soft)
                .lineLimit(1)
        }
        .frame(maxWidth: fill ? .infinity : nil, alignment: .leading)
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background {
            RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Tower.panel)
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(alert && value > 0 ? Tower.amber : Tower.line, lineWidth: 1)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(value) \(label)")
    }
}

// MARK: - Radar

/// O radar da torre: um sector por computador; o anel interior (tracejado âmbar) é quem aguarda
/// você, o do meio quem está em curso, o exterior quem está parado. Um varrimento lento roda;
/// os pontos âmbar pulsam. Com "Reduzir movimento", nada se mexe.
/// Para poupar bateria, também pára sem ninguém em curso ou à espera, em modo de pouca energia,
/// com a app fora do ecrã e, no relógio, com o ecrã sempre ligado.
public struct RadarView: View {
    let agents: [Agent]
    var showLabels = true
    var onSelect: ((Agent) -> Void)?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.scenePhase) private var scenePhase
    #if os(watchOS)
    @Environment(\.isLuminanceReduced) private var luminanceReduced
    #endif
    @State private var lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled

    /// O varrimento e os pulsos só correm quando há algo a acontecer e o ecrã o mostra.
    private var moving: Bool {
        guard !reduceMotion, !lowPower else { return false }
        #if os(macOS)
        // No Mac, uma janela visível mas sem foco continua a mostrar o radar.
        guard scenePhase != .background else { return false }
        #else
        guard scenePhase == .active else { return false }
        #endif
        #if os(watchOS)
        guard !luminanceReduced else { return false }
        #endif
        return agents.contains {
            let state = AgentState($0)
            return state == .running || state == .waiting
        }
    }

    /// Com letra grande, só os pontos à espera mantêm o nome: o radar continua a dizer quem precisa de si.
    private func labelsOn(_ state: AgentState) -> Bool {
        showLabels && (!typeSize.isAccessibilitySize || state == .waiting)
    }

    public init(agents: [Agent], showLabels: Bool = true, onSelect: ((Agent) -> Void)? = nil) {
        self.agents = agents
        self.showLabels = showLabels
        self.onSelect = onSelect
    }

    private var hosts: [String] {
        let h = Array(Set(agents.map { $0.host.isEmpty ? "—" : $0.host })).sorted()
        return h.isEmpty ? ["—"] : h
    }

    /// Posição de cada ponto: ângulo pelo sector do computador, raio pelo estado.
    private func positions(radius r: Double) -> [(Agent, CGPoint)] {
        let span = 2 * Double.pi / Double(hosts.count)
        var out: [(Agent, CGPoint)] = []
        for (i, host) in hosts.enumerated() {
            let group = agents.filter { ($0.host.isEmpty ? "—" : $0.host) == host }
            for (j, agent) in group.enumerated() {
                let a = -Double.pi / 2 + span * Double(i) + span * (Double(j) + 0.5) / Double(max(group.count, 1))
                let rr = r * AgentState(agent).ring
                out.append((agent, CGPoint(x: cos(a) * rr, y: sin(a) * rr)))
            }
        }
        return out
    }

    public var body: some View {
        GeometryReader { geo in
            let side = min(geo.size.width, geo.size.height)
            let r = side / 2 - 6
            let center = CGPoint(x: geo.size.width / 2, y: geo.size.height / 2)
            let blips = positions(radius: r).map { ($0.0, CGPoint(x: center.x + $0.1.x, y: center.y + $0.1.y), $0.1.x < -1) }
            ZStack {
                scope(center: center, r: r)
                // Só o varrimento e os pulsos mudam a cada imagem: ficam num Canvas próprio, a 20
                // imagens por segundo, e o resto do radar não volta a ser avaliado. Parado, sai.
                if moving {
                    TimelineView(.animation(minimumInterval: 1 / 20)) { ctx in
                        motion(center: center, r: r, waiting: blips.filter { AgentState($0.0) == .waiting },
                               t: ctx.date.timeIntervalSinceReferenceDate)
                    }
                }
                ForEach(blips, id: \.0.id) { agent, p, leftSide in
                    blip(agent, at: p, leftSide: leftSide)
                }
            }
        }
        .task {
            for await _ in NotificationCenter.default.notifications(named: .NSProcessInfoPowerStateDidChange) {
                lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Radar: \(agents.filter { AgentState($0) == .waiting }.count) aguardando você, \(agents.count) agentes")
    }

    private func scope(center: CGPoint, r: Double) -> some View {
        Canvas { ctx, _ in
            let ring = { (k: Double) in
                Path(ellipseIn: CGRect(x: center.x - r * k, y: center.y - r * k, width: 2 * r * k, height: 2 * r * k))
            }
            ctx.stroke(ring(1), with: .color(Tower.line), lineWidth: 1)
            ctx.stroke(ring(0.77), with: .color(Tower.line.opacity(0.7)), lineWidth: 1)
            ctx.stroke(ring(0.49), with: .color(Tower.amber.opacity(0.45)), style: StrokeStyle(lineWidth: 1, dash: [3, 5]))
            // Linhas de sector: uma fronteira por computador.
            if hosts.count > 1 {
                let span = 2 * Double.pi / Double(hosts.count)
                for i in 0..<hosts.count {
                    let a = -Double.pi / 2 + span * Double(i)
                    var p = Path()
                    p.move(to: center)
                    p.addLine(to: CGPoint(x: center.x + cos(a) * r, y: center.y + sin(a) * r))
                    ctx.stroke(p, with: .color(Tower.line), lineWidth: 1)
                }
            }
            // Nome de cada computador junto ao bordo do seu sector.
            // Com um só computador o cabeçalho já o diz; o rótulo só ocuparia o bordo.
            if showLabels && hosts.count > 1 {
                let span = 2 * Double.pi / Double(hosts.count)
                for (i, host) in hosts.enumerated() where host != "—" {
                    let a = -Double.pi / 2 + span * (Double(i) + 0.5)
                    let pt = CGPoint(x: center.x + cos(a) * (r * 0.98), y: center.y + sin(a) * (r * 0.98))
                    ctx.draw(Text(host.uppercased()).font(.caption2.weight(.semibold)).foregroundColor(Tower.soft), at: pt)
                }
            }
        }
        .accessibilityHidden(true)
    }

    private func motion(center: CGPoint, r: Double, waiting: [(Agent, CGPoint, Bool)], t: TimeInterval) -> some View {
        Canvas { ctx, _ in
            // Varrimento: uma volta a cada 7 s, como na Torre.
            let a = -Double.pi / 2 + (t.truncatingRemainder(dividingBy: 7) / 7) * 2 * Double.pi
            var wedge = Path()
            wedge.move(to: center)
            wedge.addArc(center: center, radius: r, startAngle: .radians(a - 0.5), endAngle: .radians(a), clockwise: false)
            wedge.closeSubpath()
            ctx.fill(wedge, with: .linearGradient(Gradient(colors: [Tower.blue.opacity(0), Tower.blue.opacity(0.16)]),
                                                  startPoint: CGPoint(x: center.x + cos(a - 0.5) * r, y: center.y + sin(a - 0.5) * r),
                                                  endPoint: CGPoint(x: center.x + cos(a) * r, y: center.y + sin(a) * r)))
            // Pulso de quem aguarda: o contorno cresce e apaga-se a cada 1,6 s.
            let phase = t.truncatingRemainder(dividingBy: 1.6) / 1.6
            let k = 1 + phase * 1.6
            for (agent, p, _) in waiting {
                let side = 12 * k
                let shape = AgentShape(kind: AgentKind(agent)).path(in: CGRect(x: p.x - side / 2, y: p.y - side / 2, width: side, height: side))
                ctx.stroke(shape, with: .color(Tower.amber.opacity(0.8 * (1 - phase))), lineWidth: 2 * k)
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private func blip(_ agent: Agent, at p: CGPoint, leftSide: Bool) -> some View {
        let state = AgentState(agent)
        return Button {
            onSelect?(agent)
        } label: {
            ZStack {
                if state == .waiting && !moving {
                    // Parado, o anel fica quieto e meio apagado; a mexer, o pulso vem do Canvas animado.
                    AgentShape(kind: AgentKind(agent))
                        .stroke(Tower.amber, lineWidth: 2)
                        .frame(width: 12, height: 12)
                        .opacity(0.6)
                }
                Blip(state: state, size: state == .waiting ? 12 : 9, kind: AgentKind(agent))
            }
            .frame(width: 44, height: 44)
            .contentShape(Rectangle())
            // O nome fica do lado de fora do ponto, para nunca cobrir o vizinho.
            .overlay(alignment: leftSide ? .trailing : .leading) {
                if labelsOn(state) {
                    Text(agent.title.count > 14 ? String(agent.title.prefix(13)) + "…" : agent.title)
                        .font(.caption2.weight(state == .waiting ? .bold : .regular))
                        .foregroundStyle(state == .waiting ? Tower.amber : Tower.text.opacity(0.85))
                        .lineLimit(1)
                        .fixedSize()
                        // Fundo do painel: os anéis nunca cruzam o nome.
                        .padding(.horizontal, 4)
                        .padding(.vertical, 1)
                        .background(Tower.panel, in: Capsule())
                        .offset(x: leftSide ? -26 : 26)
                }
            }
        }
        .buttonStyle(.plain)
        .position(p)
        .accessibilityLabel("\(agent.title), \(AgentKind(agent).name), \(agent.host), \(state.label)")
    }
}

// MARK: - Tempo

public enum PanelTime {
    private static let comFracao: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
    private static let semFracao = ISO8601DateFormatter()

    public static func date(_ text: String) -> Date? {
        comFracao.date(from: text) ?? semFracao.date(from: text)
    }

    /// "agora", "3 min", "1 h 12".
    public static func since(_ text: String, now: Date = Date()) -> String? {
        guard let d = date(text) else { return nil }
        let min = max(0, Int(now.timeIntervalSince(d) / 60))
        if min < 1 { return "agora" }
        if min < 60 { return "\(min) min" }
        return "\(min / 60) h \(String(format: "%02d", min % 60))"
    }
}

extension Agent {
    /// "MAC · CLAUDE", sem partes vazias.
    public var originLine: String {
        [host, provider.isEmpty ? tool : provider].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// O que o quadro mostra ao lado do nome.
    public var activityLine: String {
        if let a = activity, !a.tool.isEmpty {
            return a.detail.isEmpty ? a.tool : "\(a.tool) · \(a.detail)"
        }
        if let w = waiting {
            return w.isQuestion ? "Pergunta: \(w.detail)" : (w.title.isEmpty ? w.detail : "\(w.title) \(w.detail)")
        }
        if !aiTitle.isEmpty { return aiTitle }
        return AgentState(self).label
    }
}
