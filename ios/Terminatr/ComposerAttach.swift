import PhotosUI
import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// The composer's files on the phone (T860), as the Mac's composer
/// takes them (docs/SPEC.md §3.8): picked (the +: Photos, Files, the
/// camera), pasted (ComposerField takes the keyboard's suggestion, the
/// edit menu's Paste and ⌘V; the Paste button in the card reads the
/// pasteboard without iOS asking each time, T893) or dropped on the
/// composer (an iPad's, a Duo's, from Photos, Files or another app).
/// Each shows as a thumbnail in the card (ComposerThumbs). Each goes in
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

/// What the composer's + picks from (T893).
enum AttachPick: Hashable {
    case photos, camera, files
}

/// The composer's + (T893): a menu of Photos, Files and, on a phone
/// with one, the camera. Its pickers hang on the composer's root
/// (AttachPickers), which stays while the + moves between the pill and
/// the card.
struct AttachMenu: View {
    let tray: ComposerTray
    @Binding var pick: AttachPick?

    var body: some View {
        Menu {
            Button("Photo Library", systemImage: "photo.on.rectangle") { pick = .photos }
            if UIImagePickerController.isSourceTypeAvailable(.camera) {
                Button("Take Photo", systemImage: "camera") { pick = .camera }
            }
            Button("Choose File", systemImage: "folder") { pick = .files }
        } label: {
            Label("Attach", systemImage: "plus")
        }
        .menuStyle(.button)
        .buttonStyle(ComposerCircle())
        .disabled(tray.full)
        .accessibilityLabel("Attach")
        .accessibilityHint(tray.full ? "A prompt takes at most \(ComposerFiles.maxFiles) files" : "Adds a photo or a file to the prompt: images, PDFs or text")
    }
}

/// The pickers the + opens: Photos, Files and the camera, each into the
/// tray. A photo goes as ComposerFiles.photo makes it.
struct AttachPickers: ViewModifier {
    let tray: ComposerTray?
    @Binding var pick: AttachPick?
    @State private var picked: [PhotosPickerItem] = []

