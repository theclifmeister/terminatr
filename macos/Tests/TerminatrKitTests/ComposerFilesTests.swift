import CoreGraphics
import Foundation
import ImageIO
import Testing
import UniformTypeIdentifiers
@testable import TerminatrKit

@Test func attachingNeedsAPI15() {
    var h = AppAPI.Hello(appApi: 15, tm: "v0.57.0", build: "x")
    #expect(!Compatibility.takesFiles(h))
    h.methods = [AppAPI.Methods.sessionUpload.name]
    #expect(Compatibility.takesFiles(h))
    h.appApi = 14
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

/// A chunk's base64 with the rest of its signed line fits the bridge's
/// line, the phone's and tm's chunk both.
@Test func chunksFitTheBridgesLine() throws {
    #expect(ComposerFiles.chunk(lineLimit: ComposerFiles.maxLine) == ComposerFiles.chunk)
    #expect(ComposerFiles.chunk(lineLimit: 64 << 10) == 45 << 10)
    #expect(ComposerFiles.phoneChunk < ComposerFiles.chunk)
    for size in [ComposerFiles.chunk, ComposerFiles.phoneChunk] {
        let p = AppAPI.UploadParams(session: "s-1234", upload: String(repeating: "f", count: 24), name: String(repeating: "n", count: 255),
                                    size: ComposerFiles.maxSize, offset: ComposerFiles.maxSize, data: Data(repeating: 0xff, count: size))
        let enc = AppAPI.encoder()
        let auth = AppAPI.Auth(nonce: Data(repeating: 1, count: 32), sig: Data(repeating: 2, count: 72))
        let line = try SignedWrite.line(id: Int.max, method: AppAPI.Methods.sessionUpload.name, params: enc.encode(p), auth: auth, encoder: enc)
        #expect(line.count + 1 <= ComposerFiles.maxLine)
        #expect(line.count - size * 4 / 3 < ComposerFiles.lineOverhead)
    }
}

/// tm's session.upload as the server keeps it: an upload's bytes by its
/// id, a chunk only at the bytes in.
@MainActor private final class KeptTM {
    var uploads: [String: (size: Int, got: Int, name: String)] = [:]
    var calls: [AppAPI.UploadParams] = []
    /// Throws this before the call of that number (from 0) lands.
    var failAt: [Int: any Error] = [:]
    var next = 1

    func call(_ p: AppAPI.UploadParams) throws -> AppAPI.Upload {
        defer { calls.append(p) }
        if let e = failAt.removeValue(forKey: calls.count) { throw e }
        var id = p.upload
        if id.isEmpty {
            id = "u\(next)"
            next += 1
            uploads[id] = (p.size, 0, p.name)
        }
        guard let u = uploads[id], p.offset == u.got else { throw AppAPI.Error(code: "bad-params", message: "chunk at \(p.offset)") }
        let got = u.got + p.data.count
        uploads[id]?.got = got
        if got == u.size { return AppAPI.Upload(upload: id, received: got, ref: "ref-" + u.name, size: got) }
        return AppAPI.Upload(upload: id, received: got)
    }
}

/// Waits for the tray's uploads to settle.
@MainActor private func settle(_ t: ComposerTray) async {
    for _ in 0..<200 where t.uploading { try? await Task.sleep(for: .milliseconds(5)) }
}

private struct Dropped: Error {}

@Test @MainActor func trayUploadsResumesAndRetries() async {
    let tm = KeptTM()
    let t = ComposerTray(chunk: 4 << 10) { p in try tm.call(p) }
    t.dropped = { $0 is Dropped }
    // Refused at once: no upload.
    t.add(name: "a.zip", data: Data([1]))
    #expect(t.files[0].failed && !t.files[0].canRetry && tm.calls.isEmpty)
    // A drop at the third chunk: it waits, then goes on from the second.
    tm.failAt[2] = Dropped()
    t.add(name: "shot.png", data: Data(repeating: 1, count: 10 << 10))
    await settle(t)
    let f = t.files[1]
    #expect(f.failed && f.canRetry && f.detail.contains("link dropped"))
    t.resumeDropped()
    await settle(t)
    #expect(t.files[1].ref == "ref-shot.png" && t.refs == ["ref-shot.png"])
    #expect(tm.calls.map(\.offset) == [0, 4096, 8192, 8192])
    #expect(tm.calls.last?.upload == "u1")
    // An error that isn't a drop says Retry; tm lost it, so from the start.
    tm.failAt[tm.calls.count + 1] = AppAPI.Error(code: "internal", message: "disk full")
    t.add(name: "b.md", data: Data(repeating: 65, count: 6 << 10))
    await settle(t)
    #expect(t.files[2].detail == "disk full" && t.files[2].canRetry)
    tm.uploads["u2"] = nil
    t.retry(t.files[2].id)
    await settle(t)
    #expect(t.files[2].ref == "ref-b.md" && tm.calls.last?.upload == "u3")
    // Removed and cleared.
    t.remove(t.files[0].id)
    #expect(t.files.map(\.name) == ["shot.png", "b.md"])
    t.clear()
    #expect(t.files.isEmpty && t.refs.isEmpty)
}

@Test @MainActor func trayTakesTenFiles() {
    let t = ComposerTray()
    for i in 0..<ComposerFiles.maxFiles { #expect(t.refuse(name: "\(i).zip", why: "no")) }
    #expect(t.full && !t.add(name: "x.png", data: Data([1])) && t.note != nil)
    t.remove(t.files[0].id)
    #expect(t.note == nil && !t.full)
}

@Test func composerFileChip() {
    var f = ComposerFile(name: "IMG_1.jpg", size: 2000)
    #expect(f.image && f.spoken == "IMG_1.jpg, uploading, 0%")
    f.state = .failed("no")
    #expect(f.failed && f.spoken == "IMG_1.jpg, failed: no")
    #expect(!ComposerFile(name: "notes.md", size: 1).image)
}

/// A w×h image in format type, with properties.
private func image(_ type: UTType, w: Int, h: Int, props: [CFString: Any] = [:]) -> Data? {
    let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    let out = NSMutableData()
    guard let dst = CGImageDestinationCreateWithData(out, type.identifier as CFString, 1, nil) else { return nil }
    CGImageDestinationAddImage(dst, ctx.makeImage()!, props as CFDictionary)
    return CGImageDestinationFinalize(dst) ? out as Data : nil
}

private func pixels(_ d: Data) -> (String, Int, Int) {
    let src = CGImageSourceCreateWithData(d as CFData, nil)!
    let p = CGImageSourceCopyPropertiesAtIndex(src, 0, nil) as! [CFString: Any]
    return (CGImageSourceGetType(src)! as String, p[kCGImagePropertyPixelWidth] as! Int, p[kCGImagePropertyPixelHeight] as! Int)
}

@Test func photosAsTMTakesThem() throws {
    let at = Date(timeIntervalSince1970: 0)
    #expect(ComposerFiles.renamed("IMG_0042.HEIC", ext: "jpg") == "IMG_0042.jpg")
    #expect(ComposerFiles.renamed("dir/shot", ext: "png") == "shot.png")
    #expect(ComposerFiles.renamed("", ext: "jpg", date: at).hasPrefix("Photo ") == true)
    #expect(ComposerFiles.renamed(".hidden", ext: "jpg", date: at).hasPrefix("Photo "))
    #expect(ComposerFiles.photo(Data("text".utf8), name: "a.png") == nil)
    // A small PNG goes as it is, under its own name.
    let png = try #require(image(.png, w: 8, h: 4))
    let p = try #require(ComposerFiles.photo(png, name: "Screenshot.PNG"))
    #expect(p.name == "Screenshot.png" && p.data == png)
    // A large one stays PNG, its longer side maxPixel.
    let big = try #require(image(.png, w: ComposerFiles.maxPixel * 2, h: 100))
    let b = try #require(ComposerFiles.photo(big, name: "wide.png"))
    #expect(b.name == "wide.png")
    #expect(pixels(b.data) == (UTType.png.identifier, ComposerFiles.maxPixel, 50))
    // HEIC (where this Mac writes it) goes as JPEG, turned upright.
    if let heic = image(.heic, w: 40, h: 20, props: [kCGImagePropertyOrientation: 6]) {
        let h = try #require(ComposerFiles.photo(heic, name: "IMG_0042.HEIC"))
        #expect(h.name == "IMG_0042.jpg")
        #expect(pixels(h.data) == (UTType.jpeg.identifier, 20, 40))
    }
    // A JPEG with a place loses it.
    let gps: [CFString: Any] = [kCGImagePropertyGPSDictionary: [kCGImagePropertyGPSLatitude: 52.0, kCGImagePropertyGPSLatitudeRef: "N"]]
    let jpg = try #require(image(.jpeg, w: 10, h: 10, props: gps))
    let j = try #require(ComposerFiles.photo(jpg, name: "a.jpeg"))
    let props = CGImageSourceCopyPropertiesAtIndex(CGImageSourceCreateWithData(j.data as CFData, nil)!, 0, nil) as! [CFString: Any]
    #expect(j.name == "a.jpg" && props[kCGImagePropertyGPSDictionary] == nil)
}
