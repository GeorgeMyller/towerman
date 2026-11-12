import Foundation

/// Identificadores da Apple de quem compila: vêm do Info.plist de cada alvo, preenchido em tempo
/// de build por Config/Towerman.xcconfig (e Config/Local.xcconfig). Nada de IDs fixos no código.
public enum TowermanIDs {
    /// App Group partilhado pelo app do relógio, pelos widgets e pelos intents.
    public static let appGroup = value("TowermanAppGroup", fallback: "group.com.example.towerman")

    /// Base dos serviços do Keychain (.token, .e2ee, .e2ee.server) e do grupo partilhado (.shared).
    public static let sharedID = value("TowermanSharedID", fallback: "com.example.towerman")

    /// Lê a chave do Info.plist; sem ela (ou com a variável por expandir) usa o padrão neutro.
    private static func value(_ key: String, fallback: String) -> String {
        guard let v = Bundle.main.object(forInfoDictionaryKey: key) as? String,
              !v.isEmpty, !v.contains("$(") else { return fallback }
        return v
    }
}
