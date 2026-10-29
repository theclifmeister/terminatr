import PhotosUI
import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// The composer's files on the phone (T860), as the Mac's composer
/// takes them (docs/SPEC.md §3.8): picked (the paperclip: Photos, Files,
/// the camera), pasted (the system's paste: ⌘V on a hardware keyboard
/// reaches pasteDestination, and the Paste button over the dock reads
/// the pasteboard without iOS asking each time) or dropped on the dock
/// (an iPad's, a Duo's, from Photos, Files or another app). Each goes in
/// the session's ComposerTray, which PhoneModel keeps across screens and
/// the link's drops. A photo goes as ComposerFiles.photo makes it: HEIC
/// as JPEG, at most ComposerFiles.maxPixel on its longer side, without
/// its place.
enum Incoming {
    /// The types the composer takes: tm's extensions' (images, PDFs,
    /// text), and any image, which goes as JPEG or PNG.
    static let types: [UTType] = {
        var out: [UTType] = [.image]
        for e in ComposerFiles.extensions.sorted() {
            if let t = UTType(filenameExtension: e), !out.contains(t) { out.append(t) }
        }
        return out
    }()

    /// A file's name and bytes as tm takes them: an image through
    /// ComposerFiles.photo, anything else as it is (tm says when it
    /// won't take it).
    static func file(name: String, data: Data, type: UTType?) -> (name: String, data: Data) {
        let image = type?.conforms(to: .image) ?? ComposerFiles.imageExtensions.contains((name as NSString).pathExtension.lowercased())
        if image, let p = ComposerFiles.photo(data, name: name) { return p }
        return (name, data)
    }

    /// The file a provider carries (a drop, a paste), read off the main
    /// thread: its first type the composer takes, by name and bytes.
    static func load(_ p: NSItemProvider) async -> (name: String, data: Data)? {
        guard let id = p.registeredTypeIdentifiers.first(where: { id in
            guard let t = UTType(id) else { return false }
            return types.contains { t.conforms(to: $0) }
        }), let type = UTType(id) else { return nil }
        let data: Data? = await withCheckedContinuation { k in
            _ = p.loadDataRepresentation(forTypeIdentifier: id) { d, _ in k.resume(returning: d) }
        }
        guard let data else { return nil }
        var name = p.suggestedName ?? ""
        if let ext = type.preferredFilenameExtension, (name as NSString).pathExtension.isEmpty {
            name = name.isEmpty ? (type.conforms(to: .image) ? "" : "File.\(ext)") : name + "." + ext
        }
        if name.isEmpty, type.conforms(to: .image) {
            name = ComposerFiles.pastedName(ext: type.preferredFilenameExtension ?? "png")
        }
        return file(name: name, data: data, type: type)
    }

    /// A file the Files picker gave: read while its security scope is
    /// open.
    static func read(_ url: URL) -> Result<(name: String, data: Data), any Error> {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        return Result {
            let size = (try? url.resourceValues(forKeys: [.fileSizeKey]))?.fileSize ?? 0
            // Past tm's bound it isn't read: the chip says so.
            if size > ComposerFiles.maxSize, !ComposerFiles.imageExtensions.contains(url.pathExtension.lowercased()) {
                throw TooLarge()
            }
            let data = try Data(contentsOf: url)
            return file(name: url.lastPathComponent, data: data, type: UTType(filenameExtension: url.pathExtension))
        }
    }

    struct TooLarge: LocalizedError {
        var errorDescription: String? { "Larger than \(Attachment.size(ComposerFiles.maxSize))" }
    }
}

/// A file pasted into the composer (⌘V, the Paste button): an image,
/// a PDF or a text file of a type tm takes, by its name when the
/// pasteboard has one. Plain text isn't one: it pastes in the field.
struct PastedFile: Transferable {
    let name: String
    let data: Data

    init(name: String, data: Data, type: UTType) {
        (self.name, self.data) = Incoming.file(name: name, data: data, type: type)
    }

