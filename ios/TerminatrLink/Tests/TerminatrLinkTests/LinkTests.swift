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

@Test(.enabled(if: Spike() != nil && ProcessInfo.processInfo.environment["TM_PHONE_SPIKE_MODE"] != "impostor"))
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
