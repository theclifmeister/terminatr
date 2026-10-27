import CoreGraphics
import Foundation
import ImageIO
import Testing
import UniformTypeIdentifiers
@testable import TerminatrKit

private func file(_ ref: String, _ name: String, path: String = "", type: String = "") -> AppAPI.TranscriptFile {
    AppAPI.TranscriptFile(ref: ref, name: name, path: path, type: type)
}

/// A SendUserFile call is its own entry, a card, whatever runs of tool
/// calls are around it; its result folds into it; a pasted image goes
/// with its prompt.
@Test func sentFilesEntries() {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [
        AppAPI.TranscriptItem(kind: "prompt", text: "what is this?", files: [file("0.a.1.0", "Image #1", type: "image/png")]),
        AppAPI.TranscriptItem(kind: "prompt", files: [file("1.b.0.0", "Image #1", type: "image/jpeg")]),
        AppAPI.TranscriptItem(kind: "tool", text: "a.go", tool: "Read", call: "r1"),
        AppAPI.TranscriptItem(kind: "tool", text: "The shot", tool: "SendUserFile", call: "s1",
                              files: [file("2.c.0.0", "shot.png", path: "/tmp/s/shot.png", type: "image/png"),
                                      file("2.c.0.1", "report.pdf", path: "/tmp/s/report.pdf", type: "application/pdf"),
                                      file("2.c.0.2", "id_rsa", path: "/tmp/s/id_rsa")]),
        AppAPI.TranscriptItem(kind: "tool", text: "b.go", tool: "Read", call: "r2"),
        AppAPI.TranscriptItem(kind: "result", text: "sent", call: "s1"),
        AppAPI.TranscriptItem(kind: "tool", text: "", tool: "SendUserFile", call: "s2",
                              files: [file("3.d.0.0", "logo.svg", path: "/tmp/logo.svg", type: "image/svg+xml")], display: "attach"),
    ]))
    #expect(t.entries.map(\.kind) == [.prompt, .prompt, .tool, .files, .tool, .files])
    #expect(t.entries[3].result?.text == "sent")
    #expect(t.entries[3].text == "The shot")
    #expect(t.entries[3].who(agent: "claude") == "claude")

    let sent = t.entries[3].attachments
    #expect(sent.map(\.kind) == [.image, .pdf, .other])
    #expect(sent.map(\.inline) == [true, true, false])
    #expect(sent.map(\.sendable) == [true, true, false])
    #expect(sent[0].path == "/tmp/s/shot.png" && sent[0].id == "2.c.0.0")
    // display "attach": a card without a preview.
    #expect(t.entries[5].attachments.map(\.inline) == [false])
    #expect(t.entries[5].attachments[0].kind == .image)

    let pasted = t.entries[0].attachments
    #expect(pasted.count == 1 && pasted[0].inline && pasted[0].path.isEmpty)
    #expect(t.entries[1].text.isEmpty && t.entries[1].attachments[0].type == "image/jpeg")
    #expect(Transcript.find("report.pdf", in: t.entries) == [t.entries[3].id])
}

@Test func attachmentSize() {
    #expect(Attachment.size(2_400_000).contains("MB"))
}

/// A fake session.file over files by ref: chunks of chunk bytes; a
/// missing ref answers gone.
private actor FakeFiles {
    var files: [String: (data: Data, type: String, mtime: Date)] = [:]
    var reads = 0
    var stats = 0
    let chunk: Int
    /// Changes the file after this many chunk reads (a writer mid-read).
    var changeAfter: Int?

    init(chunk: Int = 4) { self.chunk = chunk }

    func put(_ ref: String, _ data: Data, type: String, mtime: Date = Date(timeIntervalSince1970: 1)) {
        files[ref] = (data, type, mtime)
    }

    func remove(_ ref: String) { files[ref] = nil }

    func setChangeAfter(_ n: Int?) { changeAfter = n }

    func answer(_ p: AppAPI.SessionFileParams) throws -> AppAPI.SessionFile {
        guard let f = files[p.ref] else { throw AppAPI.Error(code: "gone", message: "no longer there") }
        let sendable = !f.type.isEmpty
        if p.stat {
            stats += 1
            return AppAPI.SessionFile(name: p.ref, type: f.type, size: f.data.count, mtime: f.mtime, sendable: sendable, offset: 0)
        }
        guard sendable else { throw AppAPI.Error(code: "refused", message: "not sent") }
        reads += 1
        if let n = changeAfter, reads > n {
            files[p.ref] = (f.data + Data("more".utf8), f.type, f.mtime.addingTimeInterval(1))
            changeAfter = nil
        }
        let g = files[p.ref]!
        let end = min(g.data.count, p.offset + chunk)
        return AppAPI.SessionFile(name: p.ref, type: g.type, size: g.data.count, mtime: g.mtime, sendable: true, offset: p.offset,
                                  data: g.data.subdata(in: p.offset..<end))
    }
}

private func store(_ fake: FakeFiles, maxBytes: Int = 1 << 20) -> FileStore {
    FileStore(maxBytes: maxBytes) { p in try await fake.answer(p) }
}

