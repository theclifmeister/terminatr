import SwiftUI
import TerminatrKit
import TerminatrUI

/// The composer dock under a conversation, at the write level (T821,
/// T840; the dock T848): it floats over the conversation's end, with no
/// bar under it. In it, while the keyboard is up, a circle that hides it
/// (HideKeyboard, T857; T882), the prompt in a glass field, and a round
/// Send, queued until the agent is idle; while the agent works the field
/// has Queue and Steer (into the running turn, session.prompt "now"),
/// and Send held down has Interrupt (stops the turn for it, after a
/// confirmation); not on a paired server's session, whose prompt is
/// typed into its pane there. Over it, a line: what the last send did,
/// the queue and a held queue, and while the field has focus that
/// sending takes the thread over, as the Mac's says. The circles, the
/// field at one line and Send are one height (Theme.circle). Text
/// pastes as in any field. On this server's sessions (session.upload)
/// files go with the prompt as on the Mac (T860, ComposerAttach): the
/// paperclip circle picks them (Photos, Files, the camera), an image or
/// file copied pastes (⌘V, or the Paste button over the dock while the
/// keyboard is up, on the chips' row at their right, T882) and one
/// dropped on the dock goes in; each shows as a chip over the field, its
/// upload's progress, Retry and Remove. The session's files are
/// PhoneModel's (its tray), kept as the draft is across screens and the
/// link's drops. Keys go to the terminal on its own view
/// (TerminalScreen), not here (T882). -compose YES focuses the field as
/// it shows (the Simulator's screenshots). With a hardware keyboard (an
/// iPad's, T830) Command-Return sends as picked and Shift-Command-Return
/// steers, as Return makes a new line; no key without a modifier does
/// anything here.
struct PhoneComposer: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let ref: SessionRef
    let session: String
    let running: Bool
    let queued: Int
    /// Why the queue is held, when it is.
    var held = ""
    @State private var text = ""
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    /// The note says a prompt is queued: it goes once that prompt went in.
    @State private var queuedNote = false
    @State private var steer = false
    @State private var interrupting = false
    @State private var sends = 0
    @State private var keyboard = false
    @State private var dropping = false
    /// The pasteboard has an image or a file (PasteFiles.check).
    @State private var pasteable = PasteFiles.check()
    @FocusState private var focused: Bool

    var body: some View {
        let tray = model.tray(ref)
        let files = attaches
        let paste = keyboard && files && pasteable && !tray.full
        VStack(alignment: .leading, spacing: Spacing.item) {
            if let note {
                DockNote(note.text, failed: note.failed)
                    .transition(.opacity)
            }
            // Paste sits level with the chips, at their right; with none,
            // at their height over the field.
            if (files && !tray.files.isEmpty) || paste {
                HStack(spacing: Spacing.item) {
                    if files, !tray.files.isEmpty { ComposerChips(tray: tray) }
                    Spacer(minLength: 0)
                    if paste { PasteFiles(tray: tray) }
                }
                .frame(minHeight: tapTarget)
            }
            DockRow {
                // A Group: with neither, the row has no gap for them.
                Group {
                    if keyboard { HideKeyboard() }
                    if files { AttachMenu(tray: tray) }
                }
            } field: { field } send: { sendButton }
        }
        .overlay {
            if dropping {
                RoundedRectangle(cornerRadius: Radius.tile, style: .continuous)
                    .strokeBorder(theme.action, lineWidth: 2)
                    .padding(-Spacing.hair)
                    .allowsHitTesting(false)
            }
        }
        .onDrop(of: Incoming.types, isTargeted: files ? $dropping : .constant(false)) { providers in
            guard files else { return false }
            // NSItemProvider loads from any thread.
            nonisolated(unsafe) let providers = providers
            Task.detached { await ComposerDrop.take(providers, into: tray) }
            return true
        }
        // The conversation's column: its cards' edges (T882).
        .padding(.horizontal, Spacing.gutter)
        .padding(.bottom, Spacing.item)
        .feedback(.impact(weight: .light), trigger: sends)
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in keyboard = true }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in keyboard = false }
        .onReceive(NotificationCenter.default.publisher(for: UIPasteboard.changedNotification)) { _ in pasteable = PasteFiles.check() }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in pasteable = PasteFiles.check() }
        .onAppear { if UserDefaults.standard.bool(forKey: "compose") { focused = true } }
        .onChange(of: [queued, running ? 1 : 0]) { old, new in
            if queuedNote, PromptSent.wentIn(queued: old[0], new[0], working: old[1] == 1, new[1] == 1) {
                queuedNote = false
                said = nil
            }
        }
        .kept($text, "prompt \(ref)")
        .kept($steer, "steer \(ref)")
        .confirmationDialog("Interrupt the agent's turn?", isPresented: $interrupting, titleVisibility: .visible) {
            Button("Interrupt and Send", role: .destructive) { send(mode: "interrupt") }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The running turn stops and this prompt goes in its place. tm refuses while a draft or a dialog is on the agent's screen.")
        }
    }

    /// Whether Queue / Steer and Interrupt show: the agent works here.
    private var working: Bool { running && !ref.isRemote }

    /// Whether the composer takes files here.
    private var attaches: Bool { model.attaches(ref) }

    /// The line over the dock, if any.
    private var note: (text: String, failed: Bool)? {
        if attaches, let n = model.tray(ref).note { return (n, true) }
        if let said { return said }
        var parts: [String] = []
        if queued > 0 { parts.append(queued == 1 ? "1 prompt queued" : "\(queued) prompts queued") }
        if !held.isEmpty { return ((parts + ["Queue held: \(held)"]).joined(separator: " · "), true) }
        if focused, !ref.isCoordinator, !ref.isRemote { parts.append("Sending takes the thread over") }
        return parts.isEmpty ? nil : (parts.joined(separator: " · "), false)
    }

    /// The field; at accessibility sizes Queue and Steer go under the
    /// text, which keeps the row's width. Its text keeps the field's
    /// inset on the right as on the left (T857).
    private var field: some View {
        let under = typeSize.isAccessibilitySize
        let row = under
            ? AnyLayout(VStackLayout(alignment: .trailing, spacing: 0))
            : AnyLayout(HStackLayout(alignment: .bottom, spacing: Spacing.item))
        return row {
            TextField(placeholder, text: $text, axis: .vertical)
                .lineLimit(1...6)
                .font(theme.body(.body))
                .foregroundStyle(theme.ink)
                .focused($focused)
                .padding(.vertical, Spacing.item)
                // A line sits level with Queue and Steer.
                .frame(minHeight: tapTarget)
                .padding(.trailing, working && under ? Spacing.card - Spacing.hair : 0)
                .modifier(NoWritingTools())
                .modifier(PastesFiles(tray: attaches ? model.tray(ref) : nil))
            if working {
                DockModes([(false, "Queue"), (true, "Steer")], selection: $steer)
                    .accessibilityElement(children: .contain)
                    .accessibilityLabel("How it goes")
            }
        }
        .dockField(control: working)
    }

    private var sendButton: some View {
        Button {
            send(mode: working && steer ? "now" : "")
        } label: {
            Label(working && steer ? "Steer" : "Send", systemImage: "arrow.up")
        }
        .buttonStyle(.send)
        .keyboardShortcut(.return, modifiers: .command)
        .disabled(empty || busy)
        .contextMenu {
            if working {
                Button("Interrupt and Send", systemImage: "stop.circle", role: .destructive) { interrupting = true }
                    .disabled(empty || busy)
            }
        }
        .accessibilityHint(working ? (steer ? "Sends it into the running turn" : "Queued: it goes once the turn ends") : "")
        .accessibilityActions {
            if working, !empty, !busy {
                Button("Interrupt and Send") { interrupting = true }
            }
        }
        .background {
            // Shift-Command-Return steers, whichever is picked.
            if working {
                Button("Steer") { send(mode: "now") }
                    .keyboardShortcut(.return, modifiers: [.command, .shift])
                    .disabled(empty || busy)
                    .hidden()
            }
        }
    }

    /// Nothing to send: no text and no file in, or a file still going.
    private var empty: Bool {
        let blank = text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        guard attaches else { return blank }
        let tray = model.tray(ref)
        return (blank && tray.refs.isEmpty) || tray.uploading
    }

    private var placeholder: String {
        // Beside Queue and Steer, the word alone fits.
        if working { return "Message" }
        let who = model.tag(ref)
        if ref.isRemote { return "Message \(ref.isCoordinator ? "the coordinator" : who) on \(ref.server)" }
        return "Message \(who)"
    }

    private func send(mode: String) {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !empty, !busy else { return }
        let tray = attaches ? model.tray(ref) : nil
        let refs = tray?.refs ?? []
        busy = true
        said = nil
        sends += 1
        Task {
            do {
                let r = try await model.prompt(session, text: t, mode: mode, files: refs)
                text = ""
                tray?.clear()
                said = (PromptSent.words(r), false)
                queuedNote = r.via == "queued"
            } catch {
                queuedNote = false
                if let w = NeedActs.words(error) { said = (w, true) }
            }
            busy = false
        }
    }
}

/// A prompt goes to the agent as typed: no Writing Tools rewriting it.
struct NoWritingTools: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 18, *) {
            content.writingToolsBehavior(.disabled)
        } else {
            content
        }
    }
}

/// Folds the keyboard away (T857): a glass circle at the dock's left
/// while the keyboard is up (T882). The draft stays.
struct HideKeyboard: View {
    var body: some View {
        Button {
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        } label: {
            Label("Hide Keyboard", systemImage: "keyboard.chevron.compact.down")
        }
        .buttonStyle(.circle)
        .transition(.opacity)
    }
}
