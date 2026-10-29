import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// The prompt box under the conversation (design T734 §5): ↩ sends,
/// queued until the agent is idle; ⌥↩ steers it into the running turn;
/// Interrupt stops the turn for it, after a confirmation. ⇧↩ is a new
/// line; ⌘L focuses it. A thread's composer says that sending takes the thread over:
/// the server raises the takeover, as typing in its pane does. On a
/// paired server's session (at write) a prompt is typed into its pane
/// there, now: no steering, no queue.
/// Files pasted (⌘V with files or an image on the pasteboard), dropped
/// or picked (the paperclip) go to tm at once (session.upload, into the
/// project's uploads/) and show as chips; the prompt names them, and tm
/// puts each one's path after its text. Not on a paired server's session.
struct Composer: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?
    /// Takes the focus as it shows (the palette's).
    var autofocus = false
    @State private var text = ""
    @State private var said: String?
    @State private var failed = false
    @State private var busy = false
    @State private var interrupting = false
    @State private var files: [ComposerFile] = []
    @State private var dropping = false
    @State private var pasteMonitor: Any?
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            VStack(alignment: .leading, spacing: 8) {
                if !files.isEmpty {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 6) {
                            ForEach(files) { f in chip(f) }
                        }
                    }
                }
                HStack(alignment: .bottom, spacing: 8) {
                    if attaches {
                        Button { pick() } label: { Image(systemName: "paperclip") }
                            .buttonStyle(.borderless)
                            .disabled(files.count >= ComposerFiles.maxFiles)
                            .help("Attach files: images, PDFs or text (you can also paste or drop them)")
                            .accessibilityLabel("Attach files")
                    }
                    ZStack(alignment: .topLeading) {
                        if text.isEmpty {
                            Text(placeholder)
                                .foregroundStyle(theme.muted)
                                .padding(.leading, 5)
                                .allowsHitTesting(false)
                        }
                        TextEditor(text: $text)
                            .scrollContentBackground(.hidden)
                            .font(theme.body(.body))
                            .frame(minHeight: 20, maxHeight: 120)
                            .fixedSize(horizontal: false, vertical: true)
                            .focused($focused)
                            .onKeyPress(.return, phases: .down) { press in
                                // ⌘↩ is the open card's first choice, not a send.
                                if press.modifiers.contains(.shift) || press.modifiers.contains(.command) { return .ignored }
                                send(mode: press.modifiers.contains(.option) && !model.ref.isRemote ? "now" : "")
                                return .handled
                            }
                            .accessibilityLabel(placeholder)
                            .modifier(NoWritingTools())
                    }
                    .padding(.vertical, 2)
                    if running && !model.ref.isRemote {
                        Button("Interrupt") { interrupting = true }
                            .disabled(empty || busy)
                            .help("Stops the agent's turn for this prompt")
                        Button("Steer") { send(mode: "now") }
                            .disabled(empty || busy)
                            .help("Sends it into the running turn (⌥↩)")
                    }
                    Button("Send") { send(mode: "") }
                        .buttonStyle(.borderedProminent)
                        .tint(theme.action)
                        .disabled(empty || busy)
                        .help(running ? "Queued: it goes once the turn ends (↩)" : "Sends it (↩)")
                }
            }
            .padding(10)
            .frosted(RoundedRectangle(cornerRadius: 14))
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
            .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(dropping ? theme.action : theme.edge, lineWidth: dropping ? 2 : 1))
            .onDrop(of: [.fileURL, .image], isTargeted: attaches ? $dropping : .constant(false)) { providers in
                guard attaches else { return false }
                drop(providers)
                return true
            }
            HStack(spacing: 10) {
                if let said {
                    Text(said)
                        .foregroundStyle(failed ? theme.attention : theme.muted)
                        .lineLimit(2)
                } else if let q = session?.queuedPrompts, q > 0 {
                    Text(q == 1 ? "1 prompt queued" : "\(q) prompts queued")
                        .foregroundStyle(theme.muted)
                }
                if let held = session?.queueHeld, !held.isEmpty {
                    Text("Queue held: \(held)")
                        .foregroundStyle(theme.attention)
                }
                Spacer()
                Text(model.ref.isRemote ? "⇧↩ new line" : "⌥↩ steer · ⇧↩ new line")
                    .font(theme.mono(.caption2))
                    .foregroundStyle(theme.muted)
            }
            .font(theme.body(.footnote))
            .padding(.horizontal, 6)
        }
        .padding(.horizontal, Page.edge)
        .padding(.top, 10)
        .padding(.bottom, 14)
        .onChange(of: app.composerFocus) { focused = true }
        .onChange(of: app.draft) { takeDraft() }
        .onAppear {
            if autofocus { focused = true }
            takeDraft()
            watchPaste()
        }
        .onDisappear {
            if let m = pasteMonitor { NSEvent.removeMonitor(m) }
            pasteMonitor = nil
        }
        .confirmationDialog("Interrupt the agent's turn?", isPresented: $interrupting) {
            Button("Interrupt and Send", role: .destructive) { send(mode: "interrupt") }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The running turn stops and this prompt goes in its place. tm refuses while a draft or a dialog is on the agent's screen.")
        }
    }

    /// Nothing to send: no text and no file in, or a file still going.
    private var empty: Bool {
        let blank = text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return (blank && !files.contains { $0.ref != nil }) || files.contains(where: \.uploading)
    }

    /// Whether the composer takes files: this server's sessions, a tm
    /// with session.upload.
    private var attaches: Bool { app.canAttach && !model.ref.isRemote }

    private var running: Bool { session?.state == "working" }

    private var placeholder: String {
        if model.ref.isRemote {
            let who = model.ref.isCoordinator ? "the coordinator" : model.ref.thread
            return "Message \(who) on \(model.ref.server)"
        }
        if model.ref.isCoordinator { return "Message the coordinator" }
        let who = model.ref.thread
        return "Message \(who) · sending takes the thread over"
    }

    /// The text the window has for this composer (Ask the
    /// Coordinator), after what the user typed already.
    private func takeDraft() {
        guard let t = app.takeDraft(for: model.ref) else { return }
        text = text.isEmpty ? t : text + "\n" + t
        focused = true
    }

    private func send(mode: String) {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !empty, !busy else { return }
        let refs = files.compactMap(\.ref)
        busy = true
        Task {
            do {
                let r = try await model.prompt(t, mode: mode, files: refs)
                text = ""
                files = []
                failed = false
                said = PromptSent.words(r)
            } catch {
                failed = true
                said = error.localizedDescription
            }
            busy = false
        }
    }

    // MARK: files

    private func chip(_ f: ComposerFile) -> some View {
        let failed: Bool = { if case .failed = f.state { return true } else { return false } }()
        let image = ["png", "jpg", "jpeg", "gif", "webp"].contains((f.name as NSString).pathExtension.lowercased())
        return HStack(spacing: 6) {
            Image(systemName: failed ? "exclamationmark.triangle" : image ? "photo" : "doc.text")
                .foregroundStyle(failed ? theme.attention : theme.muted)
            VStack(alignment: .leading, spacing: 0) {
                Text(f.name).lineLimit(1).truncationMode(.middle)
                Text(f.detail)
                    .font(theme.body(.caption2))
                    .foregroundStyle(failed ? theme.attention : theme.muted)
                    .lineLimit(1)
            }
            .frame(maxWidth: 180, alignment: .leading)
            Button {
                files.removeAll { $0.id == f.id }
            } label: {
                Image(systemName: "xmark.circle.fill")
            }
            .buttonStyle(.borderless)
            .foregroundStyle(theme.muted)
            .help("Remove it from this prompt")
            .accessibilityLabel("Remove \(f.name)")
        }
        .font(theme.body(.footnote))
        .padding(.horizontal, 8)
        .padding(.vertical, 5)
        .background(theme.edge.opacity(0.35), in: RoundedRectangle(cornerRadius: 8))
        .help(failed ? f.detail : f.name)
        .accessibilityElement(children: .combine)
    }

    /// Adds a file of name and its bytes, and sends it to tm; one tm
    /// won't take shows as a chip saying why.
    private func add(name: String, data: Data) {
        guard files.count < ComposerFiles.maxFiles else {
            failed = true
            said = "A prompt takes at most \(ComposerFiles.maxFiles) files."
            return
        }
        if let why = ComposerFiles.refusal(name: name, size: data.count) {
            files.append(ComposerFile(name: name, size: data.count, state: .failed(why)))
            return
        }
        let f = ComposerFile(name: name, size: data.count)
        files.append(f)
        Task {
            let state: ComposerFile.State
            do {
                let r = try await model.upload(data, name: name) { sent in
                    set(f.id, .uploading(sent))
                }
                state = .ready(ref: r.ref)
            } catch {
                state = .failed(error.localizedDescription)
            }
            set(f.id, state)
        }
    }

    private func set(_ id: UUID, _ state: ComposerFile.State) {
        if let i = files.firstIndex(where: { $0.id == id }) { files[i].state = state }
    }

    /// Adds the file at url: a folder, or one tm won't take, says why
    /// before reading it.
    private func add(url: URL) {
        let v = try? url.resourceValues(forKeys: [.isDirectoryKey, .fileSizeKey])
        let name = url.lastPathComponent
        if v?.isDirectory == true {
            files.append(ComposerFile(name: name, size: 0, state: .failed("A folder: attach its files")))
            return
        }
        let size = v?.fileSize ?? 0
        if let why = ComposerFiles.refusal(name: name, size: size) {
            files.append(ComposerFile(name: name, size: size, state: .failed(why)))
            return
        }
        do {
            add(name: name, data: try Data(contentsOf: url))
        } catch {
            files.append(ComposerFile(name: name, size: size, state: .failed(error.localizedDescription)))
        }
    }

    private func drop(_ providers: [NSItemProvider]) {
        for p in providers {
            if p.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
                _ = p.loadObject(ofClass: URL.self) { url, _ in
                    guard let url else { return }
                    Task { @MainActor in add(url: url) }
                }
            } else if p.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
                p.loadDataRepresentation(forTypeIdentifier: UTType.image.identifier) { data, _ in
                    guard let png = data.flatMap(ComposerFiles.png) else { return }
                    Task { @MainActor in add(name: ComposerFiles.pastedName(), data: png) }
                }
            }
        }
    }

    private func pick() {
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = true
        panel.canChooseDirectories = false
        panel.allowedContentTypes = ComposerFiles.extensions.compactMap { UTType(filenameExtension: $0) }
        panel.prompt = "Attach"
        panel.begin { r in
            guard r == .OK else { return }
            for url in panel.urls { add(url: url) }
            focused = true
        }
    }

    /// ⌘V in the composer with files on the pasteboard (copied in the
    /// Finder) or an image without text (a screenshot) attaches them;
    /// anything else pastes as text.
    private func watchPaste() {
        guard pasteMonitor == nil else { return }
        pasteMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            guard focused, attaches,
                  event.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command,
                  event.charactersIgnoringModifiers == "v" else { return event }
            let pb = NSPasteboard.general
            let urls = pb.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
            if !urls.isEmpty {
                for url in urls { add(url: url) }
                return nil
            }
            if pb.string(forType: .string) == nil,
               let png = pb.data(forType: .png) ?? pb.data(forType: .tiff).flatMap(ComposerFiles.png) {
                add(name: ComposerFiles.pastedName(), data: png)
                return nil
            }
            return event
        }
    }
}

/// A prompt goes to the agent as typed: no Writing Tools rewriting it.
struct NoWritingTools: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 15, *) {
            content.writingToolsBehavior(.disabled)
        } else {
            content
        }
    }
}
