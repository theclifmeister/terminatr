import CryptoKit
import Foundation
import NIOCore
import NIOSSL

/// The link's TLS, as internal/peer/tls.go has it: 1.3 only, each end
/// presenting its own self-signed Ed25519 certificate and accepting only
/// the other's pinned key, never a CA's.
enum PinnedTLS {
    static func context(identity: LinkIdentity, client: Bool) throws -> NIOSSLContext {
        let cert = try NIOSSLCertificate(bytes: identity.certificate(name: "tm phone"), format: .der)
        let key = try NIOSSLPrivateKey(bytes: identity.pkcs8, format: .der)
        var c = client ? TLSConfiguration.makeClientConfiguration()
            : TLSConfiguration.makeServerConfiguration(certificateChain: [.certificate(cert)], privateKey: .privateKey(key))
        c.certificateChain = [.certificate(cert)]
        c.privateKey = .privateKey(key)
        c.minimumTLSVersion = .tlsv13
        c.maximumTLSVersion = .tlsv13
        // The callback below is the verification; it needs a mode that
        // asks for the other end's certificate.
        c.certificateVerification = .noHostnameVerification
        c.trustRoots = .certificates([])
        // BoringSSL leaves Ed25519 out of its defaults.
        c.verifySignatureAlgorithms = [.ed25519]
        c.signingSignatureAlgorithms = [.ed25519]
        return try NIOSSLContext(configuration: c)
    }

    /// The handler for one end of the link: the TLS client is the end
    /// with the lower server id (peer.tlsClient).
    static func handler(identity: LinkIdentity, peerKey: Data) throws -> ChannelHandler {
        let client = identity.id.lexicographicallyPrecedes(LinkIdentity.id(of: peerKey))
        let ctx = try context(identity: identity, client: client)
        let verify = pinned(peerKey)
        if client {
            return try NIOSSLClientHandler(context: ctx, serverHostname: nil, customVerificationCallback: verify)
        }
        return NIOSSLServerHandler(context: ctx, customVerificationCallback: verify)
    }

    /// Accepts exactly one certificate, whose key is want. The handshake
    /// proves the other end holds its private key.
    static func pinned(_ want: Data) -> NIOSSLCustomVerificationCallback {
        let spki = DER.spki(want)
        return { certs, promise in
            guard certs.count == 1, let got = try? certs[0].extractPublicKey().toSPKIBytes(), got == spki else {
                promise.succeed(.failed)
                return
            }
            promise.succeed(.certificateVerified)
        }
    }

    /// The outer TLS to a relay: by its key's pin (the hex SHA-256 of its
    /// SubjectPublicKeyInfo, relay.CertPin) when its URL has one, else by
    /// the system's CAs.
    static func relay(host: String, pin: String?) throws -> ChannelHandler {
        var c = TLSConfiguration.makeClientConfiguration()
        c.minimumTLSVersion = .tlsv12
        let sni = isIP(host) ? nil : host
        guard let pin else {
            return try NIOSSLClientHandler(context: NIOSSLContext(configuration: c), serverHostname: sni)
        }
        c.certificateVerification = .noHostnameVerification
        c.trustRoots = .certificates([])
        return try NIOSSLClientHandler(context: NIOSSLContext(configuration: c), serverHostname: sni) { certs, promise in
            guard let leaf = certs.first, let spki = try? leaf.extractPublicKey().toSPKIBytes(),
                  RelayWire.hex(Data(SHA256.hash(data: spki))) == pin else {
                promise.succeed(.failed)
                return
            }
            promise.succeed(.certificateVerified)
        }
    }

    static func isIP(_ host: String) -> Bool {
        var a4 = in_addr(), a6 = in6_addr()
        return inet_pton(AF_INET, host, &a4) == 1 || inet_pton(AF_INET6, host, &a6) == 1
    }
}
