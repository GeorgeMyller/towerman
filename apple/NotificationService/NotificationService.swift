import Foundation
import UserNotifications

/// Decifra o push de uma decisão. O relay e a Apple só veem «Uma sessão espera uma decisão»;
/// a pergunta e as opções vêm em `enc`, cifradas para este aparelho (X25519 + AES-256-GCM, a
/// mesma chave do estado pelo relay). Sem chave ou com falha, mostra o texto genérico.
/// O push «terminou» usa o mesmo caminho: `enc` traz o título e o início da resposta.
final class NotificationService: UNNotificationServiceExtension {
    private var contentHandler: ((UNNotificationContent) -> Void)?
    private var content: UNMutableNotificationContent?

    private struct Decision: Decodable {
        let title: String
        let detail: String
        let options: [PromptOption]
    }

    /// Fim de passo: `kind` é sempre "done".
    private struct Done: Decodable {
        let kind: String
        let title: String
        let body: String
    }

    override func didReceive(_ request: UNNotificationRequest,
                             withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        self.contentHandler = contentHandler
        guard let content = request.content.mutableCopy() as? UNMutableNotificationContent else {
            contentHandler(request.content)
            return
        }
        self.content = content
        let info = content.userInfo
        guard let enc = info["enc"] as? String, let device = info["device"] as? String,
              let server = info["server"] as? String,
              let serverKey = RelayCrypto.serverKey(serverId: server),
              let key = RelayCrypto.symmetricKey(serverPublicKeyBase64: serverKey, serverId: server),
              let clear = RelayCrypto.open(enc, using: key, deviceId: device) else {
            contentHandler(content)
            return
        }
        if let done = try? JSONDecoder().decode(Done.self, from: clear), done.kind == "done" {
            if !done.title.isEmpty { content.title = done.title }
            if !done.body.isEmpty { content.body = done.body }
            contentHandler(content)
            return
        }
        guard let decision = try? JSONDecoder().decode(Decision.self, from: clear) else {
            contentHandler(content)
            return
        }
        content.title = decision.title
        content.subtitle = ""
        content.body = decision.detail
        let category = DecisionNotification.category(options: decision.options)
        content.categoryIdentifier = category.identifier
        DecisionNotification.register(category) {
            contentHandler(content)
        }
    }

    override func serviceExtensionTimeWillExpire() {
        if let contentHandler, let content {
            contentHandler(content)
        }
    }
}