    init(_ r: ReceivedTransferredFile, type: UTType) throws {
        let name = r.file.lastPathComponent
        self.init(name: name, data: try Data(contentsOf: r.file), type: UTType(filenameExtension: r.file.pathExtension) ?? type)
    }

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(importedContentType: .pdf) { try PastedFile($0, type: .pdf) }
        FileRepresentation(importedContentType: .json) { try PastedFile($0, type: .json) }
        FileRepresentation(importedContentType: .commaSeparatedText) { try PastedFile($0, type: .commaSeparatedText) }
        FileRepresentation(importedContentType: .xml) { try PastedFile($0, type: .xml) }
        DataRepresentation(importedContentType: .png) { PastedFile(name: ComposerFiles.pastedName(), data: $0, type: .png) }
        DataRepresentation(importedContentType: .image) {
            PastedFile(name: ComposerFiles.pastedName(ext: "jpg"), data: $0, type: .image)
        }
    }

    /// The types a paste offers the composer.
    static let types: [UTType] = [.image, .pdf, .json, .commaSeparatedText, .xml]
}

/// The paperclip circle in the dock: a menu of Photos, Files and, on a
/// phone with one, the camera.
struct AttachMenu: View {
    let tray: ComposerTray
    @State private var photos = false
    @State private var picked: [PhotosPickerItem] = []
    @State private var importing = false
    @State private var camera = false

    var body: some View {
        Menu {
            Button("Photo Library", systemImage: "photo.on.rectangle") { photos = true }
            if UIImagePickerController.isSourceTypeAvailable(.camera) {
                Button("Take Photo", systemImage: "camera") { camera = true }
            }
            Button("Choose File", systemImage: "folder") { importing = true }
        } label: {
            Label("Attach", systemImage: "paperclip")
        }
        .menuStyle(.button)
        .buttonStyle(.circle)
        .disabled(tray.full)
        .accessibilityLabel("Attach")
        .accessibilityHint(tray.full ? "A prompt takes at most \(ComposerFiles.maxFiles) files" : "Adds a photo or a file to the prompt: images, PDFs or text")
        .photosPicker(isPresented: $photos, selection: $picked, maxSelectionCount: ComposerFiles.maxFiles - tray.files.count,
                      matching: .images, preferredItemEncoding: .current)
        .onChange(of: picked) { _, items in
            guard !items.isEmpty else { return }
            picked = []
            Task { await add(items) }
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: Incoming.types, allowsMultipleSelection: true) { r in
            switch r {
            case .success(let urls):
                for url in urls {
                    switch Incoming.read(url) {
                    case .success(let f): tray.add(name: f.name, data: f.data)
                    case .failure(let e): tray.refuse(name: url.lastPathComponent, why: e.localizedDescription)
                    }
                }
            case .failure(let e):
                tray.refuse(name: "File", why: e.localizedDescription)
            }
        }
        .fullScreenCover(isPresented: $camera) {
            CameraPicker { data in
                guard let data, let p = ComposerFiles.photo(data, name: "") else { return }
                tray.add(name: p.name, data: p.data)
            }
            .ignoresSafeArea()
        }
    }

    /// The picked photos, each as ComposerFiles.photo makes it.
    private func add(_ items: [PhotosPickerItem]) async {
        for item in items {
            // Its bytes as Photos keeps them (HEIC, JPEG, PNG…), whatever
            // type it lists first: always an image here.
            guard let data = try? await item.loadTransferable(type: Data.self) else {
                tray.refuse(name: "Photo", why: "Photos didn't give its bytes")
                continue
            }
            guard let f = await Task.detached(operation: { ComposerFiles.photo(data, name: "") }).value else {
                tray.refuse(name: "Photo", size: data.count, why: "Not an image terminatr can read")
                continue
            }
            tray.add(name: f.name, data: f.data)
        }
    }
}

/// The camera, for a photo to attach: its JPEG, nil when cancelled.
struct CameraPicker: UIViewControllerRepresentable {
    let done: (Data?) -> Void

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let vc = UIImagePickerController()
        vc.sourceType = .camera
        vc.delegate = context.coordinator
        return vc
    }

    func updateUIViewController(_ vc: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(done: done) }

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let done: (Data?) -> Void

        init(done: @escaping (Data?) -> Void) { self.done = done }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            let image = info[.originalImage] as? UIImage
            picker.dismiss(animated: true)
            done(image?.jpegData(compressionQuality: 0.85))
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            picker.dismiss(animated: true)
            done(nil)
        }
    }
}