    func body(content: Content) -> some View {
        content
            .photosPicker(isPresented: shown(.photos), selection: $picked,
                          maxSelectionCount: max(1, ComposerFiles.maxFiles - (tray?.files.count ?? 0)),
                          matching: .images, preferredItemEncoding: .current)
            .onChange(of: picked) { _, items in
                guard !items.isEmpty, let tray else { return }
                picked = []
                Task { await add(items, to: tray) }
            }
            .fileImporter(isPresented: shown(.files), allowedContentTypes: Incoming.types, allowsMultipleSelection: true) { r in
                guard let tray else { return }
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
            .fullScreenCover(isPresented: shown(.camera)) {
                CameraPicker { data in
                    guard let data, let p = ComposerFiles.photo(data, name: "") else { return }
                    tray?.add(name: p.name, data: p.data)
                }
                .ignoresSafeArea()
            }
    }

    private func shown(_ p: AttachPick) -> Binding<Bool> {
        Binding { pick == p } set: { if !$0, pick == p { pick = nil } }
    }

    /// The picked photos, each as ComposerFiles.photo makes it.
    private func add(_ items: [PhotosPickerItem], to tray: ComposerTray) async {
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

/// The composer's files at the top of its card (T893), a row that
/// scrolls sideways: each a thumbnail, an image's picture or a file's
/// kind and name, with an x to remove it; while it uploads, its
/// progress over it; failed, an attention edge, Retry where it can go
/// again, and why under the row. VoiceOver reads a thumbnail as one
/// line, with Retry and Remove as its actions.
struct ComposerThumbs: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let tray: ComposerTray

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.tight) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Spacing.item) {
                    ForEach(tray.files) { thumb($0) }
                }
                // Room for the x over each corner.
                .padding(.top, Spacing.tight)
                .padding(.trailing, Spacing.tight)
            }
            .scrollClipDisabled()
            ForEach(tray.files.filter(\.failed)) { f in
                HStack(alignment: .firstTextBaseline, spacing: Spacing.hair) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(theme.attention)
                        .accessibilityHidden(true)
                    Text("\(f.name): \(f.detail)")
                        .foregroundStyle(theme.ink)
                        .lineLimit(2)
                }
                .font(theme.body(.caption))
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Attached files")
    }

    /// A thumbnail's side: 64 pt, growing a little with the text.
    private var side: CGFloat { typeSize.isAccessibilitySize ? 88 : 64 }

    private func thumb(_ f: ComposerFile) -> some View {
        let shape = RoundedRectangle(cornerRadius: Radius.glyph, style: .continuous)
        return ZStack {
            picture(f)
                .frame(width: side, height: side)
                .clipShape(shape)
                .overlay(shape.strokeBorder(f.failed ? theme.attention : theme.edge, lineWidth: f.failed ? 2 : 1))
            if f.uploading, case .uploading(let sent) = f.state {
                shape.fill(.black.opacity(0.35)).frame(width: side, height: side)
                ProgressView(value: f.size > 0 ? Double(sent) : 0, total: Double(max(f.size, 1)))
                    .progressViewStyle(.circular)
                    .tint(.white)
            }
            if f.canRetry {
                Button { tray.retry(f.id) } label: {
                    Label("Retry", systemImage: "arrow.clockwise")
                        .labelStyle(.iconOnly)
                        .font(theme.body(.headline, .semibold))
                        .foregroundStyle(theme.onAction)
                        .frame(width: 32, height: 32)
                        .background(theme.action, in: Circle())
                        .frame(minWidth: tapTarget, minHeight: tapTarget)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .frame(width: side, height: side)
        .overlay(alignment: .topTrailing) {
            Button { tray.remove(f.id) } label: {
                Label("Remove", systemImage: "xmark")
                    .labelStyle(.iconOnly)
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(theme.base)
                    .frame(width: 20, height: 20)
                    .background(theme.ink, in: Circle())
                    .overlay(Circle().strokeBorder(theme.base, lineWidth: 1.5))
                    .frame(width: tapTarget, height: tapTarget)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .offset(x: tapTarget / 2 - 10 - Spacing.tight, y: -(tapTarget / 2 - 10 - Spacing.tight))
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(f.spoken)
        .accessibilityActions {
            if f.canRetry { Button("Retry") { tray.retry(f.id) } }
            Button("Remove") { tray.remove(f.id) }
        }
    }

    /// An image's own picture; a file's kind over its name.
    @ViewBuilder private func picture(_ f: ComposerFile) -> some View {
        if let data = tray.previews[f.id], let image = UIImage(data: data) {
            Image(uiImage: image)
                .resizable()
                .scaledToFill()
        } else {
            VStack(spacing: 2) {
                Image(systemName: f.image ? "photo" : "doc.text")
                    .font(theme.body(.title3))
                    .foregroundStyle(theme.muted)
                Text((f.name as NSString).pathExtension.uppercased())
                    .font(theme.mono(.caption2, .semibold))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(theme.ink.opacity(ThemeColours.inkWash))
        }
    }
}

/// The system's Paste in the card's bottom row (T893), while the field
/// has focus and the pasteboard has a file the keyboard's suggestion bar
/// doesn't offer, a PDF or a text file (the composer asks offered): its
/// tap reads the pasteboard as the user's own act, so iOS doesn't ask.
/// An image pastes from the bar ("Photo · Paste from …"), the edit menu
/// or ⌘V (ComposerField). The pasteboard is asked only whether it has
/// one (UIPasteboard's has- and contains, which don't ask either).
struct PasteFiles: View {
    let tray: ComposerTray

    var body: some View {
        PasteButton(payloadType: PastedFile.self) { files in
            Task { @MainActor in
                for f in files { tray.add(name: f.name, data: f.data) }
            }
        }
        .labelStyle(.iconOnly)
        .buttonBorderShape(.circle)
        .accessibilityHint("Attaches the image or file you copied")
        .transition(.opacity)
    }

    /// The pasteboard has a file to attach that isn't an image.
    static func offered() -> Bool {
        UIPasteboard.general.contains(pasteboardTypes: PastedFile.types.filter { $0 != .image }.map(\.identifier))
    }

    /// The pasteboard has an image or a file to attach.
    static func check() -> Bool {
        let pb = UIPasteboard.general
        return pb.hasImages || pb.contains(pasteboardTypes: PastedFile.types.filter { $0 != .image }.map(\.identifier))
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
