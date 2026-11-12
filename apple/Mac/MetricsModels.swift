import Foundation

// MARK: - Métricas (GET /v1/metrics)

/// O corpo de `/v1/metrics`, igual ao da rota local do painel
/// (_reversa_forward/012-metricas/interfaces/metricas.md). Só contagens e durações.
struct ServerMetrics: Decodable {
    struct Ratio: Decodable {
        let numerador: Int
        let denominador: Int
    }

    struct Timing: Decodable {
        let casos: Int
        let mediana_s: Int?
        let p90_s: Int?
    }

    struct ChannelRow: Decodable, Hashable {
        let canal: String
        let nome: String
        let origem: String
        let respostas: Int
    }

    struct Commits: Decodable {
        let casos: Int
        let por_semana: Double
    }

    struct Attention: Decodable {
        let tempo_permissao: Timing
        let permissoes_expiradas: Ratio
        let fora_do_terminal: Ratio
        let por_canal: [ChannelRow]
        let tempo_decisao: Timing
    }

    struct Collisions: Decodable {
        let falsos_alarmes: Ratio
        let colisoes_a_commit: Commits
    }

    struct Decisions: Decodable {
        let descartadas: Ratio
        let por_botao: Ratio
    }

    struct Reliability: Decodable {
        let lancadas_com_sucesso: Ratio
        let mensagens_falhadas: Ratio
        let fluxos_no_limite: Ratio
    }

    let computador: String
    let dias: Int
    let atencao: Attention
    let colisoes: Collisions
    let decisoes: Decisions
    let fiabilidade: Reliability
}

/// O que a janela mostra para cada computador.
enum MetricsColumn {
    case loading
    case ok(ServerMetrics)
    /// Servidor sem a capacidade `metrics` (versão anterior).
    case unsupported
    case offline(String)

    var metrics: ServerMetrics? {
        if case .ok(let m) = self { return m }
        return nil
    }
}

/// Uma linha da grelha: como ler a métrica num computador e como somá-la no total.
struct MetricRow: Identifiable {
    enum Kind {
        /// Percentagem; o total soma numeradores e denominadores.
        case ratio(KeyPath<ServerMetrics, ServerMetrics.Ratio>)
        /// Mediana e p90; sem total (as medianas não se somam).
        case timing(KeyPath<ServerMetrics, ServerMetrics.Timing>)
        /// Colisões que chegam a commit; o total soma os casos.
        case commits
    }

    let id: String
    let label: String
    let target: String
    let kind: Kind

    static let sections: [(String, [MetricRow])] = [
        ("Atenção", [
            MetricRow(id: "tp", label: "Tempo até responder a uma permissão", target: "mediana < 2 min", kind: .timing(\.atencao.tempo_permissao)),
            MetricRow(id: "pe", label: "Permissões expiradas", target: "< 5%", kind: .ratio(\.atencao.permissoes_expiradas)),
            MetricRow(id: "ft", label: "Respostas fora do terminal", target: "linha de base", kind: .ratio(\.atencao.fora_do_terminal)),
            MetricRow(id: "td", label: "Tempo até responder a uma decisão", target: "mediana < 3 min", kind: .timing(\.atencao.tempo_decisao)),
        ]),
        ("Colisões", [
            MetricRow(id: "fa", label: "Falsos alarmes", target: "< 10%", kind: .ratio(\.colisoes.falsos_alarmes)),
            MetricRow(id: "cc", label: "Colisões que chegam a commit (aprox.)", target: "0 por semana", kind: .commits),
        ]),
        ("Decisões", [
            MetricRow(id: "dd", label: "Descartadas («Não é pergunta»)", target: "< 10%", kind: .ratio(\.decisoes.descartadas)),
            MetricRow(id: "db", label: "Respondidas por botão", target: "> 70%", kind: .ratio(\.decisoes.por_botao)),
        ]),
        ("Fiabilidade", [
            MetricRow(id: "ls", label: "Sessões lançadas com sucesso", target: "> 80%", kind: .ratio(\.fiabilidade.lancadas_com_sucesso)),
            MetricRow(id: "mf", label: "Mensagens falhadas", target: "< 5%", kind: .ratio(\.fiabilidade.mensagens_falhadas)),
            MetricRow(id: "fl", label: "Fluxos no limite de passos", target: "linha de base", kind: .ratio(\.fiabilidade.fluxos_no_limite)),
        ]),
    ]

    /// O texto da célula de um computador.
    func value(_ m: ServerMetrics) -> String {
        switch kind {
        case .ratio(let k): return Self.percent(m[keyPath: k].numerador, m[keyPath: k].denominador)
        case .timing(let k): return Self.timing(m[keyPath: k])
        case .commits:
            let c = m.colisoes.colisoes_a_commit
            return "\(c.casos) · \(String(format: "%.1f", c.por_semana))/sem"
        }
    }

    /// O texto da coluna «Total», só com os computadores que responderam.
    func total(_ ms: [ServerMetrics]) -> String {
        guard !ms.isEmpty else { return "—" }
        switch kind {
        case .ratio(let k):
            return Self.percent(ms.reduce(0) { $0 + $1[keyPath: k].numerador }, ms.reduce(0) { $0 + $1[keyPath: k].denominador })
        case .timing: return "—"
        case .commits: return "\(ms.reduce(0) { $0 + $1.colisoes.colisoes_a_commit.casos })"
        }
    }

    /// Número de casos, para o aviso «poucos dados» (RN-03).
    func cases(_ m: ServerMetrics) -> Int {
        switch kind {
        case .ratio(let k): return m[keyPath: k].denominador
        case .timing(let k): return m[keyPath: k].casos
        case .commits: return m.colisoes.colisoes_a_commit.casos
        }
    }

    static func percent(_ n: Int, _ d: Int) -> String {
        d == 0 ? "sem dados" : "\(Int((Double(n) * 100 / Double(d)).rounded()))% (\(n)/\(d))"
    }

    static func timing(_ t: ServerMetrics.Timing) -> String {
        guard let med = t.mediana_s else { return "sem dados" }
        var s = duration(med)
        if let p90 = t.p90_s { s += " · p90 \(duration(p90))" }
        return s + " (\(t.casos))"
    }

    static func duration(_ s: Int) -> String {
        s < 60 ? "\(s) s" : s < 3600 ? "\(s / 60) min" : String(format: "%.1f h", Double(s) / 3600)
    }
}

/// Os computadores com menos casos do que isto mostram «poucos dados» (RN-03).
let fewCases = 5
