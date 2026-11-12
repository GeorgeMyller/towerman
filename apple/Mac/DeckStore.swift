import AppKit
import Foundation

/// O estado do deck no Mac: os computadores pareados (este Mac e o Linux), uma ligação por
/// computador e os agentes de todos juntos. Pareia-se sozinho com o Towerman deste Mac.
@MainActor
final class DeckStore: ObservableObject {
    @Published private(set) var servers: [PairedServer] = []
    @Published private(set) var status: [String: ServerStatus] = [:]
    @Published private(set) var agents: [Agent] = []
    @Published private(set) var isConnected = false
    @Published var note: String?

    struct ServerStatus: Equatable {
        let connected: Bool
        let address: String?
        let problem: String?
        let agents: Int
    }

    private var links: [String: ServerLink] = [:]
    private var agentServer: [String: String] = [:]
    private let session = URLSession.shared
    private static let storeKey = "paired_servers"

    var waitingCount: Int { agents.filter { AgentState($0) == .waiting }.count }

    init() {
        if let data = UserDefaults.standard.data(forKey: Self.storeKey),
           let list = try? JSONDecoder().decode([PairedServer].self, from: data) {
            servers = list
        }
        servers.forEach { link(for: $0).start() }
        MacNotifier.shared.answer = { [weak self] agentId, promptId, key in
            await self?.answer(agentId: agentId, promptId: promptId, key: key) ?? false
        }
        Task { await pairLocalIfNeeded() }
    }

    // MARK: - Towerman deste Mac

    /// O token local do Towerman (o mesmo do painel), em ~/.claude/towerman/token.
    private var localToken: String? {
        let home = ProcessInfo.processInfo.environment["TOWERMAN_HOME"]
            ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".claude/towerman").path
        return (try? String(contentsOfFile: home + "/token", encoding: .utf8))?.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func localRequest(_ path: String, method: String = "GET") -> URLRequest? {
        guard let token = localToken, let url = URL(string: "http://127.0.0.1:7777\(path)") else { return nil }
        var r = URLRequest(url: url)
        r.httpMethod = method
        r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        return r
    }

    /// Na primeira abertura, pareia este Mac consigo próprio: sem QR, pelo token local.
    func pairLocalIfNeeded() async {
        guard let req = localRequest("/v1/pareamento", method: "POST"),
              let (data, resp) = try? await session.data(for: req), (resp as? HTTPURLResponse)?.statusCode == 200,
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let link = json["url"] as? String, let url = URL(string: link),
              let pairing = PairingLink(url: url) else { return }
        if servers.contains(where: { $0.id == pairing.serverId }) { return }
        // Neste Mac, o endereço certo é sempre o local.
        let local = URL(string: link.replacingOccurrences(of: "&h=", with: "&h=127.0.0.1%2C")) ?? url
        note = await pair(with: local)
    }

    /// QR para o iPhone ler (o mesmo de «towerman pair»), servido pelo Towerman local.
    func iPhoneQR() async -> NSImage? {
        guard let req = localRequest("/v1/pareamento/qr.png"),
              let (data, resp) = try? await session.data(for: req), (resp as? HTTPURLResponse)?.statusCode == 200 else { return nil }
        return NSImage(data: data)
    }

    // MARK: - Computadores

    private func link(for server: PairedServer) -> ServerLink {
        if let l = links[server.id] { return l }
        let l = ServerLink(server: server, token: TokenStore.load(server.id) ?? "", session: session)
        l.onChange = { [weak self] link in self?.changed(link) }
        links[server.id] = l
        return l
    }

    private func changed(_ link: ServerLink) {
        if let i = servers.firstIndex(where: { $0.id == link.server.id }), servers[i].lastHost != link.server.lastHost || servers[i].hosts != link.server.hosts {
            servers[i].lastHost = link.server.lastHost
            servers[i].hosts = link.server.hosts
            save()
        }
        refresh()
    }

    private func save() {
        if let data = try? JSONEncoder().encode(servers) {
            UserDefaults.standard.set(data, forKey: Self.storeKey)
        }
    }

