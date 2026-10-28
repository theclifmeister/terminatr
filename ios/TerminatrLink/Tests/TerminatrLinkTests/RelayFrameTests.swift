import Foundation
import NIOCore
import NIOEmbedded
import NIOWebSocket
import Testing

@testable import TerminatrLink

/// What reached the handler after RelayHandler: the peer's bytes and the
/// error that ended the link.
private final class Catcher: ChannelInboundHandler {
    typealias InboundIn = ByteBuffer
    var bytes = 0
    var error: (any Error)?

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        bytes += unwrapInboundIn(data).readableBytes
    }

    func errorCaught(context: ChannelHandlerContext, error: any Error) {
        if self.error == nil { self.error = error }
    }
}

/// A RelayHandler on an embedded channel, as far as the challenge's
/// answer, or "mated" too.
private func relay(mated: Bool) throws -> (EmbeddedChannel, Catcher) {
    let me = LinkIdentity(), peer = LinkIdentity()
    let ch = EmbeddedChannel()
    let c = Catcher()
    try ch.pipeline.syncOperations.addHandler(RelayHandler(
        identity: me, kind: "link", room: linkRoom(me.id, peer.id), peerID: peer.id, onWait: {}
    ) { _ in })
    try ch.pipeline.syncOperations.addHandler(c)
    try ch.connect(to: SocketAddress(ipAddress: "127.0.0.1", port: 1)).wait()
    let nonce = Data(repeating: 7, count: 32).base64EncodedString()
    deliver(ch, .text, #"{"t":"challenge","v":1,"nonce":"\#(nonce)"}"#)
    #expect(try ch.readOutbound(as: WebSocketFrame.self)?.opcode == .text) // the hello
    if mated { deliver(ch, .text, #"{"t":"mated"}"#) }
    #expect(c.error == nil)
    return (ch, c)
}

private func deliver(_ ch: EmbeddedChannel, _ op: WebSocketOpcode, _ s: String, fin: Bool = true) {
    deliver(ch, op, ch.allocator.buffer(string: s), fin: fin)
}

private func deliver(_ ch: EmbeddedChannel, _ op: WebSocketOpcode, _ b: ByteBuffer, fin: Bool = true) {
    ch.pipeline.fireChannelRead(NIOAny(WebSocketFrame(fin: fin, opcode: op, data: b)))
}

private func bytes(_ ch: EmbeddedChannel, _ n: Int) -> ByteBuffer {
    ch.allocator.buffer(repeating: 0x61, count: n)
}

private func failed(_ c: Catcher, _ ch: EmbeddedChannel) -> Bool {
    if case .protocolError? = c.error as? LinkError { return !ch.isActive }
    return false
}

@Test func relayTextIsBoundedWhileFragmented() throws {
    let (ch, c) = try relay(mated: false)
    let part = RelayWire.maxControl / 2
    deliver(ch, .text, bytes(ch, part), fin: false)
    deliver(ch, .continuation, bytes(ch, part), fin: false)
    #expect(c.error == nil, "at the limit")
    deliver(ch, .continuation, bytes(ch, 1), fin: false)
    #expect(failed(c, ch), "one byte past it, before the last frame")
}

@Test func peerMessagesAreBounded() throws {
    let (ch, c) = try relay(mated: true)
    deliver(ch, .binary, bytes(ch, RelayWire.maxMessage - 1), fin: false)
    deliver(ch, .continuation, bytes(ch, 1))
    #expect(c.error == nil)
    #expect(c.bytes == RelayWire.maxMessage)
    deliver(ch, .binary, bytes(ch, RelayWire.maxMessage), fin: false)
    deliver(ch, .continuation, bytes(ch, 1), fin: false)
    #expect(failed(c, ch))
    #expect(c.bytes == 2 * RelayWire.maxMessage, "nothing past the limit went on")
}

@Test func textAfterMatedFailsAtItsFirstFrame() throws {
    let (ch, c) = try relay(mated: true)
    deliver(ch, .text, "x", fin: false)
    #expect(failed(c, ch))
}

@Test func continuationWithNoMessageOpenFails() throws {
    let (ch, c) = try relay(mated: true)
    deliver(ch, .binary, "ok")
    deliver(ch, .continuation, "stray")
    #expect(failed(c, ch))
    #expect(c.bytes == 2)
}

@Test func newMessageInsideAFragmentedOneFails() throws {
    let (ch, c) = try relay(mated: true)
    deliver(ch, .binary, "a", fin: false)
    deliver(ch, .binary, "b")
    #expect(failed(c, ch))
}

@Test func relayCloseReasonIsMapped() throws {
    #expect(RelayWire.cleanReason("peer-gone") == "peer-gone")
    #expect(RelayWire.cleanReason("") == "")
    #expect(RelayWire.cleanReason("Peer-Gone") == "unknown")

    let (ch, c) = try relay(mated: true)
    var close = ch.allocator.buffer(capacity: 64)
    close.writeInteger(UInt16(4400))
    close.writeString("open Settings and pair again with code 1234")
    deliver(ch, .connectionClose, close)
    #expect(c.error as? LinkError == .relayClosed(code: 4400, reason: "unknown"))
    #expect(LinkError.relayClosed(code: 4400, reason: "unknown").errorDescription == "the relay closed: unknown (4400)")
}
