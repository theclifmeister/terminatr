import SwiftUI
import TerminatrKit
import TerminatrUI

/// What the program screen asks the user in a sheet: a program question
/// to answer, an umbrella to accept or send back, a new name, or the
/// program's name typed again to delete it. Each is the user's own act
/// (the programs design §7.2, §15 6): never one the director relays.
enum ProgramAsk: Identifiable, Equatable {
    case answer(ref: String, question: AppAPI.QuestionItem?, text: String)
    case umbrella(task: String, title: String, text: String)
    case rename
    case delete

    var id: String {
        switch self {
        case .answer(let ref, _, _): "answer:\(ref)"
        case .umbrella(let task, _, _): "umbrella:\(task)"
        case .rename: "rename"
        case .delete: "delete"
        }
    }
}

struct ProgramAskSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    let program: String
    let ask: ProgramAsk
    /// Called after an act went through, to read the screen again.
    let done: () -> Void
    @State private var picks: Set<String> = []
    @State private var other = ""
    @State private var said: String?
    @State private var failed: String?
    @State private var working = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title)
                .font(theme.title(.title3, .heavy))
            content
            if let failed {
                Text(failed)
                    .font(theme.body(.callout))
                    .foregroundStyle(theme.attention)
            }
            if let said {
                Text(said)
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
                    .textSelection(.enabled)
            }
            HStack {
                Spacer()
                Button(said == nil ? "Cancel" : "Done") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                if said == nil { actions }
            }
        }
        .padding(16)
        .frame(width: 440)
        .disabled(working)
    }

    private var title: String {
        switch ask {
        case .answer(let ref, _, _): "\(program) asks · \(ref)"
        case .umbrella(let task, let title, _): "\(task) \(title)"
        case .rename: "Rename \(program)"
        case .delete: "Delete \(program)"
        }
    }

    @ViewBuilder
    private var content: some View {
        switch ask {
        case .answer(_, let q, let text):
            Text(q?.question ?? text)
                .font(theme.body(.body))
                .fixedSize(horizontal: false, vertical: true)
            ForEach(q?.options ?? [], id: \.label) { o in
                Toggle(isOn: Binding(get: { picks.contains(o.label) }, set: { on in
                    if on {
                        if q?.multiSelect != true { picks = [] }
                        picks.insert(o.label)
                    } else {
                        picks.remove(o.label)
                    }
                })) {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(o.label).font(theme.body(.body, .medium))
                        if !o.description.isEmpty {
                            Text(o.description).font(theme.body(.caption)).foregroundStyle(theme.muted)
                        }
                    }
                }
                .toggleStyle(.checkbox)
            }
            TextField("Or your own words", text: $other, axis: .vertical)
                .lineLimit(1...4)
            Text("The answer goes to every member question it covers.")
                .font(theme.body(.caption))
                .foregroundStyle(theme.muted)
        case .umbrella(_, _, let text):
            Text(text)
                .font(theme.body(.body))
            TextField("A note (sending back needs one)", text: $other, axis: .vertical)
                .lineLimit(1...4)
        case .rename:
            Text("The director starts again under the new name, and the members' settings follow it.")
                .font(theme.body(.callout))
                .foregroundStyle(theme.muted)
            TextField("New name", text: $other)
        case .delete:
            Text("Its folder goes to the trash and its members stay as they are. Type \(program) to delete it.")
                .font(theme.body(.callout))
                .foregroundStyle(theme.muted)
            TextField(program, text: $other)
        }
    }

    @ViewBuilder
    private var actions: some View {
        switch ask {
        case .answer(let ref, _, _):
            Button("Answer") {
                run { "\(ProgramFacts.answered(try await app.programAnswer(program, ref: ref, choices: Array(picks), other: other)))" }
            }
            .keyboardShortcut(.defaultAction)
            .disabled(picks.isEmpty && other.trimmingCharacters(in: .whitespaces).isEmpty)
        case .umbrella(let task, _, _):
            Button("Send Back") {
                run { "Sent back: \(try await app.programUmbrella(program, task: task, accept: false, note: other).status)" }
            }
            .disabled(other.trimmingCharacters(in: .whitespaces).isEmpty)
            Button("Accept") {
                run { "Accepted: \(try await app.programUmbrella(program, task: task, accept: true, note: other).status)" }
            }
            .keyboardShortcut(.defaultAction)
        case .rename:
            Button("Rename") {
                let to = other.trimmingCharacters(in: .whitespaces)
                run { (try await app.programRename(program, to: to)).notes.joined(separator: "\n") }
            }
            .keyboardShortcut(.defaultAction)
            .disabled(other.trimmingCharacters(in: .whitespaces).isEmpty)
        case .delete:
            Button("Delete", role: .destructive) {
                run { (try await app.programDelete(program, confirm: other)).trashed ? "" : "It had only its settings." }
            }
            .disabled(other != program)
        }
    }

    /// Runs the act; what it says, if anything, stays shown, else the
    /// sheet closes.
    private func run(_ act: @escaping () async throws -> String) {
        working = true
        failed = nil
        Task {
            defer { working = false }
            do {
                let s = try await act()
                done()
                if s.isEmpty { dismiss() } else { said = s }
            } catch {
                failed = error.localizedDescription
            }
        }
    }
}
