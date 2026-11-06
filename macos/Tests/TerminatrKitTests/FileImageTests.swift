import CoreGraphics
import Foundation
import ImageIO
import Testing
@testable import TerminatrKit

/// A picture encoded as type, half clear when alpha says so.
private func encoded(_ type: String, width: Int = 40, height: Int = 20, alpha: Bool = false, frames: Int = 1, props: [CFString: Any] = [:]) -> Data {
    let ctx = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0, space: CGColorSpaceCreateDeviceRGB(),
                        bitmapInfo: (alpha ? CGImageAlphaInfo.premultipliedLast : .noneSkipLast).rawValue)!
    ctx.setFillColor(CGColor(red: 1, green: 0, blue: 0, alpha: 1))
    ctx.fill(CGRect(x: 0, y: 0, width: width / 2, height: height))
    let image = ctx.makeImage()!
    let out = NSMutableData()
    let dest = CGImageDestinationCreateWithData(out, type as CFString, frames, nil)!
    for _ in 0..<frames { CGImageDestinationAddImage(dest, image, props as CFDictionary) }
    #expect(CGImageDestinationFinalize(dest))
    return out as Data
}

private func drawn(_ o: FileImage.Outcome, sourceLocation: SourceLocation = #_sourceLocation) -> FileImage.Drawn? {
    guard case .drawn(let d) = o else {
        Issue.record("not drawn: \(o)", sourceLocation: sourceLocation)
        return nil
    }
    return d
}

@Test(arguments: ["public.png", "public.jpeg", "com.compuserve.gif", "public.tiff", "public.heic"])
func theFormatsThatEncodeHereDraw(type: String) {
    let d = drawn(FileImage.decode(encoded(type, width: 64, height: 32)))
    #expect(d?.width == 64)
    #expect(d?.height == 32)
}

@Test func aWebPDraws() throws {
    // ImageIO reads WebP and writes none: a 1×1 file made elsewhere.
    let webp = try #require(Data(base64Encoded: "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA"))
    #expect(drawn(FileImage.decode(webp))?.width == 1)
}

@Test func aSmallImageStaysItsOwnSize() {
    let d = drawn(FileImage.decode(encoded("public.png", width: 16, height: 16)))
    #expect(d?.image.width == 16)
    #expect(d?.image.height == 16)
}

@Test func aLargeImageComesDownButKnowsItsOwnSize() {
    let d = drawn(FileImage.decode(encoded("public.png", width: 800, height: 200), maxSide: 100))
    #expect(d?.image.width == 100)
    #expect(d?.image.height == 25)
    #expect(d?.width == 800)
    #expect(d?.height == 200)
}

@Test func actualSizeFollowsTheFilesResolution() {
    let plain = drawn(FileImage.decode(encoded("public.png", width: 64, height: 32)))
    #expect(plain?.pointWidth == 64)
    #expect(plain?.pointHeight == 32)
    let retina = drawn(FileImage.decode(encoded("public.png", width: 64, height: 32, props: [kCGImagePropertyDPIWidth: 144, kCGImagePropertyDPIHeight: 144])))
    #expect(retina?.width == 64)
    #expect(retina?.pointWidth == 32)
    #expect(retina?.pointHeight == 16)
    // Odd ones count as none.
    let odd = drawn(FileImage.decode(encoded("public.png", width: 64, height: 32, props: [kCGImagePropertyDPIWidth: 3, kCGImagePropertyDPIHeight: 3])))
    #expect(odd?.pointWidth == 64)
}

@Test func anImageTurnsAsItsFileSays() {
    let d = drawn(FileImage.decode(encoded("public.jpeg", width: 40, height: 20, props: [kCGImagePropertyOrientation: 6])))
    #expect(d?.image.width == 20)
    #expect(d?.image.height == 40)
    #expect(d?.width == 20)
    #expect(d?.height == 40)
}

@Test func clearPartsAreKnownAndFramesCounted() {
    #expect(drawn(FileImage.decode(encoded("public.png", alpha: true)))?.opaque == false)
    #expect(drawn(FileImage.decode(encoded("public.jpeg")))?.opaque == true)
    #expect(drawn(FileImage.decode(encoded("com.compuserve.gif", frames: 3)))?.frames == 3)
    #expect(drawn(FileImage.decode(encoded("public.png")))?.frames == 1)
}

@Test func overTheBoundsNothingDecodes() {
    let png = encoded("public.png", width: 20, height: 20)
    guard case .notDrawn(let bytes) = FileImage.decode(png, maxBytes: png.count - 1) else { Issue.record("past the bytes"); return }
    #expect(bytes.contains("tm reads"))
    guard case .notDrawn(let pixels) = FileImage.decode(png, maxPixels: 399) else { Issue.record("past the pixels"); return }
    #expect(pixels.contains("20 × 20"))
    if case .notDrawn = FileImage.decode(png, maxPixels: 400) { Issue.record("at the pixels") }
}

/// A file claiming more than it could hold: ImageIO itself refuses this one.
@Test func aFileClaimingTooManyPixelsIsntDrawn() {
    var png = encoded("public.png", width: 1, height: 1)
    let ihdr = png.range(of: Data("IHDR".utf8))!.lowerBound
    let big: [UInt8] = [0, 0, 0x4E, 0x20] // 20000
    png.replaceSubrange((ihdr + 4)..<(ihdr + 8), with: big)
    png.replaceSubrange((ihdr + 8)..<(ihdr + 12), with: big)
    png.replaceSubrange((ihdr + 17)..<(ihdr + 21), with: crc(png[ihdr..<(ihdr + 17)]))
    if case .drawn = FileImage.decode(png) { Issue.record("decoded") }
}

