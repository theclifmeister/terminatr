import SwiftUI
import TerminatrKit

/// A task's actions as buttons (T748 C1): Accept and Send back… for a
/// task in review, Delegate… for an open, ready or blocked one. Each
/// only asks the project's coordinator (task.accept, task.send-back,
/// task.delegate), as the TUI's A, X and D do; the server checks it
/// against the live board, and the line under the buttons says what
/// came of it. Delegate asks first; Send back takes the user's note.
struct TaskActs: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    let task: String
    let status: String
    var compact = false
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var sendingBack = false
    @State private var delegating = false

    var body: some View {
        VStack(alignment: .trailing, spacing: 4) {
            HStack(spacing: 6) {
                if status == "review" {
                    Button("Accept") { run { try await app.accept(project: project, task: task) } done: { "Asked the coordinator to accept \(task)." } }
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
            .disabled(busy)
            if let said {
                Text(said.text)
                    .font(theme.body(.footnote))
                    .foregroundStyle(said.failed ? theme.attention : theme.muted)
                    .lineLimit(2)
                    .multilineTextAlignment(.trailing)
            }
        }
        .sheet(isPresented: $sendingBack) {
            SendBackSheet(task: task) { note in
                run { try await app.sendBack(project: project, task: task, note: note) } done: { "Asked the coordinator to send \(task) back." }
            }
        }
        .confirmationDialog("Delegate \(task)?", isPresented: $delegating) {
            Button("Ask to Delegate") {
                run { try await app.delegate(project: project, task: task) } done: { "Asked the coordinator to delegate \(task)." }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("\(project)'s coordinator is asked to delegate it: it starts a thread for it when it handles the ask.")
        }
    }

    /// Whether a task of this status has buttons here.
    static func acts(_ status: String) -> Bool {
        status == "review" || Palette.delegable(status)
    }

    private func run(_ ask: @escaping () async throws -> Bool, done: @escaping () -> String) {
        busy = true
        said = nil
        Task {
            do {
                said = try await ask() ? (done(), false) : ("\(task) waits on the coordinator already.", false)
            } catch {
                said = (Dashboard.words(error, task: task), true)
            }
            busy = false
        }
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