/// The composer's files over the dock, a row that scrolls sideways:
/// each a glass chip with its kind, name, and progress, size or why it
/// failed; Retry on one that can go again, and Remove. VoiceOver reads
/// a chip as one line, with Retry and Remove as its actions.
struct ComposerChips: View {
    @Environment(\.theme) private var theme
    let tray: ComposerTray

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Spacing.item) {
                ForEach(tray.files) { chip($0) }
            }
            .padding(.horizontal, 1)
        }
        .scrollClipDisabled()
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Attached files")
    }

    private func chip(_ f: ComposerFile) -> some View {
        let shape = Capsule()
        return HStack(spacing: Spacing.tight) {
            Group {
                if f.uploading, case .uploading(let sent) = f.state, f.size > 0 {
                    ProgressView(value: Double(sent), total: Double(f.size))
                        .progressViewStyle(.circular)
                        .controlSize(.small)
                        .tint(theme.action)
                } else {
                    Image(systemName: f.failed ? "exclamationmark.triangle.fill" : f.image ? "photo" : "doc.text")
                        .foregroundStyle(f.failed ? theme.attention : theme.muted)
                }
            }
            .font(theme.body(.subheadline))
            .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text(f.name)
                    .font(theme.body(.footnote, .medium))
                    .foregroundStyle(theme.ink)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(f.detail)
                    .font(theme.body(.caption2))
                    .foregroundStyle(f.failed ? theme.ink : theme.muted)
                    .lineLimit(1)
            }
            .frame(maxWidth: 170, alignment: .leading)
            if f.canRetry {
                Button { tray.retry(f.id) } label: {
                    Label("Retry", systemImage: "arrow.clockwise")
                        .labelStyle(.iconOnly)
                        .frame(minWidth: tapTarget, minHeight: tapTarget)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .foregroundStyle(theme.action)
            }
            Button { tray.remove(f.id) } label: {
                Label("Remove", systemImage: "xmark.circle.fill")
                    .labelStyle(.iconOnly)
                    .frame(minWidth: tapTarget, minHeight: tapTarget)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .foregroundStyle(theme.muted)
        }
        .padding(.leading, Spacing.row)
        .frame(minHeight: tapTarget)
        .surface(.glass, in: shape)
        .overlay { if f.failed { shape.strokeBorder(theme.attention.opacity(0.6), lineWidth: 1) } }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(f.spoken)
        .accessibilityActions {
            if f.canRetry { Button("Retry") { tray.retry(f.id) } }
            Button("Remove") { tray.remove(f.id) }
        }
    }
}

/// The system's Paste button over the dock, while the field has focus
/// and the pasteboard has an image or a file: its tap reads the
/// pasteboard as the user's own act, so iOS doesn't ask (a plain paste
/// in the field still pastes text). The pasteboard is asked only
/// whether it has one (UIPasteboard's has- and contains, which don't
/// ask either).
struct PasteFiles: View {
    let tray: ComposerTray
    @State private var has = PasteFiles.check()

    var body: some View {
        Group {
            if has, !tray.full {
                PasteButton(payloadType: PastedFile.self) { files in
                    Task { @MainActor in
                        for f in files { tray.add(name: f.name, data: f.data) }
                    }
                }
                .labelStyle(.titleAndIcon)
                .buttonBorderShape(.capsule)
                .accessibilityHint("Attaches the image or file you copied")
                .transition(.opacity)
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIPasteboard.changedNotification)) { _ in has = Self.check() }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in has = Self.check() }
    }

    static func check() -> Bool {
        let pb = UIPasteboard.general
        return pb.hasImages || pb.contains(pasteboardTypes: PastedFile.types.filter { $0 != .image }.map(\.identifier))
    }
}

/// ⌘V in the field (a hardware keyboard's) with an image or a file on
/// the pasteboard attaches it; text pastes in the field as ever. SwiftUI
/// pastes there from iOS 27; before it, the Paste button does.
struct PastesFiles: ViewModifier {
    let tray: ComposerTray?

    func body(content: Content) -> some View {
        if #available(iOS 27, *), let tray {
            content.pasteDestination(for: PastedFile.self) { files in
                for f in files { tray.add(name: f.name, data: f.data) }
            } validator: { files in
                tray.full ? [] : files
            }
        } else {
            content
        }
    }
}

/// Files dropped onto the dock, into its tray: those the composer
/// takes. Text dropped on the field is the field's.
enum ComposerDrop {
    /// Read off the main actor: an image is made JPEG there.
    static func take(_ providers: [NSItemProvider], into tray: ComposerTray) async {
        for p in providers {
            let name = p.suggestedName
            if let f = await Incoming.load(p) {
                await tray.add(name: f.name, data: f.data)
            } else if let name {
                await tray.refuse(name: name, why: "Only images (PNG, JPEG, GIF, WebP, HEIC), PDFs and text files")
            }
        }
    }
}
