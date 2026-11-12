import Foundation
import Network
import Security

// MARK: - Computador pareado

/// Um computador com Towerman, pareado por QR (ou ligado à mão, para servidores antigos).
public struct PairedServer: Codable, Identifiable, Equatable {
    public let id: String
    public var name: String
    /// Endereços do computador: nome .local, IPs da rede e o URL do Relay/túnel (fora de casa).
    public var hosts: [String]
    public var port: Int
    /// O último endereço que respondeu: é o primeiro a tentar.
    public var lastHost: String?
    /// Ligação manual (URL completo); sem verificação de identidade nem Bonjour.
    public var manualURL: String?
    /// Identificador deste aparelho no servidor (para o Relay E2EE).
    public var deviceId: String? = nil
    /// Chave pública X25519 do servidor em base64url (para o Relay E2EE).
    public var serverPublicKey: String? = nil

    public var isManual: Bool { manualURL != nil }
}

/// Os tokens ficam no Keychain do iPhone, um por computador. Só no simulador (sem assinatura,
/// erro -34018) se usa o fallback nas preferências do app.
enum TokenStore {
    private static func fallbackKey(_ id: String) -> String { "token.\(id)" }

    private static let service = TowermanIDs.sharedID + ".token"

    static func save(_ token: String, for id: String) {
        let base: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                   kSecAttrService as String: service, kSecAttrAccount as String: id]
        SecItemDelete(base as CFDictionary)
        var add = base
        add[kSecValueData as String] = Data(token.utf8)
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        if SecItemAdd(add as CFDictionary, nil) == errSecSuccess {
            UserDefaults.standard.removeObject(forKey: fallbackKey(id))
        } else {
            #if targetEnvironment(simulator)
            UserDefaults.standard.set(token, forKey: fallbackKey(id))
            #endif
        }
    }

    static func load(_ id: String) -> String? {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: id, kSecReturnData as String: true]
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else {
            #if targetEnvironment(simulator)
            return UserDefaults.standard.string(forKey: fallbackKey(id))
            #else
            return nil
            #endif
        }
        return String(data: data, encoding: .utf8)
    }

    static func delete(_ id: String) {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: id]
        SecItemDelete(q as CFDictionary)
        UserDefaults.standard.removeObject(forKey: fallbackKey(id))
    }
}

// MARK: - Ligação a um computador

/// Mantém a ligação ao vivo a um computador: encontra o endereço que responde (último bom,
/// endereços do pareamento, Bonjour), lê o estado e segue o stream de eventos, voltando a ligar
/// com espera de 1 s a 10 s quando cai.
final class ServerLink {
    private(set) var server: PairedServer
    private let token: String
    private let session: URLSession
    private var task: Task<Void, Never>?
    private var wsTask: URLSessionWebSocketTask?
    private(set) var baseURL: URL?
    private(set) var state: DeckState?
    private(set) var connected = false
    private(set) var problem: String?

    /// Chamado (no MainActor) sempre que estado, ligação ou endereço mudam.
    var onChange: (@MainActor (ServerLink) -> Void)?

    /// Vigia a rede do aparelho: ao sair do Wi-Fi (ou voltar a ele), a ligação antiga fica
    /// pendurada até ao fim do tempo limite; religar logo escolhe o caminho novo.
    private var monitor: NWPathMonitor?
    private var lastPath: String?

    init(server: PairedServer, token: String, session: URLSession = .shared) {
        self.server = server
        self.token = token
        self.session = session
    }

    private func watchNetwork() {
        guard monitor == nil else { return }
        let m = NWPathMonitor()
        m.pathUpdateHandler = { [weak self] path in
            let sig = "\(path.status)|" + path.availableInterfaces.map(\.name).joined(separator: ",")
            DispatchQueue.main.async {
                guard let self else { return }
                let anterior = self.lastPath
                self.lastPath = sig
                // A primeira leitura só regista o caminho; depois, cada mudança religa.
                if anterior != nil, anterior != sig, self.task != nil, path.status == .satisfied {
                    self.restart()
                }
            }
        }
        m.start(queue: DispatchQueue(label: "towerman.rede"))
        monitor = m
    }

    /// Liga (ou mantém): uma ligação ao vivo saudável não é reiniciada, para o regresso ao primeiro
    /// plano não cortar o stream que está a funcionar.
    func start() {
        if task != nil && connected { return }
        restart()
    }

