import AppIntents

// Compilado no app do relógio e na extensão de widgets: o controlo e a Siri usam os mesmos intents.

/// Abre o Towerman no pulso (controlo do Centro de Controlo e Siri).
struct OpenTowerIntent: AppIntent {
    static let title: LocalizedStringResource = "Abrir Towerman"
    static let description = IntentDescription("Mostra o radar e os pedidos em espera.")
    static let openAppWhenRun = true

    func perform() async throws -> some IntentResult { .result() }
}

/// "Pedidos em espera no Towerman": responde em voz, sem abrir o app.
struct WaitingRequestsIntent: AppIntent {
    static let title: LocalizedStringResource = "Pedidos em espera"
    static let description = IntentDescription("Diz quantos agentes aguardam resposta e qual é o mais antigo.")

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let s = TowerSummary.load()
        guard s.waiting > 0, let title = s.topTitle else {
            return .result(dialog: "\(s.headline). Nenhum pedido em espera.")
        }
        let detail = s.topDetail.map { ": \($0)" } ?? ""
        return .result(dialog: "\(s.headline). O mais antigo é \(title)\(detail)")
    }
}
