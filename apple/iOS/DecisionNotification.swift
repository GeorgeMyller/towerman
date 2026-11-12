import CryptoKit
import Foundation
import UserNotifications

/// Categorias de notificação das decisões em texto livre («Digite CONTINUAR», menu, «Posso
/// começar?»): um botão por opção e «Responder» por texto. Usado pelo app e pela extensão de
/// notificação, que regista a categoria antes de mostrar um push de decisão.
enum DecisionNotification {
    /// Ação de texto: o iOS e o relógio pedem o texto (teclado, ditado) e o app envia-o como input.
    static let replyAction = "responder"

    static func category(options: [PromptOption]) -> UNNotificationCategory {
        // Até 3 opções ativas mais «Responder»: o relógio e o ecrã bloqueado mostram 4 ações.
        let active = options.filter { !$0.disabled }.prefix(3)
        var actions: [UNNotificationAction] = active.map {
            UNNotificationAction(identifier: $0.key, title: $0.label, options: [])
        }
        actions.append(UNTextInputNotificationAction(identifier: replyAction, title: "Responder", options: [],
                                                     textInputButtonTitle: "Enviar", textInputPlaceholder: "Resposta"))
        let assinatura = active.map { $0.key + "=" + $0.label }.joined(separator: "|")
        let hash = SHA256.hash(data: Data(assinatura.utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
        return UNNotificationCategory(identifier: "decisao-\(hash)", actions: actions, intentIdentifiers: [], options: [])
    }

    /// Junta a categoria às já registadas (o app e a extensão registam categorias em momentos
    /// diferentes; substituir o conjunto apagaria os botões das notificações já entregues).
    static func register(_ category: UNNotificationCategory, done: (() -> Void)? = nil) {
        let center = UNUserNotificationCenter.current()
        center.getNotificationCategories { existing in
            var set = existing.filter { $0.identifier != category.identifier }
            set.insert(category)
            center.setNotificationCategories(set)
            done?()
        }
    }
}
