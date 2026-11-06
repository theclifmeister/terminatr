import CoreGraphics
import Foundation
import ImageIO

/// An image file as the Files view draws it: named as one of the formats
/// below, read through the same reads as the rest of the file (so never from
/// the agent's path), and decoded from those bytes within bounds. What can't
/// show that way keeps the words a file with no text has, with the reason.
public enum FileImage {
    /// The most bytes it decodes: what tm reads of any file (MaxFilesRead,
    /// MaxLibraryRead), so what's over it isn't asked for at all.
    public static let maxBytes = 25 << 20
    /// The most pixels the file's own header may claim: decoding reads them
    /// all, whatever it comes down to.
    public static let maxPixels = 100_000_000
    /// The longer side kept: as wide as a pane gets, and 64 MB at the most.
    public static let maxSide = 4096

    /// What's asked for: the panel lists files of any size, and reading a
    /// large one just to find out it's no image isn't worth it.
    private static let extensions: Set<String> = ["png", "jpg", "jpeg", "gif", "heic", "heif", "webp", "tif", "tiff"]
    /// What's drawn, by the bytes: a .png that is really a PDF isn't.
    private static let types: Set<String> = ["public.png", "public.jpeg", "com.compuserve.gif", "public.heic", "public.heif", "org.webmproject.webp", "public.tiff"]

    public struct Drawn: Equatable, Sendable {
        /// At most maxSide on its longer side, turned as the file says.
        public let image: CGImage
        /// The file's own size, turned the same way.
        public let width: Int
        public let height: Int
        /// More than one for an animation or a multi-page TIFF: one shows.
        public let frames: Int
        /// Points to the file's size at its own resolution, as macOS means
        /// actual size: 72 dpi when it names none, a Retina screenshot's 144
        /// one screen pixel to each of its own.
        public let pointWidth: Double
        public let pointHeight: Double

        public var opaque: Bool {
            switch image.alphaInfo {
            case .none, .noneSkipFirst, .noneSkipLast: true
            default: false
            }
        }
    }

    public enum Outcome: Equatable, Sendable {
        case drawn(Drawn)
        /// Why it doesn't show, as words that follow a colon.
        case notDrawn(String)
    }

    /// Whether name is worth reading as an image.
    public static func named(_ name: String) -> Bool {
        extensions.contains((name.lowercased() as NSString).pathExtension)
    }

    /// Why a file of size isn't read, nil when it may be.
    public static func tooBig(_ size: Int, maxBytes: Int = maxBytes) -> String? {
        size > maxBytes ? "it's over the \(Attachment.size(maxBytes)) tm reads" : nil
    }

    /// The size of a file over what tm reads against that bound, for a line
    /// beside its acts; nil when it may be read.
    public static func overLimit(_ size: Int, maxBytes: Int = maxBytes) -> String? {
        size > maxBytes ? "\(Attachment.size(size)), over the \(Attachment.size(maxBytes)) tm reads" : nil
    }

    /// Why Quick Look, Open and Save As can't have a file of size, which all
    /// read its bytes through tm; nil when they can. A worktree file's path
    /// (hasPath) still finds it, as an attached file's stays with the server.
    public static func unreadable(name: String, size: Int, hasPath: Bool = false, maxBytes: Int = maxBytes) -> String? {
        guard let over = overLimit(size, maxBytes: maxBytes) else { return nil }
        let then = hasPath ? "Copy Path and Reveal in Finder still find it." : "It can't open or save from here."
        return "\((name as NSString).lastPathComponent) is \(over). \(then)"
    }

    /// What shows for a file with no text and no image, and why not an image
    /// when it was meant to be one. Quick Look and Open read through the same
    /// cap, so a file over it is told only what still works: a worktree
    /// file's path (hasPath), as an attached file's stays with the server.
    public static func message(name: String, size: Int, why: String? = nil, hasPath: Bool = false, maxBytes: Int = maxBytes) -> String {
        let lead = "\(name) · \(Attachment.size(size)) has no text to show here."
        let reason = why.map { " Its image doesn't show: \($0.hasSuffix(".") ? String($0.dropLast()) : $0)." } ?? ""
        guard size > maxBytes else { return lead + reason + " Space opens Quick Look, and Open uses its app." }
        let then = hasPath ? "Copy Path and Reveal in Finder, in its menu, find the file." : "It can't open from here."
        let limit = why == nil ? "the \(Attachment.size(maxBytes)) tm reads" : "the same limit"
        return lead + reason + " Quick Look and Open stop at \(limit). " + then
    }

