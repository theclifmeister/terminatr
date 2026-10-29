import Foundation
import NIOCore
import NIOSSL
import NIOTLS
import NIOTransportServices
import os

/// The phone's link to its server through a relay: NDJSON lines inside
/// TLS 1.3 with both Ed25519 keys pinned, inside the relay's room. Its
/// shape is TerminatrKit's BridgeTransport (lines, send, close), so a
/// Bridge runs over it as it runs over tm app bridge's stdio.
public final class LinkTransport: @unchecked Sendable {
    /// Where the server is: its relay (wss://host[:port][/path], with
    /// #sha256=<pin> for a self-signed relay; ws:// in tests) and its
    /// Ed25519 public key, pinned at pairing.
    public struct Endpoint: Sendable, Codable, Equatable {
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

    init(channel: Channel, lines: AsyncThrowingStream<Data, any Error>) {
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

    /// Pings the relay now: when nothing answers within a few seconds
    /// (RelayWire.pingDeadline) the link fails, and lines ends with
    /// LinkError.pingUnanswered. For when the app comes back to the
    /// front with its link kept: is it still there?
    public func check() { channel.triggerUserOutboundEvent(PingNow(), promise: nil) }

    /// The bound on the TLS handshake once the room mates.
    public static let handshakeTimeout = TimeAmount.seconds(10)

    /// Dials the relay, joins the link room of identity and the server,
    /// waits there for the server (onWait when the relay says it isn't
    /// there yet: asleep or offline), and returns once TLS is up with the
    /// server's key checked. A device link carries the app API from
    /// there, the server's hello first (docs/SPEC.md §16.15): no peer
    /// hellos, no link.ping. Cancelling the task gives up.
    public static func connect(
        to endpoint: Endpoint, as identity: LinkIdentity,
        group: EventLoopGroup = NIOTSEventLoopGroup.singleton,
        onWait: @escaping @Sendable () -> Void = {}
    ) async throws -> LinkTransport {
        let peerID = LinkIdentity.id(of: endpoint.serverKey)
        let room = linkRoom(identity.id, peerID)
        // The room as the relay's and the server's logs name it (T809).
        let tag = RelayWire.hex(room.prefix(4))
        linkLog.debug("dialing \(endpoint.relay.host() ?? "?", privacy: .public) for link room \(tag, privacy: .public), server \(RelayWire.hex(peerID.prefix(4)), privacy: .public)")
        let (lines, sink) = AsyncThrowingStream<Data, any Error>.makeStream()
        let up = group.next().makePromise(of: Void.self)
        let inner = try PinnedTLS.handler(identity: identity, peerKey: endpoint.serverKey)
        let channel = try await RelayDial.open(
            relay: endpoint.relay, identity: identity, kind: "link", room: room, peer: peerID,
            group: group, up: up, onWait: {
                linkLog.debug("waiting in link room \(tag, privacy: .public) for the server")
                onWait()
            }
        ) { ctx in
            let ops = ctx.pipeline.syncOperations
            try ops.addHandler(inner, position: .after(ctx.handler))
            try ops.addHandler(LinesHandler(sink: sink, up: up))
            ctx.eventLoop.scheduleTask(in: handshakeTimeout) {
                up.fail(LinkError.timedOut("on the TLS handshake"))
            }
        }
        return LinkTransport(channel: channel, lines: lines)
    }
}

/// The link's debug log (Console: subsystem dev.terminatr.ios).
let linkLog = Logger(subsystem: "dev.terminatr.ios", category: "link")

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
