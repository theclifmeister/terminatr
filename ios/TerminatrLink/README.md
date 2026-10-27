# TerminatrLink (spike, T786)

The phone's end of a link to its tm server, the way peers link (docs/SPEC.md §16):

1. dial the relay (`wss://…#sha256=<pin>` for a self-signed relay, or the system's CAs);
2. join the link room of the two keys with a signed hello, and wait there for the server;
3. run TLS 1.3 inside the room, both ends showing a self-signed Ed25519 certificate and pinning the other's key (the TLS client is the end with the lower server id, as `peer.tlsClient` has it);
4. carry NDJSON lines.

The server side is unchanged: same relay, same TLS, same key types.

## Why swift-nio-ssl

Network.framework / SecKey can't hold an Ed25519 identity. swift-nio-ssl ships BoringSSL, which can:

- `NIOSSLPrivateKey` loads the CryptoKit key as PKCS #8.
- `signingSignatureAlgorithms` / `verifySignatureAlgorithms` = `[.ed25519]`, because BoringSSL's defaults leave Ed25519 out.
- A custom verification callback compares the certificate's SPKI with the pinned key, just as `peer.pinned` does.

The certificate is a few lines of hand-written DER in `Identity.swift`, so swift-certificates isn't needed.

The other option, the Go link code built for iOS (gomobile / a cgo c-archive), works too, but it isn't needed. It is no smaller, and it brings a Go toolchain into the iOS build plus a C/ObjC bridge to drive it.

| | swift-nio-ssl (this package) | Go c-archive (what gomobile wraps) |
|---|---|---|
| Added to an iOS arm64 release binary, stripped | **≈ 6.0 MB** (2.3 MB gzipped) | ≈ 5.9 MB (2.4 MB gzipped), for only the relay dial + TLS, before gomobile's glue |
| Build | SwiftPM, nothing else | Go toolchain + cgo cross-compile per arch, an xcframework |
| API | async/await, `BridgeTransport` shape directly | C/ObjC callbacks to wrap |
| Reuse | relay hello and TLS settings written again (≈ 450 lines, tested against the Go code) | the server's own code |

How it was measured: an iOS release executable with `-dead_strip`, `strip -x`, minus a baseline that already links Foundation and CryptoKit.

## Proof

`spike/run.sh [mac|sim] [link|impostor] [client|server]` runs two ends:

- **Server end:** a real `peer` link, from `TestPhoneSpikeLink` in `internal/peer/phonespike_test.go`. It goes through `tm relay serve --self-signed`'s relay code on loopback.
- **Phone end:** this package's spike tests, on this Mac or inside the iOS Simulator (`xcodebuild test`).

The CLI relay refuses to run from an agent thread (it's human-only), which is why the Go test runs the relay in-process. To use a relay you started yourself, set `TM_PHONE_SPIKE_RELAY`.

What the link test checks:

- the link comes up on the server's side;
- hellos are exchanged both ways;
- the phone's `spike.echo` request gets its answer;
- the phone answers the server's `link.ping`;
- the link ends when the phone leaves.

In the impostor test, the right relay hello comes with another key's TLS certificate, and the phone must refuse it.

All of these pass in the iOS 27.1 Simulator and on macOS, with the phone as TLS client and as TLS server.

## API

`LinkTransport` has exactly `BridgeTransport`'s members, so the Bridge runs over it unchanged:

```swift
public final class LinkTransport: @unchecked Sendable {        // : BridgeTransport
    public struct Endpoint: Sendable { var relay: URL; var serverKey: Data }  // from the pairing QR
    public static func connect(to: Endpoint, as: LinkIdentity,
                               group: EventLoopGroup = .singleton,
                               onWait: @Sendable () -> Void = {}) async throws -> LinkTransport
    public let lines: AsyncThrowingStream<Data, any Error>  // finishes with LinkError on end
    public func send(_ line: Data) throws
    public func close()                                      // leaves the room; the server hears
}
public struct LinkIdentity { init(); init(seed: Data); publicKey: Data; id: Data }  // Keychain keeps the seed
```

The conformance itself, `extension LinkTransport: BridgeTransport {}`, waits until TerminatrKit builds for iOS: `ProcessTransport` and `TMLocator` go behind `#if os(macOS)`.

## Before it ships

- **Network.framework underneath.** Use NIOTransportServices (`NWConnection`) for the TCP connection and the relay's TLS, so iOS brings up cellular and VPN. BSD sockets don't do that. The inner TLS stays BoringSSL.
- **Keepalive.** Answer `link.ping` and treat 90 s of silence as dead, in the device protocol's layer above the lines.
- **Reconnects.** Redial with backoff, and close cleanly when the app goes to the background.
- **Keychain.** Keep the identity seed in the Keychain, this device only, after first unlock.
