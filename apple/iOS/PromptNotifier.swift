import Foundation
import UIKit
import UserNotifications

/// Notificações locais e Push APNs com botões para os pedidos dos agentes.
/// Com o iPhone bloqueado e o relógio no pulso, o iOS reencaminha-as para o Apple Watch com os
/// mesmos botões.
public final class PromptNotifier: NSObject, UNUserNotificationCenterDelegate {
    public static let shared = PromptNotifier()

    /// Quem envia a resposta escolhida na notificação ao servidor.
    public weak var bridge: AgentDeckBridgeManager?

    /// Token APNs deste aparelho (hex), usado pelo Relay quando o ecrã está bloqueado.
    public private(set) var apnsToken: String? = UserDefaults.standard.string(forKey: "apns_device_token")

    private let center = UNUserNotificationCenter.current()
    private var notified: Set<String> = []
    /// Último aviso «terminou» já mostrado por agente; guardado para o app relançado não o repetir.
    private var doneSeen: [String: String] =
        UserDefaults.standard.dictionary(forKey: PromptNotifier.doneSeenKey) as? [String: String] ?? [:]
    private static let doneSeenKey = "done_seen"
    private var categories: [String: UNNotificationCategory] = [:]

    public func activate() {
        center.delegate = self
        registerDefaultCategory()
        #if DEBUG
        // Capturas no simulador: o alerta de autorização taparia o ecrã.
        if ProcessInfo.processInfo.environment["TOWERMAN_SNAPSHOT"] != nil { return }
        #endif
        center.requestAuthorization(options: [.alert, .sound, .badge]) { granted, error in
            if let error { print("Notificações recusadas: \(error)") }
            if !granted {
                print("Notificações não autorizadas")
                return
            }
            DispatchQueue.main.async {
                UIApplication.shared.registerForRemoteNotifications()
            }
        }
    }

    public func setAPNSToken(_ deviceToken: Data) {
        let hex = deviceToken.map { String(format: "%02x", $0) }.joined()
        guard !hex.isEmpty else { return }
        apnsToken = hex
        UserDefaults.standard.set(hex, forKey: "apns_device_token")
        bridge?.updatePushToken(hex)
    }

    private func registerDefaultCategory() {
        let categoryId = "prompt-allow|deny"
        let allow = UNNotificationAction(identifier: "allow", title: "Permitir", options: [])
        let deny = UNNotificationAction(identifier: "deny", title: "Negar", options: [.destructive])
        let cat = UNNotificationCategory(identifier: categoryId, actions: [allow, deny], intentIdentifiers: [], options: [])
        categories[categoryId] = cat
        DecisionNotification.register(cat)
    }

    // MARK: - Estado → notificações

    public func update(with state: DeckState) {
        var current: Set<String> = []
        for agent in state.agents {
            guard let prompt = agent.waiting, agent.can("answer") || prompt.options.isEmpty else { continue }
            let id = Self.identifier(agentId: agent.id, promptId: prompt.id)
            current.insert(id)
            if !notified.contains(id) {
                post(agent: agent, prompt: prompt, id: id)
            }
        }
        // Pedidos resolvidos noutro lado (terminal, relógio, outro aparelho) saem do ecrã.
        let gone = notified.subtracting(current)
        if !gone.isEmpty {
            center.removeDeliveredNotifications(withIdentifiers: Array(gone))
            center.removePendingNotificationRequests(withIdentifiers: Array(gone))
        }
        notified = current
        notifyDone(state.agents)
    }

    /// Aviso «terminou» sem push: o servidor marca `doneAt` quando a sessão para com a pessoa longe
    /// do computador. Com o iPhone bloqueado, o iOS espelha-o no relógio. Vale para o estado que
    /// chega pela Wi-Fi ou pelo relay.
    private func notifyDone(_ agents: [Agent]) {
        var changed = false
        for agent in agents {
            guard let doneAt = agent.doneAt else { continue }
            if WatchSyncPolicy.isFreshDone(agent, seen: doneSeen[agent.id], now: Date()) {
                postDone(agent: agent, doneAt: doneAt)
            }
            if doneSeen[agent.id] != doneAt {
                doneSeen[agent.id] = doneAt
                changed = true
            }
        }
        // Agentes que saíram deixam de ocupar a memória.
        let ids = Set(agents.map(\.id))
        if doneSeen.keys.contains(where: { !ids.contains($0) }) {
            doneSeen = doneSeen.filter { ids.contains($0.key) }
            changed = true
        }
        if changed { UserDefaults.standard.set(doneSeen, forKey: Self.doneSeenKey) }
    }

