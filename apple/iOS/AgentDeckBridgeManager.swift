import Foundation
import UIKit
import WatchConnectivity
import Combine

/// Ponte do iPhone entre os computadores (Mac e Linux, pareados por QR) e o Apple Watch.
/// Mantém uma ligação por computador e junta os agentes de todos num só estado.
public final class AgentDeckBridgeManager: NSObject, ObservableObject, WCSessionDelegate {
    @Published public private(set) var servers: [PairedServer] = []
    @Published public private(set) var isConnected: Bool = false
    @Published public private(set) var currentState: DeckState?
    @Published public var watchReachable: Bool = false
    @Published public var errorMessage: String?
    /// A frase de «sem contato» que o relógio tem; nil quando ele tem estado.
    private var watchOfflineNote: String?
    /// O que o relógio desenha do último estado enviado; nil quando ele não tem lista.
    private var watchSentSignature: [WatchAgentSignature]?
    /// A mesma memória de alertas do relógio, do lado do iPhone: diz que envio vai fazer vibrar o
    /// pulso e merece a transferência de complicação, que acorda o app do relógio.
    private var watchAlertMemory: WatchAlertMemory?
    /// Último envio ao relógio e o envio adiado à espera do intervalo.
    private var watchLastSend: Date = .distantPast
    private var watchPendingSend: DispatchWorkItem?
    /// Mudanças sem estado novo juntam-se: no máximo um envio a cada 3 s.
    private static let watchThrottle: TimeInterval = 3
    /// Estado de cada computador, para a lista na configuração.
    @Published public private(set) var serverStatus: [String: ServerStatus] = [:]
    /// Computadores que aceitam sessões lançadas daqui (servidor com a capacidade `launch`).
    @Published public private(set) var launchServers: [LaunchServer] = []
    /// Últimos combos lançados: os atalhos do relógio.
    @Published public private(set) var launchRecent: [LaunchShortcut] = LaunchRecents.load()

    public struct ServerStatus: Equatable {
        public let connected: Bool
        public let address: String?
        public let problem: String?
        public let agents: Int
    }

    private var links: [String: ServerLink] = [:]
    /// Que computador corre cada agente: as ações vão para o servidor certo.
    private var agentServer: [String: String] = [:]
    private let session: URLSession
    private static let storeKey = "paired_servers"

    /// Verdadeiro entre "Conectar" e "Desconectar": o app volta a ligar sozinho enquanto for verdade.
    public private(set) var wantsConnection = false

    /// Verdadeiro quando há pelo menos um agente a trabalhar ou à espera de resposta.
    public var hasActiveAgents: Bool {
        guard let agents = currentState?.agents else { return false }
        return agents.contains { $0.isRunning || $0.isWaiting || $0.status == "starting" || $0.status == "waiting" }
    }

    public init(session: URLSession = .shared) {
        self.session = session
        super.init()
        PromptNotifier.shared.bridge = self
        loadServers()
        setupWatchConnectivity()
    }

    // MARK: - Computadores guardados

    private func loadServers() {
        if let data = UserDefaults.standard.data(forKey: Self.storeKey),
           let list = try? JSONDecoder().decode([PairedServer].self, from: data) {
            servers = list
        }
        // Migração: o gateway antigo (URL + token) passa a ser um computador manual.
        let d = UserDefaults.standard
        if servers.isEmpty, let url = d.string(forKey: "server_url"), let token = d.string(forKey: "server_token"),
           !token.isEmpty {
            let manual = PairedServer(id: "manual-\(url)", name: URL(string: url)?.host ?? "Gateway", hosts: [],
                                      port: 0, lastHost: nil, manualURL: url)
            TokenStore.save(token, for: manual.id)
            servers = [manual]
            saveServers()
        }
    }

    private func saveServers() {
        if let data = try? JSONEncoder().encode(servers) {
            UserDefaults.standard.set(data, forKey: Self.storeKey)
        }
        // A extensão de notificação decifra as decisões com a chave pública de cada computador.
        for server in servers {
            if let pk = server.serverPublicKey { RelayCrypto.storeServerKey(pk, serverId: server.id) }
        }
    }

    // MARK: - Ligação

    public func startListening() {
        wantsConnection = true
        BackgroundAudioKeeper.shared.update(wantsConnection: true)
        for server in servers {
            link(for: server).start()
        }
        refreshAggregate()
    }

