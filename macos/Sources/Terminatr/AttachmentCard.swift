import AppKit
import PDFKit
import SwiftUI
import TerminatrKit
import UniformTypeIdentifiers

/// A call that sends the user files (Claude's SendUserFile), as its own
/// card: who and when, the caption, then each file (Attachments).
struct AttachmentCard: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    let session: String?
    let turn: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(turn)
                .font(theme.mono(.caption, .medium))
                .foregroundStyle(theme.muted)
            if !entry.text.isEmpty {
                Text(entry.text)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Attachments(files: entry.attachments, session: session, alignment: .leading)
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

/// Files in a card or under a prompt: the previews first, then a row for
/// each file without one.
struct Attachments: View {
    let files: [Attachment]
    let session: String?
    let alignment: HorizontalAlignment

    var body: some View {
        VStack(alignment: alignment, spacing: 8) {
            ForEach(files) { f in
                AttachmentView(file: f, session: session)
            }
        }
    }
}

/// What one file shows as it loads: its preview (an image or a PDF's
/// first page, at most maxHeight tall, aspect kept; a click opens it
/// larger), or its row (icon, name, size, Save As…, Reveal, Open). A file
/// no longer there says so, never a spinner.
struct AttachmentView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.displayScale) private var scale
    let file: Attachment
    let session: String?
    @State private var state = Load.loading
    @State private var previewing = false

    static let maxHeight: CGFloat = 240
    static let maxWidth: CGFloat = 520

    enum Load: Equatable {
        case loading
        case preview(CGImage)
        case row(size: Int?)
        case gone
        case unavailable(String)
    }

    var body: some View {
        content
            .task(id: "\(session ?? "")|\(file.ref)|\(app.files != nil)") { await load() }
            .sheet(isPresented: $previewing) {
                FilePreview(file: file, session: session)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch state {
        case .loading:
            if file.inline {
                RoundedRectangle(cornerRadius: 10)
                    .fill(theme.command)
                    .frame(width: 200, height: 140)
                    .overlay { ProgressView().controlSize(.small) }
                    .accessibilityLabel("Loading \(file.name)")
            } else {
                row(size: nil)
            }
        case .preview(let img):
            Button { previewing = true } label: {
                Image(decorative: img, scale: scale)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
                    .frame(maxWidth: Self.maxWidth, maxHeight: Self.maxHeight)
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.edge, lineWidth: 1))
            }
            .buttonStyle(.plain)
            .help("Open \(file.name)")
            .accessibilityLabel(file.name)
            .accessibilityHint("Opens it larger")
            .contextMenu { actions(size: nil) }
        case .row(let size):
            row(size: size)
        case .gone:
            placeholder("\(file.name): file no longer available", "doc.badge.ellipsis")
        case .unavailable(let why):
            placeholder("\(file.name): \(why)", "eye.slash")
        }
    }

    private func row(size: Int?) -> some View {
        HStack(spacing: 10) {
            Image(nsImage: Self.icon(file.name))
                .resizable()
                .frame(width: 28, height: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(file.name)
                    .font(theme.body(.subheadline, .medium))
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let size {
                    Text(Attachment.size(size))
                        .font(theme.body(.caption))
                        .foregroundStyle(theme.muted)
                }
            }
            Spacer(minLength: 8)
            HStack(spacing: 6) { actions(size: size) }
                .buttonStyle(.borderless)
                .font(theme.body(.caption))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .frame(maxWidth: Self.maxWidth, alignment: .leading)
        .background(theme.command, in: RoundedRectangle(cornerRadius: 9))
        .accessibilityElement(children: .contain)
    }

    @ViewBuilder
    private func actions(size: Int?) -> some View {
        if file.inline && state != .loading {
            Button("Preview") { previewing = true }
        }
        if file.sendable {
            Button("Save As…") { Task { await FileActs.saveAs(app, session, file) } }
            Button("Open") { Task { await FileActs.open(app, session, file, in: nil) } }
        }
        if !file.path.isEmpty, FileManager.default.fileExists(atPath: file.path) {
            Button("Reveal") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: file.path)]) }
        }
    }

    private func placeholder(_ text: String, _ symbol: String) -> some View {
        Label(text, systemImage: symbol)
            .font(theme.body(.footnote))
            .foregroundStyle(theme.muted)
            .lineLimit(2)
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .frame(maxWidth: Self.maxWidth, alignment: .leading)
            .background(theme.command.opacity(0.6), in: RoundedRectangle(cornerRadius: 9))
    }

    private func load() async {
        guard let store = app.files, let session else {
            state = .unavailable("this tm doesn't send files to the app: update it with tm update")
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
        case .success(nil):
            // ImageIO draws no SVG: AppKit does, off the main thread.
            if case .data(let d, _) = await store.data(session, file.ref), let img = await FileActs.raster(d, maxPixel: px) {
                state = .preview(img)
            } else {
                state = .row(size: nil)
            }
        }
    }

    static func icon(_ name: String) -> NSImage {
        let ext = (name as NSString).pathExtension
        return NSWorkspace.shared.icon(for: UTType(filenameExtension: ext) ?? .data)
    }
}

