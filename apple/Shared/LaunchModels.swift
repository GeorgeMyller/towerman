import Foundation

// MARK: - Lançar sessões à distância
// Catálogo de GET /v1/launch/catalog: o que um computador pode lançar (agentes instalados,
// repos conhecidos e papéis de cada um). Partilhado entre o iPhone e o relógio.

public struct LaunchAgent: Codable, Identifiable, Hashable {
    public let id: String
    public let name: String
    public let modes: [String]
    /// Limitação sem terminal (ex.: o agy só corre os comandos de permissions.allow).
    public let note: String?
}

public struct LaunchRole: Codable, Identifiable, Hashable {
    public var id: String { name }
    public let name: String
    public let description: String
}

public struct LaunchRepo: Codable, Identifiable, Hashable {
    public var id: String { root }
    public let name: String
    public let root: String
    public let base: String
    public let roles: [LaunchRole]
}

public struct LaunchCatalog: Codable, Equatable {
    public struct Headless: Codable, Equatable {
        public let open: Int
        public let max: Int
    }
    public let agents: [LaunchAgent]
    public let repos: [LaunchRepo]
    public let headless: Headless
}

/// Um combo já usado (computador + agente + repo + papel): os atalhos do relógio.
public struct LaunchShortcut: Codable, Identifiable, Hashable {
    public var id: String { [serverId, agent, root, role].joined(separator: "|") }
    public let serverId: String
    public let serverName: String
    public let agent: String
    public let agentName: String
    public let root: String
    public let repoName: String
    public let role: String

    public init(serverId: String, serverName: String, agent: String, agentName: String, root: String, repoName: String, role: String) {
        self.serverId = serverId
        self.serverName = serverName
        self.agent = agent
        self.agentName = agentName
        self.root = root
        self.repoName = repoName
        self.role = role
    }

    public var label: String { role.isEmpty ? repoName : "\(repoName) · \(role)" }
}

/// Um computador que aceita lançamentos (o servidor anuncia `launch` em capabilities).
public struct LaunchServer: Codable, Identifiable, Hashable {
    public let id: String
    public let name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

public enum LaunchBranch {
    /// O mesmo slug do servidor (internal/servico/headless.go): a branch que o computador vai criar.
    public static func slug(_ text: String, _ max: Int) -> String {
        let folded = text.lowercased().folding(options: .diacriticInsensitive, locale: Locale(identifier: "pt"))
        var out = ""
        var dash = false
        for ch in folded {
            if ch.isASCII && (ch.isLetter || ch.isNumber) {
                out.append(ch)
                dash = false
            } else if !dash && !out.isEmpty {
                out.append("-")
                dash = true
            }
        }
        while out.hasSuffix("-") { out.removeLast() }
        if out.count > max {
            out = String(out.prefix(max))
            while out.hasSuffix("-") { out.removeLast() }
        }
        return out
    }
}

public enum LaunchRecents {
    public static let key = "towerman.launch.recent"
    public static let limit = 5

    public static func load() -> [LaunchShortcut] {
        guard let data = UserDefaults.standard.data(forKey: key),
              let list = try? JSONDecoder().decode([LaunchShortcut].self, from: data) else { return [] }
        return list
    }

    /// Põe o combo à frente, sem duplicados, e guarda os últimos `limit`.
    @discardableResult
    public static func remember(_ s: LaunchShortcut) -> [LaunchShortcut] {
        var list = load().filter { $0.id != s.id }
        list.insert(s, at: 0)
        list = Array(list.prefix(limit))
        if let data = try? JSONEncoder().encode(list) {
            UserDefaults.standard.set(data, forKey: key)
        }
        return list
    }
}
