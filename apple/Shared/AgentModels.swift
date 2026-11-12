import Foundation

// MARK: - Models for Agents Deck Apple Client
// Protocolo v1 do Agents Deck (docs/protocol.md), com os campos extra do gateway do Towerman
// (host, provider, capabilities). A descodificação tolera campos em falta: um campo novo ou
// ausente no servidor nunca deve esvaziar a lista inteira de agentes.

public struct Activity: Codable, Equatable {
    public let tool: String
    public let detail: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tool = try c.decodeIfPresent(String.self, forKey: .tool) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
    }
}

public struct PromptOption: Codable, Identifiable, Equatable {
    public var id: String { key }
    public let key: String
    public let label: String
    public let primary: Bool
    /// Numa decisão em texto livre, a opção fica desativada quando o terminal não aceita texto.
    public let disabled: Bool

    public init(key: String, label: String, primary: Bool = false, disabled: Bool = false) {
        self.key = key
        self.label = label
        self.primary = primary
        self.disabled = disabled
    }

    /// Descarta uma decisão que não era pergunta (falso alarme da deteção).
    public static let dismiss = PromptOption(key: "dismiss", label: "Não é pergunta")

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.decode(String.self, forKey: .key)
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? key
        primary = try c.decodeIfPresent(Bool.self, forKey: .primary) ?? false
        disabled = try c.decodeIfPresent(Bool.self, forKey: .disabled) ?? false
    }
}

/// Algo em que o agente está bloqueado: permissão, pergunta ou texto livre.
public struct Prompt: Codable, Identifiable, Equatable {
    public let id: String
    public let kind: String // permission | question | decision | input
    public let title: String
    public let detail: String
    public let context: String
    public let options: [PromptOption]

    /// Pergunta (AskUserQuestion) ou decisão em texto livre no fim do turno: desenha-se como texto,
    /// uma opção por linha, sem os gestos de permitir/negar.
    public var isQuestion: Bool { kind == "question" || kind == "decision" }
    /// Decisão em texto livre («Digite CONTINUAR», menu, «Posso começar?»): a opção é texto
    /// entregue ao terminal, e pode responder-se com outro texto.
    public var isDecision: Bool { kind == "decision" }

    enum CodingKeys: String, CodingKey {
        case id, kind, title, detail, context, options
    }

    /// Formato antigo do linux_bridge: `prompt` com `promptId` e `message`.
    enum LegacyKeys: String, CodingKey {
        case promptId, message
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let legacy = try decoder.container(keyedBy: LegacyKeys.self)
        guard let id = try c.decodeIfPresent(String.self, forKey: .id)
                ?? legacy.decodeIfPresent(String.self, forKey: .promptId) else {
            throw DecodingError.keyNotFound(CodingKeys.id, .init(codingPath: decoder.codingPath, debugDescription: "prompt sem id"))
        }
        self.id = id
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "permission"
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .detail)
            ?? legacy.decodeIfPresent(String.self, forKey: .message) ?? ""
        context = try c.decodeIfPresent(String.self, forKey: .context) ?? ""
        options = try c.decodeIfPresent([PromptOption].self, forKey: .options) ?? []
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(kind, forKey: .kind)
        try c.encode(title, forKey: .title)
        try c.encode(detail, forKey: .detail)
        try c.encode(context, forKey: .context)
        try c.encode(options, forKey: .options)
    }
}

public struct ContextWindow: Codable, Equatable {
    public let used: Int
    public let window: Int

    public var fraction: Double { window > 0 ? Double(used) / Double(window) : 0 }
}

public struct Agent: Codable, Identifiable, Equatable {
    public let id: String
    public let slot: Int
    public let title: String
    public let aiTitle: String
    public let tool: String
    public let status: String // starting, running, waiting, idle, error, exited
    public let statusSince: String
    public let cwd: String
    public let folder: String
    public let branch: String
    public let model: String
    public let modelLabel: String
    public let activity: Activity?
    public let lastPrompt: String?
    /// Fim da última resposta do agente ao último pedido (contexto para decidir o que responder).
    public let lastReply: String?
    public let waiting: Prompt?
    public let context: ContextWindow?
    public let costUsd: Double?
    public let unseen: Bool
    public let host: String
    public let provider: String
    /// Ações que o servidor aceita para este agente. Vazio = servidor antigo, tudo permitido.
    public let capabilities: [String]
    /// Hora (ISO 8601, com milésimas) do último aviso «terminou» do servidor: a sessão parou com a
    /// pessoa longe do computador. Muda a cada aviso; nil sem aviso ou em servidores antigos.
    public let doneAt: String?

    public var isWaiting: Bool { status == "waiting" && waiting != nil }
    /// Aberta pelo Towerman e ainda a correr: só esta pode ser terminada pelo aparelho.
    public var canTerminate: Bool { capabilities.contains("terminate") }
    public var isRunning: Bool { status == "running" }

    public func can(_ capability: String) -> Bool {
        capabilities.isEmpty || capabilities.contains(capability)
    }

