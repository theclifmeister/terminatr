import Foundation
import NIOCore
import NIOEmbedded
import Testing

@testable import TerminatrLink

// T892: a line over the link's maxLine (an older tm's 1 MiB session.file
// chunk in base64) used to drop the link, and the phone asked again on
// the redial. Now it is skipped, however it comes in reads, and its
// request gets a too-big error in its place; the lines around it go on.
@Test func aLongLineIsSkippedForItsRequestsError() throws {
    let max = 64
    var buf = ByteBuffer()
    var skipping: (id: Int?, bytes: Int)?
    let long = #"{"id":42,"result":""# + String(repeating: "x", count: 200) + #""}"#
    var got: [LinesHandler.Line] = []
    // in pieces: the start of the long line, its middle, its end with
    // the next line and half of another
    let all = #"{"id":1}"# + "\n" + long + "\n" + #"{"id":2}"# + "\n" + #"{"id":3"#
    let bytes = Array(all.utf8)
    for chunk in stride(from: 0, to: bytes.count, by: 50).map({ Array(bytes[$0..<min($0 + 50, bytes.count)]) }) {
        buf.writeBytes(chunk)
        got += LinesHandler.split(&buf, skipping: &skipping, max: max)
        buf.discardReadBytes()
    }
    #expect(got == [
        .line(Data(#"{"id":1}"#.utf8)),
        .skipped(id: 42, bytes: long.utf8.count),
        .line(Data(#"{"id":2}"#.utf8)),
    ])
    #expect(skipping == nil && buf.readableBytes == #"{"id":3"#.utf8.count)

    // The error in its place parses, names the size and ends the request.
    let e = try JSONSerialization.jsonObject(with: LinesHandler.tooBig(id: 42, bytes: 1_400_000)) as! [String: Any]
    let err = e["error"] as! [String: String]
    #expect(e["id"] as? Int == 42 && e["end"] as? Bool == true && err["code"] == "too-big")
    #expect(err["message"]!.contains("1400000 bytes"))
}

@Test func aLongLineWithoutAnIDEndsTheLink() {
    var buf = ByteBuffer(string: #"{"hello":"# + String(repeating: "x", count: 100))
    var skipping: (id: Int?, bytes: Int)?
    #expect(LinesHandler.split(&buf, skipping: &skipping, max: 64) == [.skipped(id: nil, bytes: 109)])
    #expect(LinesHandler.answerID(Array(#"{"id":7,"event":"x"}"#.utf8)) == 7)
    #expect(LinesHandler.answerID(Array(#"{"hello":{}}"#.utf8)) == nil)
}