    /// The image in d, from bytes that are data: a file's header is believed
    /// only as far as the bounds. Blocks while it decodes: FileImageDecoder
    /// runs it off the main thread.
    public static func decode(_ d: Data, maxBytes: Int = maxBytes, maxPixels: Int = maxPixels, maxSide: Int = maxSide) -> Outcome {
        if let why = tooBig(d.count, maxBytes: maxBytes) { return .notDrawn(why) }
        let damaged = Outcome.notDrawn("it's damaged, or isn't the image its name says")
        guard let src = CGImageSourceCreateWithData(d as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
              let type = CGImageSourceGetType(src).map({ $0 as String }), types.contains(type),
              CGImageSourceGetCount(src) > 0 else { return damaged }
        let at = CGImageSourceGetPrimaryImageIndex(src)
        let props = CGImageSourceCopyPropertiesAtIndex(src, at, nil) as? [CFString: Any] ?? [:]
        guard let w = props[kCGImagePropertyPixelWidth] as? Int, let h = props[kCGImagePropertyPixelHeight] as? Int,
              w > 0, h > 0, w <= Int(UInt32.max), h <= Int(UInt32.max) else { return damaged }
        guard w * h <= maxPixels else {
            return .notDrawn("it's \(w) × \(h), over the \(maxPixels / 1_000_000) million pixels this draws")
        }
        guard CGImageSourceGetStatusAtIndex(src, at) == .statusComplete, whole(d, type) else { return damaged }
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: min(max(w, h), maxSide),
        ]
        guard let image = CGImageSourceCreateThumbnailAtIndex(src, at, opts as CFDictionary) else { return damaged }
        let turned = (props[kCGImagePropertyOrientation] as? Int ?? 1) >= 5
        let width = turned ? h : w, height = turned ? w : h
        // A resolution the file claims only sets how far it may grow: odd
        // ones count as none.
        let dpi = (props[kCGImagePropertyDPIWidth] as? Double).flatMap { (72.0...1200.0).contains($0) ? $0 : nil } ?? 72
        return .drawn(Drawn(image: image, width: width, height: height, frames: CGImageSourceGetCount(src),
                            pointWidth: Double(width) * 72 / dpi, pointHeight: Double(height) * 72 / dpi))
    }

    /// Whether a PNG or JPEG reaches its end: ImageIO draws one cut short,
    /// calling it complete, with the rest blank. (Other formats it refuses,
    /// or their ends say nothing; damage inside the pixels isn't looked for.)
    private static func whole(_ d: Data, _ type: String) -> Bool {
        switch type {
        case "public.png":
            let bytes = [UInt8](d)
            var at = 8
            while at + 12 <= bytes.count {
                let length = bytes[at..<(at + 4)].reduce(0) { $0 << 8 | Int($1) }
                if bytes[(at + 4)..<(at + 8)].elementsEqual("IEND".utf8) { return true }
                at += length + 12
            }
            return false
        case "public.jpeg":
            // Near the end, as cameras leave things after it; the marker
            // can't occur inside the coded pixels.
            return d.suffix(4096).range(of: Data([0xFF, 0xD9])) != nil
        default:
            return true
        }
    }
}

/// Decodes one image at a time, off the main thread: moving through a folder
/// of large ones costs the one under way, as the rest are dropped unstarted
/// when the row has moved on.
public actor FileImageDecoder {
    public static let shared = FileImageDecoder()

    public init() {}

    /// Nil when whoever asked no longer wants it.
    public func decode(_ d: Data) -> FileImage.Outcome? {
        Task.isCancelled ? nil : FileImage.decode(d)
    }
}

/// Which decoded images a Files model holds on to: all that show, and of the
/// rest the latest while they fit. A preview's arrows would otherwise keep
/// every image they passed.
public struct FileImageKeep: Sendable {
    /// Three of the largest: moving back to one just left doesn't read it.
    public static let budget = 3 * FileImage.maxSide * FileImage.maxSide

    private var kept: [(id: String, pixels: Int)] = []
    private let budget: Int

    public init(budget: Int = budget) {
        self.budget = budget
    }

    /// Notes id's image; answers the ids to let go, none of them in showing.
    public mutating func add(_ id: String, pixels: Int, showing: Set<String>) -> [String] {
        kept.removeAll { $0.id == id }
        kept.append((id, pixels))
        var total = kept.reduce(0) { $0 + $1.pixels }
        var out: [String] = []
        for k in kept where total > budget && k.id != id && !showing.contains(k.id) {
            total -= k.pixels
            out.append(k.id)
        }
        kept.removeAll { out.contains($0.id) }
        return out
    }

    /// Id's image went.
    public mutating func remove(_ id: String) {
        kept.removeAll { $0.id == id }
    }
}
