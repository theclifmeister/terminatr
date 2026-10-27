import AppKit
import SwiftUI
import TerminatrKit

/// A task's actions as buttons (T748 C1): Accept and Send back… for a
/// task in review, Delegate… for an open, ready or blocked one. Each
/// only asks the project's coordinator (task.accept, task.send-back,
/// task.delegate), as the TUI's A, X and D do; the server checks it
/// against the live board, and the card's TaskOutcome row says what
/// came of it, in place of the buttons while the ask stands (T769).
/// Delegate asks first; Send back takes the user's note.
struct TaskActs: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    let task: String
    let status: String
    var compact = false
    @State private var sendingBack = false
    @State private var delegating = false

    var body: some View {
        HStack(spacing: 6) {
            if app.asks[TaskAsk.key(project, task)]?.holdsButtons == true {
                EmptyView()
            } else if status == "review" {
                Button("Accept") { ask(.accept) }
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
                    .help("Asks the coordinator to accept \(task): it merges and closes it")
                    .accessibilityLabel("Accept \(task)")
                Button("Send back…") { sendingBack = true }
                    .help("Asks the coordinator to send \(task) back with your note")
                    .accessibilityLabel("Send \(task) back…")
            } else if Palette.delegable(status) {
                Button("Delegate…") { delegating = true }
                    .help("Asks the coordinator to start a thread for \(task)")
                    .accessibilityLabel("Delegate \(task)…")
            }
        }
        .controlSize(compact ? .small : .regular)
        .onChange(of: status, initial: true) { app.settle(project: project, task: task, status: status) }
        .sheet(isPresented: $sendingBack) {
            SendBackSheet(task: task) { note in ask(.sendBack(note: note)) }
        }
        .confirmationDialog("Delegate \(task)?", isPresented: $delegating) {
            Button("Ask to Delegate") { ask(.delegate) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("\(project)'s coordinator is asked to delegate it: it starts a thread for it when it handles the ask.")
        }
    }

    /// Whether a task of this status has buttons here.
    static func acts(_ status: String) -> Bool {
        status == "review" || Palette.delegable(status)
    }

    private func ask(_ kind: TaskAsk.Kind) {
        app.ask(kind, project: project, task: task, status: status)
    }
}

/// What came of the window's ask about a task (T769), as a row across
/// the card under its title: the ask running, made, waiting on the
/// coordinator already, or refused with Retry beside the buttons. It
/// fades in, without motion under Reduce Motion, and VoiceOver hears
/// it as one element (AppModel.ask announces it).
struct TaskOutcome: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let project: String
    let task: String

    // No wrapper around the row: an empty stack would still take the
    // card's spacing. AppModel.ask animates the change instead.
    var body: some View {
        if let ask = app.asks[TaskAsk.key(project, task)] {
            row(ask)
                .transition(reduceMotion ? .identity : .opacity.combined(with: .move(edge: .top)))
        }
    }

    private func row(_ ask: TaskAsk) -> some View {
        let colour = colour(ask.tone)
        return HStack(alignment: .center, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: ask.symbol)
                    .font(theme.body(.subheadline, .semibold))
                    .foregroundStyle(colour)
                Text(ask.text)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(ask.tone == .attention ? theme.attention : theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
            .accessibilityElement(children: .combine)
            Spacer(minLength: 0)
            if ask.tone == .attention {
                Button("Retry") { app.ask(ask.kind, project: project, task: task, status: ask.status) }
                    .controlSize(.small)
                    .help("Asks the coordinator again")
                    .accessibilityLabel(ask.retryLabel)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(colour.opacity(0.12), in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(colour.opacity(theme.contrast ? 1 : 0.3), lineWidth: 1))
    }

    private func colour(_ tone: TaskAsk.Tone) -> Color {
        switch tone {
        case .good: theme.good
        case .action: theme.action
        case .neutral: theme.muted
        case .attention: theme.attention
        }
    }
}

extension AppModel {
    /// Asks project's coordinator about task (task.accept,
    /// task.send-back, task.delegate) and keeps what came of it in
    /// asks, for the cards' TaskOutcome rows; VoiceOver hears it.
    func ask(_ kind: TaskAsk.Kind, project: String, task: String, status: String) {
        let key = TaskAsk.key(project, task)
        var ask = TaskAsk(task: task, kind: kind, status: status)
        show { asks[key] = ask }
        Task {
            do {
                let asked: Bool
                switch kind {
                case .accept: asked = try await accept(project: project, task: task)
                case let .sendBack(note): asked = try await sendBack(project: project, task: task, note: note)
                case .delegate: asked = try await delegate(project: project, task: task)
                }
                ask.phase = asked ? .asked : .waiting
            } catch {
                ask.phase = .failed(Dashboard.words(error, task: task))
            }
            // The board may have moved the task on meanwhile (settle).
            guard asks[key]?.phase == .asking else { return }
            show { asks[key] = ask }
            AccessibilityNotification.Announcement(ask.text).post()
        }
    }

    /// Drops task's ask once the board moved the task on from the
    /// status it was asked in: its buttons are the new status's.
    func settle(project: String, task: String, status: String) {
        let key = TaskAsk.key(project, task)
        if let a = asks[key], !a.stands(status) { asks[key] = nil }
    }

    /// An outcome row's change, faded in; at once under Reduce Motion.
    private func show(_ change: () -> Void) {
        withAnimation(NSWorkspace.shared.accessibilityDisplayShouldReduceMotion ? nil : .easeOut(duration: 0.2), change)
    }
}

/// Send back's note: what to change, which the coordinator passes on to
/// the thread. tm refuses an empty one.
struct SendBackSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    let task: String
    let send: (String) -> Void
    @State private var note = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Send \(task) back")
                .font(theme.title(.title2, .heavy))
            Text("Say what to change. The coordinator passes it on to the thread.")
                .foregroundStyle(theme.muted)
            TextEditor(text: $note)
                .font(theme.body(.body))
                .frame(minHeight: 90)
                .scrollContentBackground(.hidden)
                .padding(6)
                .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 8))
                .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.edge, lineWidth: 1))
                .focused($focused)
                .modifier(NoWritingTools())
                .accessibilityLabel("What to change")
            HStack {
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("Send Back") {
                    send(note.trimmingCharacters(in: .whitespacesAndNewlines))
                    dismiss()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(20)
        .frame(width: 440)
        .onAppear { focused = true }
    }
}
