import CryptoKit
import Foundation
import Testing
@testable import TerminatrKit

/// A phone with no key, or with one from before T820 (bound to Face ID),
/// makes a new key to pair with; one with a no-prompt key keeps it.
@Test func writeKeyPlan() {
    #expect(WriteKeyPlan.forPairing(hasKey: false, open: false) == .make)
    #expect(WriteKeyPlan.forPairing(hasKey: true, open: false) == .make)
    #expect(WriteKeyPlan.forPairing(hasKey: true, open: true) == .keep)
}

/// A signer that asks nothing signs a message the key's public half checks.
@Test func softwareKeySignsWithoutAsking() async throws {
    let s = SoftwareSigner()
    let m = SignedWrite.message(method: "session.answer", nonce: Data(repeating: 1, count: 32), params: Data("{}".utf8))
    let sig = try await s.sign(m, why: SignedWrite.why("session.answer"))
    let pub = s.key.publicKey
    #expect(pub.isValidSignature(try P256.Signing.ECDSASignature(derRepresentation: sig), for: m))
}
