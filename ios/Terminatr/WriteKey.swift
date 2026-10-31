import CryptoKit
import Foundation
import TerminatrKit

/// The phone's write key (docs/SPEC.md §16.15): a P-256 key in the
/// Secure Enclave, made at pairing, whose public half the server pins
/// beside the device key. It signs each act (an answer, an approval, a
/// task's accept or send-back) over the server's nonce, so the server
/// knows the act came from this phone's key. The enclave won't give the
/// key up and it asks nothing to sign (no Face ID, no passcode): the
/// Keychain keeps the enclave's handle to it (dataRepresentation), on
/// this device only. The Simulator has no Secure Enclave: there it is a
/// software key in the Keychain.
///
/// Keys made before T820 were bound to Face ID (.userPresence). The
/// server takes a write key only at pairing, so such a phone keeps
/// signing with its old key, and the system asks as before, until it
/// pairs again, which makes a new key (see `forPairing`). Each server
/// paired has a key of its own (T852).
enum WriteKey {
    /// The signer to pair with server pid: its key kept when it is of
    /// the current kind, else a new one, which the pairing hands the
    /// server and keep saves once that server took it (a pairing that
    /// fails leaves the key it had).
    static func forPairing(_ pid: String) throws -> (signer: DeviceSigner, new: Bool) {
        switch WriteKeyPlan.forPairing(hasKey: try DeviceStore.writeKey(pid) != nil, open: DeviceStore.writeKeyIsOpen(pid)) {
        case .keep:
            if let d = try DeviceStore.writeKey(pid) { return (try DeviceSigner(stored: d), false) }
            return (try DeviceSigner.make(), true)
        case .make: return (try DeviceSigner.make(), true)
        }
    }

    /// Keeps s as server pid's write key: it paired with it.
    static func keep(_ s: DeviceSigner, _ pid: String) throws {
        try DeviceStore.saveWriteKey(s.stored, pid)
    }

    /// Server pid's signer, when the phone made its key (it paired since
    /// the write key came, T791); nil for a phone paired before, which
    /// the server keeps at view until it pairs again.
    static func existing(_ pid: String) -> DeviceSigner? {
        guard let d = ((try? DeviceStore.writeKey(pid)) ?? nil) else { return nil }
        return try? DeviceSigner(stored: d)
    }
}

/// Signs with the write key, with no prompt of its own.
struct DeviceSigner: WriteSigner {
    /// What the Keychain keeps: the enclave key's handle, or in the
    /// Simulator the software key itself.
    let stored: Data
    let publicKey: Data

    static func make() throws -> DeviceSigner {
        #if targetEnvironment(simulator)
        let k = P256.Signing.PrivateKey()
        return DeviceSigner(stored: k.rawRepresentation, publicKey: k.publicKey.x963Representation)
        #else
        var err: Unmanaged<CFError>?
        guard let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
                                                           [.privateKeyUsage], &err) else {
            throw err!.takeRetainedValue() as Error
        }
        let k = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access)
        return DeviceSigner(stored: k.dataRepresentation, publicKey: k.publicKey.x963Representation)
        #endif
    }

    init(stored: Data) throws {
        self.stored = stored
        #if targetEnvironment(simulator)
        publicKey = try P256.Signing.PrivateKey(rawRepresentation: stored).publicKey.x963Representation
        #else
        publicKey = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: stored).publicKey.x963Representation
        #endif
    }

    private init(stored: Data, publicKey: Data) {
        self.stored = stored
        self.publicKey = publicKey
    }

    func sign(_ message: Data, why: String) async throws -> Data {
        let stored = self.stored
        // A key from before T820 makes the enclave block its thread on
        // the system's prompt, so not the caller's.
        return try await Task.detached(priority: .userInitiated) {
            #if targetEnvironment(simulator)
            return try P256.Signing.PrivateKey(rawRepresentation: stored).signature(for: message).derRepresentation
            #else
            let k = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: stored)
            return try k.signature(for: message).derRepresentation
            #endif
        }.value
    }
}
