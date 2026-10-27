import CoreGraphics
import Foundation
import ImageIO
import Testing
import UniformTypeIdentifiers
@testable import TerminatrKit

@Test func attachingNeedsAPI13() {
    var h = AppAPI.Hello(appApi: 13, tm: "v0.57.0", build: "x")
    #expect(!Compatibility.takesFiles(h))
    h.methods = [AppAPI.Methods.sessionUpload.name]
    #expect(Compatibility.takesFiles(h))
    h.appApi = 12
    #expect(!Compatibility.takesFiles(h))
}

@Test func composerRefusesWhatTMWont() {
    #expect(ComposerFiles.refusal(name: "shot.PNG", size: 10) == nil)
    #expect(ComposerFiles.refusal(name: "notes.md", size: 10) == nil)
    #expect(ComposerFiles.refusal(name: "id_rsa.key", size: 10) != nil)
    #expect(ComposerFiles.refusal(name: ".env.txt", size: 10) != nil)
    #expect(ComposerFiles.refusal(name: "a.zip", size: 10) != nil)
    #expect(ComposerFiles.refusal(name: "a.png", size: 0) != nil)
    #expect(ComposerFiles.refusal(name: "a.png", size: ComposerFiles.maxSize + 1) != nil)
}

@Test func composerFileDetail() {
    var f = ComposerFile(name: "a.png", size: 200)
    #expect(f.uploading && f.ref == nil && f.detail == "0%")
    f.state = .uploading(100)
    #expect(f.detail == "50%")
    f.state = .ready(ref: "20261027-120000-a.png")
    #expect(!f.uploading && f.ref == "20261027-120000-a.png")
    f.state = .failed("no")
    #expect(f.detail == "no" && f.ref == nil)
}

/// A 2×2 image in format type.
private func image(_ type: UTType) -> Data {
    let ctx = CGContext(data: nil, width: 2, height: 2, bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    let out = NSMutableData()
    let dst = CGImageDestinationCreateWithData(out, type.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(dst, ctx.makeImage()!, nil)
    CGImageDestinationFinalize(dst)
    return out as Data
}

@Test func pastedTIFFBecomesPNG() {
    let png = ComposerFiles.png(image(.tiff))
    #expect(png?.starts(with: [0x89, 0x50, 0x4E, 0x47]) == true)
    #expect(ComposerFiles.png(Data("not an image".utf8)) == nil)
    #expect(ComposerFiles.pastedName(Date(timeIntervalSince1970: 0)).hasSuffix(".png"))
}

/// Records session.upload calls and answers as tm does.
private actor FakeTM {
    var calls: [AppAPI.UploadParams] = []
    var got = 0

    func call(_ p: AppAPI.UploadParams) -> AppAPI.Upload {
        calls.append(p)
        got += p.data.count
        let size = calls[0].size
        if got == size { return AppAPI.Upload(upload: "u1", received: got, ref: "1-" + calls[0].name, name: "1-" + calls[0].name, size: size) }
        return AppAPI.Upload(upload: "u1", received: got)
    }
}

@Test func uploadSendsChunks() async throws {
    let data = Data(repeating: 7, count: ComposerFiles.chunk * 2 + 10)
    let tm = FakeTM()
    var progress: [Int] = []
    let r = try await ComposerFiles.upload(data, name: "big.txt", session: "s-1", call: { p in await tm.call(p) },
                                           progress: { progress.append($0) })
    #expect(r.ref == "1-big.txt")
    let calls = await tm.calls
    #expect(calls.count == 3)
    #expect(calls[0].upload.isEmpty && calls[0].name == "big.txt" && calls[0].size == data.count && calls[0].offset == 0)
    #expect(calls[1].upload == "u1" && calls[1].name.isEmpty && calls[1].offset == ComposerFiles.chunk)
    #expect(calls[2].offset == ComposerFiles.chunk * 2 && calls[2].data.count == 10)
    #expect(progress == [ComposerFiles.chunk, ComposerFiles.chunk * 2])
    // One chunk.
    let small = try await ComposerFiles.upload(Data("hi".utf8), name: "a.md", session: "s-1",
                                               call: { _ in AppAPI.Upload(upload: "u2", received: 2, ref: "r", size: 2) })
    #expect(small.ref == "r")
    // An answer that lost bytes throws.
    await #expect(throws: ComposerFiles.UploadError.lost) {
        _ = try await ComposerFiles.upload(data, name: "a.md", session: "s-1", call: { _ in AppAPI.Upload(upload: "u3", received: 1) })
    }
}
