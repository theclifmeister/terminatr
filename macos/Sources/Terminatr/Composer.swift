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
    /// The note says a prompt is queued: it goes once that prompt went in.
    @State private var queuedNote = false
    @State private var busy = false
    @State private var interrupting = false
    private var tray: ComposerTray { model.tray }
    @State private var dropping = false
    @State private var pasteMonitor: Any?
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !tray.files.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 6) {
                        ForEach(tray.files) { f in chip(f) }
                    }
                }
            }
            notes
            ComposerRow(leastField: ComposerFit.leastField * theme.scale) {
                field
                if running && !model.ref.isRemote {
                    ComposerButton(title: "Interrupt", help: "Stops the agent's turn for this prompt") { interrupting = true }
                        .disabled(empty || busy)
                    ComposerButton(title: "Steer", help: "Sends it into the running turn (⌥↩)") { send(mode: "now") }
                        .disabled(empty || busy)
                }
                ComposerButton(title: "Send", help: running ? "Queued: it goes once the turn ends (↩)" : "Sends it (↩)") { send(mode: "") }
                    .disabled(empty || busy)
            }
        }
        // 58 with its line at rest and the buttons beside the field, as
        // the inspector's footer (13 + 34 + 11).
        .padding(.horizontal, 12)
        .padding(.top, 13)
        .padding(.bottom, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) { theme.line.frame(height: 1) }
        .onChange(of: app.composerFocus) { focused = true }
        .onChange(of: app.draft) { takeDraft() }
        .onChange(of: [session?.queuedPrompts ?? 0, running ? 1 : 0]) { old, new in
            if queuedNote, PromptSent.wentIn(queued: old[0], new[0], working: old[1] == 1, new[1] == 1) {
                queuedNote = false
                said = nil
            }
        }
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

    /// The field: 34 high and 6 round at one line, growing as a prompt
    /// does; attach at its start and, while it is empty, what prompting
    /// says.
    private var field: some View {
        let shape = RoundedRectangle(cornerRadius: 6)
        return HStack(alignment: .top, spacing: 8) {
            if attaches {
                Button { pick() } label: {
                    Image(systemName: "paperclip")
                        .font(theme.sized(13))
                        .foregroundStyle(tray.full ? theme.faint : theme.muted)
                        .frame(height: 18)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(tray.full)
                .help("Attach files: images, PDFs or text (you can also paste or drop them)")
                .accessibilityLabel("Attach files")
            }
            ZStack(alignment: .topLeading) {
                if text.isEmpty {
                    prompting
                        .frame(height: 18)
                        .padding(.leading, 5)
                        .allowsHitTesting(false)
                }
                TextEditor(text: $text)
                    .scrollContentBackground(.hidden)
                    .font(theme.conversation)
                    .frame(minHeight: 18, maxHeight: 120)
                    .fixedSize(horizontal: false, vertical: true)
                    .focused($focused)
                    .onKeyPress(.return, phases: .down) { press in
                        // ⌘↩ is the open card's first choice, not a send.
                        if press.modifiers.contains(.shift) || press.modifiers.contains(.command) { return .ignored }
                        send(mode: press.modifiers.contains(.option) && !model.ref.isRemote ? "now" : "")
                        return .handled
                    }
                    .accessibilityLabel(placeholder)
                    .accessibilityHint(keys)
                    .modifier(NoWritingTools())
            }
        }
        .padding(.leading, attaches ? 11 : 6)
        .padding(.trailing, 11)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, minHeight: 34, alignment: .topLeading)
        .contentShape(shape)
        .overlay(shape.strokeBorder(dropping ? theme.action : theme.line, lineWidth: dropping ? 2 : theme.contrast ? 1.5 : 1))
        .onDrop(of: [.fileURL, .image], isTargeted: attaches ? $dropping : .constant(false)) { providers in
            guard attaches else { return false }
            drop(providers)
            return true
        }
    }

    /// What an empty field says: the placeholder and, at the field's end,
    /// the keys; where both don't fit the keys go first (the buttons' help
    /// and VoiceOver's hint still say them), so the placeholder keeps its
    /// words.
    private var prompting: some View {
        let words = Text(placeholder)
            .font(theme.sized(13))
            .foregroundStyle(theme.faint)
            .lineLimit(1)
        return ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) {
                words.fixedSize()
                Spacer(minLength: 0)
                Text(keys)
                    .font(theme.sized(13))
                    .foregroundStyle(theme.faint)
                    .lineLimit(1)
                    .fixedSize()
                    .accessibilityHidden(true)
            }
            words
        }
    }

    /// The keys, in words.
    private var keys: String {
        model.ref.isRemote ? "⇧↩ new line" : "⌥↩ steer · ⇧↩ new line"
    }

    /// What the last send did, the queue, a held queue: over the field,
    /// only while there is something to say.
    @ViewBuilder
    private var notes: some View {
        let held = session?.queueHeld ?? ""
        let queued = session?.queuedPrompts ?? 0
        if tray.note != nil || said != nil || queued > 0 || !held.isEmpty {
            HStack(spacing: 10) {
                if let note = tray.note {
                    Text(note)
                        .foregroundStyle(theme.attention)
                        .lineLimit(2)
                } else if let said {
                    Text(said)
                        .foregroundStyle(failed ? theme.attention : theme.muted)
                        .lineLimit(2)
                } else if queued > 0 {
                    Text(queued == 1 ? "1 prompt queued" : "\(queued) prompts queued")
                        .foregroundStyle(theme.muted)
                }
                if !held.isEmpty {
                    Text("Queue held: \(held)")
                        .foregroundStyle(theme.attention)
                }
            }
            .font(theme.sized(13))
        }
    }

    /// Nothing to send: no text and no file in, or a file still going.
    private var empty: Bool {
        let blank = text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return (blank && tray.refs.isEmpty) || tray.uploading
    }

    /// Whether the composer takes files: a tm with session.upload, and for
    /// a paired server's session, that server's word (AppModel.attaches).
    private var attaches: Bool { app.attaches(model.ref) }

    private var running: Bool { session?.state == "working" }

    private var placeholder: String {
        if model.ref.isRemote {
            let who = model.ref.isCoordinator ? "the coordinator" : model.ref.thread
            return "Message \(who) on \(model.ref.server)"
        }
        if model.ref.isCoordinator { return "Message the coordinator" }
        if model.ref.director { return "Message the director" }
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
        let refs = tray.refs
        busy = true
        Task {
            do {
                let r = try await model.prompt(t, mode: mode, files: refs)
                text = ""
                tray.clear()
                failed = false
                said = PromptSent.words(r)
                queuedNote = r.via == "queued"
            } catch {
                failed = true
                queuedNote = false
                said = error.localizedDescription
            }
            busy = false
        }
    }

    // MARK: files

    private func chip(_ f: ComposerFile) -> some View {
        HStack(spacing: 6) {
            Image(systemName: f.failed ? "exclamationmark.triangle" : f.image ? "photo" : "doc.text")
                .foregroundStyle(f.failed ? theme.attention : theme.muted)
            VStack(alignment: .leading, spacing: 0) {
                Text(f.name).lineLimit(1).truncationMode(.middle)
                Text(f.detail)
                    .font(theme.body(.footnote))
                    .foregroundStyle(f.failed ? theme.attention : theme.muted)
                    .lineLimit(1)
            }
            .frame(maxWidth: 180, alignment: .leading)
            if f.canRetry {
                Button {
                    tray.retry(f.id)
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
                .foregroundStyle(theme.muted)
                .help("Send it again")
                .accessibilityLabel("Retry \(f.name)")
            }
            Button {
                tray.remove(f.id)
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
        .background(theme.tab, in: RoundedRectangle(cornerRadius: 6))
        .help(f.failed ? f.detail : f.name)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(f.spoken)
    }

    /// Adds the file at url: a folder, or one tm won't take, says why
    /// before reading it.
    private func add(url: URL) {
        let v = try? url.resourceValues(forKeys: [.isDirectoryKey, .fileSizeKey])
        let name = url.lastPathComponent
        if v?.isDirectory == true {
            tray.refuse(name: name, why: "A folder: attach its files")
            return
        }
        let size = v?.fileSize ?? 0
        if let why = ComposerFiles.refusal(name: name, size: size) {
            tray.refuse(name: name, size: size, why: why)
            return
        }
        do {
            add(name: name, data: try Data(contentsOf: url))
        } catch {
            tray.refuse(name: name, size: size, why: error.localizedDescription)
        }
    }

    private func add(name: String, data: Data) {
        tray.add(name: name, data: data)
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

/// A composer's button: at least 79 wide with 14 of side padding, 34 high
/// like the field beside it, 6 round.
private struct ComposerButton: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    let title: String
    let help: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(theme.sized(title.count > 8 ? 12 : 13))
                .foregroundStyle(enabled ? theme.ink : theme.muted)
                .lineLimit(1)
                .padding(.horizontal, 14)
                .frame(minWidth: 79, minHeight: 34, maxHeight: 34)
                .background(theme.tab, in: RoundedRectangle(cornerRadius: 6))
                .contentShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
        .help(help)
    }
}