    public func stopListening() {
        wantsConnection = false
        BackgroundAudioKeeper.shared.update(wantsConnection: false)
        links.values.forEach { $0.stop() }
        refreshAggregate()
    }

    /// Fecha as ligações WebSocket ao bloquear o iPhone se a ligação não for pretendida.
    /// Enquanto wantsConnection for verdadeiro, o áudio de fundo mantém o processo e as ligações vivas.
    public func suspendForBackground() {
        guard !wantsConnection else { return }
        guard links.values.contains(where: { $0.connected }) else { return }
        links.values.forEach { $0.stop() }
        refreshAggregate()
    }

    /// Envia o token APNs atualizado aos computadores pareados.
    public func updatePushToken(_ tokenHex: String) {
        Task {
            for server in servers {
                guard let reqBase = link(for: server).request("/v1/push-token", method: "POST") else { continue }
                var req = reqBase
                req.setValue("application/json", forHTTPHeaderField: "Content-Type")
                req.httpBody = try? JSONSerialization.data(withJSONObject: ["push_token": tokenHex])
                _ = try? await session.data(for: req)
            }
        }
    }

    public func fetchInitialState() async {
        for link in links.values {
            await link.fetchState()
        }
    }

    private func link(for server: PairedServer) -> ServerLink {
        if let l = links[server.id] { return l }
        let l = ServerLink(server: server, token: TokenStore.load(server.id) ?? "", session: session)
        l.onChange = { [weak self] link in self?.linkChanged(link) }
        links[server.id] = l
        return l
    }

    @MainActor private func linkChanged(_ link: ServerLink) {
        // Guardar o último endereço bom (o primeiro a tentar da próxima vez) e os endereços novos.
        if let i = servers.firstIndex(where: { $0.id == link.server.id }), servers[i].lastHost != link.server.lastHost || servers[i].hosts != link.server.hosts {
            servers[i].lastHost = link.server.lastHost
            servers[i].hosts = link.server.hosts
            saveServers()
        }
        refreshAggregate()
    }

    /// Junta os agentes de todos os computadores num só estado (o radar ganha um sector por máquina).
    private func refreshAggregate() {
        var agents: [Agent] = []
        var map: [String: String] = [:]
        var status: [String: ServerStatus] = [:]
        for server in servers {
            let l = links[server.id]
            let list = l?.state?.agents ?? []
            for a in list { map[a.id] = server.id }
            agents += list
            status[server.id] = ServerStatus(connected: l?.connected ?? false, address: l?.baseURL?.host,
                                             problem: l?.problem, agents: list.count)
        }
        agentServer = map
        serverStatus = status
        let lancam = servers.filter { links[$0.id]?.supportsLaunch == true }.map { LaunchServer(id: $0.id, name: $0.name) }
        if lancam != launchServers {
            launchServers = lancam
            if currentState != nil || watchOfflineNote != nil { sendStateToWatch(currentState) }
        }
        isConnected = links.values.contains { $0.connected }
        errorMessage = servers.isEmpty ? nil : (isConnected ? nil : links.values.compactMap(\.problem).first)
        let anyState = links.values.contains { $0.state != nil }
        if anyState {
            let info = ServerInfo(name: "Towerman", version: "app", time: ISO8601DateFormatter().string(from: Date()))
            let state = DeckState(server: info, agents: agents)
            if state != currentState {
                currentState = state
                PromptNotifier.shared.update(with: state)
                Task { @MainActor in TowerActivityController.shared.update(with: agents) }
                scheduleStateToWatch(state)
            }
        } else if currentState != nil {
            currentState = nil
        }
        // Sem estado de nenhum computador: o relógio larga a lista antiga e mostra a frase do iPhone.
        // Só depois de uma tentativa falhar, para o arranque não esvaziar o relógio à toa.
        if currentState == nil, let note = servers.isEmpty ? "Nenhum computador pareado." : errorMessage,
           note != watchOfflineNote {
            watchOfflineNote = note
            sendStateToWatch(nil)
        }
    }

    // MARK: - Pareamento

