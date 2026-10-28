import Foundation
import Security
import TerminatrLink

/// What the phone keeps of its pairing, in the Keychain, on this device
/// only (never in a backup or iCloud Keychain), readable after the first
/// unlock: the device key's seed, which the server pinned, the server it
/// paired with (its name, relay and key), and the write key's handle
/// (WriteKey). One server for now.
enum DeviceStore {
    private static let service = "dev.terminatr.ios.device"
    private static let keyAccount = "identity"
    private static let serverAccount = "server"
    private static let writeKeyAccount = "write-key"
    /// Present when the write key was made without Face ID (T820); a key
    /// without it is the older kind, bound to Face ID.
    private static let writeKeyOpenAccount = "write-key-open"

    /// The device's key, made and kept the first time it is asked for.
    static func identity() throws -> LinkIdentity {
        if let seed = try read(keyAccount) {
            return try LinkIdentity(seed: seed)
        }
        let id = LinkIdentity()
        try write(keyAccount, id.key.rawRepresentation)
        return id
    }

    static func server() -> DevicePairing.Server? {
        guard let d = try? read(serverAccount) else { return nil }
        return try? JSONDecoder().decode(DevicePairing.Server.self, from: d)
    }

    static func save(_ s: DevicePairing.Server) throws {
        try write(serverAccount, JSONEncoder().encode(s))
    }

    /// The write key's handle (WriteKey), if it was made.
    static func writeKey() throws -> Data? {
        try read(writeKeyAccount)
    }

    /// Keeps a new write key, made without Face ID, and marks it so.
    static func saveWriteKey(_ d: Data) throws {
        SecItemDelete(query(writeKeyOpenAccount) as CFDictionary)
        try write(writeKeyAccount, d)
        try write(writeKeyOpenAccount, Data([1]))
    }

    /// Whether the kept write key is of the kind that signs with no prompt.
    static func writeKeyIsOpen() -> Bool {
        ((try? read(writeKeyOpenAccount)) ?? nil) != nil
    }

    /// Forgets the server and the keys: pairing again makes new ones,
    /// which the server has to take afresh.
    static func forget() {
        for a in [serverAccount, keyAccount, writeKeyAccount, writeKeyOpenAccount] {
            SecItemDelete(query(a) as CFDictionary)
        }
    }

    struct Failure: Error, LocalizedError {
        let status: OSStatus
        var errorDescription: String? {
            "The Keychain refused: \(SecCopyErrorMessageString(status, nil) as String? ?? "error \(status)")."
        }
    }

    private static func query(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: account,
         kSecAttrSynchronizable as String: false]
    }

    private static func read(_ account: String) throws -> Data? {
        var q = query(account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        let st = SecItemCopyMatching(q as CFDictionary, &out)
        if st == errSecItemNotFound { return nil }
        guard st == errSecSuccess else { throw Failure(status: st) }
        return out as? Data
    }

    private static func write(_ account: String, _ data: Data) throws {
        SecItemDelete(query(account) as CFDictionary)
        var q = query(account)
        q[kSecValueData as String] = data
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let st = SecItemAdd(q as CFDictionary, nil)
        guard st == errSecSuccess else { throw Failure(status: st) }
    }
}
