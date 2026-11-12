import AppKit
import UserNotifications

/// Notificações nativas do macOS para os pedidos dos agentes, com os botões do próprio pedido
/// (Permitir/Negar ou as opções da pergunta). Saem do ecrã quando o pedido é respondido noutro lado.
@MainActor
final class MacNotifier: NSObject, UNUserNotificationCenterDelegate {
    static let shared = MacNotifier()
    /// Pedido da notificação para abrir a janela do deck.
    static let openDeck = Notification.Name("towerman.openDeck")

    /// Quem envia ao computador a opção escolhida na notificação.
    var answer: ((_ agentId: String, _ promptId: String, _ key: String) async -> Bool)?

    private let center = UNUserNotificationCenter.current()
    private var notified: Set<String> = []
    private var categories: [String: UNNotificationCategory] = [:]

    func activate() {
        center.delegate = self
        center.requestAuthorization(options: [.alert, .sound, .badge]) { _, error in
            if let error { print("Notificações recusadas: \(error)") }
        }
    }

    func update(with agents: [Agent]) {
        var current: Set<String> = []
        for agent in agents {
            guard AgentState(agent) == .waiting, let prompt = agent.waiting else { continue }
            let id = "\(agent.id)#\(prompt.id)"
            current.insert(id)
            if !notified.contains(id) { post(agent, prompt, id: id) }
        }
        let gone = notified.subtracting(current)
        if !gone.isEmpty {
            center.removeDeliveredNotifications(withIdentifiers: Array(gone))
            center.removePendingNotificationRequests(withIdentifiers: Array(gone))
        }
        notified = current
    }

    private func post(_ agent: Agent, _ prompt: Prompt, id: String) {
        let canAnswer = agent.can("answer") && !prompt.options.isEmpty
        let categoryId = canAnswer ? "prompt-" + prompt.options.map(\.key).joined(separator: "|") : "prompt-none"
        if categories[categoryId] == nil {
            let actions = canAnswer ? prompt.options.prefix(4).map {
                UNNotificationAction(identifier: $0.key, title: $0.label, options: $0.key == "deny" ? [.destructive] : [])
            } : []
            categories[categoryId] = UNNotificationCategory(identifier: categoryId, actions: Array(actions),
                                                            intentIdentifiers: [], options: [])
            center.setNotificationCategories(Set(categories.values))
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

    // MARK: - UNUserNotificationCenterDelegate

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .sound, .list])
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let action = response.actionIdentifier
        guard let agentId = info["agent_id"] as? String, let promptId = info["prompt_id"] as? String else {
            completionHandler()
            return
        }
        Task { @MainActor in
            if action == UNNotificationDefaultActionIdentifier {
                NotificationCenter.default.post(name: Self.openDeck, object: nil)
            } else if action != UNNotificationDismissActionIdentifier, let answer = self.answer {
                _ = await answer(agentId, promptId, action)
            }
            completionHandler()
        }
    }
}