    private func refresh() {
        var all: [Agent] = []
        var map: [String: String] = [:]
        var st: [String: ServerStatus] = [:]
        for server in servers {
            let l = links[server.id]
            let list = l?.state?.agents ?? []
            list.forEach { map[$0.id] = server.id }
            all += list
            st[server.id] = ServerStatus(connected: l?.connected ?? false, address: l?.baseURL?.host,
                                         problem: l?.problem, agents: list.count)
        }
        agentServer = map
        status = st
        agents = all
        isConnected = links.values.contains { $0.connected }
        MacNotifier.shared.update(with: all)
    }

    /// Lê um link towerman://pair (do QR ou copiado) e troca o código por um token deste Mac.
    func pair(with url: URL) async -> String {
        guard let pairing = PairingLink(url: url) else { return "Este link não é de pareamento do Towerman." }
        let device = Host.current().localizedName ?? "Mac"
        let body = try? JSONSerialization.data(withJSONObject: [
            "code": pairing.code,
            "device": device,
            "device_pk": RelayCrypto.devicePublicKeyBase64
        ])
        var lastError = "Não consegui chegar a \(pairing.name). Está na mesma rede?"
        for host in pairing.hosts {
            guard let endpoint = ServerLink.baseURL(host: host, port: pairing.port)?.appendingPathComponent("v1/pair") else { continue }
            var req = URLRequest(url: endpoint)
            req.httpMethod = "POST"
            req.timeoutInterval = 4
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = body
            guard let (data, resp) = try? await session.data(for: req) else { continue }
            let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
            guard (resp as? HTTPURLResponse)?.statusCode == 200, let token = json["token"] as? String else {
                lastError = (json["error"] as? String).map { "\(pairing.name) recusou: \($0)." } ?? lastError
                break
            }
            let srvInfo = json["server"] as? [String: Any]
            let name = (srvInfo?["name"] as? String) ?? pairing.name
            let serverPK = (srvInfo?["pk"] as? String) ?? pairing.serverPublicKey
            let deviceId = json["device"] as? String
            let server = PairedServer(id: pairing.serverId, name: name, hosts: pairing.hosts, port: pairing.port,
                                      lastHost: host, manualURL: nil, deviceId: deviceId, serverPublicKey: serverPK)
            TokenStore.save(token, for: server.id)
            links[server.id]?.stop()
            links[server.id] = nil
            servers.removeAll { $0.id == server.id }
            servers.append(server)
            save()
            link(for: server).start()
            refresh()
            return "\(name) pareado."
        }
        return lastError
    }

    func remove(_ server: PairedServer) {
        links[server.id]?.stop()
        links[server.id] = nil
        TokenStore.delete(server.id)
        servers.removeAll { $0.id == server.id }
        save()
        refresh()
    }

    func reconnect() {
        links.values.forEach { $0.restart() }
    }

    // MARK: - Ações no computador do agente

    struct Result {
        let ok: Bool
        let message: String
    }

    private func perform(_ agentId: String, _ action: String, body: [String: Any]? = nil) async -> (Int, Data)? {
        guard let sid = agentServer[agentId], let l = links[sid] else { return nil }
        var body = body
        // O computador regista que a resposta veio do Mac (métricas).
        if action == "answer" || action == "input" { body?["origin"] = "mac" }
        return await l.performAction(agentId: agentId, action: action, body: body)
    }

    private static func error(_ data: Data) -> String? {
        ((try? JSONSerialization.jsonObject(with: data)) as? [String: Any])?["error"] as? String
    }

    func answer(_ agent: Agent, prompt: Prompt, key: String) async -> Result {
        guard let (code, data) = await perform(agent.id, "answer", body: ["promptId": prompt.id, "key": key]) else {
            return Result(ok: false, message: "Sem ligação ao computador deste agente.")
        }
        if (200...299).contains(code) { return Result(ok: true, message: "Respondido.") }
        return Result(ok: false, message: Self.error(data) ?? "O pedido pode já ter sido respondido no terminal.")
    }

    /// Resposta vinda de uma notificação: só os ids do agente e do pedido.
    func answer(agentId: String, promptId: String, key: String) async -> Bool {
        guard let (code, _) = await perform(agentId, "answer", body: ["promptId": promptId, "key": key]) else { return false }
        return (200...299).contains(code)
    }

