import Foundation
import ImageIO
import Observation
import UniformTypeIdentifiers

/// A file the user pastes, drops or picks into the composer (docs/SPEC.md
/// §3.8, Attaching files). Its bytes go to tm through session.upload,
/// which stores them in the session's project's uploads/ and answers a
/// ref; the prompt names the refs (session.prompt's files) and tm puts
/// each file's path after the text. The app never sends a path.
public struct ComposerFile: Identifiable, Sendable, Equatable {
    public enum State: Sendable, Equatable {
        /// Bytes sent so far.
        case uploading(Int)
        /// In uploads/: ref is what the prompt names.
        case ready(ref: String)
        case failed(String)
    }

    public let id: UUID
    public let name: String
    public let size: Int
    public var state: State
    /// A failed upload its bytes are kept for: Retry sends it again. A
    /// file tm won't take has none.
    public var canRetry = false

    public init(name: String, size: Int, state: State = .uploading(0)) {
        id = UUID()
        self.name = name
        self.size = size
        self.state = state
    }

    public var ref: String? {
        if case .ready(let ref) = state { return ref }
        return nil
    }

    public var uploading: Bool {
        if case .uploading = state { return true }
        return false
    }

    public var failed: Bool {
        if case .failed = state { return true }
        return false
    }

    /// Whether it is an image (its chip's icon).
    public var image: Bool {
        ComposerFiles.imageExtensions.contains((name as NSString).pathExtension.lowercased())
    }

    /// What VoiceOver says of its chip: the name, then the detail.
    public var spoken: String {
        switch state {
        case .uploading: return "\(name), uploading, \(detail)"
        case .ready: return "\(name), \(detail), attached"
        case .failed(let why): return "\(name), failed: \(why)"
        }
    }

    /// What the chip says under its name: the size, the progress, or why
    /// it failed.
    public var detail: String {
        switch state {
        case .uploading(let sent):
            return size > 0 ? "\(min(100, sent * 100 / size))%" : "…"
        case .ready:
            return Attachment.size(size)
        case .failed(let why):
            return why
        }
    }
}

public enum ComposerFiles {
    /// session.upload's bounds, as tm's: a file of at most 25 MB, sent
    /// 512 KiB at a time, and at most 10 files to a prompt.
    public static let maxSize = 25 << 20
    public static let chunk = 512 << 10
    public static let maxFiles = 10

    /// The app bridge's longest request line (appapi.MaxLine).
    public static let maxLine = 1 << 20

    /// What a session.upload line carries besides its chunk's base64, at
    /// most: the method, the session, the name and, on a device's link,
    /// the auth with its nonce and signature (SignedWrite.line).
    public static let lineOverhead = 4 << 10

    /// The largest chunk, in whole KiB, whose request line fits a line
    /// of lineLimit bytes (base64 makes 4 bytes of 3), at most tm's.
    public static func chunk(lineLimit: Int) -> Int {
        let raw = (lineLimit - lineOverhead) / 4 * 3
        return min(chunk, max(1, raw >> 10) << 10)
    }

    /// The phone's chunk, half tm's: a link that drops mid-file loses
    /// little and the chip's progress moves on a slow network. Its line
    /// (some 350 KB) fits the bridge's (chunk(lineLimit:)). The relay
    /// carries a line in as many messages as it takes: its 64 KiB is per
    /// message, not per line (relay.Conn and RelayHandler split the TLS
    /// stream), so the line is the bound.
    public static let phoneChunk = 256 << 10

    /// The image extensions tm takes.
    public static let imageExtensions: Set<String> = ["png", "jpg", "jpeg", "gif", "webp"]

    /// The extensions tm takes: images agents read, PDFs and text.
    public static let extensions: Set<String> = [
        "png", "jpg", "jpeg", "gif", "webp", "pdf",
        "txt", "md", "csv", "tsv", "log", "json", "yaml", "yml", "toml", "xml", "html", "diff", "patch",
    ]

    /// Why tm won't take a file named name of size bytes, nil when it
    /// will. tm checks again, and the bytes too.
    public static func refusal(name: String, size: Int) -> String? {
        let ext = (name as NSString).pathExtension.lowercased()
        if name.hasPrefix(".") || !extensions.contains(ext) {
            return "Only images (PNG, JPEG, GIF, WebP), PDFs and text files"
        }
        if size <= 0 { return "Empty file" }
        if size > maxSize { return "Larger than \(Attachment.size(maxSize))" }
        return nil
    }

