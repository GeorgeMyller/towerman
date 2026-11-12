import Foundation
import ServiceManagement

/// O servidor Towerman vem dentro do app. Ao abrir, o app deixa tudo pronto sem terminal: o
/// comando `towerman` no PATH (os hooks chamam-no), os hooks do Claude, do Codex e do
/// Antigravity, o serviço no launchd (arranca a cada sessão) e o servidor a responder.
@MainActor
final class ServerManager: ObservableObject {
    @Published private(set) var running = false
    @Published private(set) var detail: String?
    @Published private(set) var startAtLogin = SMAppService.mainApp.status == .enabled

    private var bundled: URL? { Bundle.main.url(forAuxiliaryExecutable: "towerman-cli") }
    private var home: URL { FileManager.default.homeDirectoryForCurrentUser }

    func start() async {
        guard let bin = bundled else {
            // Build sem o servidor (ex.: compilado sem o repositório do towerman): usa o que houver.
            running = await healthy()
            detail = running ? "Servidor externo" : "Este build não traz o servidor."
            return
        }
        linkCommand(to: bin)
        let (code, out) = await run(bin.path, ["install", "--sem-extensao"])
        if code != 0 {
            detail = "Instalação falhou: \(out.split(separator: "\n").last ?? "")"
        }
        if await waitHealthy() { return }
        await restart()
    }

    func restart() async {
        // Rótulo atual do LaunchAgent; o antigo fica para instalações feitas antes da mudança.
        for label in ["io.github.georgemyller.towerman", "legado.towerman"] {
            let (code, _) = await run("/bin/launchctl", ["kickstart", "-k", "gui/\(getuid())/\(label)"])
            if code == 0 { break }
        }
        if !(await waitHealthy()) {
            detail = "O servidor não respondeu. Veja ~/Library/Logs ou rode towerman serve."
        }
    }

    func setStartAtLogin(_ on: Bool) {
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
        } catch {
            detail = "Não foi possível mudar o arranque: \(error.localizedDescription)"
        }
        startAtLogin = SMAppService.mainApp.status == .enabled
    }

    // MARK: - Peças

    /// ~/.local/bin/towerman aponta para o binário do app: atualizar o app atualiza o servidor
    /// e os hooks. Um binário anterior (go install) fica guardado ao lado.
    private func linkCommand(to bin: URL) {
        let fm = FileManager.default
        let dir = home.appendingPathComponent(".local/bin")
        let dest = dir.appendingPathComponent("towerman")
        try? fm.createDirectory(at: dir, withIntermediateDirectories: true)
        if let target = try? fm.destinationOfSymbolicLink(atPath: dest.path), target == bin.path { return }
        if fm.fileExists(atPath: dest.path) {
            let isLink = (try? fm.destinationOfSymbolicLink(atPath: dest.path)) != nil
            let backup = dir.appendingPathComponent("towerman.antes-do-app")
            if !isLink && !fm.fileExists(atPath: backup.path) {
                try? fm.moveItem(at: dest, to: backup)
            } else {
                try? fm.removeItem(at: dest)
            }
        }
        do {
            try fm.createSymbolicLink(at: dest, withDestinationURL: bin)
        } catch {
            detail = "Não consegui criar \(dest.path): \(error.localizedDescription)"
        }
    }

    private func waitHealthy() async -> Bool {
        for _ in 0..<12 {
            if await healthy() {
                running = true
                if detail?.hasPrefix("O servidor") == true { detail = nil }
                return true
            }
            try? await Task.sleep(nanoseconds: 500_000_000)
        }
        running = false
        return false
    }

    /// A porta de rede do Towerman responde a /health sem token.
    private func healthy() async -> Bool {
        guard let url = URL(string: "http://127.0.0.1:7352/health") else { return false }
        var req = URLRequest(url: url)
        req.timeoutInterval = 1
        guard let (_, resp) = try? await URLSession.shared.data(for: req) else { return false }
        return (resp as? HTTPURLResponse)?.statusCode == 200
    }

    private func run(_ path: String, _ args: [String]) async -> (Int32, String) {
        await withCheckedContinuation { cont in
            let p = Process()
            p.executableURL = URL(fileURLWithPath: path)
            p.arguments = args
            let pipe = Pipe()
            p.standardOutput = pipe
            p.standardError = pipe
            p.terminationHandler = { proc in
                let out = String(data: pipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
                cont.resume(returning: (proc.terminationStatus, out))
            }
            do { try p.run() } catch { cont.resume(returning: (-1, error.localizedDescription)) }
        }
    }
}
