# TerminatrLink

The phone's end of its device link to a tm server (docs/SPEC.md §16.15), the way peers link (§16): pairing by tm device pair's code (`DevicePairing.join`, `PairingCode`), then the link (`LinkTransport`), which the app runs its Bridge over. Started as the T786 spike; T792 made it the app's.

The link:

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

`spike/run.sh [mac|sim] [device|declined|link|impostor] [client|server]` runs two ends:

- **device** (the default): the server's real `device.Pair` and a `device.Manager` link (`TestPhoneDevice` in `internal/device/phone_test.go`), through `tm relay serve --self-signed`'s relay code on loopback. The phone pairs, checks a spent code finds nobody, links and swaps lines. **declined** has the server say no.
- **link**, **impostor**: T786's peer-link spike against `internal/peer/phonespike_test.go` (peer hellos, `link.ping`, an impostor's key refused).

The CLI relay and `tm device pair` refuse to run from an agent thread (they're human-only), which is why the Go tests run them in-process. For the app itself, `make -C ios dev-server` runs a real tm server and relay for it (`TestPhoneDev`), and `make -C ios run-paired DEVICE=…` pairs the Simulator's app with it.

## The device link

A device link carries the app API's NDJSON right after the TLS, the server's hello first: no peer hellos, no `link.ping`. Nothing keeps it alive above the relay: the relay pings every 30 s and 90 s of silence ends the link (`RelayWire.idle`).

## The pairing's exporter

The pairing proof binds the TLS session with `ExportKeyingMaterial("tm-pair v1", nil, 32)`. swift-nio-ssl has no exporter, so `PairKeys` takes BoringSSL's `EXPORTER_SECRET` from the key log callback and expands it as RFC 8446 §7.5 says (SHA-256 or SHA-384 by the secret's size). The device test against Go's `device.Pair` is what checks it: a wrong exporter fails the proof.

## Network.framework

The TCP connection and the relay's TLS go through NIOTransportServices (`NWConnection`), so iOS brings up cellular and VPNs; a pinned relay is checked in the TLS options' verify block. The link's TLS inside stays BoringSSL.

## Still to come

- Push and a link while the app is in the back (T785).
- Answer and write levels (the server enables only view).
