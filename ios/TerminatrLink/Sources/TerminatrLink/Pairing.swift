import CryptoKit
import Foundation
import NIOCore
import NIOSSL
import NIOTransportServices

/// A device's pairing code (internal/device/pair.go), what tm device
/// pair's QR code holds:
/// terminatr://pair?v=1&relay=<URL>&secret=<16 digits>&server=<64 hex>.
public struct PairingCode: Sendable, Equatable {
    /// The relay both ends meet on, with its pin when it has one.
    public let relay: URL
    /// The one-time secret, 80 bits.
    public let secret: Data
    /// The server's id: the SHA-256 of the only key the phone takes.
    public let server: Data

    public static let scheme = "terminatr"

    public init(_ s: String) throws {
        guard let u = URLComponents(string: s.trimmingCharacters(in: .whitespacesAndNewlines)),
              u.scheme == Self.scheme, u.host == "pair" else {
            throw PairingCode.Bad("This isn't a terminatr pairing code. On your computer, tm device pair shows one.")
        }
        let q = Dictionary((u.queryItems ?? []).map { ($0.name, $0.value ?? "") }, uniquingKeysWith: { a, _ in a })
        guard q["v"] == "1" else {
            throw PairingCode.Bad("This pairing code is version \(q["v"] ?? "none"); this app reads 1. Update the app.")
        }
        guard let r = q["relay"].flatMap(Self.relayURL) else {
            throw PairingCode.Bad("The pairing code's relay isn't a wss:// URL.")
        }
        guard let sec = q["secret"].flatMap(Self.parseSecret) else {
            throw PairingCode.Bad("The pairing code's secret isn't 16 digits and letters.")
        }
        guard let id = q["server"].flatMap({ Data(hexString: $0) }), id.count == 32 else {
            throw PairingCode.Bad("The pairing code's server isn't 64 hex digits.")
        }
        relay = r
        secret = sec
        server = id
    }

    public struct Bad: Error, LocalizedError, Equatable {
        public let message: String
        init(_ m: String) { message = m }
        public var errorDescription: String? { message }
    }

    /// The relay as peer.RelayURL gives it: wss:// when it names none.
    static func relayURL(_ s: String) -> URL? {
        let t = s.contains("://") ? s : "wss://" + s
        guard let u = URL(string: t), let scheme = u.scheme, scheme == "wss" || scheme == "ws", u.host != nil else { return nil }
        return u
    }

    /// Crockford's base32, as peer.ParseSecret reads it: 16 digits,
    /// case, hyphens and spaces ignored, O for 0 and I or L for 1.
    static func parseSecret(_ s: String) -> Data? {
        let alphabet = Array("0123456789ABCDEFGHJKMNPQRSTVWXYZ")
        var out = Data()
        var acc: UInt64 = 0
        var bits = 0, n = 0
        for var c in s.uppercased() {
            switch c {
            case "-", " ": continue
            case "O": c = "0"
            case "I", "L": c = "1"
            default: break
            }
            guard let v = alphabet.firstIndex(of: c) else { return nil }
            n += 1
            if n > 16 { return nil }
            acc = acc << 5 | UInt64(v)
            bits += 5
            if bits >= 8 {
                bits -= 8
                out.append(UInt8(truncatingIfNeeded: acc >> UInt64(bits)))
            }
        }
        return n == 16 ? out : nil
    }
}

/// The phone's side of a device pairing (docs/SPEC.md §16.15,
/// device.Join): it joins the code's pair room, takes only the server
/// key the code names, proves it knows the secret and gives its name;
/// the server's user says yes there.
public enum DevicePairing {
    /// The server the phone paired with: what it keeps to link to it.
    public struct Server: Sendable, Codable, Equatable {
        public var name: String
        public var version: String
        public var endpoint: LinkTransport.Endpoint

        public init(name: String, version: String, endpoint: LinkTransport.Endpoint) {
            self.name = name
            self.version = version
            self.endpoint = endpoint
        }
    }

    /// The bound on the exchange once the room mates: the relay's two
    /// minutes for a mated pair room, which the user's yes is within.
    public static let timeout = Duration.seconds(120)

