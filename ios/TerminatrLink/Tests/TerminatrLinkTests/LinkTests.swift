import CryptoKit
import Foundation
import NIOPosix
import NIOSSL
import Testing

@testable import TerminatrLink

@Test func certificateCarriesTheKey() throws {
    let id = LinkIdentity()
    let cert = try NIOSSLCertificate(bytes: id.certificate(name: "tm phone"), format: .der)
    #expect(try cert.extractPublicKey().toSPKIBytes() == DER.spki(id.publicKey))
    // BoringSSL takes the key, so it can sign the handshake with it.
    _ = try NIOSSLPrivateKey(bytes: id.pkcs8, format: .der)
}

@Test func linkRoomIsTheSameFromEitherEnd() {
    let a = LinkIdentity().id, b = LinkIdentity().id
    #expect(linkRoom(a, b) == linkRoom(b, a))
    #expect(linkRoom(a, b) != linkRoom(a, a))
}

@Test func pinRefusesAnotherKey() async throws {
    let want = LinkIdentity(), other = LinkIdentity()
    let cert = try NIOSSLCertificate(bytes: other.certificate(name: "x"), format: .der)
    let loop = MultiThreadedEventLoopGroup.singleton.next()
    let p = loop.makePromise(of: NIOSSLVerificationResult.self)
    PinnedTLS.pinned(want.publicKey)([cert], p)
    #expect(try await p.futureResult.get() == .failed)
}

// The spike against a real relay and a server's real link
// (internal/peer/phonespike_test.go): set TM_PHONE_SPIKE_DIR, as
// spike/run.sh does; the Go end puts relay.url and server.pub there.
struct Spike {
    let dir: URL
    let identity = LinkIdentity()

    init?() {
        guard let d = ProcessInfo.processInfo.environment["TM_PHONE_SPIKE_DIR"] else { return nil }
        dir = URL(fileURLWithPath: d)
    }

    /// Waits for the relay and the server's key, then writes phone.pub:
    /// a key that makes the phone the TLS client or server as
    /// TM_PHONE_SPIKE_ROLE says, either when it says neither.
    func endpoint() async throws -> (LinkTransport.Endpoint, LinkIdentity) {
        for _ in 0..<1800 {
            if let r = read("relay.url").flatMap(URL.init(string:)), let k = read("server.pub").flatMap(Data.init(hex:)), k.count == 32 {
                var id = identity
                let role = ProcessInfo.processInfo.environment["TM_PHONE_SPIKE_ROLE"]
                while role == "client" && !id.id.lexicographicallyPrecedes(LinkIdentity.id(of: k))
                    || role == "server" && id.id.lexicographicallyPrecedes(LinkIdentity.id(of: k)) {
                    id = LinkIdentity()
                }
                try Data(RelayWire.hex(id.publicKey).utf8).write(to: dir.appendingPathComponent("phone.pub"))
                return (.init(relay: r, serverKey: k), id)
            }
            try await Task.sleep(for: .milliseconds(100))
        }
        throw LinkError.timedOut("waiting for relay.url and server.pub")
    }

