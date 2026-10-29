import SwiftUI
import TerminatrKit
import TerminatrUI

/// The composer dock under a conversation, at the write level (T821,
/// T840; the dock T848): it floats over the conversation's end, with no
/// bar under it. In it, a circle that switches it to the terminal's
/// keys, the prompt in a glass field, and a round Send, queued until
/// the agent is idle; while the agent works the field has Queue and
/// Steer (into the running turn, session.prompt "now"), and Send held
/// down has Interrupt (stops the turn for it, after a confirmation);
/// not on a paired server's session, whose prompt is typed into its
/// pane there. Over it, a line: what the last send did, the queue and a
/// held queue, and while the field has focus that sending takes the
/// thread over, as the Mac's says; while the keyboard is up, at its
/// end, a circle that hides it (HideKeyboard, T857). The circles, the
/// field at one line and Send are one height (Theme.circle). Text
/// pastes as in any field; files stay the Mac's. In keys mode (-keys YES at launch, the Simulator's
/// screenshots) the key strip stays over the dock, keyboard up or not
/// (KeyStrip), and the field types into the terminal. -compose YES
/// focuses the field (the prompt or the keys') as it shows (the Simulator's screenshots). With a hardware
/// keyboard (an iPad's, T830) Command-Return sends as picked and
/// Shift-Command-Return steers, as Return makes a new line; no key
/// without a modifier does anything here.
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
    @State private var keys = UserDefaults.standard.bool(forKey: "keys")
    @State private var steer = false
    @State private var interrupting = false
    @State private var sends = 0
    @State private var keyboard = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.item) {
            if note != nil || keyboard {
                HStack(spacing: Spacing.item) {
                    if let note {
                        DockNote(note.text, failed: note.failed)
                            .transition(.opacity)
                    }
                    Spacer(minLength: 0)
                    if keyboard { HideKeyboard() }
                }
            }
            if keys {
                TerminalKeys(session: session, mode: modeButton) { said = $0 }
            } else {
                DockRow { modeButton } field: { field } send: { sendButton }
            }
        }
        .padding(.horizontal, 12)
        .padding(.top, Spacing.tight)
        .padding(.bottom, Spacing.item)
        .feedback(.impact(weight: .light), trigger: sends)
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in keyboard = true }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in keyboard = false }
        .onAppear { if UserDefaults.standard.bool(forKey: "compose"), !keys { focused = true } }
        .onChange(of: [queued, running ? 1 : 0]) { old, new in
            if queuedNote, PromptSent.wentIn(queued: old[0], new[0], working: old[1] == 1, new[1] == 1) {
                queuedNote = false
                said = nil
            }
        }
        .kept($text, "prompt \(ref)")
        .kept($keys, "keys \(ref)")
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

    /// The line over the dock, if any.
    private var note: (text: String, failed: Bool)? {
        if let said { return said }
        var parts: [String] = []
        if queued > 0 { parts.append(queued == 1 ? "1 prompt queued" : "\(queued) prompts queued") }
        if !held.isEmpty { return ((parts + ["Queue held: \(held)"]).joined(separator: " · "), true) }
        if focused, !keys, !ref.isCoordinator, !ref.isRemote { parts.append("Sending takes the thread over") }
        return parts.isEmpty ? nil : (parts.joined(separator: " · "), false)
    }

    /// The circle that switches the dock between the prompt and the keys.
    @ViewBuilder private var modeButton: some View {
        if model.can(AppAPI.Methods.sessionKeys.name) {
            Button {
                keys.toggle()
                said = nil
            } label: {
                Label(keys ? "Message" : "Keys", systemImage: keys ? "text.bubble" : "keyboard")
            }
            .buttonStyle(.circle)
            .accessibilityHint(keys ? "Shows the prompt box" : "Shows keys for its terminal: Esc, Tab, arrows, Control C, Return")
        }
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

    private var empty: Bool {
        text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
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
        guard !t.isEmpty, !busy else { return }
        busy = true
        said = nil
        sends += 1
        Task {
            do {
                let r = try await model.prompt(session, text: t, mode: mode)
                text = ""
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

/// The keys a terminal wants that the phone's keyboard hasn't (T821),
/// each written to the session's terminal as typed (session.keys), and
/// a line of text typed there, without a return unless asked for: the
/// key strip over the dock's row (T848), the mode circle, the field
/// and a round Type.
struct TerminalKeys<Mode: View>: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let session: String
    let mode: Mode
    let said: ((text: String, failed: Bool)?) -> Void
    @State private var typed = ""
    @State private var busy = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: Spacing.item) {
            KeyStrip { write($0.data) }
                .disabled(busy)
            DockRow { mode } field: {
                TextField("Type into its terminal", text: $typed)
                    .font(theme.mono(.body))
                    .foregroundStyle(theme.ink)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.send)
                    .onSubmit { type(enter: true) }
                    .focused($focused)
                    .padding(.vertical, Spacing.item)
                    .dockField()
            } send: {
                Button { type(enter: false) } label: {
                    Label("Type", systemImage: "arrow.up")
                }
                .buttonStyle(.send)
                .disabled(typed.isEmpty || busy)
                .accessibilityHint("Types it without Return; Return on the keyboard types it with one")
            }
        }
        // Not the field: disabled, it would give up the keyboard.
        .kept($typed, "typed \(session)")
        .onAppear { if UserDefaults.standard.bool(forKey: "compose") { focused = true } }
    }

    private func type(enter: Bool) {
        guard !typed.isEmpty || enter, !busy else { return }
        let t = typed + (enter ? TerminalKey.enter.data : "")
        typed = ""
        write(t)
    }

    private func write(_ data: String) {
        busy = true
        said(nil)
        Task {
            do {
                try await model.keys(session, data)
            } catch {
                if let w = NeedActs.words(error) { said((w, true)) }
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

/// Folds the keyboard away, the system's or the key strip's field's
/// (T857): a glass circle over the dock while the keyboard is up. The
/// draft stays.
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
