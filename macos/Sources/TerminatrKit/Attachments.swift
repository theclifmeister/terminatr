import CoreGraphics
import Foundation
import ImageIO

/// A file the conversation puts in front of the user (docs/SPEC.md
/// §3.8): an image pasted into a prompt, or one a call sends (Claude's
/// SendUserFile). An image or a PDF the call renders draws a preview in
/// its card; an attached one, or any other file, is a row with its name
/// and size. The bytes come only through session.file, by the ref the
/// transcript gave.
public struct Attachment: Sendable, Equatable, Identifiable {
    public enum Kind: Sendable, Equatable {
        case image
        case pdf
        case other
    }

    public let ref: String
    public let name: String
    /// A sent file's path (Reveal in Finder); empty for a pasted image.
    public let path: String
    /// The media type session.file sends it as; empty when it sends no
    /// bytes of it (a key, a binary): a row without a preview or Save.
    public let type: String
    public let kind: Kind
    /// Whether the card draws its preview: an image or PDF, not attached.
    public let inline: Bool

    public var id: String { ref }

    /// What tm sends of it: Open, Save As and the preview need its bytes.
    public var sendable: Bool { !type.isEmpty }

    public init(_ f: AppAPI.TranscriptFile, display: String = "") {
        ref = f.ref
        name = f.name
        path = f.path
        type = f.type
        kind = Self.kind(of: f.type)
        inline = kind != .other && display != "attach"
    }

    static func kind(of type: String) -> Kind {
        if type == "application/pdf" { return .pdf }
        if type.hasPrefix("image/") { return .image }
        return .other
    }

    /// A size for people: "2.4 MB".
    public static func size(_ bytes: Int) -> String {
        ByteCountFormatter.string(fromByteCount: Int64(bytes), countStyle: .file)
    }
}

extension Transcript.Entry {
    /// The files this entry puts in front of the user, as cards draw them.
    public var attachments: [Attachment] {
        files.map { Attachment($0, display: kind == .files ? display : "") }
    }
}

/// What reading a file came to.
public enum FileOutcome: Sendable, Equatable {
    /// Its bytes, and when it was last modified (nil for a pasted image).
    case data(Data, Date?)
    /// The file, or the transcript's line naming it, is no longer there:
    /// scratch folders are cleaned.
    case gone
    /// tm doesn't send it (its type, its size) or couldn't: why.
    case unavailable(String)
}

