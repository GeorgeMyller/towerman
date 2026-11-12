import AppIntents

/// Frases da Siri no relógio. Ficam no app: a extensão só partilha os intents.
struct TowerShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(intent: WaitingRequestsIntent(),
                    phrases: ["Pedidos em espera no \(.applicationName)",
                              "O que espera por mim no \(.applicationName)"],
                    shortTitle: "Pedidos em espera",
                    systemImageName: "hand.raised")
        AppShortcut(intent: OpenTowerIntent(),
                    phrases: ["Abrir \(.applicationName)"],
                    shortTitle: "Abrir Towerman",
                    systemImageName: "dot.radiowaves.left.and.right")
    }
}