/// A file opened larger, in a sheet: the image, or the PDF's pages, with
/// Copy, Save As… and Open in Preview.
struct FilePreview: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    let file: Attachment
    let session: String?
    @State private var data: FileOutcome?

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Text(file.name)
                    .font(theme.title(.headline, .bold))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer()
                if case .data(let d, _) = data {
                    Button("Copy") { FileActs.copy(d, file) }
                        .keyboardShortcut("c")
                    Button("Save As…") { Task { await FileActs.saveAs(app, session, file) } }
                        .keyboardShortcut("s")
                    Button("Open in Preview") { Task { await FileActs.open(app, session, file, in: "com.apple.Preview") } }
                }
                Button("Done") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(12)
            Divider()
            Group {
                switch data {
                case nil:
                    ProgressView()
                case .gone:
                    Label("\(file.name): file no longer available", systemImage: "doc.badge.ellipsis")
                        .foregroundStyle(theme.muted)
                case .unavailable(let why):
                    Label(why, systemImage: "eye.slash")
                        .foregroundStyle(theme.muted)
                case .data(let d, _):
                    if file.kind == .pdf {
                        PDFPages(data: d)
                    } else if let img = NSImage(data: d) {
                        Image(nsImage: img)
                            .resizable()
                            .aspectRatio(contentMode: .fit)
                            .padding(12)
                    } else {
                        Label("This image can't be drawn", systemImage: "eye.slash")
                            .foregroundStyle(theme.muted)
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(minWidth: 520, idealWidth: 900, minHeight: 380, idealHeight: 700)
        .task {
            guard let store = app.files, let session else {
                data = .unavailable("this tm doesn't send files to the app")
                return
            }
            data = await store.data(session, file.ref)
        }
    }
}

/// A PDF's pages, scrolling.
struct PDFPages: NSViewRepresentable {
    let data: Data

    func makeNSView(context: Context) -> PDFView {
        let v = PDFView()
        v.autoScales = true
        v.document = PDFDocument(data: data)
        return v
    }

    func updateNSView(_ v: PDFView, context: Context) {}
}

/// What the cards do with a file's bytes, which come only through
/// session.file: copy them, save them where the user picks, or open a
/// copy of them. The app never opens the agent's path itself (a file of
/// a type tm doesn't send has no Open).
@MainActor
enum FileActs {
    static func bytes(_ app: AppModel, _ session: String?, _ f: Attachment) async -> Data? {
        guard let store = app.files, let session, case .data(let d, _) = await store.data(session, f.ref) else {
            NSSound.beep()
            return nil
        }
        return d
    }

    static func copy(_ d: Data, _ f: Attachment) {
        let pb = NSPasteboard.general
        pb.clearContents()
        if f.kind == .image, let img = NSImage(data: d) {
            pb.writeObjects([img])
        } else if f.kind == .pdf {
            pb.setData(d, forType: .pdf)
        } else if let t = UTType(mimeType: f.type) {
            pb.setData(d, forType: NSPasteboard.PasteboardType(t.identifier))
        }
    }

    static func saveAs(_ app: AppModel, _ session: String?, _ f: Attachment) async {
        guard let d = await bytes(app, session, f) else { return }
        let panel = NSSavePanel()
        panel.nameFieldStringValue = f.name.hasPrefix("Image #") ? "Image.\(ext(f))" : f.name
        guard panel.runModal() == .OK, let url = panel.url else { return }
        do {
            try d.write(to: url, options: .atomic)
        } catch {
            NSAlert(error: error).runModal()
        }
    }

    /// Opens a copy of the file, in the app of bundle id (Preview) or
    /// the type's default.
    static func open(_ app: AppModel, _ session: String?, _ f: Attachment, in bundle: String?) async {
        guard let d = await bytes(app, session, f) else { return }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("terminatr-files/\(UUID().uuidString)")
        let name = f.name.hasPrefix("Image #") ? "Image.\(ext(f))" : f.name
        let url = dir.appendingPathComponent((name as NSString).lastPathComponent)
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try d.write(to: url, options: .atomic)
        } catch {
            NSAlert(error: error).runModal()
            return
        }
        if let bundle, let appURL = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundle) {
            _ = try? await NSWorkspace.shared.open([url], withApplicationAt: appURL, configuration: NSWorkspace.OpenConfiguration())
        } else {
            NSWorkspace.shared.open(url)
        }
    }

    static func ext(_ f: Attachment) -> String {
        UTType(mimeType: f.type)?.preferredFilenameExtension ?? "png"
    }

    /// An image AppKit draws (an SVG), rasterised off the main thread at
    /// most maxPixel on its longer side.
    nonisolated static func raster(_ d: Data, maxPixel: Int) async -> CGImage? {
        await Task.detached(priority: .userInitiated) {
            guard let img = NSImage(data: d), img.size.width > 0, img.size.height > 0 else { return nil }
            let s = CGFloat(maxPixel) / max(img.size.width, img.size.height)
            var rect = CGRect(x: 0, y: 0, width: img.size.width * min(s, 4), height: img.size.height * min(s, 4))
            return img.cgImage(forProposedRect: &rect, context: nil, hints: nil)
        }.value
    }
}
