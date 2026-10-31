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
/// handshake's end completes the dial. A line over maxLine (an older
/// tm's answer: since app API 37 the server's bridge never writes one)
/// is skipped rather than dropping the link (T892): its request gets a
/// too-big error in its place, which the Bridge names the call in.
final class LinesHandler: ChannelInboundHandler {
    typealias InboundIn = ByteBuffer

    /// The longest line taken, as the link's maxLine.
    static let maxLine = 1 << 20

    private let sink: AsyncThrowingStream<Data, any Error>.Continuation
    private let up: EventLoopPromise<Void>
    private var buf = ByteBuffer()
    private var error: Error?
    /// The line being skipped: its request's id (nil when its start
    /// didn't say), and how many of its bytes went.
    private var skipping: (id: Int?, bytes: Int)?

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
        for line in Self.split(&buf, skipping: &skipping, max: Self.maxLine) {
            switch line {
            case .line(let d): sink.yield(d)
            case .skipped(let id?, let n):
                linkLog.error("skipped a line of \(n) bytes, the answer to request \(id)")
                sink.yield(Self.tooBig(id: id, bytes: n))
            case .skipped(nil, _):
                // not an answer (a hello?): nothing to put in its place
                errorCaught(context: context, error: LinkError.protocolError("a line over \(Self.maxLine) bytes"))
                context.close(promise: nil)
                return
            }
        }
        buf.discardReadBytes()
    }

    enum Line: Equatable {
        case line(Data)
        /// A line over the bound, gone: its request's id, if its start
        /// said, and its size.
        case skipped(id: Int?, bytes: Int)
    }

    /// Takes the whole lines out of buf, and starts or goes on skipping
    /// one over max: a line ends at its newline; one longer than max,
    /// whole or not yet, is skipped to its newline.
    static func split(_ buf: inout ByteBuffer, skipping: inout (id: Int?, bytes: Int)?, max: Int) -> [Line] {
        var out: [Line] = []
        while true {
            let nl = buf.readableBytesView.firstIndex(of: UInt8(ascii: "\n"))
            if var s = skipping {
                guard let i = nl else {
                    s.bytes += buf.readableBytes
                    buf.moveReaderIndex(forwardBy: buf.readableBytes)
                    skipping = s
                    return out
                }
                s.bytes += i - buf.readerIndex
                buf.moveReaderIndex(to: i + 1)
                skipping = nil
                out.append(.skipped(id: s.id, bytes: s.bytes))
                continue
            }
            let n = (nl ?? buf.writerIndex) - buf.readerIndex
            if n > max {
                // its id is in its first bytes: tm writes "id" first
                let head = buf.getBytes(at: buf.readerIndex, length: min(n, 64)) ?? []
                guard let id = answerID(head) else {
                    out.append(.skipped(id: nil, bytes: n))
                    return out
                }
                skipping = (id: id, bytes: 0)
                continue
            }
            guard let i = nl else { return out }
            out.append(.line(Data(buf.readBytes(length: i - buf.readerIndex)!)))
            buf.moveReaderIndex(forwardBy: 1)
        }
    }

    /// The id a line of the bridge's starts with, {"id":N,…: an answer's
    /// or a stream's (a hello has none).
    static func answerID(_ head: [UInt8]) -> Int? {
        let prefix = Array(#"{"id":"#.utf8)
        guard head.starts(with: prefix) else { return nil }
        let digits = head.dropFirst(prefix.count).prefix { $0 >= UInt8(ascii: "0") && $0 <= UInt8(ascii: "9") }
        guard !digits.isEmpty, digits.count < 19 else { return nil }
        return Int(String(decoding: digits, as: UTF8.self))
    }

    /// The line in a skipped one's place: request id's too-big error,
    /// which ends it whether it is a call or a stream.
    static func tooBig(id: Int, bytes: Int) -> Data {
        let msg = "its answer is over the \(maxLine) bytes the link carries in a line (\(bytes) bytes), so the phone skipped it; a newer tm on the server cuts it"
        return Data(#"{"id":\#(id),"error":{"code":"too-big","message":"\#(msg)"},"end":true}"#.utf8)
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
