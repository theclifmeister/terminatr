import SwiftUI
import TerminatrKit
import TerminatrUI

/// The composer under a conversation, at the write level (T821, T840;
/// the Claude app's look, T893), in the conversation's column (T882).
/// Folded, it is one pill: + on its left (files, on this server's
/// sessions), the prompt's placeholder, and the round send button on
/// its right. With the field focused, the keyboard up, a file in or a
/// draft, it opens into one card: the files' thumbnails at its top
/// (ComposerThumbs), the text under them, and a row inside it at its
/// foot: +, the "how it goes" chip while the agent works (Queue, after
/// the turn, or Steer, into it, session.prompt "now"; and Interrupt and
/// Send), Paste for a file the keyboard's suggestion bar doesn't offer
/// (PasteFiles), and send. Send is filled with the action colour when there is
/// something to send, dimmed otherwise. While the agent works the
/// placeholder says the prompt waits for the turn, and with nothing
/// typed the send button is Stop (a filled square): the turn stops,
/// nothing sent (PhoneModel.stop). Send held down has Interrupt and
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
        // The conversation's column: its cards' edges (T882).
        .padding(.horizontal, Spacing.gutter)
        .padding(.bottom, Spacing.item)
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

    /// The pill, or open, the card. The field keeps its place in the
    /// view tree either way, so it keeps the keyboard as the card opens.
    private func card(_ tray: ComposerTray?) -> some View {
        let shape = RoundedRectangle(cornerRadius: Radius.card, style: .continuous)
        return VStack(alignment: .leading, spacing: 0) {
            if open, let tray, !tray.files.isEmpty {
                ComposerThumbs(tray: tray)
                    .padding(.horizontal, Spacing.item)
                    .padding(.top, Spacing.item)
            }
            HStack(alignment: .bottom, spacing: Spacing.hair) {
                if !open, let tray { AttachMenu(tray: tray, pick: $pick) }
                ComposerField(text: $text, focused: $focused, placeholder: placeholder,
                              files: tray.map { tray in { take($0, into: tray) } })
                    .frame(minHeight: tapTarget)
                    .padding(.leading, open || tray == nil ? Spacing.card - Spacing.hair : 0)
                    .padding(.trailing, open ? Spacing.card - Spacing.hair : 0)
                if !open { sendButton }
            }
            if open { bottomRow(tray) }
        }
        .padding(Spacing.hair)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: shape)
        .overlay {
            if dropping {
                shape.strokeBorder(theme.action, lineWidth: 2)
                    .allowsHitTesting(false)
            }
        }
        .contentShape(shape)
        // A tap on the pill's free space starts typing.
        .onTapGesture { if !focused { focused = true } }
    }

    /// The card's foot: + and the chip on the left, Paste and send on
    /// the right; at accessibility sizes the chip takes a line of its own.
    @ViewBuilder private func bottomRow(_ tray: ComposerTray?) -> some View {
        let paste = focused && pasteable && tray.map { !$0.full } == true
        let chipLine = typeSize.isAccessibilitySize && working
        VStack(alignment: .leading, spacing: Spacing.hair) {
            if chipLine { chip.padding(.leading, Spacing.hair) }
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
                    .font(theme.body(.subheadline, .semibold))
                    .foregroundStyle(theme.ink)
                Image(systemName: "chevron.up.chevron.down")
                    .font(theme.body(.caption2, .semibold))
                    .foregroundStyle(theme.muted)
            }
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, Spacing.row + 2)
            .frame(minHeight: ComposerCircle.side(theme))
            .background(theme.ink.opacity(ThemeColours.inkWash), in: Capsule())
            .frame(minHeight: tapTarget)
            .contentShape(Capsule())
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
        if focused, !ref.isCoordinator, !ref.isRemote { return ("Sending takes the thread over", false) }
        return nil
    }

    /// Stop instead of send: the agent works and nothing is typed.
    private var stops: Bool { working && empty && !(attaches && model.tray(ref).uploading) }

    private var sendButton: some View {
        Group {
            if stops {
                Button(action: stop) {
                    Label("Stop", systemImage: "stop.fill")
                }
                .buttonStyle(ComposerCircle(fill: theme.ink, on: theme.base))
                .disabled(busy)
                .accessibilityHint("Stops the agent's turn; nothing is sent")
            } else {
                Button {
                    send(mode: working && steer ? "now" : "")
                } label: {
                    Label(working && steer ? "Steer" : working ? "Queue" : "Send", systemImage: "arrow.up")
                }
                .buttonStyle(ComposerCircle(fill: theme.action, on: theme.onAction))
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
        return "Message \(who)"
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
                    if !r.why.isEmpty { said = (PromptSent.words(r), false) }
                    focused = false
                } else {
                    said = (PromptSent.words(r), false)
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
                said = ("Stopped the turn.", false)
            } catch {
                if let w = NeedActs.words(error) { said = (w, true) }
            }
            busy = false
        }
    }
}

/// A round button inside the composer (T893): + on an ink wash, send
/// filled, dimmed while it can't. Its circle is 36 pt (growing with
/// the text, to 56), its tap area at least 44.
struct ComposerCircle: ButtonStyle {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    /// The fill and the symbol's colour; nil fill: the ink wash.
    var fill: Color?
    var on: Color?

    static func side(_ theme: Theme) -> CGFloat { min(56, max(36, 36 * theme.scale)) }

    func makeBody(configuration: Configuration) -> some View {
        let side = Self.side(theme)
        configuration.label
            .labelStyle(.iconOnly)
            .font(.system(size: side * 0.45, weight: fill == nil ? .medium : .bold))
            .foregroundStyle(enabled ? (on ?? theme.ink) : theme.muted)
            .frame(width: side, height: side)
            .background {
                Circle().fill(fill.map { enabled ? $0 : theme.ink.opacity(ThemeColours.inkWash * 1.5) }
                    ?? theme.ink.opacity(ThemeColours.inkWash))
            }
            .opacity(configuration.isPressed ? 0.7 : 1)
            .frame(minWidth: tapTarget, minHeight: tapTarget)
            .contentShape(Rectangle())
    }
}
