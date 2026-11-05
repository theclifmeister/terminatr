import SwiftUI
import TerminatrKit
import TerminatrUI

/// A need that acts in place (T748 milestone C), where answering it lives:
/// stacked in the inspector's Needs you section (panel), its answers
/// wrapping at the panel's width, or as a row of a hairline list over a
/// coordinator's conversation. It answers: an
/// approval's dialog as the server reads it (Allow, the last option,
/// Open), a question tm read with its options, the coordinator's open
/// questions (Answer now), a task in review (Accept, Send back…) or
/// blocked (Delegate…), a link's held message, offered file or link
/// request, or a share request (LinkActs). What the card can't answer
/// safely (an escape's dialog or question, which needs the command named,
/// a question tm couldn't read, several options) opens the session
/// instead. The server checks every answer, as it checks the session's
/// own cards.
struct NeedCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let need: Need
    /// The inspector's: stacked for its width, not one row.
    var panel = false
    @State private var dialog: AppAPI.ApproveResult?
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var sendingBack: Need.PeerAct.Choice?
    @State private var note = ""
    /// A paired server's multi-select's options ticked, by index (T904).
    @State private var ticked: Set<Int> = []

    var body: some View {
        Group {
            if panel {
                stacked
            } else {
                ListRow { row }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(need.title)
        .task(id: need.id) { await check() }
        .alert("Send \(need.task) back", isPresented: Binding(get: { sendingBack != nil }, set: { if !$0 { sendingBack = nil } })) {
            TextField("What's missing", text: $note)
            Button("Send Back") {
                if let c = sendingBack { send(c.params(note: note)) }
            }
            .disabled(note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("\(need.peer?.server ?? "Its") coordinator hands your note to the thread.")
        }
    }

    /// In the panel: what it names, a click showing it, its answers under
    /// that, then what came of them.
    private var stacked: some View {
        VStack(alignment: .leading, spacing: 7) {
            Button {
                if showsTask {
                    app.showTask(project: need.project, task: need.task)
                } else {
                    app.open(project: need.project, session: need.session)
                }
            } label: {
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Dot(colour: theme.colour(need.kind))
                    VStack(alignment: .leading, spacing: 0) {
                        Text(need.what).font(theme.sized(13)).lineLimit(2)
                        if !detail.isEmpty {
                            Text(detail).font(theme.sized(12.5)).foregroundStyle(theme.muted).lineLimit(2)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .multilineTextAlignment(.leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(showsTask ? "Shows \(need.task) in full: how to check it, its PR, steps and notes" : "Shows it in the window")
            FlowRow(spacing: 6) { actButtons }
                .buttonStyle(FlatButtonStyle(lines: 3))
                .disabled(busy)
                .padding(.leading, 14)
            outcomes(indent: 14)
        }
    }

    private var row: some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack(alignment: .center, spacing: 10) {
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
                HStack(spacing: 6) { actButtons }
                    .fixedSize()
                    .disabled(busy)
            }
            outcomes(indent: 17)
        }
    }

    /// What the server said, and what came of a link's or a task's ask.
    @ViewBuilder
    private func outcomes(indent: CGFloat) -> some View {
        if let said {
            Text(said.text)
                .font(theme.body(.footnote))
                .foregroundStyle(said.failed ? theme.attention : theme.muted)
                .padding(.leading, indent)
        }
        if let o = app.linkOutcomes[need.id] {
            LinkOutcomeRow(outcome: o)
                .padding(.leading, indent)
        }
        if !need.task.isEmpty, need.kind == .review || need.kind == .blocked {
            TaskOutcome(project: need.project, task: need.task)
                .padding(.leading, indent)
        }
    }

    /// The primary answer's style, its words wrapping in the panel.
    private var primary: FlatButtonStyle { FlatButtonStyle(primary: true, lines: panel ? 3 : 1) }

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

    /// Its answers, as many as it has: what lays them out is the caller's.
    @ViewBuilder
    private var actButtons: some View {
            switch need.kind {
            case .approval:
                // Options that allow once only: one for the session needs
                // the confirmation the session's own card asks.
                if let dialog, !dialog.escape, let first = dialog.options.first, once(dialog, 1) {
                    Button(first) { approve(1) }
                        .accessibilityLabel(named(first))
                        .buttonStyle(primary)
                    if dialog.options.count > 1, let last = dialog.options.last, once(dialog, dialog.options.count) {
                        Button(last) { approve(dialog.options.count) }
                            .accessibilityLabel(named(last))
                    }
                }
                open("Open")
            case .question where need.id.hasPrefix("questions:"):
                Button("Answer now") { answerQuestions() }
                    .accessibilityLabel(named("Answer now"))
                    .buttonStyle(primary)
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
                    TaskActs(project: need.project, task: need.task, status: need.kind == .review ? "review" : "blocked")
                }
                if !need.session.isEmpty { open("Open") }
            case .link, .network:
                if let l = need.link {
                    LinkActs(need: need, act: l)
                } else if let p = need.peer {
                    // A paired server's need, answered there (app API 30);
                    // a multi-select's options tick, Submit sends them (T904).
                    if let t = p.ticks {
                        ForEach(Array(t.options.enumerated()), id: \.offset) { i, o in
                            Toggle(o, isOn: Binding(get: { ticked.contains(i) },
                                                    set: { if $0 { ticked.insert(i) } else { ticked.remove(i) } }))
                                .toggleStyle(.checkbox)
                                .accessibilityLabel(named(o))
                                .help(i < t.descriptions.count && !t.descriptions[i].isEmpty ? t.descriptions[i] : "Ticks it; Submit sends the options ticked")
                        }
                        Button("Submit") { if let a = t.params(picks: Array(ticked)) { send(a) } }
                            .disabled(ticked.isEmpty)
                            .accessibilityLabel(named("Submit"))
                            .help("Goes to \(p.server) as your answer, the options ticked; it refuses it if the menu changed meanwhile")
                    }
                    ForEach(p.choices, id: \.self) { c in
                        Button(c.title) { peerAct(c) }
                            .accessibilityLabel(named(c.title))
                            .help("Goes to \(p.server) as your answer; it refuses it if the need changed meanwhile")
                    }
                } else {
                    open("Open")
                }
            case .checks, .held:
                open("Open")
            }
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

    /// Sends choice c of a paired server's need; send back asks for its
    /// note first.
    private func peerAct(_ c: Need.PeerAct.Choice) {
        if c.note {
            note = ""
            sendingBack = c
            return
        }
        send(c.params)
    }

    private func send(_ p: AppAPI.PeerNeedActParams) {
        act {
            let r = try await app.actOnNeed(p)
            let server = need.peer?.server ?? "the paired server"
            switch r.act {
            case "accept", "send-back": return r.asked ? "Sent to \(server)'s coordinator." : "\(server)'s coordinator was asked already."
            case "questions": return "\(server)'s coordinator asks them now, in its session there."
            default: return "Answered on \(server)."
            }
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
            } catch is QuestionGone {
                // Answered elsewhere: the card goes with the sessions read.
            } catch let e as AppAPI.Error where e.code == "need-changed" {
                said = ("That changed meanwhile: look again.", true)
            } catch let e as AppAPI.Error where e.code == "always-allow" {
                said = ("That option allows more than once: only you pick it, in the Terminal tab.", true)
            } catch {
                said = (Dashboard.words(error), true)
            }
            busy = false
        }
    }
}
