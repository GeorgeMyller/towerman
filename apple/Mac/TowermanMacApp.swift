import SwiftUI

@main
struct TowermanMacApp: App {
    @StateObject private var store = DeckStore()
    @StateObject private var server = ServerManager()
    @Environment(\.openWindow) private var openWindow

    /// --janela abre o deck ao arrancar (ex.: item de arranque da sessão).
    @MainActor private func launchOptions() async {
        let args = CommandLine.arguments
        if !args.contains("--snapshot") {
            MacNotifier.shared.activate()
            // O servidor vem dentro do app: instala, arranca e só então pareia este Mac.
            await server.start()
            store.reconnect()
            await store.pairLocalIfNeeded()
        }
        if args.contains("--janela") {
            openWindow(id: "deck")
            NSApp.activate(ignoringOtherApps: true)
        }
        #if DEBUG
        // Verificação visual sem gravação de ecrã: desenha o deck num PNG e sai.
        if let i = args.firstIndex(of: "--snapshot"), i + 1 < args.count {
            try? await Task.sleep(nanoseconds: 5_000_000_000)
            for (name, compact, size) in [("deck", false, CGSize(width: 1180, height: 760)), ("menu", true, CGSize(width: 420, height: 640))] {
                let view = DeckView(store: store, compact: compact).frame(width: size.width, height: size.height)
                let r = ImageRenderer(content: view)
                r.scale = 2
                if let img = r.nsImage, let tiff = img.tiffRepresentation, let rep = NSBitmapImageRep(data: tiff),
                   let png = rep.representation(using: .png, properties: [:]) {
                    try? png.write(to: URL(fileURLWithPath: args[i + 1] + "-\(name).png"))
                }
            }
            NSApp.terminate(nil)
        }
        #endif
    }

    var body: some Scene {
        // Ícone do radar na barra de menus, com o número de pedidos à espera.
        MenuBarExtra {
            DeckView(store: store, compact: true)
                .environmentObject(server)
                .frame(width: 420, height: 640)
        } label: {
            HStack(spacing: 3) {
                Image(systemName: store.waitingCount > 0 ? "dot.radiowaves.left.and.right" : "scope")
                if store.waitingCount > 0 {
                    Text("\(store.waitingCount)")
                }
            }
            .accessibilityLabel(store.waitingCount > 0 ? "Towerman: \(store.waitingCount) aguardando você" : "Towerman")
            .task { await launchOptions() }
            // Clique no corpo de uma notificação: abre a janela do deck.
            .onReceive(NotificationCenter.default.publisher(for: MacNotifier.openDeck)) { _ in
                openWindow(id: "deck")
                NSApp.activate(ignoringOtherApps: true)
            }
        }
        .menuBarExtraStyle(.window)

        Window("Towerman", id: "deck") {
            DeckView(store: store, compact: false)
                .environmentObject(server)
                .frame(minWidth: 860, minHeight: 600)
        }
        .defaultSize(width: 1180, height: 760)

        Window("Nova tarefa", id: "launch") {
            MacNovaTarefaView(store: store)
                .frame(minWidth: 460, minHeight: 520)
        }
        .windowResizability(.contentSize)

        Window("Métricas", id: "metrics") {
            MetricsView(store: store)
                .frame(minWidth: 720, minHeight: 520)
        }
        .defaultSize(width: 900, height: 640)

        Window("Computadores", id: "computers") {
            ComputersView(store: store)
                .frame(minWidth: 460, minHeight: 520)
        }
        .windowResizability(.contentSize)
    }
}
