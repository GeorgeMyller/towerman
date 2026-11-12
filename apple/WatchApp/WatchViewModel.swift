import Foundation
import WatchConnectivity
import WatchKit
import Combine
import WidgetKit
import UserNotifications

/// ViewModel para a interface do Apple Watch.
/// Recebe dados do iPhone via WCSession e dispara alertas táteis.
public final class WatchViewModel: NSObject, ObservableObject, WCSessionDelegate, UNUserNotificationCenterDelegate {
    @Published public var agents: [Agent] = []
    @Published public var isConnectedToPhone: Bool = false
    @Published public var lastUpdated: Date?
    /// Computadores que aceitam lançamentos e últimos combos usados, vindos do iPhone.
    @Published public var launchServers: [LaunchServer] = []
    @Published public var launchRecent: [LaunchShortcut] = []
    /// Catálogos já lidos nesta abertura, por computador.
    @Published public var catalogs: [String: LaunchCatalog] = [:]
    /// O iPhone não fala com nenhum computador: a frase dele, para o relógio mostrar o mesmo.
    @Published public var offlineNote: String?
    /// Pedidos que já fizeram vibrar o pulso e último estado de cada agente; sobrevive ao «sem
    /// contato» para o regresso não vibrar outra vez. Os avisos «terminou» já dados ficam também
    /// guardados no disco: o app acordado pela transferência de complicação não os repete.
    private var alertMemory: WatchAlertMemory? = {
        guard let done = UserDefaults.standard.dictionary(forKey: "done_seen") as? [String: String] else { return nil }
        return WatchAlertMemory(done: done)
    }()
    /// Categorias de notificação registadas para permitir responder com botões e toque duplo.
    private var notificationCategories: [String: UNNotificationCategory] = [:]

    public override init() {
        super.init()
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        center.requestAuthorization(options: [.alert, .sound]) { _, _ in }
        if WCSession.isSupported() {
            let session = WCSession.default
            session.delegate = self
            session.activate()
        }
    }

    // MARK: - Solicitação de Estado ao iPhone
    public func requestStateFromPhone() {
        guard WCSession.isSupported() else { return }
        let session = WCSession.default
        if session.activationState != .activated {
            session.activate()
            return
        }
        
        // Verifica se há dados no context já recebidos
        absorb(session.receivedApplicationContext)
        self.absorbState(session.receivedApplicationContext)

        // Se o iPhone estiver acessível, pede os dados atualizados
        if session.isReachable {
            session.sendMessage(["action": "request_state"], replyHandler: { [weak self] reply in
                self?.absorb(reply)
                self?.absorbState(reply)
            }, errorHandler: { error in
                print("Erro ao solicitar estado do iPhone: \(error)")
            })
        }
    }

    /// Atualização pedida pela pessoa: dá vibração e devolve uma frase quando falha.
    public func refresh(done: @escaping (Bool, String?) -> Void) {
        guard WCSession.isSupported() else {
            done(false, "Sem ligação ao iPhone.")
            return
        }
        let session = WCSession.default
        if session.activationState != .activated {
            session.activate()
            done(false, "A ligar ao iPhone…")
            return
        }
        guard session.isReachable else {
            absorb(session.receivedApplicationContext)
            absorbState(session.receivedApplicationContext)
            isConnectedToPhone = false
            WKInterfaceDevice.current().play(.failure)
            done(false, "iPhone fora de alcance.")
            return
        }

        WKInterfaceDevice.current().play(.click)
        session.sendMessage(["action": "request_state"], replyHandler: { [weak self] reply in
            self?.absorb(reply)
            self?.absorbState(reply)
            DispatchQueue.main.async {
                WKInterfaceDevice.current().play(.success)
                done(true, nil)
            }
        }, errorHandler: { error in
            DispatchQueue.main.async {
                WKInterfaceDevice.current().play(.failure)
                done(false, error.localizedDescription)
            }
        })
    }

    /// Só uma falha de alcance garante que a mensagem não chegou ao iPhone. Noutra falha (prazo da
    /// resposta esgotado) o iPhone pode já ter agido: reenviar escreveria o texto duas vezes.
    private static func notDelivered(_ error: Error) -> Bool {
        (error as? WCError)?.code == .notReachable
    }