@Test func fileStoreReadsInChunksAndKeeps() async {
    let fake = FakeFiles(chunk: 4)
    await fake.put("a", Data("0123456789".utf8), type: "text/plain")
    let s = store(fake)
    #expect(await s.data("s-1", "a") == .data(Data("0123456789".utf8), Date(timeIntervalSince1970: 1)))
    #expect(await fake.reads == 3)
    // Kept by ref and time: no second read.
    _ = await s.data("s-1", "a")
    #expect(await fake.reads == 3)
    // Changed since: read again.
    await fake.put("a", Data("new".utf8), type: "text/plain", mtime: Date(timeIntervalSince1970: 2))
    #expect(await s.data("s-1", "a") == .data(Data("new".utf8), Date(timeIntervalSince1970: 2)))
}

@Test func fileStoreGoneAndRefused() async {
    let fake = FakeFiles()
    await fake.put("key", Data("KEY".utf8), type: "")
    let s = store(fake)
    #expect(await s.data("s-1", "nope") == .gone)
    if case .unavailable = await s.data("s-1", "key") {} else { Issue.record("a file tm doesn't send") }
    #expect(await fake.reads == 0)
    if case .failure(.gone) = await s.thumbnail("s-1", "nope", maxPixel: 64) {} else { Issue.record("thumbnail of a gone file") }
    // A stat answers a file it doesn't send.
    if case .success(let st) = await s.stat("s-1", "key") { #expect(st.size == 3 && !st.sendable) } else { Issue.record("stat") }
    // Gone after it was kept: the stat says so first.
    await fake.put("a", Data("x".utf8), type: "text/plain")
    _ = await s.data("s-1", "a")
    await fake.remove("a")
    #expect(await s.data("s-1", "a") == .gone)
}

@Test func fileStoreRereadsAFileChangedMidRead() async {
    let fake = FakeFiles(chunk: 4)
    await fake.put("a", Data("0123456789".utf8), type: "text/plain")
    await fake.setChangeAfter(1)
    let s = store(fake)
    #expect(await s.data("s-1", "a") == .data(Data("0123456789more".utf8), Date(timeIntervalSince1970: 2)))
}

/// A PNG of w×h pixels.
private func png(_ w: Int, _ h: Int) -> Data {
    let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    ctx.setFillColor(CGColor(red: 1, green: 0, blue: 0, alpha: 1))
    ctx.fill(CGRect(x: 0, y: 0, width: w, height: h))
    let out = NSMutableData()
    let dest = CGImageDestinationCreateWithData(out, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, ctx.makeImage()!, nil)
    CGImageDestinationFinalize(dest)
    return out as Data
}

@Test func fileStoreThumbnailsDownsample() async throws {
    let fake = FakeFiles(chunk: 1 << 20)
    await fake.put("big", png(3000, 1500), type: "image/png")
    await fake.put("bad", Data("not an image".utf8), type: "image/png")
    let s = store(fake)
    guard case .success(let img?) = await s.thumbnail("s-1", "big", maxPixel: 400) else {
        Issue.record("no thumbnail")
        return
    }
    #expect(img.width == 400 && img.height == 200)
    let reads = await fake.reads
    _ = await s.thumbnail("s-1", "big", maxPixel: 400)
    #expect(await fake.reads == reads)
    if case .success(let none) = await s.thumbnail("s-1", "bad", maxPixel: 400) { #expect(none == nil) } else { Issue.record("bad image") }
}

@Test func filesNeedAPI11() {
    let caps = AppAPI.Server(version: "0.56.0", protocol: 22, caps: ["transcript", "session-file"])
    var h = AppAPI.Hello(appApi: 11, tm: "0.56.0", build: "", capabilities: [], methods: ["session.file"], server: caps)
    #expect(Compatibility.sendsFiles(h))
    h.server?.caps = ["transcript"]
    #expect(!Compatibility.sendsFiles(h))
    h = AppAPI.Hello(appApi: 10, tm: "0.55.0", build: "", capabilities: [], methods: [], server: caps)
    #expect(!Compatibility.sendsFiles(h))
}

/// Tool runs (#822) never fold a SendUserFile call: it stays its own row
/// between the runs it splits.
@Test func sentFilesSplitToolRuns() {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [
        AppAPI.TranscriptItem(kind: "tool", text: "a", tool: "Read", call: "1"),
        AppAPI.TranscriptItem(kind: "tool", text: "b", tool: "Read", call: "2"),
        AppAPI.TranscriptItem(kind: "tool", tool: "SendUserFile", call: "3", files: [file("0.a.0.0", "a.png", path: "/a.png", type: "image/png")]),
        AppAPI.TranscriptItem(kind: "tool", text: "c", tool: "Read", call: "4"),
        AppAPI.TranscriptItem(kind: "tool", text: "d", tool: "Read", call: "5"),
    ]))
    let rows = Transcript.rows(t.entries)
    #expect(rows.count == 3)
    guard rows.count == 3, case .entry(let e) = rows[1] else {
        Issue.record("no card row: \(rows)")
        return
    }
    #expect(e.kind == .files)
}
