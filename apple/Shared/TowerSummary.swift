import Foundation

/// O essencial da torre num relance: o que a Live Activity, o widget e a Siri mostram.
/// Pequeno de propósito; o estado completo continua no app.
public struct TowerSummary: Codable, Hashable {
    public var waiting: Int
    public var running: Int
    /// O pedido mais antigo em espera: é o primeiro a resolver.
    public var topId: String?
    public var topTitle: String?
    public var topDetail: String?
    public var topSince: Date?

    public static let empty = TowerSummary(waiting: 0, running: 0)

    public init(waiting: Int, running: Int, topId: String? = nil, topTitle: String? = nil,
                topDetail: String? = nil, topSince: Date? = nil) {
        self.waiting = waiting
        self.running = running
        self.topId = topId
        self.topTitle = topTitle
        self.topDetail = topDetail
        self.topSince = topSince
    }

    public init(agents: [Agent]) {
        let waiting = agents.filter { AgentState($0) == .waiting }
        let top = waiting.min { ($0.statusSince) < ($1.statusSince) }
        self.init(waiting: waiting.count,
                  running: agents.filter { AgentState($0) == .running }.count,
                  topId: top?.id,
                  topTitle: top?.title,
                  topDetail: top.map { a in
                      guard let w = a.waiting else { return a.activityLine }
                      return w.isQuestion ? w.detail : [w.title, w.detail].filter { !$0.isEmpty }.joined(separator: " ")
                  },
                  topSince: top.flatMap { PanelTime.date($0.statusSince) })
    }

    /// "2 em espera", "1 em curso", "Céu limpo".
    public var headline: String {
        if waiting > 0 { return "\(waiting) em espera" }
        if running > 0 { return "\(running) em curso" }
        return "Céu limpo"
    }

    // MARK: - Partilha entre o app do relógio, o widget e os intents

    public static let appGroup = TowermanIDs.appGroup
    private static let key = "tower_summary"

    public static func load() -> TowerSummary {
        guard let data = UserDefaults(suiteName: appGroup)?.data(forKey: key),
              let s = try? JSONDecoder().decode(TowerSummary.self, from: data) else { return .empty }
        return s
    }

    /// Grava e diz se mudou: quem chama só recarrega os widgets quando há novidade.
    @discardableResult
    public func save() -> Bool {
        guard self != Self.load(), let data = try? JSONEncoder().encode(self) else { return false }
        UserDefaults(suiteName: Self.appGroup)?.set(data, forKey: Self.key)
        return true
    }
}

/// towerman://agent/<id>: o widget e a Live Activity abrem o app no pedido certo.
public enum TowerLink {
    public static func agent(_ id: String) -> URL? {
        URL(string: "towerman://agent/\(id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id)")
    }

    public static func agentId(from url: URL) -> String? {
        guard url.scheme == "towerman", url.host == "agent" else { return nil }
        let id = url.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        return id.isEmpty ? nil : id.removingPercentEncoding ?? id
    }
}
