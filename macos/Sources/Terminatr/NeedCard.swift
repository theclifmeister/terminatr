import SwiftUI
import TerminatrKit

/// A need on the dashboard that acts in place (T748 milestone C): an
/// approval's dialog as the server reads it (Allow, the last option,
/// Open), a question tm read with its options, the coordinator's open
/// questions (Answer now), a task in review (Accept, Send back…) or
/// blocked (Delegate…). What the card can't answer safely (an escape's
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

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center, spacing: 8) {
                Image(systemName: NeedRow.symbol(need.kind))
                    .foregroundStyle(NeedRow.colour(need.kind, theme))
                    .frame(width: 16)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 1) {
                    Text(need.what)
                        .font(theme.body(.body, .semibold))
                        .lineLimit(1)
                    if !detail.isEmpty {
                        Text(detail)
                            .font(theme.body(.subheadline))
                            .foregroundStyle(theme.muted)
                            .lineLimit(2)
                    }
                }
                Spacer(minLength: 8)
                acts
            }
            if let said {
                Text(said.text)
                    .font(theme.body(.footnote))
                    .foregroundStyle(said.failed ? theme.attention : theme.muted)
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
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(NeedRow.colour(need.kind, theme).opacity(theme.contrast ? 1 : 0.35), lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(need.title)
        .task(id: need.id) { await check() }
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
            case .checks, .held, .network:
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
