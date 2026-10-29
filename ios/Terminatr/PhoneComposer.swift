import SwiftUI
import TerminatrKit
import TerminatrUI

/// The prompt box under a conversation, at the write level (T821), as
/// the Mac's Composer looks and words it (T840): a card with the text,
/// Send (queued until the agent is idle) and, while it works, Interrupt
/// (stops the turn for it, after a confirmation) and Steer (into the
/// running turn, session.prompt "now"); not on a paired server's
/// session, whose prompt is typed into its pane there. Below it, what
/// the last send did, the queue and a held queue, and the Keys switch,
/// where the Mac has its key hints. Sending to a thread takes it over,
/// as the Mac's says. Text pastes as in any field; files stay the Mac's.
/// The keys button swaps the card for the keys bar (TerminalKeys); -keys
/// YES opens it at launch (the Simulator's screenshots). With a hardware
/// keyboard (an iPad's, T830) Command-Return sends and
/// Shift-Command-Return steers, as Return makes a new line.
struct PhoneComposer: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let ref: SessionRef
    let session: String
    let running: Bool
    let queued: Int
    /// Why the queue is held, when it is.
    var held = ""
    @State private var text = ""
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var keys = UserDefaults.standard.bool(forKey: "keys")
    @State private var interrupting = false
    /// The card's width, which decides where its buttons go.
    @State private var width: CGFloat = 0
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if keys {
                TerminalKeys(session: session) { said = $0 }
            } else {
                card
            }
            HStack(spacing: 10) {
                if let said {
                    Text(said.text)
                        .foregroundStyle(said.failed ? theme.attention : theme.muted)
                        .lineLimit(2)
                } else if queued > 0 {
                    Text(queued == 1 ? "1 prompt queued" : "\(queued) prompts queued")
                        .foregroundStyle(theme.muted)
                }
                if !held.isEmpty {
                    Text("Queue held: \(held)")
                        .foregroundStyle(theme.attention)
                        .lineLimit(2)
                }
                Spacer(minLength: 0)
                if model.can(AppAPI.Methods.sessionKeys.name) {
                    Button {
                        keys.toggle()
                        said = nil
                    } label: {
                        Label(keys ? "Message" : "Keys", systemImage: keys ? "text.bubble" : "keyboard")
                    }
                    .buttonStyle(.borderless)
                    .accessibilityHint(keys ? "Shows the prompt box" : "Shows keys for its terminal: Esc, Tab, arrows, Control C, Return")
                }
            }
            .font(theme.body(.footnote))
            .padding(.horizontal, 6)
        }
        .padding(.horizontal, 12)
        .padding(.top, 8)
        .padding(.bottom, 6)
        // To the bottom edge only: on iPhone Duo (T824) the bars sit on
        // a side, which this stops at.
        .background(.bar, ignoresSafeAreaEdges: .bottom)
        .kept($text, "prompt \(ref)")
        .kept($keys, "keys \(ref)")
        .confirmationDialog("Interrupt the agent's turn?", isPresented: $interrupting, titleVisibility: .visible) {
            Button("Interrupt and Send", role: .destructive) { send(mode: "interrupt") }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The running turn stops and this prompt goes in its place. tm refuses while a draft or a dialog is on the agent's screen.")
        }
    }

    /// The Mac's card: the text and its buttons on one line, but for a
    /// working agent's three in a card narrower than oneLine (an
    /// iPhone, an iPad's column), on a line below it, the placeholder
    /// whole.
    private var card: some View {
        Group {
            if running && !ref.isRemote && width < Self.oneLine {
                VStack(alignment: .trailing, spacing: 8) {
                    field
                    HStack(spacing: 8) { buttons }
                }
            } else {
                HStack(alignment: .bottom, spacing: 8) {
                    field
                    buttons
                }
            }
        }
        .font(theme.body(.subheadline, .semibold))
        .padding(10)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .frosted(RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
    }

    /// The narrowest card that holds a thread's placeholder beside
    /// Interrupt, Steer and Send.
    static let oneLine: CGFloat = 600

    private var field: some View {
        TextField(placeholder, text: $text, axis: .vertical)
            .lineLimit(1...6)
            .font(theme.body(.body))
            .focused($focused)
            .padding(.vertical, 6)
            .padding(.leading, 4)
            .modifier(NoWritingTools())
    }

    @ViewBuilder
    private var buttons: some View {
        if running && !ref.isRemote {
            Button("Interrupt") { interrupting = true }
                .buttonStyle(.bordered)
                .disabled(empty || busy)
                .accessibilityHint("Stops the agent's turn for this prompt")
            Button("Steer") { send(mode: "now") }
                .buttonStyle(.bordered)
                .keyboardShortcut(.return, modifiers: [.command, .shift])
                .disabled(empty || busy)
                .accessibilityHint("Sends it into the running turn")
        }
        Button("Send") { send(mode: "") }
            .buttonStyle(.borderedProminent)
            .tint(theme.action)
            .keyboardShortcut(.return, modifiers: .command)
            .disabled(empty || busy)
            .accessibilityHint(running && !ref.isRemote ? "Queued: it goes once the turn ends" : "")
    }

    private var empty: Bool {
        text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private var placeholder: String {
        if ref.isRemote {
            return "Message \(ref.isCoordinator ? "the coordinator" : ref.thread) on \(ref.server)"
        }
        if ref.isCoordinator { return "Message the coordinator" }
        return "Message \(ref.thread) · sending takes the thread over"
    }

    private func send(mode: String) {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty, !busy else { return }
        busy = true
        said = nil
        Task {
            do {
                let r = try await model.prompt(session, text: t, mode: mode)
                text = ""
                said = (PromptSent.words(r), false)
            } catch {
                if let w = NeedActs.words(error) { said = (w, true) }
            }
            busy = false
        }
    }
}

/// The keys a terminal wants that the phone's keyboard hasn't (T821),
/// each written to the session's terminal as typed (session.keys), and
/// a line of text typed there, without a return unless asked for.
struct TerminalKeys: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let session: String
    let said: ((text: String, failed: Bool)?) -> Void
    @State private var typed = ""
    @State private var busy = false

    var body: some View {
        VStack(spacing: 8) {
            HStack(spacing: 4) {
                ForEach(TerminalKey.allCases) { k in
                    Button {
                        write(k.data)
                    } label: {
                        Group {
                            if let cap = k.cap {
                                Text(cap).font(theme.mono(.footnote, .semibold))
                            } else {
                                Image(systemName: k.symbol).font(theme.body(.footnote, .semibold))
                            }
                        }
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                        .frame(maxWidth: .infinity, minHeight: 28)
                    }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.roundedRectangle(radius: 8))
                    .tint(k == .ctrlC ? theme.attention : nil)
                    .accessibilityLabel(k.spoken)
                }
            }
            .controlSize(.small)
            HStack(spacing: 8) {
                TextField("Type into its terminal", text: $typed)
                    .font(theme.mono(.body))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.send)
                    .onSubmit { type(enter: true) }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 18))
                    .overlay(RoundedRectangle(cornerRadius: 18).strokeBorder(theme.edge, lineWidth: 1))
                Button("Type") { type(enter: false) }
                    .buttonStyle(.bordered)
                    .disabled(typed.isEmpty)
                    .accessibilityHint("Types it without Return; Return on the keyboard types it with one")
            }
        }
        .disabled(busy)
        .kept($typed, "typed \(session)")
    }

    private func type(enter: Bool) {
        guard !typed.isEmpty || enter else { return }
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
