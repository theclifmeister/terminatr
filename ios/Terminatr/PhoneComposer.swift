import SwiftUI
import TerminatrKit
import TerminatrUI

/// The composer under a conversation, at the write level (T821, T840),
/// the design page's flat dock (T1001): on the window's colour under a
/// line. Folded, it is one row: + (files, on this server's sessions), the
/// field with the prompt's placeholder, and the Send button. With the field
/// focused, the keyboard up, a file in or a draft, it opens: the files'
/// thumbnails at its top (ComposerThumbs), the field the whole width, and
/// under it +, the "how it goes" chip while the agent works (Queue, after
/// the turn, or Steer, into it, session.prompt "now"; and Interrupt and
/// Send), Paste for a file the keyboard's suggestion bar doesn't offer
/// (PasteFiles), and Send. Send is filled in ink when there is something to
/// send, quiet otherwise. While the agent works the placeholder says the
/// prompt waits for the turn, and with nothing typed the button is Stop: the
/// turn stops, nothing sent (PhoneModel.stop). Send held down has Interrupt and
/// Send, after a confirmation. Not on a paired server's session, whose
/// prompt is typed into its pane there. A prompt queued while the agent
/// works shows at the conversation's end as the user's own bubble
/// (PhoneModel.queued), and the card folds back to the pill. Over the
/// composer, a line: what the last send did, a held queue, and while
/// the field has focus that sending takes the thread over, as the
/// Mac's says. Files pasted (the keyboard's "Photo · Paste from …",
/// the edit menu, ⌘V: ComposerField), picked or dropped go with the
/// prompt as on the Mac (T860, ComposerAttach); they are PhoneModel's
/// (its tray), kept as the draft is across screens and the link's
/// drops. A drag down the conversation hides the keyboard (T857); the
/// draft stays. No mic: the keyboard's dictation is the system's own.
/// -compose YES focuses the field as it shows (the Simulator's
/// screenshots). With a hardware keyboard (an iPad's, T830)
/// Command-Return sends as picked and Shift-Command-Return steers, as
/// Return makes a new line.
struct PhoneComposer: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let ref: SessionRef
    let session: String
    let running: Bool
    /// Why the queue is held, when it is.
    var held = ""
    @State private var text = ""
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var steer = false
    @State private var interrupting = false
    @State private var sends = 0
    @State private var dropping = false
    @State private var focused = false
    @State private var pick: AttachPick?
    /// The pasteboard has a file the keyboard doesn't offer
    /// (PasteFiles.offered).
    @State private var pasteable = PasteFiles.offered()

    var body: some View {
        let tray = attaches ? model.tray(ref) : nil
        VStack(alignment: .leading, spacing: Spacing.item) {
            if let note {
                DockNote(note.text, failed: note.failed)
                    .transition(.opacity)
            }
            card(tray)
        }
        .onDrop(of: Incoming.types, isTargeted: tray != nil ? $dropping : .constant(false)) { providers in
            guard let tray else { return false }
            take(providers, into: tray)
            return true
        }
        .padding(.horizontal, 12)
        .padding(.top, 11)
        // A fixed foot: never sized from the composer's own measured safe
        // area, whose change re-ran this body in layout (T1026).
        .padding(.bottom, Spacing.row)
        .background { theme.chrome.ignoresSafeArea(edges: .bottom) }
        .overlay(alignment: .top) { theme.line.frame(height: 1) }
        .animation(reduceMotion ? nil : .snappy(duration: 0.25), value: open)
        .modifier(AttachPickers(tray: tray, pick: $pick))
        .feedback(.impact(weight: .light), trigger: sends)
        .onReceive(NotificationCenter.default.publisher(for: UIPasteboard.changedNotification)) { _ in pasteable = PasteFiles.offered() }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in pasteable = PasteFiles.offered() }
        .onAppear { if UserDefaults.standard.bool(forKey: "compose") { focused = true } }
        .onChange(of: running) { _, now in if !now, said?.failed == false { said = nil } }
        .kept($text, "prompt \(ref)")
        .kept($steer, "steer \(ref)")
        .confirmationDialog("Interrupt the agent's turn?", isPresented: $interrupting, titleVisibility: .visible) {
            Button("Interrupt and Send", role: .destructive) { send(mode: "interrupt") }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The running turn stops and this prompt goes in its place. tm refuses while a draft or a dialog is on the agent's screen.")
        }
    }

    /// The dock's rows. The field keeps its place in the view tree folded
    /// or open, so it keeps the keyboard as it opens.
    private func card(_ tray: ComposerTray?) -> some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        return VStack(alignment: .leading, spacing: Spacing.item) {
            if open, let tray, !tray.files.isEmpty {
                ComposerThumbs(tray: tray)
            }
            HStack(alignment: .bottom, spacing: 7) {
                if !open, let tray { AttachMenu(tray: tray, pick: $pick) }
                ComposerField(text: $text, focused: $focused, placeholder: placeholder,
                              files: tray.map { tray in { take($0, into: tray) } })
                    .frame(minHeight: ComposerButton.height(theme))
                    .padding(.horizontal, 11)
                    .background(theme.pane, in: shape)
                    .overlay {
                        shape.strokeBorder(dropping ? theme.action : focused ? theme.focus : theme.line, lineWidth: dropping ? 2 : 1)
                            .allowsHitTesting(false)
                    }
                    .contentShape(shape)
                    // A tap on the field's free space starts typing.
                    .onTapGesture { if !focused { focused = true } }
                if !open { sendButton }
            }
            if open { bottomRow(tray) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The foot, open: + and the chip on the left, Paste and Send on the
    /// right; at accessibility sizes the chip takes a line of its own.
    @ViewBuilder private func bottomRow(_ tray: ComposerTray?) -> some View {
        let paste = focused && pasteable && tray.map { !$0.full } == true
        let chipLine = typeSize.isAccessibilitySize && working
        VStack(alignment: .leading, spacing: Spacing.hair) {
            if chipLine { chip }
            HStack(spacing: Spacing.item) {
                if let tray { AttachMenu(tray: tray, pick: $pick) }
                if working, !chipLine { chip }
                Spacer(minLength: 0)
                if paste, let tray { PasteFiles(tray: tray) }
                sendButton
            }
        }
    }

    /// How a prompt goes while the agent works: Queue (after the turn)
    /// or Steer (into it), a menu that also has Interrupt and Send.
    private var chip: some View {
        Menu {
            Picker("How it goes", selection: $steer) {
                Label("Queue for after this turn", systemImage: "text.line.last.and.arrowtriangle.forward").tag(false)
                Label("Steer this turn", systemImage: "arrow.turn.down.right").tag(true)
            }
            Divider()
            Button("Interrupt and Send", systemImage: "stop.circle", role: .destructive) { interrupting = true }
                .disabled(empty || busy)
        } label: {
            HStack(spacing: Spacing.hair) {
                Text(steer ? "Steer" : "Queue")
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(theme.ink)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 9 * theme.scale, weight: .semibold))
                    .foregroundStyle(theme.ink)
            }
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 10)
            .frame(minHeight: ComposerButton.height(theme))
            .background(theme.tab, in: RoundedRectangle(cornerRadius: Radius.row))
            .frame(minHeight: tapTarget)
            .contentShape(Rectangle())
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .feedback(.selection, trigger: steer)
        .accessibilityLabel("How it goes")
        .accessibilityValue(steer ? "Steer this turn" : "Queue for after this turn")
    }

    /// Whether the card is open: focused, a file in, a draft, or at an
    /// accessibility size, where the pill's row has no room.
    private var open: Bool {
        focused || !text.isEmpty || (attaches && !model.tray(ref).files.isEmpty) || typeSize.isAccessibilitySize
    }

    /// Whether Queue / Steer and Stop show: the agent works here.
    private var working: Bool { running && !ref.isRemote }

    /// Whether the composer takes files here.
    private var attaches: Bool { model.attaches(ref) }

    /// The line over the composer, if any.
    private var note: (text: String, failed: Bool)? {
        if attaches, let n = model.tray(ref).note { return (n, true) }
        if let said { return said }
        if !held.isEmpty { return ("Queue held: \(held)", true) }
        if focused, !ref.thread.isEmpty, !ref.isRemote { return ("Sending takes the thread over", false) }
        return nil
    }

    /// Stop instead of send: the agent works and nothing is typed.
    private var stops: Bool { working && empty && !(attaches && model.tray(ref).uploading) }

    private var sendButton: some View {
        Group {
            if stops {
                Button("Stop", action: stop)
                    .buttonStyle(ComposerButton(kind: .word(primary: false)))
                .disabled(busy)
                .accessibilityHint("Stops the agent's turn; nothing is sent")
            } else {
                Button(working && steer ? "Steer" : working ? "Queue" : "Send") {
                    send(mode: working && steer ? "now" : "")
                }
                .buttonStyle(ComposerButton(kind: .word(primary: true)))
                .disabled(empty || busy)
                .accessibilityHint(working ? (steer ? "Sends it into the running turn" : "Queued: it goes once the turn ends") : "")
            }
        }
        .keyboardShortcut(.return, modifiers: .command)
        .contextMenu {
            if working {
                Button("Interrupt and Send", systemImage: "stop.circle", role: .destructive) { interrupting = true }
                    .disabled(empty || busy)
            }
        }
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
        if working { return steer ? "Steer this turn…" : "Queue for after this turn…" }
        let who = model.tag(ref)
        if ref.isRemote { return "Message \(ref.isCoordinator ? "the coordinator" : who) on \(ref.server)" }
        return "Message \(ref.thread.isEmpty ? who : ref.thread)"
    }

    private func take(_ providers: [NSItemProvider], into tray: ComposerTray) {
        // NSItemProvider loads from any thread.
        nonisolated(unsafe) let providers = providers
        Task.detached { await ComposerDrop.take(providers, into: tray) }
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
                if r.via == "queued" {
                    // Its bubble says so at the conversation's end; the
                    // card folds to the pill.
                    model.queued(ref, QueuedPrompt(text: t, files: refs.count))
                    if !r.why.isEmpty, let w = PromptSent.words(r) { said = (w, false) }
                    focused = false
                } else if let w = PromptSent.words(r) {
                    said = (w, false)
                }
            } catch {
                if let w = NeedActs.words(error) { said = (w, true) }
            }
            busy = false
        }
    }

    private func stop() {
        guard !busy else { return }
        busy = true
        said = nil
        sends += 1
        Task {
            do {
                try await model.stop(session)
            } catch {
                if let w = NeedActs.words(error) { said = (w, true) }
            }
            busy = false
        }
    }
}

