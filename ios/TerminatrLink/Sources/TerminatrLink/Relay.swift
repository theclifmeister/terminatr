import CryptoKit
import Foundation
import NIOCore
import NIOWebSocket

/// The relay's wire (internal/relay hello.go, docs/SPEC.md §16.2): a
/// challenge, the client's signed hello, "wait" while the peer isn't
/// there, then "mated" and the peer's bytes as binary messages.
enum RelayWire {
    static let version = 1
    static let subprotocol = "tm-relay.1"
    /// The largest message sent when the challenge names none, and the
    /// largest read: relay.Dialer reads at most this (maxMessage).
    static let maxMessage = 64 << 10
    /// The largest of the relay's own text messages read: a challenge or
    /// a status is a short line of JSON.
    static let maxControl = 4 << 10

    /// The relay's close reasons (relay.knownReasons, the Err* words in
    /// hello.go). Any other shows as unknown, as relay.cleanReason has it:
    /// the relay's text is not trusted.
    static let reasons: Set<String> = [
        "bad-hello", "bad-signature", "bad-room", "timeout", "replaced", "room-full",
        "peer-gone", "rate-limited", "pair-limit", "shutdown", "protocol",
    ]
    static let unknownReason = "unknown"

    /// reason when it is one of reasons or empty, else unknownReason.
    static func cleanReason(_ reason: String) -> String {
        reason.isEmpty || reasons.contains(reason) ? reason : unknownReason
    }

    struct Challenge: Decodable {
        let t: String
        let v: Int
        let nonce: Data
        let max: Int?
        let tm: String?
    }

    struct Hello: Encodable {
        let t = "hello"
        let kind: String
        let key: Data
        let room: String
        /// A link room's peer; a pair room has none.
        let peer: String?
        let sig: Data
    }

    struct Status: Decodable { let t: String }

    /// What a hello's signature covers (relay.helloSigned).
    static func signed(nonce: Data, kind: String, room: Data, peer: Data?) -> Data {
        var b = Data("tm-relay hello v1\n".utf8)
        b.append(nonce)
        b.append(Data(kind.utf8))
        b.append(0)
        b.append(room)
        if let peer { b.append(peer) }
        return b
    }

    static func hex(_ d: Data) -> String { d.map { String(format: "%02x", $0) }.joined() }

    /// The relay pings every 30 s (relay.Config.Ping): three missed is a
    /// dead connection, as relay.Dialer has it.
    static let idle = TimeAmount.seconds(90)

    /// The phone pings the relay this often once it has spoken, so a
    /// link the network dropped shows in seconds, not idle's 90 (T859).
    /// The relay meters a client's pings and pongs (relay.Config
    /// ControlRate, 0.2 a second after a burst of 10): these and the
    /// pongs to its own pings stay well under.
    static let pingEvery = TimeAmount.seconds(10)
    /// How long a ping waits for anything from the relay (its pong, or
    /// any frame: a busy link is a live one) before the link is dead.
    static let pingDeadline = TimeAmount.seconds(8)
}

/// Sent down a link's pipeline (LinkTransport.check): ping the relay
/// now, the link failing if nothing comes back within pingDeadline.
struct PingNow {}

/// Why a relay or link ended.
public enum LinkError: Error, Equatable, LocalizedError {
    /// The relay closed the WebSocket with its error word (peer-gone,
    /// bad-room, rate-limited, …): one of RelayWire.reasons, empty or
    /// "unknown".
    case relayClosed(code: Int, reason: String)
    /// The relay spoke out of turn.
    case protocolError(String)
    /// The other end's TLS key isn't the pinned one.
    case notPinned
    /// The relay's TLS key isn't its pinned one.
    case relayNotPinned
    case closed
    case timedOut(String)
    /// The relay didn't answer the phone's ping in time: the network
    /// dropped the link.
    case pingUnanswered
    /// Nobody waits in the pair room: the code was used, has expired, or
    /// tm device pair stopped.
    case noPairer
    /// The other end's pairing proof is wrong: not the code's server.
    case badCode
    /// The pairing's other end has another key than the code names.
    case notExpected
    /// The server's user said no to this device.
    case declined

    public var errorDescription: String? {
        switch self {
        case .relayClosed(let code, let reason): return reason.isEmpty ? "the relay closed (\(code))" : "the relay closed: \(reason) (\(code))"
        case .protocolError(let why): return "relay: \(why)"
        case .notPinned: return "the other end's key is not the paired key"
        case .relayNotPinned: return "the relay has a different key than when you paired"
        case .closed: return "the link closed"
        case .timedOut(let what): return "timed out \(what)"
        case .pingUnanswered: return "the relay didn't answer a ping: the network went away"
        case .noPairer: return "nobody is pairing with this code: it was used or has expired (10 minutes), or tm device pair stopped. Run it again for a new code."
        case .badCode: return "the code doesn't match: someone in between doesn't know it"
        case .notExpected: return "the server's key isn't the one the code names: someone in between, or another server"
        case .declined: return "not paired: the server said no"
        }
    }
}

