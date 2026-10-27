import SwiftUI
import TerminatrKit
import TerminatrUI

/// The prompt box under the conversation (design T734 §5): ↩ sends,
/// queued until the agent is idle; ⌥↩ steers it into the running turn;
/// Interrupt stops the turn for it, after a confirmation. ⇧↩ is a new
/// line; ⌘L focuses it. A thread's composer says that sending takes the thread over:
/// the server raises the takeover, as typing in its pane does. On a
/// paired server's session (at write) a prompt is typed into its pane
/// there, now: no steering, no queue.
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
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .bottom, spacing: 8) {
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
            .padding(10)
            .frosted(RoundedRectangle(cornerRadius: 14))
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
            .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
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
        .onAppear { if autofocus { focused = true } }
        .confirmationDialog("Interrupt the agent's turn?", isPresented: $interrupting) {
            Button("Interrupt and Send", role: .destructive) { send(mode: "interrupt") }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The running turn stops and this prompt goes in its place. tm refuses while a draft or a dialog is on the agent's screen.")
        }
    }

    private var empty: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    private var running: Bool { session?.state == "working" }

    private var placeholder: String {
        if model.ref.isRemote {
            let who = model.ref.isCoordinator ? "the coordinator" : model.ref.thread
            return "Message \(who) on \(model.ref.server) · typed into its pane"
        }
        if model.ref.isCoordinator { return "Message the coordinator" }
        let who = model.ref.thread
        return "Message \(who) · sending takes the thread over"
    }

    private func send(mode: String) {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty, !busy else { return }
        busy = true
        Task {
            do {
                let r = try await model.prompt(t, mode: mode)
                text = ""
                failed = false
                said = Self.words(r)
            } catch {
                failed = true
                said = error.localizedDescription
            }
            busy = false
        }
    }

    /// How the prompt went in, in the window's words.
    static func words(_ r: AppAPI.PromptResult) -> String {
        var s: String
        switch r.via {
        case "queued": s = r.running ? "Queued: it goes once the turn ends." : "Queued: it goes once the agent is idle."
        case "steered": s = "Steered into the running turn."
        case "interrupted": s = "The turn stopped for it."
        case "channel": s = "Sent."
        case "typed": s = "Typed into its pane."
        default: s = r.via.isEmpty ? "Sent." : "Sent (\(r.via))."
        }
        if !r.why.isEmpty { s += " " + r.why }
        return s
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