    /// The name of an image pasted at date: "Pasted image 14.03.27.png".
    public static func pastedName(_ date: Date = Date(), ext: String = "png") -> String {
        "Pasted image \(clock(date)).\(ext)"
    }

    private static func clock(_ date: Date) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "HH.mm.ss"
        return f.string(from: date)
    }

    /// The longer side of a photo the phone sends, in pixels: a larger
    /// one (a 48 MP camera's 8064) is scaled down to it, which keeps a
    /// screenshot's detail (an iPad's 2752) and stays under what models
    /// read (8000).
    public static let maxPixel = 4096

    /// A photo or image the phone attaches (Photos, the camera, a paste
    /// or a drop), as tm takes it: PNG and GIF as they are, WebP and
    /// JPEG as they are when small enough; anything else (HEIC, TIFF) or
    /// larger than maxPixel goes as JPEG (a PNG stays PNG), turned as
    /// its orientation says, without its metadata (a photo's place).
    /// name is what it came as ("IMG_0042.HEIC"), empty for none; nil
    /// when data isn't an image.
    public static func photo(_ data: Data, name: String, date: Date = Date()) -> (name: String, data: Data)? {
        guard let src = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
              let type = CGImageSourceGetType(src).map({ $0 as String }),
              CGImageSourceGetCount(src) > 0 else { return nil }
        let props = CGImageSourceCopyPropertiesAtIndex(src, 0, nil) as? [CFString: Any] ?? [:]
        let w = props[kCGImagePropertyPixelWidth] as? Int ?? 0, h = props[kCGImagePropertyPixelHeight] as? Int ?? 0
        let large = max(w, h) > maxPixel
        let ext: String
        switch type {
        case UTType.gif.identifier: return (renamed(name, ext: "gif", date: date), data)
        case UTType.png.identifier where !large: return (renamed(name, ext: "png", date: date), data)
        case UTType.jpeg.identifier where !large: ext = "jpg"
        case UTType.webP.identifier where !large: return (renamed(name, ext: "webp", date: date), data)
        case UTType.png.identifier: ext = "png"
        default: ext = "jpg"
        }
        // A JPEG small enough goes as it is, less its metadata: drawn
        // again only when it carries any.
        if type == UTType.jpeg.identifier, !large, props[kCGImagePropertyGPSDictionary] == nil,
           (props[kCGImagePropertyOrientation] as? Int ?? 1) == 1 {
            return (renamed(name, ext: ext, date: date), data)
        }
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: min(max(w, h, 1), maxPixel),
        ]
        guard let deep = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary),
              let image = eightBit(deep) else { return nil }
        let out = NSMutableData()
        let utType = ext == "png" ? UTType.png : UTType.jpeg
        guard let dst = CGImageDestinationCreateWithData(out, utType.identifier as CFString, 1, nil) else { return nil }
        CGImageDestinationAddImage(dst, image, ext == "png" ? nil : [kCGImageDestinationLossyCompressionQuality: 0.85] as CFDictionary)
        guard CGImageDestinationFinalize(dst) else { return nil }
        return (renamed(name, ext: ext, date: date), out as Data)
    }

    /// A small picture of an image for its thumbnail in the composer
    /// (T893): a PNG at most maxPixel on its longer side, turned as its
    /// orientation says; nil when data isn't an image.
    public static func thumbnail(_ data: Data, maxPixel: Int = 240) -> Data? {
        guard let src = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
              CGImageSourceGetCount(src) > 0 else { return nil }
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixel,
        ]
        guard let small = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary),
              let image = eightBit(small) else { return nil }
        let out = NSMutableData()
        guard let dst = CGImageDestinationCreateWithData(out, UTType.png.identifier as CFString, 1, nil) else { return nil }
        CGImageDestinationAddImage(dst, image, nil)
        guard CGImageDestinationFinalize(dst) else { return nil }
        return out as Data
    }

    /// image at 8 bits a component, its colour space kept: a phone's
    /// HEIC decodes at 10 (HDR, Display P3), which a JPEG encoder may
    /// refuse (the Simulator's does).
    static func eightBit(_ image: CGImage) -> CGImage? {
        guard image.bitsPerComponent > 8 else { return image }
        let space = image.colorSpace.flatMap { $0.model == .rgb ? $0 : nil } ?? CGColorSpace(name: CGColorSpace.sRGB)!
        let alpha = image.alphaInfo
        let opaque = alpha == .none || alpha == .noneSkipLast || alpha == .noneSkipFirst
        guard let ctx = CGContext(data: nil, width: image.width, height: image.height, bitsPerComponent: 8, bytesPerRow: 0, space: space,
                                  bitmapInfo: (opaque ? CGImageAlphaInfo.noneSkipLast : .premultipliedLast).rawValue) else { return nil }
        ctx.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
        return ctx.makeImage()
    }

    /// name with its extension ext ("IMG_0042.HEIC" as "IMG_0042.jpg"),
    /// a photo of date's when it has none ("Photo 14.03.27.jpg").
    public static func renamed(_ name: String, ext: String, date: Date = Date()) -> String {
        let base = ((name as NSString).lastPathComponent as NSString).deletingPathExtension
            .trimmingCharacters(in: .whitespaces)
        let stem = base.isEmpty || base.hasPrefix(".") ? "Photo \(clock(date))" : base
        return stem + "." + ext
    }

    /// An image's bytes (TIFF, as a pasteboard holds a screenshot) as a
    /// PNG; nil when they aren't an image.
    public static func png(_ data: Data) -> Data? {
        guard let src = CGImageSourceCreateWithData(data as CFData, nil),
              let image = CGImageSourceCreateImageAtIndex(src, 0, nil) else { return nil }
        let out = NSMutableData()
        guard let dst = CGImageDestinationCreateWithData(out, UTType.png.identifier as CFString, 1, nil) else { return nil }
        CGImageDestinationAddImage(dst, image, nil)
        return CGImageDestinationFinalize(dst) ? out as Data : nil
    }

    /// Where an upload got to: tm's id of it and the bytes it took. tm
    /// keeps an upload 10 minutes from its last chunk, so one a dropped
    /// link cut goes on from here.
    public struct Resume: Sendable, Equatable {
        public var upload = ""
        public var offset = 0

        public init(upload: String = "", offset: Int = 0) {
            self.upload = upload
            self.offset = offset
        }
    }

    /// Sends data, the file name, to session chunk by chunk through call
    /// (session.upload), telling progress the bytes sent after each; the
    /// last answer has the ref. It runs on the caller's actor.
    public static func upload(_ data: Data, name: String, session: String,
                              isolation: isolated (any Actor)? = #isolation,
                              call: (AppAPI.UploadParams) async throws -> AppAPI.Upload,
                              progress: (Int) -> Void = { _ in }) async throws -> AppAPI.Upload {
        try await upload(data, name: name, session: session, call: call, sent: { progress($0.offset) })
    }

    /// Sends data as upload above, chunk bytes at a time, from resume
    /// (a new upload when it has no id), telling sent where it got to
    /// after each chunk.
    public static func upload(_ data: Data, name: String, session: String, chunk: Int = chunk, from resume: Resume = Resume(),
                              isolation: isolated (any Actor)? = #isolation,
                              call: (AppAPI.UploadParams) async throws -> AppAPI.Upload,
                              sent: (Resume) -> Void) async throws -> AppAPI.Upload {
        var id = resume.upload
        var offset = id.isEmpty ? 0 : resume.offset
        repeat {
            let end = min(data.count, offset + chunk)
            let p = AppAPI.UploadParams(session: session, upload: id, name: id.isEmpty ? name : "",
                                        size: id.isEmpty ? data.count : 0, offset: offset,
                                        data: data.subdata(in: data.startIndex + offset ..< data.startIndex + end))
            let r = try await call(p)
            if !r.ref.isEmpty { return r }
            guard r.received == end, !r.upload.isEmpty else { throw UploadError.lost }
            id = r.upload
            offset = end
            sent(Resume(upload: id, offset: offset))
            try Task.checkCancellation()
        } while offset < data.count
        throw UploadError.lost
    }

    public enum UploadError: Error, LocalizedError, Equatable {
        case lost

        public var errorDescription: String? { "tm lost the upload: attach the file again" }
    }
}
