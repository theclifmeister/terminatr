import CryptoKit
import Foundation
import Security
import TerminatrKit
import TerminatrLink

/// What the phone keeps of its pairings, in the Keychain, on this device
/// only (never in a backup or iCloud Keychain), readable after the first
/// unlock. One pairing per server (T852), by the server's id (Pairings):
/// its own device key's seed, which that server pinned, the server (its
/// name, relay and key), and its own write key's handle (WriteKey), so
/// no two servers see the same key from this phone. An index keeps the
/// ids in the order they were paired.
enum DeviceStore {
    private static let service = "dev.terminatr.ios.device"
    private static let indexAccount = "pairings"
    private static let keyAccount = "identity"
    private static let serverAccount = "server"
    private static let writeKeyAccount = "write-key"
    /// Present when the write key was made without Face ID (T820); a key
    /// without it is the older kind, bound to Face ID.
    private static let writeKeyOpenAccount = "write-key-open"
    private static let accounts = [keyAccount, serverAccount, writeKeyAccount, writeKeyOpenAccount]

    /// A server's id as the store keys it: the SHA-256 of its key in hex,
    /// as its pairing code names it.
    static func id(of s: DevicePairing.Server) -> String {
        hex(LinkIdentity.id(of: s.endpoint.serverKey))
    }

    static func id(of c: PairingCode) -> String {
        hex(c.server)
    }

    private static func hex(_ d: Data) -> String {
        d.map { String(format: "%02x", $0) }.joined()
    }

    /// The ids of the servers paired, in the order they were. A phone
    /// paired before T852 kept one server under plain accounts: it moves
    /// under its id, its keys as they were (the server pinned them), and
    /// its link's log with it.
    static func pairings() -> [String] {
        migrate()
        guard let d = try? read(indexAccount), let ids = try? JSONDecoder().decode([String].self, from: d) else { return [] }
        return ids
    }

    private static func setPairings(_ ids: [String]) throws {
        try write(indexAccount, JSONEncoder().encode(ids))
    }

    private static func migrate() {
        guard let d = ((try? read(serverAccount)) ?? nil),
              let s = try? JSONDecoder().decode(DevicePairing.Server.self, from: d) else { return }
        let pid = id(of: s)
        do {
            for a in accounts {
                if let v = try read(a) { try write(a + ":" + pid, v) }
            }
            let ids = ((try? read(indexAccount)) ?? nil).flatMap { try? JSONDecoder().decode([String].self, from: $0) } ?? []
            try setPairings(Pairings.adding(pid, to: ids))
        } catch {
            return
        }
        for a in accounts { SecItemDelete(query(a) as CFDictionary) }
        LinkLog.move(to: pid)
    }

    /// The device key for pairing pid: made and kept the first time it is
    /// asked for, so a new server gets a key of its own and one paired
    /// again keeps the key it pinned.
    static func identity(_ pid: String) throws -> LinkIdentity {
        if let seed = try read(keyAccount + ":" + pid) {
            return try LinkIdentity(seed: seed)
        }
        let id = LinkIdentity()
        try write(keyAccount + ":" + pid, id.key.rawRepresentation)
        return id
    }

    static func server(_ pid: String) -> DevicePairing.Server? {
        guard let d = ((try? read(serverAccount + ":" + pid)) ?? nil) else { return nil }
        return try? JSONDecoder().decode(DevicePairing.Server.self, from: d)
    }

    /// Keeps s, paired: added last, or in its place when it was paired.
    static func save(_ s: DevicePairing.Server) throws {
        let pid = id(of: s)
        try write(serverAccount + ":" + pid, JSONEncoder().encode(s))
        try setPairings(Pairings.adding(pid, to: pairings()))
    }

    /// Pairing pid's write key's handle (WriteKey), if it was made.
    static func writeKey(_ pid: String) throws -> Data? {
        try read(writeKeyAccount + ":" + pid)
    }

    /// Keeps a new write key for pid, made without Face ID, and marks it so.
    static func saveWriteKey(_ d: Data, _ pid: String) throws {
        SecItemDelete(query(writeKeyOpenAccount + ":" + pid) as CFDictionary)
        try write(writeKeyAccount + ":" + pid, d)
        try write(writeKeyOpenAccount + ":" + pid, Data([1]))
    }

    /// Whether pid's write key is of the kind that signs with no prompt.
    static func writeKeyIsOpen(_ pid: String) -> Bool {
        ((try? read(writeKeyOpenAccount + ":" + pid)) ?? nil) != nil
    }

    /// Forgets the server pid and its keys: pairing with it again makes
    /// new ones, which it has to take afresh. The other servers stay.
    static func forget(_ pid: String) {
        for a in accounts { SecItemDelete(query(a + ":" + pid) as CFDictionary) }
        try? setPairings(Pairings.removing(pid, from: pairings()))
        LinkLog.forget(pairing: pid)
    }

    /// Keys made for a pairing that never finished (refused, timed out),
    /// for a server not paired: gone, so the next try starts afresh.
    static func dropUnpaired(_ pid: String) {
        guard !pairings().contains(pid) else { return }
        for a in accounts { SecItemDelete(query(a + ":" + pid) as CFDictionary) }
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