    private func postDone(agent: Agent, doneAt: String) {
        let content = UNMutableNotificationContent()
        content.title = agent.host.isEmpty ? agent.title : "\(agent.title) · \(agent.host)"
        content.subtitle = "Terminou"
        let reply = (agent.lastReply ?? "").split(whereSeparator: \.isWhitespace).joined(separator: " ")
        content.body = reply.isEmpty ? "O agente terminou e espera o seu próximo pedido." : String(reply.prefix(140))
        content.sound = .default
        content.interruptionLevel = .timeSensitive
        content.userInfo = ["agent_id": agent.id]
        center.add(UNNotificationRequest(identifier: Self.doneIdentifier(agentId: agent.id, doneAt: doneAt),
                                         content: content, trigger: nil))
    }

    /// O relógio usa o mesmo identificador na notificação que ele próprio publica.
    static func doneIdentifier(agentId: String, doneAt: String) -> String {
        "done#\(agentId)#\(doneAt)"
    }

    private func post(agent: Agent, prompt: Prompt, id: String) {
        var categoryId = "prompt-\(prompt.options.map(\.key).joined(separator: "|"))"
        if prompt.isDecision {
            // Decisão em texto livre: botões com o texto de cada opção e «Responder» por texto.
            let category = DecisionNotification.category(options: prompt.options)
            categoryId = category.identifier
            if categories[categoryId] == nil {
                categories[categoryId] = category
                DecisionNotification.register(category)
            }
        } else if categories[categoryId] == nil {
            // A primeira ação é a do toque duplo no relógio: a opção principal (aprovar) vem à frente.
            let ordered = prompt.options.filter(\.primary) + prompt.options.filter { !$0.primary }
            let actions = ordered.prefix(4).map { option in
                UNNotificationAction(identifier: option.key, title: option.label,
                                     options: option.key == "deny" ? [.destructive] : [])
            }
            let category = UNNotificationCategory(identifier: categoryId, actions: Array(actions),
                                                  intentIdentifiers: [], options: [])
            categories[categoryId] = category
            DecisionNotification.register(category)
        }

        let content = UNMutableNotificationContent()
        content.title = agent.host.isEmpty ? agent.title : "\(agent.title) · \(agent.host)"
        content.subtitle = prompt.title
        content.body = prompt.detail
        content.sound = .default
        content.categoryIdentifier = categoryId
        content.interruptionLevel = .timeSensitive
        content.userInfo = ["agent_id": agent.id, "prompt_id": prompt.id]
        center.add(UNNotificationRequest(identifier: id, content: content, trigger: nil))
    }

    static func identifier(agentId: String, promptId: String) -> String {
        "\(agentId)#\(promptId)"
    }

    // MARK: - UNUserNotificationCenterDelegate

    public func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                       withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .sound, .list])
    }

    public func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                       withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        // Toque na notificação: abre o cartão da sessão.
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier, let agentId = info["agent_id"] as? String, !agentId.isEmpty {
            DispatchQueue.main.async {
                NotificationCenter.default.post(name: .towerOpenAgent, object: agentId)
            }
        }
        guard let agentId = info["agent_id"] as? String, let promptId = info["prompt_id"] as? String,
              response.actionIdentifier != UNNotificationDefaultActionIdentifier,
              response.actionIdentifier != UNNotificationDismissActionIdentifier,
              let bridge else {
            completionHandler()
            return
        }
        if let text = (response as? UNTextInputNotificationResponse)?.userText {
            // «Responder» numa decisão: o texto ditado ou escrito vai ao terminal como input.
            Task {
                let r = await bridge.sendInput(agentId: agentId, text: text)
                if !r.ok { print("Resposta de texto pela notificação falhou (\(agentId)): \(r.message)") }
                completionHandler()
            }
            return
        }
        Task {
            let ok = await bridge.sendAnswer(agentId: agentId, promptId: promptId, key: response.actionIdentifier)
            if !ok { print("Resposta pela notificação falhou (\(agentId) \(promptId))") }
            completionHandler()
        }
    }
}

extension Notification.Name {
    /// Pedido para abrir o cartão de uma sessão (object: id do agente).
    static let towerOpenAgent = Notification.Name("towerman.open-agent")
}
