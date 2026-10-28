import CryptoKit
import Foundation
import NIOCore
import NIOEmbedded
import NIOSSL
import NIOWebSocket
import Testing

@testable import TerminatrLink

// T809: the server's last line and the relay's close can come in one
// read, as a relay across the internet sends them (the pairing's final
// "ok", then peer-gone as the server leaves). NIOSSL hands on what it
// decrypted only at channelReadComplete, so the line must reach the
// lines before the close ends the channel; else the phone's pairing
// failed with peer-gone (4410) though the server had paired it.
@Test func lastLineBeforeTheRelaysCloseArrives() async throws {
    let phone = LinkIdentity(), server = LinkIdentity()
    let ch = EmbeddedChannel()
    let up = ch.eventLoop.makePromise(of: Void.self)
    let (lines, sink) = AsyncThrowingStream<Data, any Error>.makeStream()
    let tls = try PinnedTLS.handler(identity: phone, peerKey: server.publicKey)
    let room = linkRoom(phone.id, server.id)
    try ch.pipeline.syncOperations.addHandler(RelayHandler(
        identity: phone, kind: "link", room: room, peerID: server.id, onWait: {}
    ) { ctx in
        let ops = ctx.pipeline.syncOperations
        try ops.addHandler(tls, position: .after(ctx.handler))
        try ops.addHandler(LinesHandler(sink: sink, up: up))
    })
    let srv = EmbeddedChannel()
    try srv.pipeline.syncOperations.addHandler(try PinnedTLS.handler(identity: server, peerKey: phone.publicKey))
    try srv.connect(to: SocketAddress(ipAddress: "127.0.0.1", port: 1)).wait()

    func text(_ s: String) -> WebSocketFrame {
        WebSocketFrame(fin: true, opcode: .text, data: ch.allocator.buffer(string: s))
    }
    func deliver(_ frames: [WebSocketFrame]) {
        for f in frames { ch.pipeline.fireChannelRead(NIOAny(f)) }
        ch.pipeline.fireChannelReadComplete()
    }
    /// Carries each end's bytes to the other until neither has more.
    func shuttle() throws {
        while true {
            var moved = false
            ch.embeddedEventLoop.run()
            srv.embeddedEventLoop.run()
            while let f = try ch.readOutbound(as: WebSocketFrame.self) {
                if f.opcode == .binary { try srv.writeInbound(f.data) }
                moved = true
            }
            var out: [WebSocketFrame] = []
            while let b = try srv.readOutbound(as: ByteBuffer.self) {
                out.append(WebSocketFrame(fin: true, opcode: .binary, data: b))
            }
            if !out.isEmpty {
                deliver(out)
                moved = true
            }
            if !moved { return }
        }
    }

    try ch.connect(to: SocketAddress(ipAddress: "127.0.0.1", port: 1)).wait()
    let nonce = Data(repeating: 7, count: 32).base64EncodedString()
    deliver([text(#"{"t":"challenge","v":1,"nonce":"\#(nonce)","max":65536}"#)])
    #expect(try ch.readOutbound(as: WebSocketFrame.self)?.opcode == .text) // the hello
    deliver([text(#"{"t":"mated"}"#)])
    try shuttle()
    var hs = false
    up.futureResult.whenSuccess { hs = true }
    #expect(hs, "the TLS handshake")

    // The server's last line, then the relay's close, in one read.
    srv.writeAndFlush(srv.allocator.buffer(string: "{\"ok\":true}\n"), promise: nil)
    var last: [WebSocketFrame] = []
    while let b = try srv.readOutbound(as: ByteBuffer.self) {
        last.append(WebSocketFrame(fin: true, opcode: .binary, data: b))
    }
    var close = ch.allocator.buffer(capacity: 16)
    close.writeInteger(UInt16(4410))
    close.writeString("peer-gone")
    last.append(WebSocketFrame(fin: true, opcode: .connectionClose, data: close))
    deliver(last)
    _ = try? ch.finish()

    // A channel that never ended would leave the lines open: end them.
    let guardTask = Task {
        try await Task.sleep(for: .seconds(5))
        sink.finish(throwing: LinkError.timedOut("the test: the lines never ended"))
    }
    defer { guardTask.cancel() }
    var got: [String] = []
    var failure: (any Error)?
    do {
        for try await l in lines { got.append(String(decoding: l, as: UTF8.self)) }
    } catch {
        failure = error
    }
    #expect(got == [#"{"ok":true}"#])
    #expect(failure as? LinkError == .relayClosed(code: 4410, reason: "peer-gone"))
}
