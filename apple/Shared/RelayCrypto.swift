import CryptoKit
import Foundation
import Security

/// Criptografia ponta a ponta (X25519 + HKDF-SHA256 + AES-256-GCM) para o Relay WebSocket.
/// O formato coincide bit a bit com `internal/relay/cripto.go` no servidor Go.
public enum RelayCrypto {
    private static let info = Data("towerman-e2ee-v1".utf8)
    private static let keyService = TowermanIDs.sharedID + ".e2ee"
    private static let keyAccount = "device-x25519"
    private static let fallbackDefaultsKey = "e2ee.device.x25519"
    private static let serverKeyService = TowermanIDs.sharedID + ".e2ee.server"

    /// Grupo de Keychain partilhado com a extensão de notificação, que decifra as decisões no
    /// push. Só no iPhone; vem do Info.plist (`TowermanKeychainGroup`, com o prefixo da equipa).
    private static var sharedGroup: String? {
        #if os(iOS)
        return Bundle.main.object(forInfoDictionaryKey: "TowermanKeychainGroup") as? String
        #else
        return nil
        #endif
    }

    /// Lê um item do Keychain em qualquer grupo acessível (o antigo, do app, ou o partilhado).
    private static func readItem(service: String, account: String) -> Data? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess else { return nil }
        return item as? Data
    }

    /// Grava no grupo partilhado (quando existe); devolve falso se o Keychain recusar.
    @discardableResult
    private static func writeItem(_ data: Data, service: String, account: String) -> Bool {
        var add: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock
        ]
        if let group = sharedGroup { add[kSecAttrAccessGroup as String] = group }
        SecItemDelete(add as CFDictionary)
        add[kSecValueData as String] = data
        return SecItemAdd(add as CFDictionary, nil) == errSecSuccess
    }

    /// Migração: a chave criada antes da extensão vive no grupo do app; copia-a para o partilhado.
    private static func ensureShared(_ raw: Data) {
        guard let group = sharedGroup else { return }
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keyService,
            kSecAttrAccount as String: keyAccount,
            kSecAttrAccessGroup as String: group
        ]
        if SecItemCopyMatching(query as CFDictionary, nil) != errSecSuccess {
            writeItem(raw, service: keyService, account: keyAccount)
        }
    }

    /// Guarda a chave pública de um computador pareado, para a extensão de notificação decifrar.
    public static func storeServerKey(_ publicKeyBase64: String, serverId: String) {
        guard readItem(service: serverKeyService, account: serverId) != Data(publicKeyBase64.utf8) else { return }
        writeItem(Data(publicKeyBase64.utf8), service: serverKeyService, account: serverId)
    }

    /// Chave pública guardada de um computador pareado.
    public static func serverKey(serverId: String) -> String? {
        readItem(service: serverKeyService, account: serverId).flatMap { String(data: $0, encoding: .utf8) }
    }

    /// Devolve o par de chaves X25519 deste aparelho (guardado no Keychain).
    public static func devicePrivateKey() -> Curve25519.KeyAgreement.PrivateKey {
        if let raw = readItem(service: keyService, account: keyAccount),
           let key = try? Curve25519.KeyAgreement.PrivateKey(rawRepresentation: raw) {
            ensureShared(raw)
            return key
        }
        #if targetEnvironment(simulator)
        if let raw = UserDefaults.standard.data(forKey: fallbackDefaultsKey),
           let key = try? Curve25519.KeyAgreement.PrivateKey(rawRepresentation: raw) {
            return key
        }
        #endif

        let created = Curve25519.KeyAgreement.PrivateKey()
        let raw = created.rawRepresentation
        if !writeItem(raw, service: keyService, account: keyAccount) {
            #if targetEnvironment(simulator)
            UserDefaults.standard.set(raw, forKey: fallbackDefaultsKey)
            #endif
        }
        return created
    }

    /// Chave pública X25519 deste aparelho em base64url sem padding.
    public static var devicePublicKeyBase64: String {
        base64URLEncode(devicePrivateKey().publicKey.rawRepresentation)
    }

    /// Deriva a chave simétrica AES-256 (32 bytes) a partir da chave pública X25519 do Mac.
    public static func symmetricKey(serverPublicKeyBase64: String, serverId: String) -> SymmetricKey? {
        guard let pubData = base64URLDecode(serverPublicKeyBase64),
              let peerPub = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: pubData),
              let shared = try? devicePrivateKey().sharedSecretFromKeyAgreement(with: peerPub) else {
            return nil
        }
        return shared.hkdfDerivedSymmetricKey(
            using: SHA256.self,
            salt: Data(serverId.utf8),
            sharedInfo: info,
            outputByteCount: 32
        )
    }

    /// Cifra dados com AES-256-GCM autenticando o deviceId no AAD.
    public static func seal(_ plaintext: Data, using key: SymmetricKey, deviceId: String) -> String? {
        guard let box = try? AES.GCM.seal(plaintext, using: key, authenticating: Data(deviceId.utf8)),
              let combined = box.combined else {
            return nil
        }
        return base64URLEncode(combined)
    }

    /// Valida a tag AES-256-GCM e decifra um payload base64url(nonce || ciphertext || tag).
    public static func open(_ payloadBase64: String, using key: SymmetricKey, deviceId: String) -> Data? {
        guard let combined = base64URLDecode(payloadBase64),
              let box = try? AES.GCM.SealedBox(combined: combined) else {
            return nil
        }
        return try? AES.GCM.open(box, using: key, authenticating: Data(deviceId.utf8))
    }

    public static func base64URLEncode(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    public static func base64URLDecode(_ text: String) -> Data? {
        var b64 = text.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        let rem = b64.count % 4
        if rem > 0 {
            b64 += String(repeating: "=", count: 4 - rem)
        }
        return Data(base64Encoded: b64)
    }
}