    // MARK: - Envio de Resposta ao iPhone
    /// Envia a resposta ao iPhone; `done(true)` quando o servidor a aceitou.
    public func sendAnswer(agentId: String, promptId: String, key: String, done: ((Bool) -> Void)? = nil) {
        let message: [String: Any] = [
            "action": "answer",
            "agent_id": agentId,
            "prompt_id": promptId,
            "key": key
        ]

        WKInterfaceDevice.current().play(.click)
        let session = WCSession.default

        if session.isReachable {
            session.sendMessage(message, replyHandler: { reply in
                let ok = reply["success"] as? Bool ?? false
                DispatchQueue.main.async {
                    WKInterfaceDevice.current().play(ok ? .success : .failure)
                    done?(ok)
                }
            }) { error in
                guard Self.notDelivered(error) else {
                    DispatchQueue.main.async {
                        WKInterfaceDevice.current().play(.failure)
                        done?(false)
                    }
                    return
                }
                session.transferUserInfo(message)
                DispatchQueue.main.async {
                    WKInterfaceDevice.current().play(.success)
                    done?(true)
                }
            }
        } else {
            session.transferUserInfo(message)
            DispatchQueue.main.async {
                WKInterfaceDevice.current().play(.success)
                done?(true)
            }
        }
    }

    public func interruptAgent(agentId: String) {
        let message: [String: Any] = [
            "action": "interrupt",
            "agent_id": agentId
        ]

        WKInterfaceDevice.current().play(.click)
        let session = WCSession.default
        if session.isReachable {
            session.sendMessage(message, replyHandler: nil, errorHandler: { _ in
                session.transferUserInfo(message)
            })
        } else {
            session.transferUserInfo(message)
        }
    }

    /// Pede ao iPhone uma ação no computador ("focus" ou "input") e devolve a frase do resultado.
    public func act(_ action: String, agentId: String, text: String? = nil, done: @escaping (Bool, String) -> Void) {
        var message: [String: Any] = ["action": action, "agent_id": agentId]
        if let text { message["text"] = text }
        WKInterfaceDevice.current().play(.click)
        let session = WCSession.default

        if session.isReachable {
            session.sendMessage(message, replyHandler: { reply in
                let ok = reply["success"] as? Bool ?? false
                let note = reply["message"] as? String ?? (ok ? "Feito." : "Não foi possível.")
                DispatchQueue.main.async {
                    WKInterfaceDevice.current().play(ok ? .success : .failure)
                    done(ok, note)
                }
            }) { error in
                guard Self.notDelivered(error) else {
                    DispatchQueue.main.async {
                        WKInterfaceDevice.current().play(.failure)
                        done(false, "Sem confirmação do iPhone. Veja no computador antes de repetir.")
                    }
                    return
                }
                session.transferUserInfo(message)
                DispatchQueue.main.async {
                    WKInterfaceDevice.current().play(.success)
                    done(true, "Enviado em segundo plano.")
                }
            }
        } else {
            session.transferUserInfo(message)
            DispatchQueue.main.async {
                WKInterfaceDevice.current().play(.success)
                done(true, "Enviado em segundo plano.")
            }
        }
    }

    public func focusAgent(agentId: String) {
        let message: [String: Any] = [
            "action": "focus",
            "agent_id": agentId
        ]

        WKInterfaceDevice.current().play(.click)
        let session = WCSession.default
        if session.isReachable {
            session.sendMessage(message, replyHandler: nil) { _ in
                session.transferUserInfo(message)
            }
        } else {
            session.transferUserInfo(message)
        }
    }

    // MARK: - Lançar sessões (pelo iPhone, que fala com o computador)

    /// Respostas rápidas e dados de lançamento que vêm do iPhone.
    private func absorb(_ dict: [String: Any]) {
        QuickReplies.absorb(dict)
        let servers = (dict["launch_servers"] as? Data).flatMap { try? JSONDecoder().decode([LaunchServer].self, from: $0) }
        let recent = (dict["launch_recent"] as? Data).flatMap { try? JSONDecoder().decode([LaunchShortcut].self, from: $0) }
        guard servers != nil || recent != nil else { return }
        DispatchQueue.main.async {
            if let servers { self.launchServers = servers }
            if let recent { self.launchRecent = recent }
        }
    }