    /// Joins the pairing code names as identity, with the device's name,
    /// the app's version and its write key (an uncompressed P-256 point,
    /// which signs its acts; nil for none: the server lets it only view).
    /// It returns once the server's user said yes.
    public static func join(
        _ code: PairingCode, as identity: LinkIdentity, name: String, version: String, writeKey: Data? = nil,
        group: EventLoopGroup = NIOTSEventLoopGroup.singleton
    ) async throws -> Server {
        let (lines, sink) = AsyncThrowingStream<Data, any Error>.makeStream()
        let up = group.next().makePromise(of: Void.self)
        let keys = PairKeys()
        // The joiner is the TLS client (peer.runPair); the proof, not a
        // pin, checks the server's key.
        let ctx = try PinnedTLS.context(identity: identity, client: true, keyLog: keys.log)
        let tls = try NIOSSLClientHandler(context: ctx, serverHostname: nil) { certs, promise in
            guard certs.count == 1, let spki = try? certs[0].extractPublicKey().toSPKIBytes(),
                  spki.count == 44, spki.prefix(12) == DER.spki(Data(count: 32)).prefix(12) else {
                promise.succeed(.failed)
                return
            }
            keys.server = Data(spki.suffix(32))
            promise.succeed(.certificateVerified)
        }
        let channel: Channel
        do {
            channel = try await RelayDial.open(
                relay: code.relay, identity: identity, kind: "pair", room: room(code.secret), peer: nil,
                group: group, up: up, onWait: {
                    // Nobody waits there: the code is spent.
                    up.fail(LinkError.noPairer)
                }
            ) { ctx in
                let ops = ctx.pipeline.syncOperations
                try ops.addHandler(tls, position: .after(ctx.handler))
                try ops.addHandler(LinesHandler(sink: sink, up: up))
            }
        } catch LinkError.relayClosed(_, "timeout") {
            throw LinkError.noPairer
        }
        let link = LinkTransport(channel: channel, lines: lines)
        defer { link.close() }
        return try await withThrowingTaskGroup(of: Server.self) { g in
            g.addTask {
                try await exchange(link, code: code, identity: identity, keys: keys, name: name, version: version, writeKey: writeKey)
            }
            g.addTask {
                try await Task.sleep(for: timeout)
                throw LinkError.timedOut("waiting for the server to say yes")
            }
            defer { g.cancelAll() }
            return try await g.next()!
        }
    }

    private static func exchange(_ link: LinkTransport, code: PairingCode, identity: LinkIdentity, keys: PairKeys,
                                 name: String, version: String, writeKey: Data?) async throws -> Server {
        guard let serverKey = keys.server else { throw LinkError.notPinned }
        let serverID = LinkIdentity.id(of: serverKey)
        guard serverID != identity.id else { throw LinkError.protocolError("that's this phone's own pairing") }
        guard serverID == code.server else { throw LinkError.notExpected }
        guard let exp = keys.exporter(label: "tm-pair v1", length: 32) else {
            throw LinkError.protocolError("the pairing's TLS gave no exporter secret")
        }
        var it = link.lines.makeAsyncIterator()
        func recv() async throws -> Message {
            guard let line = try await it.next() else { throw LinkError.closed }
            do {
                return try JSONDecoder().decode(Message.self, from: line)
            } catch {
                throw LinkError.protocolError("the server sent something unexpected")
            }
        }
        func send(_ m: Message) throws { try link.send(JSONEncoder().encode(m)) }

        try send(Message(proof: proof(code.secret, role: "join", pairer: serverID, joiner: identity.id, exp: exp)))
        let theirs = try await recv()
        guard let p = theirs.proof, p == proof(code.secret, role: "pair", pairer: serverID, joiner: identity.id, exp: exp) else {
            throw LinkError.badCode
        }
        try send(Message(name: name, version: version, writeKey: writeKey))
        let about = try await recv()
        guard about.proof == nil, about.ok == nil else { throw LinkError.protocolError("the server sent something unexpected") }
        // The code named the server: nothing to confirm on this side.
        try send(Message(ok: true))
        let answer = try await recv()
        guard let ok = answer.ok else { throw LinkError.protocolError("the server sent something unexpected") }
        guard ok else { throw LinkError.declined }
        linkLog.debug("paired with server \(RelayWire.hex(serverID.prefix(4)), privacy: .public): link room \(RelayWire.hex(linkRoom(identity.id, serverID).prefix(4)), privacy: .public)")
        return Server(name: clean(about.name ?? ""), version: clean(about.version ?? ""),
                      endpoint: .init(relay: code.relay, serverKey: serverKey))
    }