    func restart() {
        watchNetwork()
        task?.cancel()
        task = Task { [weak self] in
            var espera: UInt64 = 1
            while !Task.isCancelled {
                guard let self else { return }
                let inicio = Date()
                if await self.resolve() {
                    if self.viaRelay {
                        await self.streamRelay()
                    } else if self.viaTunel {
                        await self.poll()
                    } else {
                        await self.fetchState()
                        await self.stream()
                    }
                }
                // Uma tarefa cancelada (substituída por outra) não mexe no estado da ligação nova.
                if Task.isCancelled { return }
                await self.update { $0.connected = false }
                espera = Date().timeIntervalSince(inicio) > 30 ? 1 : min(espera * 2, 10)
                try? await Task.sleep(nanoseconds: espera * 1_000_000_000)
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
        wsTask?.cancel(with: .goingAway, reason: nil)
        wsTask = nil
        monitor?.cancel()
        monitor = nil
        lastPath = nil
        connected = false
    }

    // MARK: Endereço

    /// Escolhe o endereço que responde primeiro ao /health com a identidade deste computador. Os
    /// endereços são testados ao mesmo tempo: fora de casa, os da rede local não fazem esperar o
    /// do túnel.
    private func resolve() async -> Bool {
        if let manual = server.manualURL {
            baseURL = URL(string: manual)
            return baseURL != nil
        }
        var candidatos: [String] = []
        for h in [server.lastHost].compactMap({ $0 }) + server.hosts where !candidatos.contains(h) {
            candidatos.append(h)
        }
        let port = server.port
        let vencedor = await withTaskGroup(of: (String, URL)?.self) { group -> (String, URL)? in
            for host in candidatos {
                group.addTask { [self] in
                    // Em casa, a rede local ganha ao túnel: este só entra se ela não responder logo.
                    if Self.isTunnel(host) { try? await Task.sleep(nanoseconds: 800_000_000) }
                    guard let url = await self.probe(host: host, port: port) else { return nil }
                    return (host, url)
                }
            }
            for await r in group {
                if let r {
                    group.cancelAll()
                    return r
                }
            }
            return nil
        }
        if let (host, url) = vencedor {
            await adopt(url, host: host)
            return true
        }
        // Nenhum endereço guardado respondeu (IP novo, rede nova): procurar por Bonjour.
        if let (host, port) = await BonjourFinder.find(serverId: server.id),
           let url = await probe(host: host, port: port) {
            await adopt(url, host: host)
            return true
        }
        let temTunel = server.hosts.contains(where: Self.isTunnel)
        await update {
            $0.problem = temTunel
                ? "Não encontrei \($0.server.name). O computador está ligado e com internet?"
                : "Não encontrei \($0.server.name) na rede. Abra o app uma vez na mesma Wi-Fi para ativar o acesso de fora."
        }
        return false
    }

    /// O servidor diz os seus endereços atuais em cada estado: ficam guardados para a próxima
    /// ligação (ex.: o endereço novo do túnel depois de o computador reiniciar).
    private func learnHosts(_ st: DeckState) async {
        guard server.manualURL == nil, let hosts = st.server.hosts, !hosts.isEmpty, hosts != server.hosts else { return }
        await update { $0.server.hosts = hosts }
    }

    /// Um endereço é um nome ou IP (liga por http na porta do Towerman) ou um URL completo, como
    /// o do túnel da Cloudflare (https://….trycloudflare.com), que chega de qualquer rede.
    static func baseURL(host: String, port: Int) -> URL? {
        if host.hasPrefix("https://") || host.hasPrefix("http://") { return URL(string: host) }
        let h = host.contains(":") ? "[\(host)]" : host
        return URL(string: "http://\(h):\(port)")
    }

    static func isTunnel(_ host: String) -> Bool {
        host.hasPrefix("https://") || host.contains("/s/")
    }

    /// A ligação atual passa pelo Relay WebSocket E2EE (fora de casa).
    var viaRelay: Bool { baseURL?.path.contains("/s/") == true && server.manualURL == nil }

    /// A ligação atual passa pelo túnel rápido da Cloudflare (fallback fora de casa).
    var viaTunel: Bool { baseURL?.scheme == "https" && !viaRelay && server.manualURL == nil }

    /// Endereço do Relay guardado neste computador (para ações em segundo plano).
    var relayBaseURL: URL? {
        if viaRelay, let baseURL { return baseURL }
        guard let host = server.hosts.first(where: { $0.contains("/s/") }) else { return nil }
        return URL(string: host)
    }

    private func probe(host: String, port: Int) async -> URL? {
        guard let url = Self.baseURL(host: host, port: port) else { return nil }
        let health = url.appendingPathComponent("health")
        var req = URLRequest(url: health)
        req.timeoutInterval = 2.5
        guard let (data, resp) = try? await session.data(for: req),
              (resp as? HTTPURLResponse)?.statusCode == 200,
              let body = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              body["id"] as? String == server.id else { return nil }
        return url
    }

    private func adopt(_ url: URL, host: String) async {
        await update {
            $0.baseURL = url
            $0.server.lastHost = host
            $0.problem = nil
        }
    }

    // MARK: Estado e eventos

    func request(_ path: String, method: String = "GET") -> URLRequest? {
        guard let base = baseURL, let url = URL(string: path, relativeTo: base) else { return nil }
        var r = URLRequest(url: url)
        r.httpMethod = method
        r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        return r
    }

    func fetchState() async {
        guard let req = request("/v1/state") else { return }
        guard let (data, resp) = try? await session.data(for: req) else { return }
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if code == 401 {
            await update { $0.problem = "Este iPhone já não está pareado com \($0.server.name). Leia o QR de novo." }
            return
        }
        if code == 200, let st = try? JSONDecoder().decode(DeckState.self, from: data) {
            await update { $0.state = st }
            await learnHosts(st)
        }
    }

    /// Pelo túnel, o stream de eventos fica retido pela Cloudflare até ao fim da resposta. Em vez
    /// dele, long-poll: o servidor segura cada pedido até o estado mudar (ou 25 s).
    private func poll() async {
        var versao = ""
        while !Task.isCancelled {
            guard var req = request("/v1/state?espera=\(versao)") else { return }
            req.timeoutInterval = 40
            guard let (data, resp) = try? await session.data(for: req) else {
                if !Task.isCancelled {
                    await update { $0.problem = "Ligação a \($0.server.name) perdida. Reconectando…" }
                }
                return
            }
            let http = resp as? HTTPURLResponse
            if http?.statusCode == 401 {
                await update { $0.problem = "Este iPhone já não está pareado com \($0.server.name). Leia o QR de novo." }
                return
            }
            guard http?.statusCode == 200, let st = try? JSONDecoder().decode(DeckState.self, from: data) else { return }
            versao = http?.value(forHTTPHeaderField: "X-Towerman-Versao") ?? ""
            await update {
                $0.state = st
                $0.connected = true
                $0.problem = nil
            }
            await learnHosts(st)
            // Servidor sem long-poll (sem versão): responde logo; não martelar.
            if versao.isEmpty { try? await Task.sleep(nanoseconds: 3_000_000_000) }
        }
    }

    private func stream() async {
        guard var req = request("/v1/events") else { return }
        // O servidor manda um ping a cada 15 s; 40 s sem bytes é uma ligação morta.
        req.timeoutInterval = 40
        req.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        do {
            let (bytes, resp) = try await session.bytes(for: req)
            guard (resp as? HTTPURLResponse)?.statusCode == 200 else { return }
            await update {
                $0.connected = true
                $0.problem = nil
            }
            var evento = ""
            for try await line in bytes.lines {
                if line.hasPrefix("event: ") {
                    evento = String(line.dropFirst(7)).trimmingCharacters(in: .whitespaces)
                } else if line.hasPrefix("data: "), evento == "state",
                          let data = String(line.dropFirst(6)).data(using: .utf8),
                          let st = try? JSONDecoder().decode(DeckState.self, from: data) {
                    await update { $0.state = st }
                    await learnHosts(st)
                }
            }
        } catch {
            if !Task.isCancelled {
                await update { $0.problem = "Ligação a \($0.server.name) perdida. Reconectando…" }
            }
        }
    }

    // MARK: Relay WebSocket E2EE

    /// Mantém o WebSocket cifrado ponta a ponta aberto apenas enquanto o app está ativo.
    /// Ao fechar/bloquear o ecrã, `stop()` fecha o socket e o Relay avisa o Mac (`viewers = 0`).
    private func streamRelay() async {
        guard let base = baseURL,
              let deviceId = server.deviceId, !deviceId.isEmpty,
              let serverPK = server.serverPublicKey, !serverPK.isEmpty,
              let symKey = RelayCrypto.symmetricKey(serverPublicKeyBase64: serverPK, serverId: server.id),
              var comps = URLComponents(url: base.appendingPathComponent("ws"), resolvingAgainstBaseURL: false) else {
            await update { $0.problem = "Pareie novamente com \($0.server.name) para ativar a chave E2EE." }
            return
        }
        comps.scheme = (comps.scheme == "https") ? "wss" : "ws"
        comps.queryItems = [
            URLQueryItem(name: "role", value: "viewer"),
            URLQueryItem(name: "device", value: deviceId)
        ]
        guard let wsURL = comps.url else { return }

        let task = session.webSocketTask(with: wsURL)
        wsTask = task
        task.resume()
        await update {
            $0.connected = true
            $0.problem = nil
        }

        defer {
            task.cancel(with: .goingAway, reason: nil)
            if wsTask === task { wsTask = nil }
        }

        while !Task.isCancelled {
            do {
                let message = try await task.receive()
                let raw: Data?
                switch message {
                case .string(let s): raw = s.data(using: .utf8)
                case .data(let d): raw = d
                @unknown default: raw = nil
                }
                guard let raw,
                      let env = try? JSONSerialization.jsonObject(with: raw) as? [String: Any],
                      env["type"] as? String == "state",
                      let payload = env["payload"] as? String,
                      let clear = RelayCrypto.open(payload, using: symKey, deviceId: deviceId),
                      let st = try? JSONDecoder().decode(DeckState.self, from: clear) else {
                    continue
                }
                await update {
                    $0.state = st
                    $0.connected = true
                    $0.problem = nil
                }
                await learnHosts(st)
            } catch {
                if !Task.isCancelled {
                    await update { $0.problem = "Ligação ao Relay de \($0.server.name) perdida. Reconectando…" }
                }
                return
            }
        }
    }

    /// Executa uma ação no computador: diretamente na LAN/Tailscale ou cifrada via Relay E2EE.
    func performAction(agentId: String, action: String, body: [String: Any]? = nil) async -> (Int, Data)? {
        if !viaRelay, let reqBase = request("/v1/agents/\(agentId)/\(action)", method: "POST") {
            var req = reqBase
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            if let body { req.httpBody = try? JSONSerialization.data(withJSONObject: body) }
            if let (data, resp) = try? await session.data(for: req) {
                return ((resp as? HTTPURLResponse)?.statusCode ?? 0, data)
            }
        }
        guard let relayURL = relayBaseURL,
              let deviceId = server.deviceId, !deviceId.isEmpty,
              let serverPK = server.serverPublicKey, !serverPK.isEmpty,
              let symKey = RelayCrypto.symmetricKey(serverPublicKeyBase64: serverPK, serverId: server.id) else {
            return nil
        }
        var clearObj: [String: Any] = ["agent_id": agentId, "action": action]
        if let body {
            for (k, v) in body { clearObj[k] = v }
        }
        guard let clearData = try? JSONSerialization.data(withJSONObject: clearObj),
              let sealed = RelayCrypto.seal(clearData, using: symKey, deviceId: deviceId),
              let reqBody = try? JSONSerialization.data(withJSONObject: ["device": deviceId, "payload": sealed]) else {
            return nil
        }
        var req = URLRequest(url: relayURL.appendingPathComponent("action"))
        req.httpMethod = "POST"
        req.timeoutInterval = 7
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = reqBody
        guard let (data, resp) = try? await session.data(for: req) else { return nil }
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if let env = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let payload = env["payload"] as? String,
           let clearResp = RelayCrypto.open(payload, using: symKey, deviceId: deviceId) {
            return (code, clearResp)
        }
        return (code, data)
    }

    /// Este computador aceita sessões lançadas à distância.
    var supportsLaunch: Bool { state?.capabilities.contains("launch") ?? false }

    /// Este computador calcula métricas (servidores anteriores à 0.8 não).
    var supportsMetrics: Bool { state?.capabilities.contains("metrics") ?? false }

    /// Ação do servidor, sem agente: `catalog` (GET /v1/launch/catalog), `launch` (POST /v1/launch)
    /// ou `metrics` (GET /v1/metrics?days=N, com `days` no corpo).
    /// Na rede local vai direta; fora dela, no envelope cifrado do relay com o campo `op`.
    func performServerAction(op: String, body: [String: Any]? = nil) async -> (Int, Data)? {
        if op == "metrics" {
            let days = (body?["days"] as? Int) ?? 7
            if !viaRelay, let req = request("/v1/metrics?days=\(days)", method: "GET"),
               let (data, resp) = try? await session.data(for: req) {
                return ((resp as? HTTPURLResponse)?.statusCode ?? 0, data)
            }
            return await sendRelay(["op": "metrics", "days": days], timeout: 15)
        }
        let local: (String, String) = op == "catalog" ? ("/v1/launch/catalog", "GET") : ("/v1/launch", "POST")
        if !viaRelay, let reqBase = request(local.0, method: local.1) {
            var req = reqBase
            req.timeoutInterval = 15
            if let body {
                req.setValue("application/json", forHTTPHeaderField: "Content-Type")
                req.httpBody = try? JSONSerialization.data(withJSONObject: body)
            }
            if let (data, resp) = try? await session.data(for: req) {
                return ((resp as? HTTPURLResponse)?.statusCode ?? 0, data)
            }
        }
        var clearObj: [String: Any] = ["op": op]
        if let body { clearObj["launch"] = body }
        return await sendRelay(clearObj, timeout: 15)
    }

    /// Sela o pedido com a chave deste aparelho e envia-o pelo relay; devolve a resposta aberta.
    private func sendRelay(_ clearObj: [String: Any], timeout: TimeInterval) async -> (Int, Data)? {
        guard let relayURL = relayBaseURL,
              let deviceId = server.deviceId, !deviceId.isEmpty,
              let serverPK = server.serverPublicKey, !serverPK.isEmpty,
              let symKey = RelayCrypto.symmetricKey(serverPublicKeyBase64: serverPK, serverId: server.id),
              let clearData = try? JSONSerialization.data(withJSONObject: clearObj),
              let sealed = RelayCrypto.seal(clearData, using: symKey, deviceId: deviceId),
              let reqBody = try? JSONSerialization.data(withJSONObject: ["device": deviceId, "payload": sealed]) else {
            return nil
        }
        var req = URLRequest(url: relayURL.appendingPathComponent("action"))
        req.httpMethod = "POST"
        req.timeoutInterval = timeout
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = reqBody
        guard let (data, resp) = try? await session.data(for: req) else { return nil }
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if let env = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let payload = env["payload"] as? String,
           let clearResp = RelayCrypto.open(payload, using: symKey, deviceId: deviceId) {
            return (code, clearResp)
        }
        return (code, data)
    }

    @MainActor private func apply(_ change: (ServerLink) -> Void) {
        change(self)
        onChange?(self)
    }

    private func update(_ change: @escaping (ServerLink) -> Void) async {
        await MainActor.run { apply(change) }
    }
}

// MARK: - Bonjour

/// Procura `_towerman._tcp` na rede local e devolve o endereço IPv4 do computador com este id.
enum BonjourFinder {
    static func find(serverId: String, timeout: TimeInterval = 4) async -> (String, Int)? {
        await withCheckedContinuation { cont in
            let browser = NWBrowser(for: .bonjourWithTXTRecord(type: "_towerman._tcp", domain: "local."), using: .tcp)
            let queue = DispatchQueue(label: "towerman.bonjour")
            var acabou = false
            var conexoes: [NWConnection] = []
            let terminar: ((String, Int)?) -> Void = { r in
                guard !acabou else { return }
                acabou = true
                browser.cancel()
                conexoes.forEach { $0.cancel() }
                cont.resume(returning: r)
            }
            browser.browseResultsChangedHandler = { results, _ in
                for result in results {
                    guard case let .bonjour(txt) = result.metadata, txt["id"] == serverId else { continue }
                    // Resolver: ligar ao serviço e ler o endereço do outro lado.
                    let params = NWParameters.tcp
                    if let ip = params.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options {
                        ip.version = .v4
                    }
                    let c = NWConnection(to: result.endpoint, using: params)
                    conexoes.append(c)
                    c.stateUpdateHandler = { st in
                        if case .ready = st, case let .hostPort(host, port)? = c.currentPath?.remoteEndpoint {
                            var h = "\(host)"
                            if let i = h.firstIndex(of: "%") { h = String(h[..<i]) }
                            terminar((h, Int(port.rawValue)))
                        }
                    }
                    c.start(queue: queue)
                }
            }
            browser.start(queue: queue)
            queue.asyncAfter(deadline: .now() + timeout) { terminar(nil) }
        }
    }
}

// MARK: - Link de pareamento

/// O conteúdo do QR: towerman://pair?v=1&id=…&nome=…&porta=…&codigo=…&pk=…&h=host1,host2
public struct PairingLink {
    public let serverId: String
    public let name: String
    public let port: Int
    public let code: String
    public let serverPublicKey: String?
    public let hosts: [String]

    public init?(url: URL) {
        guard url.scheme == "towerman", url.host == "pair",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems else { return nil }
        let q = Dictionary(items.map { ($0.name, $0.value ?? "") }, uniquingKeysWith: { a, _ in a })
        guard let id = q["id"], !id.isEmpty, let code = q["codigo"], !code.isEmpty,
              let port = Int(q["porta"] ?? "") else { return nil }
        serverId = id
        name = q["nome"].flatMap { $0.isEmpty ? nil : $0 } ?? "Computador"
        self.port = port
        self.code = code
        serverPublicKey = q["pk"].flatMap { $0.isEmpty ? nil : $0 }
        hosts = (q["h"] ?? "").split(separator: ",").map(String.init).filter { !$0.isEmpty }
    }
}