    public func loadCatalog(serverId: String, done: @escaping (String?) -> Void) {
        guard WCSession.default.isReachable else {
            done("Abra o Towerman no iPhone.")
            return
        }
        WCSession.default.sendMessage(["action": "launch_catalog", "server_id": serverId], replyHandler: { [weak self] reply in
            let cat = (reply["catalog"] as? Data).flatMap { try? JSONDecoder().decode(LaunchCatalog.self, from: $0) }
            DispatchQueue.main.async {
                if let cat { self?.catalogs[serverId] = cat }
                done(cat == nil ? (reply["message"] as? String ?? "O computador não respondeu.") : nil)
            }
        }) { error in
            DispatchQueue.main.async { done(error.localizedDescription) }
        }
    }

    /// Lança a sessão; o iPhone força o modo default para pedidos vindos do relógio.
    public func launch(_ combo: LaunchShortcut, task: String, done: @escaping (Bool, String, String?) -> Void) {
        guard WCSession.default.isReachable, let data = try? JSONEncoder().encode(combo) else {
            done(false, "Abra o Towerman no iPhone.", nil)
            return
        }
        WKInterfaceDevice.current().play(.click)
        WCSession.default.sendMessage(["action": "launch", "combo": data, "task": task], replyHandler: { reply in
            let ok = reply["success"] as? Bool ?? false
            let note = reply["message"] as? String ?? (ok ? "Sessão lançada." : "Não foi possível lançar.")
            let id = (reply["session_id"] as? String).flatMap { $0.isEmpty ? nil : $0 }
            DispatchQueue.main.async {
                WKInterfaceDevice.current().play(ok ? .success : .failure)
                done(ok, note, id)
            }
        }) { error in
            DispatchQueue.main.async {
                WKInterfaceDevice.current().play(.failure)
                done(false, error.localizedDescription, nil)
            }
        }
    }

    // MARK: - Processamento de Estado

    /// Estado novo do iPhone, ou o aviso de que ele não fala com nenhum computador.
    private func absorbState(_ dict: [String: Any]) {
        if let data = dict["deck_state"] as? Data {
            processIncomingState(data: data)
        } else if let note = dict["deck_offline"] as? String {
            DispatchQueue.main.async {
                self.offlineNote = note
                self.agents = []
                self.lastUpdated = Date()
                if TowerSummary.empty.save() {
                    WidgetCenter.shared.reloadAllTimelines()
                }
            }
        }
    }

    private func processIncomingState(data: Data) {
        guard let state = try? JSONDecoder().decode(DeckState.self, from: data) else { return }

        DispatchQueue.main.async {
            // Pedido novo em espera, ou agente que estava a trabalhar e concluiu ou falhou:
            // alerta tátil no pulso. A ferramenta de cada passo não vibra.
            let done = WatchSyncPolicy.freshlyDone(after: self.alertMemory, agents: state.agents).first
            // O pedido que faz vibrar é o novo; um pedido antigo ainda em espera já foi avisado.
            let seenWaiting = self.alertMemory?.waiting ?? []
            let newWaiting = state.agents.first { $0.isWaiting && !seenWaiting.contains($0.id) }
            let (vibra, memory) = WatchSyncPolicy.alert(after: self.alertMemory, agents: state.agents)
            if memory.done != self.alertMemory?.done {
                UserDefaults.standard.set(memory.done, forKey: "done_seen")
            }
            self.alertMemory = memory
            if vibra {
                WKInterfaceDevice.current().play(.notification)
                self.triggerHapticNotification(waiting: newWaiting, done: done)
            }

            self.offlineNote = nil
            self.agents = state.agents
            self.lastUpdated = Date()
            // Widget e Smart Stack leem o resumo do App Group; só recarregam quando muda.
            if TowerSummary(agents: state.agents).save() {
                WidgetCenter.shared.reloadAllTimelines()
            }
        }
    }

