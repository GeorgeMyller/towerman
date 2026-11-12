import Foundation

/// O que o relógio guarda entre receções para decidir se o pulso vibra: os agentes cujo pedido
/// já vibrou, o último estado visto de cada agente e o último aviso «terminou» já dado.
public struct WatchAlertMemory: Equatable {
    public var waiting: Set<String>
    public var status: [String: String]
    public var done: [String: String]

    public init(waiting: Set<String> = [], status: [String: String] = [:], done: [String: String] = [:]) {
        self.waiting = waiting
        self.status = status
        self.done = done
    }
}

/// Política de envio do iPhone ao relógio e de alerta no pulso. Pura, sem WatchConnectivity,
/// para os testes em `apple/tests/` correrem com o swiftc.
///
/// Urgente é o que o utilizador espera sentir no pulso sem abrir o app: um agente que entra ou
/// sai, um estado que muda (a trabalhar, concluído, à espera, erro), um pedido que entra, sai
/// ou é trocado, ou um aviso «terminou». O resto (a ferramenta de cada passo, a última resposta)
/// junta-se ao intervalo.
public enum WatchSyncPolicy {
    /// Um aviso «terminou» só conta durante este tempo: o app que acorda tarde, ou que perdeu a
    /// memória, não repete avisos antigos.
    public static let doneWindow: TimeInterval = 10 * 60

    /// Chave de urgência de um agente: muda com o estado, com o pedido em espera e com o aviso
    /// «terminou».
    public static func urgencyKey(id: String, status: String, waitingId: String?, doneAt: String? = nil) -> String {
        "\(id)#\(status)#\(waitingId ?? "")#\(doneAt ?? "")"
    }

    public static func urgencyKey(_ a: Agent) -> String {
        urgencyKey(id: a.id, status: a.status, waitingId: a.waiting?.id, doneAt: a.doneAt)
    }

    /// O aviso «terminou» do agente é novo: diferente do último já dado e recente.
    public static func isFreshDone(_ a: Agent, seen: String?, now: Date) -> Bool {
        guard let doneAt = a.doneAt, doneAt != seen, let when = parseDate(doneAt) else { return false }
        return now.timeIntervalSince(when) < doneWindow
    }

    /// Lê a hora ISO 8601 do servidor, com ou sem fração de segundo.
    public static func parseDate(_ text: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: text) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: text)
    }

    /// Vai já ao relógio, com mensagem direta: nunca houve envio, ou o conjunto de chaves de
    /// urgência mudou (agente que entra ou sai, estado ou pedido diferentes).
    public static func isUrgent(sent: Set<String>?, next: Set<String>) -> Bool {
        guard let sent else { return true }
        return sent != next
    }

    public static func isUrgent(sent: [Agent]?, next: [Agent]) -> Bool {
        isUrgent(sent: sent.map { Set($0.map(urgencyKey)) }, next: Set(next.map(urgencyKey)))
    }

    /// O pulso vibra com um pedido novo em espera, ou com um aviso «terminou» novo do servidor
    /// (a sessão parou com a pessoa longe do computador). Na primeira receção, os pedidos contam e
    /// os avisos «terminou» só se forem recentes.
    public static func alert(after memory: WatchAlertMemory?, agents: [Agent],
                             now: Date = Date()) -> (Bool, WatchAlertMemory) {
        let waitingNow = Set(agents.filter { $0.status == "waiting" }.map(\.id))
        let statusNow = Dictionary(agents.map { ($0.id, $0.status) }, uniquingKeysWith: { a, _ in a })
        let newPrompt = !waitingNow.subtracting(memory?.waiting ?? []).isEmpty
        let newDone = !freshlyDone(after: memory, agents: agents, now: now).isEmpty
        var done = memory?.done ?? [:]
        for a in agents { if let d = a.doneAt { done[a.id] = d } }
        // Agentes que saíram deixam de ocupar a memória.
        let ids = Set(agents.map(\.id))
        done = done.filter { ids.contains($0.key) }
        return (newPrompt || newDone, WatchAlertMemory(waiting: waitingNow, status: statusNow, done: done))
    }

    /// Agentes com um aviso «terminou» novo em relação à memória.
    public static func freshlyDone(after memory: WatchAlertMemory?, agents: [Agent], now: Date = Date()) -> [Agent] {
        agents.filter { isFreshDone($0, seen: memory?.done[$0.id], now: now) }
    }
}