/// The files of the shown conversations, off the main thread: their
/// stats, bytes and thumbnails, kept by the session's ref and the file's
/// modification time, so a file changed since shows again and the
/// conversation redraws without reading anything twice. Bounded: the
/// bytes kept at most maxBytes, the thumbnails at most maxThumbnails.
public actor FileStore {
    /// session.file, as the bridge answers it.
    public typealias Fetch = @Sendable (AppAPI.SessionFileParams) async throws -> AppAPI.SessionFile

    private let fetch: Fetch
    private let maxBytes: Int
    private let maxThumbnails: Int
    private var stats: [String: AppAPI.SessionFile] = [:]
    private var bytes: [String: Data] = [:]
    private var byteOrder: [String] = []
    private var thumbs: [String: CGImage] = [:]
    private var thumbOrder: [String] = []
    private var loading: [String: Task<FileOutcome, Never>] = [:]

    public init(maxBytes: Int = 128 << 20, maxThumbnails: Int = 200, fetch: @escaping Fetch) {
        self.fetch = fetch
        self.maxBytes = maxBytes
        self.maxThumbnails = maxThumbnails
    }

    /// The file's name, type, size and modification time, without its
    /// bytes; asked each time, since the file may have changed or gone.
    public func stat(_ session: String, _ ref: String) async -> Result<AppAPI.SessionFile, FileProblem> {
        do {
            let s = try await fetch(AppAPI.SessionFileParams(session: session, ref: ref, stat: true))
            stats[session + "|" + ref] = s
            return .success(s)
        } catch {
            return .failure(FileProblem(error))
        }
    }

    /// The file's bytes: kept by its ref and modification time, else
    /// read chunk by chunk. A file that changed while it was read is
    /// read again, once.
    public func data(_ session: String, _ ref: String) async -> FileOutcome {
        let st: AppAPI.SessionFile
        switch await stat(session, ref) {
        case .failure(let p): return p.outcome
        case .success(let s): st = s
        }
        guard st.sendable else { return .unavailable("tm doesn't send \(st.name.isEmpty ? "this file" : st.name): its type or size") }
        let key = Self.key(session, ref, st.mtime)
        if let d = bytes[key] { return .data(d, st.mtime) }
        if let t = loading[key] { return await t.value }
        let fetch = self.fetch
        let t = Task { await Self.read(fetch, session, ref, st) }
        loading[key] = t
        let out = await t.value
        loading[key] = nil
        if case .data(let d, let m) = out {
            keep(Self.key(session, ref, m), d)
        }
        return out
    }

    /// A thumbnail at most maxPixel on its longer side, kept aspect: an
    /// image's (ImageIO, downsampled, never decoded at full size) or a
    /// PDF's first page; nil when it has none (an SVG: the app draws it).
    public func thumbnail(_ session: String, _ ref: String, maxPixel: Int) async -> Result<CGImage?, FileProblem> {
        switch await data(session, ref) {
        case .gone: return .failure(.gone)
        case .unavailable(let why): return .failure(.unavailable(why))
        case .data(let d, let m):
            let key = Self.key(session, ref, m) + "|\(maxPixel)"
            if let t = thumbs[key] { return .success(t) }
            let type = stats[session + "|" + ref]?.type ?? ""
            let img = type == "application/pdf" ? Self.pdfThumbnail(d, maxPixel: maxPixel) : Self.imageThumbnail(d, maxPixel: maxPixel)
            if let img {
                thumbs[key] = img
                thumbOrder.append(key)
                while thumbOrder.count > maxThumbnails { thumbs[thumbOrder.removeFirst()] = nil }
            }
            return .success(img)
        }
    }

    private func keep(_ key: String, _ d: Data) {
        bytes[key] = d
        byteOrder.removeAll { $0 == key }
        byteOrder.append(key)
        var total = bytes.values.reduce(0) { $0 + $1.count }
        while total > maxBytes, byteOrder.count > 1 {
            let k = byteOrder.removeFirst()
            total -= bytes[k]?.count ?? 0
            bytes[k] = nil
        }
    }

    static func key(_ session: String, _ ref: String, _ mtime: Date?) -> String {
        "\(session)|\(ref)|\(mtime?.timeIntervalSince1970 ?? 0)"
    }

    /// Reads the file st describes from its first chunk on; again from
    /// the start, once, when its size or time changed between chunks.
    static func read(_ fetch: Fetch, _ session: String, _ ref: String, _ st: AppAPI.SessionFile) async -> FileOutcome {
        var want = st
        for _ in 0..<2 {
            var out = Data()
            out.reserveCapacity(want.size)
            var changed = false
            do {
                while out.count < want.size {
                    let c = try await fetch(AppAPI.SessionFileParams(session: session, ref: ref, offset: out.count))
                    if c.size != want.size || c.mtime != want.mtime {
                        want = c
                        changed = true
                        break
                    }
                    if c.data.isEmpty { break }
                    out.append(c.data)
                }
            } catch {
                return FileProblem(error).outcome
            }
            if !changed {
                return out.count == want.size ? .data(out, want.mtime) : .gone
            }
        }
        return .unavailable("the file kept changing while it was read")
    }

    /// An image's thumbnail through ImageIO: downsampled as it decodes,
    /// turned as its orientation says.
    public static func imageThumbnail(_ d: Data, maxPixel: Int) -> CGImage? {
        guard let src = CGImageSourceCreateWithData(d as CFData, [kCGImageSourceShouldCache: false] as CFDictionary) else { return nil }
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixel,
        ]
        return CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary)
    }

    /// A PDF's first page, drawn white-backed at most maxPixel.
    public static func pdfThumbnail(_ d: Data, maxPixel: Int) -> CGImage? {
        guard let provider = CGDataProvider(data: d as CFData), let doc = CGPDFDocument(provider), let page = doc.page(at: 1) else { return nil }
        let box = page.getBoxRect(.cropBox)
        guard box.width > 0, box.height > 0 else { return nil }
        let scale = CGFloat(maxPixel) / max(box.width, box.height)
        let w = max(1, Int(box.width * scale)), h = max(1, Int(box.height * scale))
        guard let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                                  space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue) else { return nil }
        ctx.setFillColor(CGColor(red: 1, green: 1, blue: 1, alpha: 1))
        ctx.fill(CGRect(x: 0, y: 0, width: w, height: h))
        ctx.scaleBy(x: scale, y: scale)
        ctx.translateBy(x: -box.minX, y: -box.minY)
        ctx.drawPDFPage(page)
        return ctx.makeImage()
    }
}

/// Why a file can't show.
public enum FileProblem: Error, Sendable, Equatable {
    /// No longer there: the card says so, never a spinner.
    case gone
    case unavailable(String)

    init(_ error: any Error) {
        if let e = error as? AppAPI.Error {
            self = e.code == "gone" ? .gone : .unavailable(e.errorDescription ?? e.code)
        } else {
            self = .unavailable(error.localizedDescription)
        }
    }

    var outcome: FileOutcome {
        switch self {
        case .gone: return .gone
        case .unavailable(let why): return .unavailable(why)
        }
    }
}