    /// Dispara notificação local no Apple Watch com som e haptic tátil.
    /// Funciona mesmo quando o app do relógio está em segundo plano.
    /// `waiting` é o agente com pedido novo; `done` o agente com aviso «terminou» novo. Cada um
    /// tem a sua notificação: um não esconde o outro.
    private func triggerHapticNotification(waiting: Agent?, done: Agent?) {
        if let waiting, let p = waiting.waiting {
            postPrompt(agent: waiting, prompt: p)
        }
        if let done, let doneAt = done.doneAt {
            postDone(agent: done, doneAt: doneAt)
        }
    }

    private func postPrompt(agent waiting: Agent, prompt p: Prompt) {
        let center = UNUserNotificationCenter.current()
        let content = UNMutableNotificationContent()
        content.title = waiting.title.isEmpty ? "Towerman" : waiting.title
        content.body = !p.detail.isEmpty ? p.detail : (p.title.isEmpty ? "Decisão pendente" : p.title)
        content.userInfo = ["agent_id": waiting.id, "prompt_id": p.id]
        if !p.options.isEmpty {
            // Ordena com a opção principal primeiro (ativa pelo gesto rápido / toque duplo)
            let ordered = p.options.filter(\.primary) + p.options.filter { !$0.primary }
            let categoryId = "watch-prompt-" + ordered.map(\.key).joined(separator: "|")
            if notificationCategories[categoryId] == nil {
                let actions = ordered.prefix(4).map { opt in
                    UNNotificationAction(identifier: opt.key, title: opt.label,
                                         options: opt.key == "deny" ? [.destructive] : [])
                }
                let category = UNNotificationCategory(identifier: categoryId, actions: Array(actions),
                                                      intentIdentifiers: [], options: [])
                notificationCategories[categoryId] = category
                center.setNotificationCategories(Set(notificationCategories.values))
            }
            content.categoryIdentifier = categoryId
        }
        content.sound = .default
        content.interruptionLevel = .timeSensitive
        // O mesmo pedido não gera duas notificações.
        center.add(UNNotificationRequest(identifier: "prompt#\(waiting.id)#\(p.id)", content: content, trigger: nil))
    }

    private func postDone(agent done: Agent, doneAt: String) {
        let content = UNMutableNotificationContent()
        content.title = done.title.isEmpty ? "Towerman" : done.title
        content.subtitle = "Terminou"
        let reply = (done.lastReply ?? "").split(whereSeparator: \.isWhitespace).joined(separator: " ")
        content.body = reply.isEmpty ? "O agente terminou e espera o seu próximo pedido." : String(reply.prefix(140))
        content.userInfo = ["agent_id": done.id]
        content.sound = .default
        content.interruptionLevel = .timeSensitive
        // O mesmo identificador da notificação do iPhone (PromptNotifier.doneIdentifier).
        UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: "done#\(done.id)#\(doneAt)", content: content, trigger: nil))
    }

    // MARK: - UNUserNotificationCenterDelegate (watchOS)

    public func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                       withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .sound, .list])
    }

    public func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                       withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let action = response.actionIdentifier

        guard let agentId = info["agent_id"] as? String, let promptId = info["prompt_id"] as? String,
              action != UNNotificationDefaultActionIdentifier,
              action != UNNotificationDismissActionIdentifier else {
            completionHandler()
            return
        }

        sendAnswer(agentId: agentId, promptId: promptId, key: action) { _ in
            completionHandler()
        }
    }

    // MARK: - WCSessionDelegate (watchOS)
    public func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        DispatchQueue.main.async {
            self.isConnectedToPhone = session.isReachable
            self.requestStateFromPhone()
        }
    }

    public func sessionReachabilityDidChange(_ session: WCSession) {
        DispatchQueue.main.async {
            self.isConnectedToPhone = session.isReachable
        }
    }

    public func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String : Any]) {
        absorb(applicationContext)
        absorbState(applicationContext)
    }

    public func session(_ session: WCSession, didReceiveUserInfo userInfo: [String : Any] = [:]) {
        absorb(userInfo)
        absorbState(userInfo)
    }

    public func session(_ session: WCSession, didReceiveMessage message: [String : Any]) {
        absorb(message)
        absorbState(message)
    }
}