    enum CodingKeys: String, CodingKey {
        case id, slot, title, aiTitle, tool, status, statusSince, cwd, folder, branch, model, modelLabel
        case activity, lastPrompt, lastReply, waiting, prompt, context, costUsd, unseen, host, provider, capabilities
        case doneAt
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        slot = try c.decodeIfPresent(Int.self, forKey: .slot) ?? 0
        folder = try c.decodeIfPresent(String.self, forKey: .folder) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? folder
        aiTitle = try c.decodeIfPresent(String.self, forKey: .aiTitle) ?? ""
        tool = try c.decodeIfPresent(String.self, forKey: .tool) ?? ""
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? "idle"
        statusSince = try c.decodeIfPresent(String.self, forKey: .statusSince) ?? ""
        cwd = try c.decodeIfPresent(String.self, forKey: .cwd) ?? ""
        branch = try c.decodeIfPresent(String.self, forKey: .branch) ?? ""
        model = try c.decodeIfPresent(String.self, forKey: .model) ?? ""
        modelLabel = try c.decodeIfPresent(String.self, forKey: .modelLabel) ?? ""
        activity = try? c.decodeIfPresent(Activity.self, forKey: .activity)
        lastPrompt = try c.decodeIfPresent(String.self, forKey: .lastPrompt)
        lastReply = try c.decodeIfPresent(String.self, forKey: .lastReply)
        waiting = (try? c.decodeIfPresent(Prompt.self, forKey: .waiting))
            ?? (try? c.decodeIfPresent(Prompt.self, forKey: .prompt))
        context = try? c.decodeIfPresent(ContextWindow.self, forKey: .context)
        costUsd = try c.decodeIfPresent(Double.self, forKey: .costUsd)
        unseen = try c.decodeIfPresent(Bool.self, forKey: .unseen) ?? false
        host = try c.decodeIfPresent(String.self, forKey: .host) ?? ""
        provider = try c.decodeIfPresent(String.self, forKey: .provider) ?? tool
        capabilities = try c.decodeIfPresent([String].self, forKey: .capabilities) ?? []
        doneAt = try c.decodeIfPresent(String.self, forKey: .doneAt)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(slot, forKey: .slot)
        try c.encode(title, forKey: .title)
        try c.encode(aiTitle, forKey: .aiTitle)
        try c.encode(tool, forKey: .tool)
        try c.encode(status, forKey: .status)
        try c.encode(statusSince, forKey: .statusSince)
        try c.encode(cwd, forKey: .cwd)
        try c.encode(folder, forKey: .folder)
        try c.encode(branch, forKey: .branch)
        try c.encode(model, forKey: .model)
        try c.encode(modelLabel, forKey: .modelLabel)
        try c.encodeIfPresent(activity, forKey: .activity)
        try c.encodeIfPresent(lastPrompt, forKey: .lastPrompt)
        try c.encodeIfPresent(lastReply, forKey: .lastReply)
        try c.encodeIfPresent(waiting, forKey: .waiting)
        try c.encodeIfPresent(context, forKey: .context)
        try c.encodeIfPresent(costUsd, forKey: .costUsd)
        try c.encode(unseen, forKey: .unseen)
        try c.encode(host, forKey: .host)
        try c.encode(provider, forKey: .provider)
        try c.encode(capabilities, forKey: .capabilities)
        try c.encodeIfPresent(doneAt, forKey: .doneAt)
    }
}

public struct ServerInfo: Codable, Equatable {
    public let name: String
    public let version: String
    public let time: String
    /// Endereços atuais do computador (rede local e túnel): o app aprende-os sem novo pareamento.
    public var hosts: [String]? = nil
}

public struct DeckState: Codable, Equatable {
    public let server: ServerInfo
    public let agents: [Agent]
    /// Capacidades do servidor (ex.: `launch`). Vazio = servidor antigo.
    public let capabilities: [String]

    public init(server: ServerInfo, agents: [Agent], capabilities: [String] = []) {
        self.server = server
        self.agents = agents
        self.capabilities = capabilities
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        server = try c.decode(ServerInfo.self, forKey: .server)
        // Um agente malformado não apaga os outros.
        agents = (try c.decodeIfPresent([Lossy<Agent>].self, forKey: .agents) ?? []).compactMap(\.value)
        capabilities = (try? c.decodeIfPresent([String].self, forKey: .capabilities)) ?? []
    }
}

/// Descodifica um elemento de lista sem falhar a lista inteira.
struct Lossy<T: Decodable>: Decodable {
    let value: T?

    init(from decoder: Decoder) throws {
        value = try? T(from: decoder)
    }
}

public struct AnswerPayload: Codable {
    public let promptId: String
    public let key: String
}

public extension Agent {
    /// O fim da última resposta, numa linha corrida: é onde costuma estar a pergunta do agente.
    func replyTail(_ maxChars: Int) -> String? {
        guard let reply = lastReply else { return nil }
        let flat = reply.split(whereSeparator: \.isNewline).joined(separator: " ")
            .replacingOccurrences(of: "  ", with: " ").trimmingCharacters(in: .whitespaces)
        guard !flat.isEmpty else { return nil }
        if flat.count <= maxChars { return flat }
        var tail = String(flat.suffix(maxChars))
        if let space = tail.firstIndex(of: " ") { tail = String(tail[tail.index(after: space)...]) }
        return "…" + tail.trimmingCharacters(in: CharacterSet(charactersIn: "…").union(.whitespaces))
    }

    /// O último pedido da pessoa (da transcrição; sem ela, o título que a IA deu à sessão).
    var askedText: String {
        if let p = lastPrompt, !p.isEmpty { return p }
        return aiTitle
    }
}