/// The composer's flat buttons (T1001): 38 high, 6 round, growing with the
/// text, at least 44 to tap. A word: Send and its like on the tab's fill,
/// primary in ink while it can act, as an answer's primary button; quiet
/// while it can't. A symbol: + in a 38 square with a line.
struct ComposerButton: ButtonStyle {
    enum Kind {
        case word(primary: Bool)
        case symbol
    }

    let kind: Kind

    static func height(_ theme: Theme) -> CGFloat { min(56, 38 * theme.scale) }

    func makeBody(configuration: Configuration) -> some View {
        Face(kind: kind, configuration: configuration)
    }

    private struct Face: View {
        @Environment(\.theme) private var theme
        @Environment(\.isEnabled) private var enabled
        let kind: Kind
        let configuration: ButtonStyleConfiguration

        var body: some View {
            let side = ComposerButton.height(theme)
            let shape = RoundedRectangle(cornerRadius: Radius.row)
            Group {
                switch kind {
                case .word(let primary):
                    let lit = primary && enabled
                    configuration.label
                        .font(theme.sized(13.5))
                        .foregroundStyle(lit ? theme.pane : enabled ? theme.ink : theme.muted)
                        .lineLimit(1)
                        .fixedSize()
                        .padding(.horizontal, 12)
                        .frame(minWidth: 58 * min(theme.scale, 1.3), minHeight: side)
                        .background(lit ? theme.ink : theme.tab, in: shape)
                case .symbol:
                    configuration.label
                        .labelStyle(.iconOnly)
                        .font(.system(size: 14 * theme.scale, weight: .medium))
                        .foregroundStyle(enabled ? theme.ink : theme.muted)
                        .frame(width: side, height: side)
                        .background(theme.pane, in: shape)
                        .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1) }
                }
            }
            .opacity(configuration.isPressed ? 0.7 : 1)
            .padding(.vertical, 4)
            .contentShape(Rectangle())
            .padding(.vertical, -4)
        }
    }
}
