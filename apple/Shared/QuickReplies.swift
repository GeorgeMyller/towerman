import Foundation

/// Frases prontas para mandar a um agente com um gesto (arrastar para a esquerda). A lista é
/// editada no iPhone e segue para o relógio junto com o estado.
public enum QuickReplies {
    public static let key = "quick_replies"
    public static let defaults = ["Continua", "Sim, avança", "Faz commit", "Explica melhor"]

    public static func load() -> [String] {
        let list = UserDefaults.standard.stringArray(forKey: key) ?? defaults
        return list.isEmpty ? defaults : list
    }

    public static func save(_ list: [String]) {
        let clean = list.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
        UserDefaults.standard.set(clean, forKey: key)
    }

    /// Guarda a lista que veio do iPhone numa mensagem do WatchConnectivity.
    public static func absorb(_ payload: [String: Any]) {
        if let list = payload[key] as? [String], !list.isEmpty {
            UserDefaults.standard.set(list, forKey: key)
        }
    }
}