    /// Lê o link do QR, troca o código por um token deste iPhone e começa a ligar ao computador.
    @MainActor
    public func pair(with url: URL) async -> ActionResult {
        guard let pairing = PairingLink(url: url) else {
            return ActionResult(ok: false, message: "Este código não é de pareamento do Towerman.")
        }
        var payload: [String: String] = [
            "code": pairing.code,
            "device": UIDevice.current.name,
            "device_pk": RelayCrypto.devicePublicKeyBase64
        ]
        if let pushToken = PromptNotifier.shared.apnsToken, !pushToken.isEmpty {
            payload["push_token"] = pushToken
        }
        let body = try? JSONSerialization.data(withJSONObject: payload)
        var ultimoErro = "Não consegui chegar a \(pairing.name). O iPhone está na mesma Wi-Fi?"
        for host in pairing.hosts {
            guard let url = ServerLink.baseURL(host: host, port: pairing.port)?.appendingPathComponent("v1/pair") else { continue }
            var req = URLRequest(url: url)
            req.httpMethod = "POST"
            req.timeoutInterval = 4
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = body
            guard let (data, resp) = try? await session.data(for: req) else { continue }
            let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
            guard (resp as? HTTPURLResponse)?.statusCode == 200, let token = json["token"] as? String else {
                // O servidor respondeu e recusou: não adianta tentar os outros endereços.
                ultimoErro = (json["error"] as? String).map { "\(pairing.name) recusou: \($0)." } ?? ultimoErro
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
            saveServers()
            wantsConnection = true
            link(for: server).start()
            refreshAggregate()
            return ActionResult(ok: true, message: "\(name) pareado.")
        }
        return ActionResult(ok: false, message: ultimoErro)
    }

    /// Liga à mão a um gateway (servidores antigos sem QR).
    @MainActor
    public func addManual(url: String, token: String) {
        let clean = url.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !clean.isEmpty else { return }
        let server = PairedServer(id: "manual-\(clean)", name: URL(string: clean)?.host ?? clean, hosts: [], port: 0,
                                  lastHost: nil, manualURL: clean)
        TokenStore.save(token.trimmingCharacters(in: .whitespacesAndNewlines), for: server.id)
        links[server.id]?.stop()
        links[server.id] = nil
        servers.removeAll { $0.id == server.id }
        servers.append(server)
        saveServers()
        wantsConnection = true
        link(for: server).start()
        refreshAggregate()
    }

    @MainActor
    public func remove(_ server: PairedServer) {
        links[server.id]?.stop()
        links[server.id] = nil
        TokenStore.delete(server.id)
        servers.removeAll { $0.id == server.id }
        saveServers()
        refreshAggregate()
        if servers.isEmpty { currentState = nil }
    }

    // MARK: - Envio de estado para o Apple Watch

    private func setupWatchConnectivity() {
        if WCSession.isSupported() {
            let wc = WCSession.default
            wc.delegate = self
            wc.activate()
        }
    }

    /// A lista de respostas rápidas mudou: o relógio recebe-a já.
    public func syncQuickReplies() {
        if currentState != nil || watchOfflineNote != nil { sendStateToWatch(currentState) }
    }

    /// O estado vai em `deck_state`; sem estado, `deck_offline` leva a frase do iPhone.
    private func watchStatePayload(_ state: DeckState?) -> [String: Any]? {
        if let state {
            guard let encoded = try? JSONEncoder().encode(state) else { return nil }
            return ["deck_state": encoded]
        }
        return watchOfflineNote.map { ["deck_offline": $0] }
    }

    /// Só o que o ecrã do relógio mostra de cada agente (lista, detalhe, radar e widgets).
    /// Custo, janela de contexto, modelo, pasta e ramo ficam de fora: não acordam o relógio.
    private struct WatchAgentSignature: Equatable {
        let id: String
        let title: String
        let aiTitle: String
        let tool: String
        let provider: String
        let host: String
        let status: String
        let statusSince: String
        let unseen: Bool
        let activity: Activity?
        let lastPrompt: String?
        let lastReply: String?
        let waiting: Prompt?
        let capabilities: [String]
        let doneAt: String?

        init(_ a: Agent) {
            id = a.id; title = a.title; aiTitle = a.aiTitle; tool = a.tool; provider = a.provider
            host = a.host; status = a.status; statusSince = a.statusSince; unseen = a.unseen
            activity = a.activity; lastPrompt = a.lastPrompt; lastReply = a.lastReply
            waiting = a.waiting; capabilities = a.capabilities; doneAt = a.doneAt
        }

        /// Chave de urgência: muda quando o estado muda, um pedido entra, sai ou é trocado, ou
        /// chega um aviso «terminou».
        var urgencyKey: String {
            WatchSyncPolicy.urgencyKey(id: id, status: status, waitingId: waiting?.id, doneAt: doneAt)
        }
    }

    private func watchSignature(_ state: DeckState) -> [WatchAgentSignature] {
        state.agents.map(WatchAgentSignature.init)
    }

    /// Estado novo dos computadores. Estado de agente que muda (concluiu, ficou à espera, falhou),
    /// pedido novo ou resolvido, ou agente que entra ou sai: vai já, com mensagem direta se o relógio
    /// está ao alcance, ou com transferência que o acorda em segundo plano. O resto (a ferramenta
    /// de cada passo) junta-se e vai só no contexto, no máximo a cada 3 s: o rádio do relógio não
    /// acorda a cada passo.
    private func scheduleStateToWatch(_ state: DeckState) {
        let sig = watchSignature(state)
        guard sig != watchSentSignature else { return }
        if WatchSyncPolicy.isUrgent(sent: watchSentSignature.map { Set($0.map(\.urgencyKey)) },
                                    next: Set(sig.map(\.urgencyKey))) {
            sendStateToWatch(state)
            return
        }
        guard watchPendingSend == nil else { return }
        let wait = Self.watchThrottle - Date().timeIntervalSince(watchLastSend)
        if wait <= 0 {
            sendStateToWatch(state, live: false)
            return
        }
        // Envio no fim do intervalo, com o estado mais recente: o último passo chega sempre.
        let work = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.watchPendingSend = nil
            guard let latest = self.currentState, self.watchSignature(latest) != self.watchSentSignature else { return }
            self.sendStateToWatch(latest, live: false)
        }
        watchPendingSend = work
        DispatchQueue.main.asyncAfter(deadline: .now() + wait, execute: work)
    }

