import CryptoKit
import Foundation

/// The phone's Ed25519 identity: the key it signs relay hellos with and
/// presents in the link's TLS, as a server's identity.key does
/// (internal/peer/identity.go). Its id is the key's SHA-256.
public struct LinkIdentity: Sendable {
    public let key: Curve25519.Signing.PrivateKey

    public init(key: Curve25519.Signing.PrivateKey = .init()) { self.key = key }

    /// The 32-byte seed, as a keychain keeps it.
    public init(seed: Data) throws { key = try .init(rawRepresentation: seed) }

    public var publicKey: Data { key.publicKey.rawRepresentation }
    public var id: Data { LinkIdentity.id(of: publicKey) }

    /// A key's server id: SHA-256 of the raw public key.
    public static func id(of publicKey: Data) -> Data { Data(SHA256.hash(data: publicKey)) }

    /// The key in PKCS #8 DER, for BoringSSL.
    var pkcs8: [UInt8] {
        DER.sequence([
            DER.integer(0),
            DER.sequence([DER.ed25519]),
            DER.octetString(DER.octetString(Array(key.rawRepresentation))),
        ])
    }

    /// A self-signed certificate for the key. As internal/peer/tls.go has
    /// it, nobody checks its name or dates: the other end pins the key.
    func certificate(name: String) throws -> [UInt8] {
        let cn = DER.sequence([DER.set([DER.sequence([DER.oid([0x55, 0x04, 0x03]), DER.utf8(name)])])])
        let tbs = DER.sequence([
            DER.explicit(0, DER.integer(2)),
            DER.integer(1),
            DER.sequence([DER.ed25519]),
            cn,
            DER.sequence([DER.time(0x17, "700101000000Z"), DER.time(0x18, "99991231000000Z")]),
            cn,
            DER.spki(publicKey),
        ])
        let sig = try key.signature(for: Data(tbs))
        return DER.sequence([tbs, DER.sequence([DER.ed25519]), DER.bitString(Array(sig))])
    }
}

/// The room two keys meet in on a relay (relay.LinkRoom): the same for
/// both ends, whichever dials.
public func linkRoom(_ a: Data, _ b: Data) -> Data {
    let (lo, hi) = a.lexicographicallyPrecedes(b) ? (a, b) : (b, a)
    var h = SHA256()
    h.update(data: Data("tm-relay room v1\n".utf8))
    h.update(data: lo)
    h.update(data: hi)
    return Data(h.finalize())
}

/// The little DER the certificate and key need.
enum DER {
    static let ed25519 = oid([0x2B, 0x65, 0x70]) // 1.3.101.112

    static func tlv(_ tag: UInt8, _ body: [UInt8]) -> [UInt8] {
        var len: [UInt8]
        if body.count < 0x80 {
            len = [UInt8(body.count)]
        } else {
            var n = body.count
            len = []
            while n > 0 {
                len.insert(UInt8(n & 0xFF), at: 0)
                n >>= 8
            }
            len.insert(0x80 | UInt8(len.count), at: 0)
        }
        return [tag] + len + body
    }

    static func sequence(_ parts: [[UInt8]]) -> [UInt8] { tlv(0x30, parts.flatMap { $0 }) }
    static func set(_ parts: [[UInt8]]) -> [UInt8] { tlv(0x31, parts.flatMap { $0 }) }
    static func integer(_ v: UInt8) -> [UInt8] { tlv(0x02, [v]) }
    static func oid(_ body: [UInt8]) -> [UInt8] { tlv(0x06, body) }
    static func octetString(_ b: [UInt8]) -> [UInt8] { tlv(0x04, b) }
    static func bitString(_ b: [UInt8]) -> [UInt8] { tlv(0x03, [0] + b) }
    static func utf8(_ s: String) -> [UInt8] { tlv(0x0C, Array(s.utf8)) }
    static func time(_ tag: UInt8, _ s: String) -> [UInt8] { tlv(tag, Array(s.utf8)) }
    static func explicit(_ n: UInt8, _ inner: [UInt8]) -> [UInt8] { tlv(0xA0 | n, inner) }

    /// An Ed25519 SubjectPublicKeyInfo.
    static func spki(_ pub: Data) -> [UInt8] { sequence([sequence([ed25519]), bitString(Array(pub))]) }
}
