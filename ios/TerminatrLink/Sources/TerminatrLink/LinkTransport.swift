import Foundation
import NIOCore
import NIOHTTP1
import NIOPosix
import NIOSSL
import NIOTLS
import NIOWebSocket

/// The phone's link to its server through a relay: NDJSON lines inside
/// TLS 1.3 with both Ed25519 keys pinned, inside the relay's room. Its
/// shape is TerminatrKit's BridgeTransport (lines, send, close), so a
/// Bridge runs over it as it runs over tm app bridge's stdio.
public final class LinkTransport: @unchecked Sendable {
    /// Where the server is: its relay (wss://host[:port][/path], with
    /// #sha256=<pin> for a self-signed relay; ws:// in tests) and its
    /// Ed25519 public key, pinned at pairing.
    public struct Endpoint: Sendable {
        public var relay: URL
        public var serverKey: Data
        public init(relay: URL, serverKey: Data) {
            self.relay = relay
            self.serverKey = serverKey
        }
    }

    /// The server's lines, without their newline. It finishes when the
    /// link ends, throwing why.
    public let lines: AsyncThrowingStream<Data, any Error>
    private let channel: Channel

    private init(channel: Channel, lines: AsyncThrowingStream<Data, any Error>) {
        self.channel = channel
        self.lines = lines
    }

    /// Writes one line (the newline is the transport's).
    public func send(_ line: Data) throws {
        guard channel.isActive else { throw LinkError.closed }
        var b = channel.allocator.buffer(capacity: line.count + 1)
        b.writeBytes(line)
        b.writeInteger(UInt8(ascii: "\n"))
        channel.writeAndFlush(b, promise: nil)
    }

    /// Leaves the room; the relay tells the server.
    public func close() { channel.close(promise: nil) }

    /// The bound on reaching the relay and its hello; waiting for the
    /// server has none but the task's.
    public static let dialTimeout = TimeAmount.seconds(30)
    /// The bound on the TLS handshake once the room mates.
    public static let handshakeTimeout = TimeAmount.seconds(10)

    /// Dials the relay, joins the link room of identity and the server,
    /// waits there for the server (onWait when the relay says it isn't
    /// there yet), and returns once TLS is up with the server's key
    /// checked. Cancelling the task gives up.
    public static func connect(
        to endpoint: Endpoint, as identity: LinkIdentity,
        group: EventLoopGroup = MultiThreadedEventLoopGroup.singleton,
        onWait: @escaping @Sendable () -> Void = {}
    ) async throws -> LinkTransport {
        guard var comps = URLComponents(url: endpoint.relay, resolvingAgainstBaseURL: false),
              let scheme = comps.scheme, scheme == "wss" || scheme == "ws", let host = comps.host else {
            throw LinkError.protocolError("\(endpoint.relay): wss:// or ws:// only")
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
        let port = comps.port ?? (scheme == "wss" ? 443 : 80)
        let path = comps.path.isEmpty || comps.path == "/" ? "/v1" : comps.path
        let hostHeader = comps.port.map { "\(host):\($0)" } ?? host
        let peerID = LinkIdentity.id(of: endpoint.serverKey)

        let (lines, sink) = AsyncThrowingStream<Data, any Error>.makeStream()
        let up = group.next().makePromise(of: Void.self)
        let inner = try PinnedTLS.handler(identity: identity, peerKey: endpoint.serverKey)
        let outer = scheme == "wss" ? try PinnedTLS.relay(host: host, pin: pin) : nil

        let bootstrap = ClientBootstrap(group: group)
            .connectTimeout(dialTimeout)
            .channelInitializer { channel in
                channel.eventLoop.makeCompletedFuture {
                    let sync = channel.pipeline.syncOperations
                    if let outer { try sync.addHandler(outer) }
                    let upgrader = NIOWebSocketClientUpgrader(maxFrameSize: 1 << 20, automaticErrorHandling: false) { channel, _ in
                        channel.eventLoop.makeCompletedFuture {
                            let relay = RelayHandler(identity: identity, peerID: peerID, onWait: onWait) { ctx in
                                // Before the server's first byte: TLS, lines.
                                let ops = ctx.pipeline.syncOperations
                                try ops.addHandler(inner, position: .after(ctx.handler))
                                try ops.addHandler(LinesHandler(sink: sink, up: up))
                                ctx.eventLoop.scheduleTask(in: handshakeTimeout) {
                                    up.fail(LinkError.timedOut("on the TLS handshake"))
                                }
                            }
                            try channel.pipeline.syncOperations.addHandler(relay)
                            try channel.pipeline.syncOperations.addHandler(Watch(up: up))
                        }
                    }
                    let request = UpgradeRequest(host: hostHeader, path: path)
                    // The relay's challenge can come in the same read as
                    // its 101: forward what follows the upgrade, never drop it.
                    try sync.addHTTPClientHandlers(leftOverBytesStrategy: .forwardBytes, withClientUpgrade: (upgraders: [upgrader], completionHandler: { ctx in
                        ctx.pipeline.syncOperations.removeHandler(request, promise: nil)
                    }))
                    try sync.addHandler(request)
                }
            }
        let channel: Channel
        do {
            channel = try await bootstrap.connect(host: host, port: port).get()
        } catch {
            up.fail(error)
            throw error
        }
        channel.closeFuture.whenComplete { _ in up.fail(LinkError.closed) }
        // The hello must come within dialTimeout; the wait is the task's.
        channel.eventLoop.scheduleTask(in: dialTimeout) {
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
            throw error
        }
        return LinkTransport(channel: channel, lines: lines)
    }
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

/// Splits the server's bytes into lines for the stream; the TLS
/// handshake's end completes the dial.
final class LinesHandler: ChannelInboundHandler {
    typealias InboundIn = ByteBuffer

    /// The longest line taken, as the link's maxLine.
    static let maxLine = 1 << 20

    private let sink: AsyncThrowingStream<Data, any Error>.Continuation
    private let up: EventLoopPromise<Void>
    private var buf = ByteBuffer()
    private var error: Error?

    init(sink: AsyncThrowingStream<Data, any Error>.Continuation, up: EventLoopPromise<Void>) {
        self.sink = sink
        self.up = up
    }

    func userInboundEventTriggered(context: ChannelHandlerContext, event: Any) {
        if case TLSUserEvent.handshakeCompleted? = event as? TLSUserEvent { up.succeed(()) }
        context.fireUserInboundEventTriggered(event)
    }

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        var in_ = unwrapInboundIn(data)
        buf.writeBuffer(&in_)
        while let i = buf.readableBytesView.firstIndex(of: UInt8(ascii: "\n")) {
            let n = i - buf.readerIndex
            sink.yield(Data(buf.readBytes(length: n)!))
            buf.moveReaderIndex(forwardBy: 1)
        }
        if buf.readableBytes > Self.maxLine {
            errorCaught(context: context, error: LinkError.protocolError("a line over \(Self.maxLine) bytes"))
            context.close(promise: nil)
        }
        buf.discardReadBytes()
    }

    func errorCaught(context: ChannelHandlerContext, error: Error) {
        if self.error == nil { self.error = error }
        up.fail(error)
    }

    func channelInactive(context: ChannelHandlerContext) {
        sink.finish(throwing: error ?? LinkError.closed)
        context.fireChannelInactive()
    }
}