    /// `live` junta a mensagem direta quando o relógio está ao alcance e, fora de alcance, uma
    /// transferência de informação; sem ela vai só o contexto, que o sistema entrega quando lhe
    /// convém. Um envio que faz vibrar o pulso (pedido novo, aviso «terminou») vai, fora de
    /// alcance, como transferência de complicação: com a complicação do Towerman no mostrador, o
    /// watchOS acorda o app do relógio logo (até 50 por dia).
    private func sendStateToWatch(_ state: DeckState?, live: Bool = true) {
        guard WCSession.isSupported() else { return }
        let session = WCSession.default
        if session.activationState != .activated {
            session.activate()
        }
        if state != nil { watchOfflineNote = nil }
        var vibra = false
        if let state {
            (vibra, watchAlertMemory) = WatchSyncPolicy.alert(after: watchAlertMemory, agents: state.agents)
        }
        guard var context = watchStatePayload(state) else { return }
        watchPendingSend?.cancel()
        watchPendingSend = nil
        watchLastSend = Date()
        watchSentSignature = state.map(watchSignature)
        context[QuickReplies.key] = QuickReplies.load()
        if let recent = try? JSONEncoder().encode(launchRecent) { context["launch_recent"] = recent }
        if let lancam = try? JSONEncoder().encode(launchServers) { context["launch_servers"] = lancam }
        try? session.updateApplicationContext(context)
        guard live else { return }
        if session.isReachable {
            session.sendMessage(context, replyHandler: nil, errorHandler: nil)
        } else {
            // Só o estado mais recente interessa: as transferências ainda na fila saem. A da
            // complicação fica: é a que acorda o relógio para vibrar, e gastou orçamento.
            session.outstandingUserInfoTransfers.filter { !$0.isCurrentComplicationInfo }.forEach { $0.cancel() }
            if vibra && session.isComplicationEnabled && session.remainingComplicationUserInfoTransfers > 0 {
                session.transferCurrentComplicationUserInfo(context)
            } else {
                session.transferUserInfo(context)
            }
        }
    }

    // MARK: - Ações enviadas ao computador do agente

    /// Resultado de uma ação no computador, com a frase para mostrar a quem a pediu.
    public struct ActionResult {
        public let ok: Bool
        public let message: String
    }

    private func perform(agentId: String, action: String, body: [String: Any]? = nil) async -> (Int, Data)? {
        if let sid = agentServer[agentId], let l = links[sid] {
            return await l.performAction(agentId: agentId, action: action, body: body)
        }
        // Em segundo plano (notificação Push APNs), o estado pode ainda não estar carregado:
        // tenta nos computadores pareados.
        for server in servers {
            let l = link(for: server)
            if let res = await l.performAction(agentId: agentId, action: action, body: body),
               (200...299).contains(res.0) {
                return res
            }
        }
        return nil
    }