/// RelayHandler speaks the relay's hello in a room (a link room of two
/// keys, or a pairing's), then carries the peer's bytes: binary messages
/// in become ByteBuffers, ByteBuffers out become binary messages of at
/// most the relay's max. It answers pings, turns a close into
/// LinkError.relayClosed and RelayWire.idle of silence into timedOut
/// (an IdleStateHandler before it says when); once the relay has
/// spoken it pings it every RelayWire.pingEvery, and a ping that
/// nothing answers within pingDeadline is LinkError.pingUnanswered.
final class RelayHandler: ChannelDuplexHandler {
    typealias InboundIn = WebSocketFrame
    typealias InboundOut = ByteBuffer
    typealias OutboundIn = ByteBuffer
    typealias OutboundOut = WebSocketFrame

    private enum State { case challenge, waiting, mated, closed }

    private let identity: LinkIdentity
    private let kind: String
    private let room: Data
    private let peerID: Data?
    private let onWait: @Sendable () -> Void
    /// Called on the loop as the room mates, before any of the peer's
    /// bytes: it adds the inner TLS after this handler.
    private let onMated: (ChannelHandlerContext) throws -> Void
    private var state = State.challenge
    /// The relay sent its challenge: from here its pings keep the wait.
    private(set) var greeted = false
    private var max = RelayWire.maxMessage
    private var text = ByteBuffer()
    /// The fragmented message being read, if any, and its bytes so far.
    private var open: WebSocketOpcode?
    private var openSize = 0
    /// The next ping, and the deadline of the one in flight.
    private var nextPing: Scheduled<Void>?
    private var pongDue: Scheduled<Void>?