    private func read(_ name: String) -> String? {
        (try? String(contentsOf: dir.appendingPathComponent(name), encoding: .utf8))?.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

@Test(.enabled(if: Spike() != nil && [nil, "link"].contains(ProcessInfo.processInfo.environment["TM_PHONE_SPIKE_MODE"])))
func spikeLink() async throws {
    let s = Spike()!
    let (ep, identity) = try await s.endpoint()
    let t0 = ContinuousClock.now
    let link = try await LinkTransport.connect(to: ep, as: identity) {
        print("spike: the server isn't in the room yet")
    }
    let role = identity.id.lexicographicallyPrecedes(LinkIdentity.id(of: ep.serverKey)) ? "client" : "server"
    print("spike: TLS up as the TLS \(role), server key pinned, in \(ContinuousClock.now - t0)")
    try link.send(Data(#"{"link_protocol":1,"version":"phone-spike","caps":[]}"#.utf8))
    try link.send(Data(#"{"id":1,"method":"spike.echo","params":{"text":"hello from the phone"}}"#.utf8))
    var hello = false, echo = false, pinged = false
    for try await line in link.lines {
        let m = try JSONSerialization.jsonObject(with: line) as! [String: Any]
        print("spike: <- \(String(decoding: line, as: UTF8.self))")
        if !hello {
            #expect(m["link_protocol"] as? Int == 1)
            hello = true
        } else if m["method"] as? String == "link.ping", let id = m["id"] as? Int {
            try link.send(Data(#"{"id":\#(id),"result":{}}"#.utf8))
            pinged = true
        } else if m["id"] as? Int == 1 {
            #expect((m["result"] as? [String: Any])?["text"] as? String == "hello from the phone")
            echo = true
        }
        if hello && echo && pinged { break }
    }
    #expect(hello && echo && pinged)
    // Let the last answer go before leaving.
    try await Task.sleep(for: .milliseconds(200))
    link.close()
}

@Test(.enabled(if: Spike() != nil && ProcessInfo.processInfo.environment["TM_PHONE_SPIKE_MODE"] == "impostor"))
func spikeImpostor() async throws {
    let s = Spike()!
    let (ep, identity) = try await s.endpoint()
    await #expect(throws: (any Error).self) {
        let link = try await LinkTransport.connect(to: ep, as: identity)
        for try await _ in link.lines {}
    }
}

extension Data {
    init?(hex: String) {
        guard hex.count % 2 == 0 else { return nil }
        var d = Data(capacity: hex.count / 2)
        var i = hex.startIndex
        while i < hex.endIndex {
            let j = hex.index(i, offsetBy: 2)
            guard let b = UInt8(hex[i..<j], radix: 16) else { return nil }
            d.append(b)
            i = j
        }
        self = d
    }
}

@Test func pairingCodeReadsTmDevicePairs() throws {
    // device.Invite.String's form (internal/device TestInvite).
    let pin = String(repeating: "0f", count: 32)
    let server = String(repeating: "ab", count: 32)
    let c = try PairingCode("terminatr://pair?v=1&relay=wss%3A%2F%2Frelay.example.net%23sha256%3D\(pin)&secret=7Q4M-K2XD-9HVA-PT3C&server=\(server)")
    #expect(c.relay.absoluteString == "wss://relay.example.net#sha256=\(pin)")
    #expect(c.secret.count == 10)
    #expect(c.server == Data(hexString: server))
    #expect(PairingCode.parseSecret("7q4m k2xd 9hva pt3c") == c.secret)
    #expect(PairingCode.parseSecret("7Q4M-K2XD-9HVA-PT3") == nil)
    for bad in ["", "https://pair?v=1", "terminatr://pair?v=2",
                "terminatr://pair?v=1&relay=wss%3A%2F%2Fr&secret=7Q4M-K2XD-9HVA-PT3C&server=00",
                "terminatr://pair?v=1&relay=https%3A%2F%2Fr&secret=7Q4M-K2XD-9HVA-PT3C&server=\(server)"] {
        #expect(throws: PairingCode.Bad.self) { try PairingCode(bad) }
    }
}

// The phone's pairing and device link against a server's real
// device.Pair and link (internal/device/phone_test.go), as spike/run.sh
// device runs them: the invite comes through TM_PHONE_SPIKE_DIR.
@Test(.enabled(if: Spike() != nil && ["device", "declined"].contains(ProcessInfo.processInfo.environment["TM_PHONE_SPIKE_MODE"])))
func spikeDevice() async throws {
    let s = Spike()!
    var invite: String?
    for _ in 0..<1800 where invite == nil {
        invite = try? String(contentsOf: s.dir.appendingPathComponent("invite.txt"), encoding: .utf8)
        if invite == nil { try await Task.sleep(for: .milliseconds(100)) }
    }
    let code = try PairingCode(try #require(invite))
    let identity = LinkIdentity()
    if ProcessInfo.processInfo.environment["TM_PHONE_SPIKE_MODE"] == "declined" {
        await #expect(throws: LinkError.declined) {
            _ = try await DevicePairing.join(code, as: identity, name: "tm phone test", version: "1.0")
        }
        return
    }
    // The write key goes with the name, for the server to pin.
    let writeKey = P256.Signing.PrivateKey().publicKey.x963Representation
    let server = try await DevicePairing.join(code, as: identity, name: "tm phone test", version: "1.0", writeKey: writeKey)
    print("spike: paired with \(server.name) \(server.version)")
    #expect(server.name == "studio")
    #expect(LinkIdentity.id(of: server.endpoint.serverKey) == code.server)
    // A spent code finds nobody.
    await #expect(throws: LinkError.noPairer) {
        _ = try await DevicePairing.join(code, as: LinkIdentity(), name: "again", version: "1.0")
    }
    let link = try await LinkTransport.connect(to: server.endpoint, as: identity) {
        print("spike: the server isn't in the room yet")
    }
    var it = link.lines.makeAsyncIterator()
    // The server speaks first: the echo's name and level line.
    #expect(try await it.next().map { String(decoding: $0, as: UTF8.self) } == "tm phone test view")
    try link.send(Data("hello from the phone".utf8))
    #expect(try await it.next().map { String(decoding: $0, as: UTF8.self) } == "hello from the phone")
    print("spike: device link up, lines both ways")
    link.close()
}
