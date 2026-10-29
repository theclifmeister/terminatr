import Photos
import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// The files a conversation puts in front of the user (T860), as the
/// Mac's Attachments shows them: an image or a PDF's first page as a
/// preview, any other file as a row with its name and size, a file no
/// longer there saying so. The bytes come through session.file
/// (PhoneModel.files), by the ref the transcript gave. Held down, each
/// offers Copy (to the pasteboard), Share… (the share sheet) and, for an
/// image, Save to Photos; VoiceOver has them as actions.
struct PhoneAttachments: View {
    let files: [Attachment]
    let session: String?
    let alignment: HorizontalAlignment

    var body: some View {
        VStack(alignment: alignment, spacing: Spacing.item) {
            ForEach(files) { PhoneAttachment(file: $0, session: session) }
        }
    }
}

/// A call that sends the user files (Claude's SendUserFile), as a card:
/// who and when, the caption, then its files.
struct PhoneAttachmentCard: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    let session: String?
    let turn: String

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.item) {
            Text(turn)
                .font(theme.mono(.caption, .medium))
                .foregroundStyle(theme.muted)
            if !entry.text.isEmpty {
                Text(entry.text)
                    .foregroundStyle(theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            PhoneAttachments(files: entry.attachments, session: session, alignment: .leading)
            if let r = entry.result, r.error {
                Text(r.text)
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.attention)
                    .lineLimit(2)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .frosted(RoundedRectangle(cornerRadius: 14))
        .background(theme.agentCard, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
    }
}

struct PhoneAttachment: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.displayScale) private var scale
    let file: Attachment
    let session: String?
    @State private var state = Load.loading
    @State private var sharing: SharedFile?
    @State private var said: String?

    static let maxHeight: CGFloat = 240
    static let maxWidth: CGFloat = 320

    enum Load: Equatable {
        case loading
        case preview(CGImage)
        case row(size: Int?)
        case gone
        case unavailable(String)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.hair) {
            content
            if let said {
                Text(said)
                    .font(theme.body(.caption))
                    .foregroundStyle(theme.muted)
                    .transition(.opacity)
            }
        }
        .task(id: "\(session ?? "")|\(file.ref)|\(model.files != nil)") { await load() }
        .sheet(item: $sharing) { ShareSheet(url: $0.url).presentationDetents([.medium, .large]) }
    }

    @ViewBuilder private var content: some View {
        switch state {
        case .loading:
            if file.inline {
                RoundedRectangle(cornerRadius: 10)
                    .fill(theme.command)
                    .frame(width: 200, height: 140)
                    .overlay { ProgressView() }
                    .accessibilityLabel("Loading \(file.name)")
            } else {
                row(size: nil)
            }
        case .preview(let img):
            Image(decorative: img, scale: scale)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(maxWidth: Self.maxWidth, maxHeight: Self.maxHeight)
                .clipShape(RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.edge, lineWidth: 1))
                .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: 10))
                .acts(file.sendable, image: file.kind == .image) { act($0) }
                .accessibilityElement()
                .accessibilityLabel(file.name)
                .accessibilityAddTraits(.isImage)
        case .row(let size):
            row(size: size)
        case .gone:
            placeholder("\(file.name): file no longer available", "doc.badge.ellipsis")
        case .unavailable(let why):
            placeholder("\(file.name): \(why)", "eye.slash")
        }
    }

    private func row(size: Int?) -> some View {
        HStack(spacing: Spacing.row) {
            Image(systemName: file.kind == .pdf ? "doc.richtext" : file.kind == .image ? "photo" : "doc.text")
                .font(theme.body(.title3))
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(file.name)
                    .font(theme.body(.subheadline, .medium))
                    .foregroundStyle(theme.ink)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let size {
                    Text(Attachment.size(size))
                        .font(theme.body(.caption))
                        .foregroundStyle(theme.muted)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Spacing.row)
        .frame(maxWidth: Self.maxWidth, minHeight: tapTarget, alignment: .leading)
        .background(theme.command, in: RoundedRectangle(cornerRadius: 9))
        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: 9))
        .acts(file.sendable, image: file.kind == .image) { act($0) }
        .accessibilityElement(children: .combine)
    }

    private func placeholder(_ text: String, _ symbol: String) -> some View {
        Label(text, systemImage: symbol)
            .font(theme.body(.footnote))
            .foregroundStyle(theme.muted)
            .lineLimit(2)
            .padding(.horizontal, Spacing.row)
            .padding(.vertical, 7)
            .frame(maxWidth: Self.maxWidth, alignment: .leading)
            .background(theme.command.opacity(0.6), in: RoundedRectangle(cornerRadius: 9))
    }

    private func load() async {
        guard let store = model.files, let session else {
            state = .unavailable("this tm doesn't send files to the app")
            return
        }
        guard file.inline else {
            switch await store.stat(session, file.ref) {
            case .success(let s): state = .row(size: s.size)
            case .failure(.gone): state = .gone
            case .failure(.unavailable): state = .row(size: nil)
            }
            return
        }
        let px = Int(max(Self.maxWidth, Self.maxHeight) * scale)
        switch await store.thumbnail(session, file.ref, maxPixel: px) {
        case .failure(.gone): state = .gone
        case .failure(.unavailable(let why)): state = .unavailable(why)
        case .success(let img?): state = .preview(img)
        case .success(nil): state = .row(size: nil)
        }
    }

    private func act(_ a: FileAct) {
        Task {
            guard let store = model.files, let session else { return }
            let d: Data
            switch await store.data(session, file.ref) {
            case .data(let data, _): d = data
            case .gone: return show("\(file.name) is no longer there")
            case .unavailable(let why): return show(why)
            }
            switch a {
            case .copy:
                FileCopy.toPasteboard(d, name: file.name, type: file.type)
                show("Copied")
            case .share:
                do {
                    sharing = SharedFile(url: try FileCopy.temporary(d, name: file.name))
                } catch {
                    show(error.localizedDescription)
                }
            case .photos:
                do {
                    try await FileCopy.toPhotos(d)
                    show("Saved to Photos")
                } catch {
                    show(error.localizedDescription)
                }
            }
        }
    }

    private func show(_ s: String) {
        withAnimation { said = s }
        Task {
            try? await Task.sleep(for: .seconds(2))
            if said == s { withAnimation { said = nil } }
        }
    }
}