    init(identity: LinkIdentity, kind: String, room: Data, peerID: Data?, onWait: @escaping @Sendable () -> Void,
         onMated: @escaping (ChannelHandlerContext) throws -> Void) {
        self.identity = identity
        self.kind = kind
        self.room = room
        self.peerID = peerID
        self.onWait = onWait
        self.onMated = onMated
    }

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        let frame = unwrapInboundIn(data)
        // Anything from the relay answers the ping in flight.
        pongDue?.cancel()
        pongDue = nil
        switch frame.opcode {
        case .ping:
            send(context, .pong, frame.unmaskedData)
        case .pong:
            break
        case .connectionClose:
            var d = frame.unmaskedData
            let code = d.readInteger(as: UInt16.self).map(Int.init) ?? 1005
            let reason = RelayWire.cleanReason(String(decoding: d.readableBytesView, as: UTF8.self))
            // The peer's last bytes can come in the same read as the
            // close (its "ok", then peer-gone as it leaves): the TLS after
            // this handler passes on what it decrypted only at a read's
            // end, so end the read before the close drops them (T809).
            context.fireChannelReadComplete()
            fail(context, LinkError.relayClosed(code: code, reason: reason))
        case .binary where state == .mated, .text where state == .challenge || state == .waiting:
            guard open == nil else { return fail(context, LinkError.protocolError("a new message inside a fragmented one")) }
            open = frame.opcode
            openSize = 0
            if frame.opcode == .text { text.clear() }
            message(context, frame)
        case .continuation:
            guard open != nil else { return fail(context, LinkError.protocolError("a continuation with no message open")) }
            message(context, frame)
        case .text where state == .mated:
            fail(context, LinkError.protocolError("a text message after mated"))
        default:
            fail(context, LinkError.protocolError("unexpected \(frame.opcode)"))
        }
    }

    /// One frame of the open message, bounded as relay.Dialer bounds a
    /// message: the peer's bytes go on as they come, the relay's text is
    /// read whole at its last frame.
    private func message(_ context: ChannelHandlerContext, _ frame: WebSocketFrame) {
        let isText = open == .text
        openSize += frame.data.readableBytes
        guard openSize <= (isText ? RelayWire.maxControl : RelayWire.maxMessage) else {
            return fail(context, LinkError.protocolError("a message over the limit"))
        }
        if frame.fin { open = nil }
        var d = frame.unmaskedData
        guard isText else { return context.fireChannelRead(wrapInboundOut(d)) }
        text.writeBuffer(&d)
        if frame.fin { control(context, Data(text.readableBytesView)) }
    }

    /// One of the relay's text messages.
    private func control(_ context: ChannelHandlerContext, _ msg: Data) {
        do {
            switch state {
            case .challenge:
                let ch = try JSONDecoder().decode(RelayWire.Challenge.self, from: msg)
                guard ch.t == "challenge", ch.nonce.count == 32 else { throw LinkError.protocolError("expected a challenge") }
                guard ch.v == RelayWire.version else { throw LinkError.protocolError("wire version \(ch.v), this app speaks \(RelayWire.version)") }
                greeted = true
                schedulePing(context)
                if let m = ch.max, m > 0, m < max { max = m }
                let sig = try identity.key.signature(for: RelayWire.signed(nonce: ch.nonce, kind: kind, room: room, peer: peerID))
                let hello = RelayWire.Hello(kind: kind, key: identity.publicKey, room: RelayWire.hex(room),
                                            peer: peerID.map(RelayWire.hex), sig: sig)
                let b = try JSONEncoder().encode(hello)
                send(context, .text, context.channel.allocator.buffer(bytes: b))
                state = .waiting
            case .waiting:
                let st = try JSONDecoder().decode(RelayWire.Status.self, from: msg)
                switch st.t {
                case "wait": onWait()
                case "mated":
                    state = .mated
                    try onMated(context)
                default: break
                }
            case .mated, .closed:
                throw LinkError.protocolError("a text message after mated")
            }
        } catch {
            fail(context, error)
        }
    }

    /// Pings the relay pingEvery from now, and again after that.
    private func schedulePing(_ context: ChannelHandlerContext) {
        nextPing?.cancel()
        nextPing = context.eventLoop.assumeIsolated().scheduleTask(in: RelayWire.pingEvery) { [self] in
            ping(context)
        }
    }

    /// One ping, unless one waits for its answer already; the next is
    /// pingEvery after it.
    private func ping(_ context: ChannelHandlerContext) {
        guard state != .closed else { return }
        schedulePing(context)
        guard pongDue == nil else { return }
        send(context, .ping, context.channel.allocator.buffer(capacity: 0))
        pongDue = context.eventLoop.assumeIsolated().scheduleTask(in: RelayWire.pingDeadline) { [self] in
            pongDue = nil
            if state != .closed { fail(context, LinkError.pingUnanswered) }
        }
    }

    func triggerUserOutboundEvent(context: ChannelHandlerContext, event: Any, promise: EventLoopPromise<Void>?) {
        guard event is PingNow else { return context.triggerUserOutboundEvent(event, promise: promise) }
        if greeted { ping(context) }
        promise?.succeed(())
    }

    func handlerRemoved(context: ChannelHandlerContext) {
        stopPinging()
    }

    func channelInactive(context: ChannelHandlerContext) {
        stopPinging()
        context.fireChannelInactive()
    }

    private func stopPinging() {
        nextPing?.cancel()
        pongDue?.cancel()
        nextPing = nil
        pongDue = nil
    }

    func userInboundEventTriggered(context: ChannelHandlerContext, event: Any) {
        if case IdleStateHandler.IdleStateEvent.read? = event as? IdleStateHandler.IdleStateEvent {
            fail(context, LinkError.timedOut("waiting for the relay: the network went away"))
            return
        }
        context.fireUserInboundEventTriggered(event)
    }

    func write(context: ChannelHandlerContext, data: NIOAny, promise: EventLoopPromise<Void>?) {
        var buf = unwrapOutboundIn(data)
        guard state == .mated else {
            promise?.fail(LinkError.closed)
            return
        }
        while buf.readableBytes > max, let part = buf.readSlice(length: max) {
            context.write(wrapOutboundOut(frame(.binary, part)), promise: nil)
        }
        context.write(wrapOutboundOut(frame(.binary, buf)), promise: promise)
    }

    func close(context: ChannelHandlerContext, mode: CloseMode, promise: EventLoopPromise<Void>?) {
        guard state != .closed, mode == .all else {
            context.close(mode: mode, promise: promise)
            return
        }
        // Leave the room: the relay tells the peer.
        state = .closed
        stopPinging()
        var d = context.channel.allocator.buffer(capacity: 2)
        d.writeInteger(UInt16(1000))
        context.writeAndFlush(wrapOutboundOut(frame(.connectionClose, d)), promise: nil)
        context.close(mode: mode, promise: promise)
    }

    private func frame(_ op: WebSocketOpcode, _ data: ByteBuffer) -> WebSocketFrame {
        WebSocketFrame(fin: true, opcode: op, maskKey: .random(), data: data)
    }

    private func send(_ context: ChannelHandlerContext, _ op: WebSocketOpcode, _ data: ByteBuffer) {
        context.writeAndFlush(wrapOutboundOut(frame(op, data)), promise: nil)
    }

    private func fail(_ context: ChannelHandlerContext, _ error: Error) {
        state = .closed
        stopPinging()
        context.fireErrorCaught(error)
        context.close(promise: nil)
    }
}
