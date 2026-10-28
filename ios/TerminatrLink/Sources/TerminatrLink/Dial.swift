import Foundation
import NIOCore
import NIOHTTP1
import NIOTransportServices
import NIOWebSocket

/// A relay's URL, as peer.RelayURL keeps it: wss://host[:port][/path],
/// with #sha256=<pin> for a self-signed relay; ws:// in tests.
struct RelayAddress {
    let host: String
    let port: Int
    let path: String
    let hostHeader: String
    let tls: Bool
    let pin: String?

    init(_ url: URL) throws {
        guard var comps = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let scheme = comps.scheme, scheme == "wss" || scheme == "ws", let host = comps.host, !host.isEmpty else {
            throw LinkError.protocolError("\(url): wss:// or ws:// only")
        }
        var pin: String?
        if let f = comps.fragment, !f.isEmpty {
            let h = f.lowercased()
            guard h.hasPrefix("sha256="), h.count == 7 + 64, scheme == "wss" else {
                throw LinkError.protocolError("#\(f): a relay's pin is #sha256= and 64 hex digits, with wss://")
            }
            pin = String(h.dropFirst(7))
        }
        comps.fragment = nil
        self.host = host
        self.port = comps.port ?? (scheme == "wss" ? 443 : 80)
        self.path = comps.path.isEmpty || comps.path == "/" ? "/v1" : comps.path
        self.hostHeader = comps.port.map { "\(host):\($0)" } ?? host
        self.tls = scheme == "wss"
        self.pin = pin
    }
}

/// Dialing a relay and joining a room on it: the TCP connection and the
/// relay's TLS in Network.framework (NIOTransportServices), so iOS
/// brings up cellular or a VPN for it; the WebSocket upgrade; the relay's
/// hello. What runs inside the room (TLS with a pinned or a proved key)
/// is the caller's.
enum RelayDial {
    /// The bound on reaching the relay and its hello; waiting for the
    /// other end has none but the task's.
    static let timeout = TimeAmount.seconds(30)

    /// Dials relay and joins room as kind. onWait is told when the relay
    /// says the other end isn't there yet; onMated runs on the loop as
    /// the room mates, before any of the other end's bytes, and adds the
    /// inner handlers after the relay's (its context). It returns once
    /// up succeeds, which those handlers do, and fails with the first
    /// error before. Cancelling the task gives up.
    static func open(
        relay url: URL, identity: LinkIdentity, kind: String, room: Data, peer: Data?,
        group: EventLoopGroup, up: EventLoopPromise<Void>,
        onWait: @escaping @Sendable () -> Void,
        onMated: @escaping (ChannelHandlerContext) throws -> Void
    ) async throws -> Channel {
        let addr: RelayAddress
        do {
            addr = try RelayAddress(url)
        } catch {
            up.fail(error)
            throw error
        }
        let refused = Flag()
        // Both run on the channel's loop only.
        let mated = Unchecked(onMated)
        var bootstrap = NIOTSConnectionBootstrap(group: group)
            .connectTimeout(timeout)
            .channelInitializer { channel in
                channel.eventLoop.makeCompletedFuture {
                    let sync = channel.pipeline.syncOperations
                    let upgrader = NIOWebSocketClientUpgrader(maxFrameSize: RelayWire.maxMessage, automaticErrorHandling: false) { channel, _ in
                        channel.eventLoop.makeCompletedFuture {
                            let ops = channel.pipeline.syncOperations
                            try ops.addHandler(IdleStateHandler(readTimeout: RelayWire.idle))
                            try ops.addHandler(RelayHandler(identity: identity, kind: kind, room: room, peerID: peer,
                                                            onWait: onWait, onMated: mated.value))
                            try ops.addHandler(Watch(up: up))
                        }
                    }
                    let request = Unchecked(UpgradeRequest(host: addr.hostHeader, path: addr.path))
                    // The relay's challenge can come in the same read as
                    // its 101: forward what follows the upgrade, never drop it.
                    try sync.addHTTPClientHandlers(leftOverBytesStrategy: .forwardBytes, withClientUpgrade: (upgraders: [upgrader], completionHandler: { ctx in
                        ctx.pipeline.syncOperations.removeHandler(request.value, promise: nil)
                    }))
                    try sync.addHandler(request.value)
                }
            }
        if addr.tls {
            bootstrap = bootstrap.tlsOptions(PinnedTLS.relay(pin: addr.pin) { refused.set() })
        }
        let channel: Channel
        do {
            channel = try await bootstrap.connect(host: addr.host, port: addr.port).get()
        } catch {
            let e: any Error = refused.isSet ? LinkError.relayNotPinned : error
            up.fail(e)
            throw e
        }
        channel.closeFuture.whenComplete { _ in up.fail(refused.isSet ? LinkError.relayNotPinned : LinkError.closed) }
        // The hello must come within timeout; the wait is the task's.
        channel.eventLoop.scheduleTask(in: timeout) {
            let relay = try? channel.pipeline.syncOperations.handler(type: RelayHandler.self)
            if relay?.greeted != true {
                up.fail(LinkError.timedOut("reaching the relay"))
                channel.close(promise: nil)
            }
        }
        do {
            try await withTaskCancellationHandler {
                try await up.futureResult.get()
            } onCancel: {
                channel.close(promise: nil)
            }
        } catch {
            channel.close(promise: nil)
            throw refused.isSet ? LinkError.relayNotPinned : error
        }
        return channel
    }
}

/// A value only one event loop touches, carried across a @Sendable
/// closure to it.
struct Unchecked<T>: @unchecked Sendable {
    let value: T
    init(_ v: T) { value = v }
}

/// A flag set from Network.framework's queue, read on the loop.
final class Flag: @unchecked Sendable {
    private let lock = NSLock()
    private var v = false
    func set() { lock.withLock { v = true } }
    var isSet: Bool { lock.withLock { v } }
}

/// Sends the WebSocket upgrade request once the channel is up.
private final class UpgradeRequest: ChannelInboundHandler, RemovableChannelHandler {
    typealias InboundIn = HTTPClientResponsePart
    typealias OutboundOut = HTTPClientRequestPart

    let host: String
    let path: String
    init(host: String, path: String) {
        self.host = host
        self.path = path
    }

    func channelActive(context: ChannelHandlerContext) {
        var head = HTTPRequestHead(version: .http1_1, method: .GET, uri: path)
        head.headers.add(name: "Host", value: host)
        head.headers.add(name: "Sec-WebSocket-Protocol", value: RelayWire.subprotocol)
        head.headers.add(name: "Content-Length", value: "0")
        context.write(wrapOutboundOut(.head(head)), promise: nil)
        context.writeAndFlush(wrapOutboundOut(.end(nil)), promise: nil)
        context.fireChannelActive()
    }

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        // A response that isn't the upgrade: the relay refused it.
        if case .head(let h) = unwrapInboundIn(data) {
            context.fireErrorCaught(LinkError.protocolError("the relay answered the upgrade with \(h.status.code)"))
            context.close(promise: nil)
        }
    }
}

/// Fails the dial with the first error before the lines take over.
private final class Watch: ChannelInboundHandler {
    typealias InboundIn = ByteBuffer
    let up: EventLoopPromise<Void>
    init(up: EventLoopPromise<Void>) { self.up = up }
    func errorCaught(context: ChannelHandlerContext, error: Error) {
        up.fail(error)
        context.fireErrorCaught(error)
    }
}