    private static func serverError(_ data: Data) -> String? {
        ((try? JSONSerialization.jsonObject(with: data)) as? [String: Any])?["error"] as? String
    }

    /// `origin` diz ao computador onde se respondeu (iphone ou watch); só alimenta as métricas.
    public func sendAnswer(agentId: String, promptId: String, key: String, origin: String = "iphone") async -> Bool {
        guard let (code, _) = await perform(agentId: agentId, action: "answer",
                                            body: ["promptId": promptId, "key": key, "origin": origin]) else { return false }
        return (200...299).contains(code)
    }

    public func interruptAgent(agentId: String) async -> Bool {
        guard let (code, _) = await perform(agentId: agentId, action: "interrupt") else { return false }
        return (200...299).contains(code)
    }

    public func focusAgent(agentId: String) async -> Bool {
        await focus(agentId: agentId).ok
    }

    /// Traz para a frente, no computador, o terminal da sessão.
    public func focus(agentId: String) async -> ActionResult {
        guard let (code, data) = await perform(agentId: agentId, action: "focus") else {
            return ActionResult(ok: false, message: "Sem ligação ao computador deste agente.")
        }
        if (200...299).contains(code) {
            return ActionResult(ok: true, message: "Terminal em primeiro plano no computador.")
        }
        return ActionResult(ok: false, message: Self.serverError(data) ?? "Não foi possível focar o terminal.")
    }

    /// Envia texto à sessão, como se fosse escrito no terminal.
    public func sendInput(agentId: String, text: String, origin: String = "iphone") async -> ActionResult {
        guard let (code, data) = await perform(agentId: agentId, action: "input", body: ["text": text, "origin": origin]) else {
            return ActionResult(ok: false, message: "Sem ligação ao computador deste agente.")
        }
        guard (200...299).contains(code) else {
            return ActionResult(ok: false, message: Self.serverError(data) ?? "Não foi possível enviar.")
        }
        let r = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
        if r["state"] as? String == "entregue" {
            return ActionResult(ok: true, message: "Entregue no terminal.")
        }
        let reason = (r["reason"] as? String) ?? ""
        return ActionResult(ok: true, message: reason.isEmpty ? "Na fila: entra quando o passo atual terminar." : "Na fila: \(reason).")
    }

    // MARK: - Lançar sessões à distância

    public struct LaunchResult {
        public let ok: Bool
        public let message: String
        public let sessionId: String?
    }

    /// O que este computador pode lançar (agentes, repos e papéis).
    public func catalog(serverId: String) async -> (LaunchCatalog?, String?) {
        guard let server = servers.first(where: { $0.id == serverId }) else { return (nil, "Computador desconhecido.") }
        guard let (code, data) = await link(for: server).performServerAction(op: "catalog") else {
            return (nil, "Sem ligação a \(server.name).")
        }
        guard code == 200, let cat = try? JSONDecoder().decode(LaunchCatalog.self, from: data) else {
            return (nil, Self.serverError(data) ?? "\(server.name) não devolveu o catálogo.")
        }
        return (cat, nil)
    }

    /// Lança uma sessão nova. O mesmo request_id vai na retentativa: o servidor não abre outra.
    public func launch(_ combo: LaunchShortcut, task: String, mode: String?, origin: String, branch: String? = nil) async -> LaunchResult {
        guard let server = servers.first(where: { $0.id == combo.serverId }) else {
            return LaunchResult(ok: false, message: "Computador desconhecido.", sessionId: nil)
        }
        var body: [String: Any] = ["request_id": UUID().uuidString, "origin": origin, "agent": combo.agent,
                                   "root": combo.root, "role": combo.role, "task": task]
        if let mode { body["mode"] = mode }
        if let branch, !branch.isEmpty { body["branch"] = branch }
        let l = link(for: server)
        var res = await l.performServerAction(op: "launch", body: body)
        if res == nil { res = await l.performServerAction(op: "launch", body: body) }
        guard let (code, data) = res else {
            return LaunchResult(ok: false, message: "Sem ligação a \(server.name).", sessionId: nil)
        }
        guard code == 200 else {
            return LaunchResult(ok: false, message: Self.serverError(data) ?? "\(server.name) recusou o lançamento.", sessionId: nil)
        }
        let r = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
        await MainActor.run {
            launchRecent = LaunchRecents.remember(combo)
            if let state = currentState { sendStateToWatch(state) }
        }
        await l.fetchState()
        return LaunchResult(ok: true, message: "Sessão \((r["name"] as? String) ?? "") a arrancar em \(server.name).",
                            sessionId: r["session_id"] as? String)
    }

