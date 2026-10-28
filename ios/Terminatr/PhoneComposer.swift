import SwiftUI
import TerminatrKit
import TerminatrUI

/// The prompt box under a conversation, at the write level (T821), as
/// the Mac's Composer: Send queues the prompt until the agent is idle;
/// while it works, Steer sends it into the running turn (session.prompt
/// "now"), not on a paired server's session, whose prompt is typed into
/// its pane there. Sending to a thread takes it over, as the Mac's
/// says. Text pastes as in any field; files stay the Mac's. The keys
/// button swaps it for the keys bar (TerminalKeys); -keys YES opens it
/// at launch (the Simulator's screenshots).
struct PhoneComposer: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let ref: SessionRef
    let session: String
    let running: Bool
    let queued: Int
    @State private var text = ""
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var keys = UserDefaults.standard.bool(forKey: "keys")
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if keys {
                TerminalKeys(session: session) { said = $0 }
            } else {
                field
            }
            HStack(spacing: 8) {
                if let said {
                    Text(said.text)
                        .foregroundStyle(said.failed ? theme.attention : theme.muted)
                        .lineLimit(2)
                } else if queued > 0 {
                    Text(queued == 1 ? "1 prompt queued" : "\(queued) prompts queued")
                        .foregroundStyle(theme.muted)
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
            .padding(.horizontal, 4)
        }
        .padding(.horizontal, 12)
        .padding(.top, 8)
        .padding(.bottom, 6)
        // To the bottom edge only: on iPhone Duo (T824) the bars sit on
        // a side, which this stops at.
        .background(.bar, ignoresSafeAreaEdges: .bottom)
    }

    private var field: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField(placeholder, text: $text, axis: .vertical)
                .lineLimit(1...6)
                .font(theme.body(.body))
                .focused($focused)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 18))
                .overlay(RoundedRectangle(cornerRadius: 18).strokeBorder(theme.edge, lineWidth: 1))
                .modifier(NoWritingTools())
            if running && !ref.isRemote {
                Button("Steer") { send(mode: "now") }
                    .buttonStyle(.bordered)
                    .disabled(empty || busy)
                    .accessibilityHint("Sends it into the running turn")
            }
            Button {
                send(mode: "")
            } label: {
                Image(systemName: "arrow.up")
                    .font(theme.body(.body, .bold))
                    .frame(width: 22, height: 22)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.circle)
            .tint(theme.action)
            .disabled(empty || busy)
            .accessibilityLabel("Send")
            .accessibilityHint(running && !ref.isRemote ? "Queued: it goes once the turn ends" : "")
        }
    }

    private var empty: Bool {
        text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private var placeholder: String {
        if ref.isRemote {
            return "Message \(ref.isCoordinator ? "the coordinator" : ref.thread) on \(ref.server)"
        }
        if ref.isCoordinator { return "Message the coordinator" }
        return "Message \(ref.thread) · takes it over"
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