    func focus(_ agent: Agent) async -> Result {
        guard let (code, data) = await perform(agent.id, "focus") else {
            return Result(ok: false, message: "Sem ligação ao computador deste agente.")
        }
        if (200...299).contains(code) { return Result(ok: true, message: "Terminal em primeiro plano.") }
        return Result(ok: false, message: Self.error(data) ?? "Não foi possível focar o terminal.")
    }

    // MARK: - Métricas

    /// As métricas de um computador; um servidor sem a capacidade `metrics` é de uma versão anterior.
    func metrics(serverId: String, days: Int) async -> MetricsColumn {
        guard let server = servers.first(where: { $0.id == serverId }) else { return .offline("Computador desconhecido.") }
        let l = link(for: server)
        if l.state != nil && !l.supportsMetrics { return .unsupported }
        guard let (code, data) = await l.performServerAction(op: "metrics", body: ["days": days]) else {
            return .offline("Sem ligação a \(server.name).")
        }
        guard code == 200, let m = try? JSONDecoder().decode(ServerMetrics.self, from: data) else {
            return code == 404 ? .unsupported : .offline(Self.error(data) ?? "\(server.name) não devolveu as métricas.")
        }
        return .ok(m)
    }

    // MARK: - Lançar sessões

    /// Computadores que aceitam sessões lançadas daqui (servidor com a capacidade `launch`).
    var launchServers: [LaunchServer] {
        servers.filter { links[$0.id]?.supportsLaunch == true }.map { LaunchServer(id: $0.id, name: $0.name) }
    }

    func catalog(serverId: String) async -> (LaunchCatalog?, String?) {
        guard let server = servers.first(where: { $0.id == serverId }) else { return (nil, "Computador desconhecido.") }
        guard let (code, data) = await link(for: server).performServerAction(op: "catalog") else {
            return (nil, "Sem ligação a \(server.name).")
        }
        guard code == 200, let cat = try? JSONDecoder().decode(LaunchCatalog.self, from: data) else {
            return (nil, Self.error(data) ?? "\(server.name) não devolveu o catálogo.")
        }
        return (cat, nil)
    }

    /// Lança uma sessão nova; o mesmo request_id vai na retentativa.
    func launch(_ combo: LaunchShortcut, task: String, mode: String?) async -> Result {
        guard let server = servers.first(where: { $0.id == combo.serverId }) else {
            return Result(ok: false, message: "Computador desconhecido.")
        }
        var body: [String: Any] = ["request_id": UUID().uuidString, "origin": "mac", "agent": combo.agent,
                                   "root": combo.root, "role": combo.role, "task": task]
        if let mode { body["mode"] = mode }
        let l = link(for: server)
        var res = await l.performServerAction(op: "launch", body: body)
        if res == nil { res = await l.performServerAction(op: "launch", body: body) }
        guard let (code, data) = res else { return Result(ok: false, message: "Sem ligação a \(server.name).") }
        guard code == 200 else { return Result(ok: false, message: Self.error(data) ?? "\(server.name) recusou o lançamento.") }
        LaunchRecents.remember(combo)
        await l.fetchState()
        let r = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
        return Result(ok: true, message: "Sessão \((r["name"] as? String) ?? "") a arrancar em \(server.name).")
    }

    func terminate(_ agent: Agent) async -> Result {
        guard let (code, data) = await perform(agent.id, "terminate") else {
            return Result(ok: false, message: "Sem ligação ao computador deste agente.")
        }
        if (200...299).contains(code) { return Result(ok: true, message: "Sessão terminada.") }
        return Result(ok: false, message: Self.error(data) ?? "Não foi possível terminar a sessão.")
    }

    func send(_ agent: Agent, text: String) async -> Result {
        guard let (code, data) = await perform(agent.id, "input", body: ["text": text]) else {
            return Result(ok: false, message: "Sem ligação ao computador deste agente.")
        }
        guard (200...299).contains(code) else { return Result(ok: false, message: Self.error(data) ?? "Não foi possível enviar.") }
        let r = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
        if r["state"] as? String == "entregue" { return Result(ok: true, message: "Entregue no terminal.") }
        let reason = (r["reason"] as? String) ?? ""
        return Result(ok: true, message: reason.isEmpty ? "Na fila: entra quando o passo atual terminar." : "Na fila: \(reason).")
    }
}
