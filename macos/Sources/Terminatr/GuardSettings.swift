import SwiftUI
import TerminatrKit
import TerminatrUI

/// Settings › Guard (T998): the calls the guard refused, the latest
/// first, as the TUI's Guard refusals lists them: each with its whole
/// command or file, to allow for its project or all projects (asked
/// first), or keep refusing, which stops listing it. The bridge reads and
/// answers these itself, for the user only. The log keeps a refusal until
/// it is answered, so this list is long-lived and never counts as a need.
struct GuardSettings: View {
    @Environment(AppModel.self) private var model
    @State private var list: [AppAPI.GuardRefusal]?
    @State private var failure: String?
    @State private var said: String?

    /// How often the list is read while it shows: the bridge reads the
    /// guard's log, a small file, itself.
    private static let every: Duration = .seconds(3)

    private var answers: Bool {
        if case .connected(let h) = model.phase { return Compatibility.answersRefusals(h) }
        return false
    }

    var body: some View {
        Group {
            if let rows = list, !rows.isEmpty, answers {
                // The log keeps up to 500: this one scrolls, in a set height.
                form(rows).frame(height: 520)
            } else {
                form([]).fixedSize(horizontal: false, vertical: true)
            }
        }
        .task(id: answers) {
            guard answers else { return }
            while !Task.isCancelled {
                await read()
                try? await Task.sleep(for: Self.every)
            }
        }
    }

    private func form(_ rows: [AppAPI.GuardRefusal]) -> some View {
        Form {
            Section {
                ForEach(rows, id: \.id) { r in
                    RefusalRow(refusal: r, what: r.what(sessions: model.sessions)) { how in
                        try await decide(r, how)
                    }
                }
                if rows.isEmpty {
                    Text(empty)
                        .foregroundStyle(.secondary)
                }
            } header: {
                Text("Guard refusals")
            } footer: {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Calls the guard refused, the latest first. Each stays here until you answer it.")
                        .foregroundStyle(.secondary)
                    if let said {
                        Text(said)
                            .foregroundStyle(.secondary)
                    }
                    if let failure {
                        Text(failure)
                            .foregroundStyle(.red)
                    }
                }
                .font(.caption)
            }
        }
        .formStyle(.grouped)
    }

    private var empty: String {
        if !model.isConnected { return "Not connected to terminatr." }
        if !answers { return "This tm can't answer the guard's refusals from the app: update it with tm update." }
        return list == nil ? "Reading…" : "None to review."
    }

    private func read() async {
        do {
            list = try await model.connected().call(AppAPI.Methods.guardRefusals).refusals
            failure = nil
        } catch {
            failure = Dashboard.words(error)
        }
    }

    /// Answers r (guard.decide): it leaves the list and tm's words stay
    /// under it. One answered elsewhere leaves quietly; another failure is
    /// the row's to say.
    private func decide(_ r: AppAPI.GuardRefusal, _ how: String) async throws {
        do {
            let d = try await model.connected().call(AppAPI.Methods.guardDecide, AppAPI.GuardDecideParams(id: r.id, how: how))
            said = d.message
        } catch let e as AppAPI.Error where e.code == "gone" {
            said = nil
        }
        list?.removeAll { $0.id == r.id }
        await read()
    }
}

/// One refused call: who and why, its whole command or file, and its
/// answers. Allowing it needs a choice the guard would honour; Keep
/// Refusing is always there, as the TUI's K is.
private struct RefusalRow: View {
    let refusal: AppAPI.GuardRefusal
    let what: String
    let decide: (String) async throws -> Void
    @State private var busy = false
    @State private var failure: String?
    @State private var confirmAll = false

    private var kind: String { refusal.path ? "file" : "command" }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(what)
                .font(.body.weight(.semibold))
            HStack(spacing: 0) {
                Text(refusal.detail)
                Text(" · ")
                Text(refusal.last, format: .relative(presentation: .named))
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            ScrollView(.vertical) {
                Text(refusal.subject)
                    .font(JetBrainsMono.swiftUI(size: 12))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(6)
            }
            .frame(maxHeight: 120)
            .fixedSize(horizontal: false, vertical: true)
            .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 6))
            .accessibilityLabel(refusal.path ? "File: \(refusal.subject)" : "Command: \(refusal.subject)")
            if let why = refusal.neverWhy {
                Text(why)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            HStack(spacing: 6) {
                Spacer()
                if refusal.actionable {
                    Button("Allow Here") { act("here") }
                        .accessibilityLabel("Allow in \(refusal.project): \(what)")
                        .help("Allows this exact \(kind) in \(refusal.project); the thread is told it may retry")
                    Button("All Projects…") { confirmAll = true }
                        .accessibilityLabel("Allow for all projects: \(what)")
                        .help("Allows this exact \(kind) in every project, after you confirm")
                }
                Button("Keep Refusing") { act("keep") }
                    .accessibilityLabel("Keep refusing: \(what)")
                    .help("The guard keeps refusing it, and it is no longer listed")
            }
            .controlSize(.small)
            .disabled(busy)
            if let failure {
                Text(failure)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
        .padding(.vertical, 2)
        .confirmationDialog("Allow this \(kind) for all projects?", isPresented: $confirmAll) {
            Button("Allow for All Projects") { act("all") }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The guard lets every project's agents run it as it is, from now on:\n\n\(refusal.subject)")
        }
    }

    private func act(_ how: String) {
        busy = true
        failure = nil
        Task {
            do {
                try await decide(how)
            } catch {
                failure = Dashboard.words(error)
            }
            busy = false
        }
    }
}
