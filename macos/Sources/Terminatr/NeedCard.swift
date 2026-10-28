import SwiftUI
import TerminatrKit
import TerminatrUI

/// A need on the dashboard that acts in place (T748 milestone C): an
/// approval's dialog as the server reads it (Allow, the last option,
/// Open), a question tm read with its options, the coordinator's open
/// questions (Answer now), a task in review (Accept, Send back…) or
/// blocked (Delegate…), a link's held message, offered file or link
/// request, or a share request (LinkActs), a call the guard refused with
/// its whole command (Allow here, All projects… after a confirmation, Keep
/// refusing; the floor's only kept). What the card can't answer safely (an escape's
/// dialog or question, which needs the command named, a question tm
/// couldn't read, several options) opens the session instead. The
/// server checks every answer, as it checks the session's own cards.
struct NeedCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let need: Need
    @State private var dialog: AppAPI.ApproveResult?
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var confirmAll = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center, spacing: 8) {
                if showsTask {
                    // A button, so Full Keyboard Access reaches it.
                    Button { app.showTask(project: need.project, task: need.task) } label: {
                        NeedSummary(need: need, detail: detail).contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help("Shows \(need.task) in full: how to check it, its PR, steps and notes")
                    .accessibilityHint("Shows the task in full")
                } else {
                    NeedSummary(need: need, detail: detail)
                }
                Spacer(minLength: 8)
                acts
            }
            if let r = need.refusal {
                RefusedCall(refusal: r)
                    .padding(.leading, 24)
            }
            if let said {
                Text(said.text)
                    .font(theme.body(.footnote))
                    .foregroundStyle(said.failed ? theme.attention : theme.muted)
                    .padding(.leading, 24)
            }
            if let o = app.linkOutcomes[need.id] {
                LinkOutcomeRow(outcome: o)
                    .padding(.leading, 24)
            }
            if !need.task.isEmpty, need.kind == .review || need.kind == .blocked {
                TaskOutcome(project: need.project, task: need.task)
                    .padding(.leading, 24)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frosted(RoundedRectangle(cornerRadius: 10))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.colour(need.kind).opacity(theme.contrast ? 1 : 0.35), lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(need.title)
        .task(id: need.id) { await check() }
        .confirmationDialog("Allow this \(need.refusal?.path == true ? "file" : "command") for all projects?", isPresented: $confirmAll) {
            if let r = need.refusal {
                Button("Allow for All Projects") { decide(r, "all") }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            if let r = need.refusal {
                Text("The guard lets every project's agents run it as it is, from now on:\n\n\(r.subject)")
            }
        }
    }

    /// Whether the card's words show its task in the task view (T780):
    /// a task in review's or a blocked one's.
    private var showsTask: Bool {
        !need.task.isEmpty && (need.kind == .review || need.kind == .blocked)
    }

    /// The second line: the task's title, and an approval's question
    /// once the server read it.
    private var detail: String {
        guard need.kind == .approval, let q = dialog?.question, !q.isEmpty else { return need.detail }
        return [need.detail, q].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    @ViewBuilder
    private var acts: some View {
        HStack(spacing: 6) {
            switch need.kind {
            case .approval:
                // Options that allow once only: one for the session needs
                // the confirmation the session's own card asks.
                if let dialog, !dialog.escape, let first = dialog.options.first, once(dialog, 1) {
                    Button(first) { approve(1) }
                        .accessibilityLabel(named(first))
                        .buttonStyle(.borderedProminent)
                        .tint(theme.action)
                    if dialog.options.count > 1, let last = dialog.options.last, once(dialog, dialog.options.count) {
                        Button(last) { approve(dialog.options.count) }
                            .accessibilityLabel(named(last))
                    }
                }
                open("Open")
            case .question where need.id.hasPrefix("questions:"):
                Button("Answer now") { answerQuestions() }
                    .accessibilityLabel(named("Answer now"))
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
                    .help("The coordinator puts its questions to you in its conversation")
            case .question:
                if let a = need.answer {
                    ForEach(a.options, id: \.self) { o in
                        Button(o) { answer(a, o) }
                            .accessibilityLabel(named(o))
                    }
                    open("Open")
                } else if need.detail.hasSuffix("answer in Terminal") {
                    Button("Open Terminal") { openTerminal() }
                        .accessibilityLabel(named("Open Terminal"))
                } else {
                    open("Answer…")
                }
            case .review, .blocked:
                if !need.task.isEmpty {
                    TaskActs(project: need.project, task: need.task, status: need.kind == .review ? "review" : "blocked", compact: true)
                }
                if !need.session.isEmpty { open("Open") }
            case .refusal:
                if let r = need.refusal {
                    if !r.floor {
                        Button("Allow Here") { decide(r, "here") }
                            .accessibilityLabel(named("Allow in \(r.project)"))
                            .help("Allows this exact \(r.path ? "file" : "command") in \(r.project); the thread is told it may retry")
                        Button("All Projects…") { confirmAll = true }
                            .accessibilityLabel(named("Allow for all projects"))
                            .help("Allows this exact \(r.path ? "file" : "command") in every project, after you confirm")
                    }
                    Button("Keep Refusing") { decide(r, "keep") }
                        .accessibilityLabel(named("Keep refusing"))
                        .help("The guard keeps refusing it, and it is no longer listed")
                }
            case .link, .network:
                if let l = need.link {
                    LinkActs(need: need, act: l)
                } else {
                    open("Open")
                }
            case .checks, .held:
                open("Open")
            }
        }
        .controlSize(.small)
        .disabled(busy)
    }

    private func open(_ title: String) -> some View {
        Button(title) { app.open(project: need.project, session: need.session) }
            .help("Shows it in the window")
            .accessibilityLabel(named(title))
    }

    /// A button's name for VoiceOver: its title and the need it acts on
    /// ("Yes: T1 needs approval"), since a card's buttons read alike.
    private func named(_ title: String) -> String {
        "\(title): \(need.what)"
    }

    private func openTerminal() {
        app.open(project: need.project, session: need.session)
        if let ref = app.selectedRef { app.model(ref).pick(.terminal) }
    }

    private func once(_ d: AppAPI.ApproveResult, _ choice: Int) -> Bool {
        choice > d.scopes.count || d.scopes[choice - 1] == "once"
    }

    /// Asks the server what the dialog on screen offers (check).
    private func check() async {
        guard need.kind == .approval, !need.session.isEmpty else { return }
        dialog = try? await app.approve(AppAPI.ApproveParams(id: need.session, choice: 1, check: true))
    }

    private func approve(_ choice: Int) {
        act {
            _ = try await app.approve(AppAPI.ApproveParams(id: need.session, choice: choice))
            return nil
        }
    }

    private func answer(_ a: Need.Answer, _ option: String) {
        act {
            _ = try await app.answer(a.params(option))
            return nil
        }
    }

    /// Answers refusal r of the guard (guard.decide): the need leaves
    /// once tm wrote it.
    private func decide(_ r: AppAPI.GuardRefusal, _ how: String) {
        act {
            try await app.decide(refusal: r.id, how: how)
        }
    }

    private func answerQuestions() {
        act {
            try await app.openQuestions(need.project)
            app.select(.session(SessionRef(project: need.project)))
            return nil
        }
    }

    private func act(_ f: @escaping () async throws -> String?) {
        busy = true
        said = nil
        Task {
            do {
                if let s = try await f() { said = (s, false) }
            } catch let e as AppAPI.Error where e.code == "always-allow" {
                said = ("That option allows more than once: only you pick it, in the Terminal tab.", true)
            } catch {
                said = (Dashboard.words(error), true)
            }
            busy = false
        }
    }
}

/// A refused call's whole command (or file), as the guard's log keeps it
/// and an allowance names it, selectable; and for the floor's, why the
/// guard never allows it.
struct RefusedCall: View {
    @Environment(\.theme) private var theme
    let refusal: AppAPI.GuardRefusal

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ScrollView(.vertical) {
                Text(refusal.subject)
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(6)
            }
            .frame(maxHeight: 120)
            .fixedSize(horizontal: false, vertical: true)
            .background(theme.muted.opacity(0.08), in: RoundedRectangle(cornerRadius: 6))
            .accessibilityLabel(refusal.path ? "File: \(refusal.subject)" : "Command: \(refusal.subject)")
            if refusal.floor {
                Text("Never allowed: \(Need.floorWhy(refusal.rule))")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
            }
        }
    }
}