enum FileAct { case copy, share, photos }

extension View {
    /// A file's acts, held down and for VoiceOver: Copy, Share… and, for
    /// an image, Save to Photos; none for a file tm doesn't send.
    fileprivate func acts(_ sendable: Bool, image: Bool, _ act: @escaping (FileAct) -> Void) -> some View {
        contextMenu {
            if sendable {
                Button("Copy", systemImage: "doc.on.doc") { act(.copy) }
                Button("Share…", systemImage: "square.and.arrow.up") { act(.share) }
                if image { Button("Save to Photos", systemImage: "square.and.arrow.down") { act(.photos) } }
            }
        }
        .accessibilityActions {
            if sendable {
                Button("Copy") { act(.copy) }
                Button("Share") { act(.share) }
                if image { Button("Save to Photos") { act(.photos) } }
            }
        }
    }
}

/// A conversation's file, out of the app.
enum FileCopy {
    /// On the pasteboard: an image as an image, text as text too, any
    /// file by its type with its name.
    static func toPasteboard(_ d: Data, name: String, type: String) {
        let ut = UTType(mimeType: type) ?? UTType(filenameExtension: (name as NSString).pathExtension) ?? .data
        var item: [String: Any] = [ut.identifier: d]
        if ut.conforms(to: .text), let s = String(data: d, encoding: .utf8) { item[UTType.utf8PlainText.identifier] = s }
        UIPasteboard.general.setItems([item])
    }

    /// A copy in a temporary folder of the app's own, for the share
    /// sheet, under the file's name.
    static func temporary(_ d: Data, name: String) throws -> URL {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("terminatr-files/\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent((name as NSString).lastPathComponent)
        try d.write(to: url, options: .atomic)
        return url
    }

    /// Into the photo library, asking once to add to it (add only).
    static func toPhotos(_ d: Data) async throws {
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else { throw Refused() }
        try await PHPhotoLibrary.shared().performChanges {
            PHAssetCreationRequest.forAsset().addResource(with: .photo, data: d, options: nil)
        }
    }

    struct Refused: LocalizedError {
        var errorDescription: String? { "Photos access is off for terminatr: Settings › terminatr › Photos" }
    }
}