private func crc(_ d: Data) -> [UInt8] {
    var c: UInt32 = 0xFFFF_FFFF
    for b in d {
        c ^= UInt32(b)
        for _ in 0..<8 { c = c & 1 == 1 ? (c >> 1) ^ 0xEDB8_8320 : c >> 1 }
    }
    c ^= 0xFFFF_FFFF
    return [UInt8(c >> 24), UInt8(c >> 16 & 255), UInt8(c >> 8 & 255), UInt8(c & 255)]
}

@Test func whatsDamagedOrNoImageSaysSo() {
    let png = encoded("public.png", width: 200, height: 200)
    let jpeg = encoded("public.jpeg", width: 200, height: 200)
    let cases: [Data] = [
        Data(),
        Data(repeating: 0x7F, count: 4096),
        png.prefix(20),
        png.prefix(png.count / 2),
        png.prefix(png.count - 12),
        jpeg.prefix(jpeg.count - 2),
    ]
    for c in cases {
        guard case .notDrawn(let why) = FileImage.decode(c) else { Issue.record("drew \(c.count) bytes"); continue }
        #expect(why.contains("damaged"))
    }
}

@Test func aPDFNamedAsAnImageIsntDrawn() {
    let pdf = NSMutableData()
    let ctx = CGContext(consumer: CGDataConsumer(data: pdf)!, mediaBox: nil, nil)!
    ctx.beginPDFPage(nil)
    ctx.endPDFPage()
    ctx.closePDF()
    if case .drawn = FileImage.decode(pdf as Data) { Issue.record("drew a PDF") }
}

@Test func onlyNamesOfTheFormatsAreRead() {
    for n in ["a.png", "A.PNG", "b.jpg", "b.jpeg", "c.gif", "d.heic", "d.heif", "e.webp", "f.tif", "f.tiff"] {
        #expect(FileImage.named(n), "\(n)")
    }
    for n in ["a.svg", "a.pdf", "a.zip", "png", ".png.txt", "a.bmp"] {
        #expect(!FileImage.named(n), "\(n)")
    }
}

@Test func aFileOverWhatTmReadsIsntAskedFor() {
    #expect(FileImage.tooBig(FileImage.maxBytes) == nil)
    #expect(FileImage.tooBig(FileImage.maxBytes + 1)?.contains("tm reads") == true)
}

@Test func theWordsKeepTheCurrentMessageAndAddTheReason() {
    let plain = "a.pdf · 2 KB has no text to show here. Space opens Quick Look, and Open uses its app."
    #expect(FileImage.message(name: "a.pdf", size: 2000) == plain)
    let why = FileImage.message(name: "a.png", size: 2000, why: "it's damaged")
    #expect(why == "a.png · 2 KB has no text to show here. Its image doesn't show: it's damaged. Space opens Quick Look, and Open uses its app.")
    // The server's own words already end.
    #expect(FileImage.message(name: "a.png", size: 2000, why: "Update it.").contains("doesn't show: Update it. Space"))
}

@Test func aDecodeNobodyWantsIsDropped() async {
    let png = encoded("public.png")
    let decoder = FileImageDecoder()
    #expect(await decoder.decode(png) != nil)
    let t = Task {
        withUnsafeCurrentTask { $0?.cancel() }
        return await decoder.decode(png)
    }
    #expect(await t.value == nil)
}

@Test func aCancelledReadStopsAskingForBytes() async {
    let asked = Asked()
    let t = Task {
        withUnsafeCurrentTask { $0?.cancel() }
        return await WorktreeRead.bytes({ p in
            asked.add()
            return AppAPI.FilesChunk(name: "f", path: p.path, size: 3, offset: 0, data: Data([1, 2, 3]))
        }, session: "s", path: "f", ignored: false)
    }
    if case .data = await t.value { Issue.record("read it") }
    #expect(asked.count == 0)

    let library = Task {
        withUnsafeCurrentTask { $0?.cancel() }
        return await LibraryPage.read({ _ in
            asked.add()
            return AppAPI.LibraryChunk(name: "f", size: 3, offset: 0, data: Data([1, 2, 3]))
        }, project: "p", file: AppAPI.LibraryFile(thread: "t-1", title: "t", state: "done", name: "f", size: 3, time: Date()))
    }
    if case .data = await library.value { Issue.record("read it") }
    #expect(asked.count == 0)
}

private final class Asked: @unchecked Sendable {
    private let lock = NSLock()
    private var n = 0
    var count: Int { lock.withLock { n } }
    func add() { lock.withLock { n += 1 } }
}

@Test func keepingLetsGoOfTheOldestThatDontShow() {
    var keep = FileImageKeep(budget: 100)
    #expect(keep.add("a", pixels: 50, showing: []) == [])
    #expect(keep.add("b", pixels: 50, showing: []) == [])
    #expect(keep.add("c", pixels: 50, showing: ["a"]) == ["b"])
    // Showing beats the budget, the newest stays whatever its size.
    #expect(keep.add("d", pixels: 500, showing: ["a"]) == ["c"])
    #expect(keep.add("a", pixels: 50, showing: ["a", "d"]) == [])
    keep.remove("d")
    #expect(keep.add("e", pixels: 40, showing: []) == [])
}