    /// One line of the pairing (peer.pairMsg).
    struct Message: Codable {
        var proof: Data?
        var name: String?
        var version: String?
        var ok: Bool?
        var writeKey: Data?

        enum CodingKeys: String, CodingKey {
            case proof, name, version, ok
            case writeKey = "write_key"
        }
    }

    /// The pair room a device code's secret names (peer.pairRoom).
    static func room(_ secret: Data) -> Data {
        Data(SHA256.hash(data: Data("tm-device-pair room v1\n".utf8) + secret))
    }

    /// role's proof that it knows the secret, bound to both keys, this
    /// TLS session and devices (peer.pairProof).
    static func proof(_ secret: Data, role: String, pairer: Data, joiner: Data, exp: Data) -> Data {
        var m = HMAC<SHA256>(key: SymmetricKey(data: secret))
        m.update(data: Data("tm-device-pair proof v1\n".utf8))
        m.update(data: Data(role.utf8))
        m.update(data: Data([0]))
        m.update(data: pairer)
        m.update(data: joiner)
        m.update(data: exp)
        return Data(m.finalize())
    }

    /// The server's words, as one short line of plain text.
    static func clean(_ s: String) -> String {
        String(s.unicodeScalars.filter { !CharacterSet.controlCharacters.contains($0) }.prefix(64).map(Character.init))
    }
}

/// What the pairing's TLS tells: the server's key, and the exporter
/// secret. swift-nio-ssl has no keying-material exporter, so it comes
/// from BoringSSL's key log (EXPORTER_SECRET) and is expanded here as
/// RFC 8446 §7.5 says, which is what Go's ExportKeyingMaterial does.
final class PairKeys: @unchecked Sendable {
    private let lock = NSLock()
    private var _server: Data?
    private var secret: Data?

    var server: Data? {
        get { lock.withLock { _server } }
        set { lock.withLock { _server = newValue } }
    }

    var log: NIOSSLKeyLogCallback {
        { [weak self] buf in
            let line = String(buffer: buf).trimmingCharacters(in: .whitespacesAndNewlines)
            let f = line.split(separator: " ")
            guard f.count == 3, f[0] == "EXPORTER_SECRET", let s = Data(hexString: String(f[2])) else { return }
            self?.lock.withLock { self?.secret = s }
        }
    }

    /// TLS-Exporter(label, "", length) of the session.
    func exporter(label: String, length: Int) -> Data? {
        guard let s = lock.withLock({ secret }) else { return nil }
        switch s.count {
        case 32: return Self.exporter(SHA256.self, secret: s, label: label, length: length)
        case 48: return Self.exporter(SHA384.self, secret: s, label: label, length: length)
        default: return nil
        }
    }

    static func exporter<H: HashFunction>(_: H.Type, secret: Data, label: String, length: Int) -> Data {
        let empty = Data(H.hash(data: Data()))
        let derived = expandLabel(H.self, secret: secret, label: label, context: empty, length: H.Digest.byteCount)
        return expandLabel(H.self, secret: derived, label: "exporter", context: empty, length: length)
    }

    /// HKDF-Expand-Label (RFC 8446 §7.1).
    static func expandLabel<H: HashFunction>(_: H.Type, secret: Data, label: String, context: Data, length: Int) -> Data {
        let l = Data("tls13 \(label)".utf8)
        var info = Data([UInt8(length >> 8), UInt8(length & 0xFF), UInt8(l.count)])
        info += l
        info.append(UInt8(context.count))
        info += context
        let key = HKDF<H>.expand(pseudoRandomKey: SymmetricKey(data: secret), info: info, outputByteCount: length)
        return key.withUnsafeBytes { Data($0) }
    }
}

extension Data {
    /// Hex digits, either case; nil for anything else.
    init?(hexString s: String) {
        guard s.count % 2 == 0 else { return nil }
        var d = Data(capacity: s.count / 2)
        var i = s.startIndex
        while i < s.endIndex {
            let j = s.index(i, offsetBy: 2)
            guard let b = UInt8(s[i..<j], radix: 16) else { return nil }
            d.append(b)
            i = j
        }
        self = d
    }
}
