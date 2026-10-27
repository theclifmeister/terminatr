import Foundation
import ImageIO
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
    public static func pastedName(_ date: Date = Date()) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "HH.mm.ss"
        return "Pasted image \(f.string(from: date)).png"
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

    /// Sends data, the file name, to session chunk by chunk through call
    /// (session.upload), telling progress the bytes sent after each; the
    /// last answer has the ref. It runs on the caller's actor.
    public static func upload(_ data: Data, name: String, session: String,
                              isolation: isolated (any Actor)? = #isolation,
                              call: (AppAPI.UploadParams) async throws -> AppAPI.Upload,
                              progress: (Int) -> Void = { _ in }) async throws -> AppAPI.Upload {
        var id = ""
        var offset = 0
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
            progress(offset)
            try Task.checkCancellation()
        } while offset < data.count
        throw UploadError.lost
    }

    public enum UploadError: Error, LocalizedError, Equatable {
        case lost

        public var errorDescription: String? { "tm lost the upload: attach the file again" }
    }
}