    /// Fecha uma sessão aberta pelo Towerman; a worktree fica no computador.
    public func terminate(agentId: String) async -> ActionResult {
        guard let (code, data) = await perform(agentId: agentId, action: "terminate") else {
            return ActionResult(ok: false, message: "Sem ligação ao computador deste agente.")
        }
        if (200...299).contains(code) { return ActionResult(ok: true, message: "Sessão terminada.") }
        return ActionResult(ok: false, message: Self.serverError(data) ?? "Não foi possível terminar a sessão.")
    }

    // MARK: - Delegado do WCSession (iPhone)

    public func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        Task { @MainActor in
            self.watchReachable = session.isReachable
            if let state = self.currentState {
                self.sendStateToWatch(state)
            }
        }
    }

    public func sessionDidBecomeInactive(_ session: WCSession) {}

    public func sessionDidDeactivate(_ session: WCSession) {
        WCSession.default.activate()
    }

    public func sessionReachabilityDidChange(_ session: WCSession) {
        Task { @MainActor in
            self.watchReachable = session.isReachable
        }
    }

    public func session(_ session: WCSession, didReceiveMessage message: [String : Any], replyHandler: @escaping ([String : Any]) -> Void) {
        handleWatchAction(message, replyHandler: replyHandler)
    }

    public func session(_ session: WCSession, didReceiveUserInfo userInfo: [String : Any]) {
        handleWatchAction(userInfo, replyHandler: nil)
    }

    private func handleWatchAction(_ message: [String : Any], replyHandler: (([String : Any]) -> Void)? = nil) {
        if let action = message["action"] as? String, action == "request_state" {
            Task {
                if self.currentState == nil {
                    await self.fetchInitialState()
                }
                if var reply = self.watchStatePayload(self.currentState) {
                    reply[QuickReplies.key] = QuickReplies.load()
                    replyHandler?(reply)
                } else {
                    replyHandler?([:])
                }
            }
            return
        }

        // Lançar a partir do relógio: o iPhone fala com o computador e força o modo default.
        if let action = message["action"] as? String, action == "launch_catalog" {
            let serverId = message["server_id"] as? String ?? ""
            Task {
                let (cat, erro) = await self.catalog(serverId: serverId)
                if let cat, let data = try? JSONEncoder().encode(cat) {
                    replyHandler?(["success": true, "catalog": data])
                } else {
                    replyHandler?(["success": false, "message": erro ?? ""])
                }
            }
            return
        }
        if let action = message["action"] as? String, action == "launch",
           let data = message["combo"] as? Data, let combo = try? JSONDecoder().decode(LaunchShortcut.self, from: data),
           let task = message["task"] as? String {
            Task {
                let r = await self.launch(combo, task: task, mode: "default", origin: "watch")
                replyHandler?(["success": r.ok, "message": r.message, "session_id": r.sessionId ?? ""])
            }
            return
        }

        // Ações pedidas no Apple Watch.
        guard let action = message["action"] as? String, let agentId = message["agent_id"] as? String else {
            replyHandler?(["success": false])
            return
        }

        Task {
            var success = false
            var note = ""
            if action == "answer", let promptId = message["prompt_id"] as? String, let key = message["key"] as? String {
                success = await self.sendAnswer(agentId: agentId, promptId: promptId, key: key, origin: "watch")
            } else if action == "interrupt" {
                success = await self.interruptAgent(agentId: agentId)
            } else if action == "focus" {
                let r = await self.focus(agentId: agentId)
                success = r.ok
                note = r.message
            } else if action == "terminate" {
                let r = await self.terminate(agentId: agentId)
                success = r.ok
                note = r.message
            } else if action == "input", let text = message["text"] as? String {
                let r = await self.sendInput(agentId: agentId, text: text, origin: "watch")
                success = r.ok
                note = r.message
            }
            replyHandler?(["success": success, "message": note])
        }
    }
}
