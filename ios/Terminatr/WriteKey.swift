import CryptoKit
import Foundation
import LocalAuthentication
import TerminatrKit

/// The phone's write key (docs/SPEC.md §16.15): a P-256 key in the
/// Secure Enclave that only Face ID or the passcode unlocks, made at
/// pairing, whose public half the server pins beside the device key. It
/// signs each act (an answer, an approval, a task's accept or send-back)
/// over the server's nonce, so the server can tell the act had the user
/// behind it, not just the phone. The Keychain keeps the enclave's
/// handle to it (dataRepresentation), on this device only; it never
/// leaves the enclave. The Simulator has no Secure Enclave: there it is
/// a software key in the Keychain, behind the same prompt when the
/// Simulator can give one.
enum WriteKey {
    /// The signer for the bridge (WriteSigner), its key made the first
    /// time it is asked for.
    static func signer() throws -> FaceIDSigner {
        if let d = try DeviceStore.writeKey() {
            return try FaceIDSigner(stored: d)
        }
        let s = try FaceIDSigner.make()
        try DeviceStore.saveWriteKey(s.stored)
        return s
    }

    /// The signer, when the phone made its key (it paired since the
    /// write key came, T791); nil for a phone paired before, which the
    /// server keeps at view until it pairs again.
    static func existing() -> FaceIDSigner? {
        guard let d = try? DeviceStore.writeKey() else { return nil }
        return try? FaceIDSigner(stored: d)
    }
}

/// Signs with the write key, asking for Face ID (or the passcode) each
/// time: the enclave won't use the key otherwise.
struct FaceIDSigner: WriteSigner {
    /// What the Keychain keeps: the enclave key's handle, or in the
    /// Simulator the software key itself.
    let stored: Data
    let publicKey: Data

    static func make() throws -> FaceIDSigner {
        #if targetEnvironment(simulator)
        let k = P256.Signing.PrivateKey()
        return FaceIDSigner(stored: k.rawRepresentation, publicKey: k.publicKey.x963Representation)
        #else
        var err: Unmanaged<CFError>?
        guard let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
                                                           [.privateKeyUsage, .userPresence], &err) else {
            throw err!.takeRetainedValue() as Error
        }
        let k = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access)
        return FaceIDSigner(stored: k.dataRepresentation, publicKey: k.publicKey.x963Representation)
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
        // The enclave's prompt blocks its thread until the user answers.
        return try await Task.detached(priority: .userInitiated) {
            let ctx = LAContext()
            ctx.localizedReason = why
            #if targetEnvironment(simulator)
            if ctx.canEvaluatePolicy(.deviceOwnerAuthentication, error: nil) {
                try await ctx.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: why)
            }
            return try P256.Signing.PrivateKey(rawRepresentation: stored).signature(for: message).derRepresentation
            #else
            let k = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: stored, authenticationContext: ctx)
            return try k.signature(for: message).derRepresentation
            #endif
        }.value
    }
}
