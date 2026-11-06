import SwiftUI
import TerminatrKit
import TerminatrUI

/// A need in the inspector (T748 milestone C, T1016), where answering it
/// lives: a card with who it's about and a plain title, its answers
/// showing only while it's picked (NeedCards), wrapping at the panel's
/// width. It answers: an approval's dialog as the server reads it (Allow,
/// the last option, Open), a question tm read with its options, the
/// coordinator's open questions (Answer now), a task in review (Accept,
/// Send back) or blocked (Delegate…), a link's held message, offered file
/// or link request, or a share request (LinkActs). What the card can't
/// answer safely (an escape's dialog or question, which needs the command
/// named, a question tm couldn't read, several options) opens the session
/// instead. The server checks every answer, as it checks the session's own
/// cards. What came of an answer shows whether it's picked or not.
struct NeedCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let need: Need
    var picked = true
    /// Names its project: in a list of a program's, whose needs are
    /// several projects'.
    var named = false
    var pick: () -> Void = {}
    /// An answer went without a refusal.
    var answered: () -> Void = {}
    @State private var dialog: AppAPI.ApproveResult?
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var sendingBack: Need.PeerAct.Choice?
    /// A paired server's multi-select's options ticked, by index (T904).
    @State private var ticked: Set<Int> = []

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            head
            if picked {
                FlowRow(spacing: 6) { actButtons }
                    .buttonStyle(FlatButtonStyle(lines: 3))
                    .disabled(busy)
            }
            outcomes
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.line, lineWidth: 1).allowsHitTesting(false))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(need.title)
        .task(id: need.id) { await check() }
        .sheet(isPresented: Binding(get: { sendingBack != nil }, set: { if !$0 { sendingBack = nil } })) {
            if let c = sendingBack {
                SendBackSheet(task: need.task) { note in send(c.params(note: note)) }
            }
        }
    }

    /// Who and what: a click picks the card, and once it's picked shows
    /// what it names.
    private var head: some View {
        let card = need.card(detail: detail)
        let line = named && !need.project.isEmpty ? "\(need.project) · \(card.line)" : card.line
        return Button {
            if !picked {
                pick()
            } else if showsTask {
                app.showTask(project: need.project, task: need.task)
            } else {
                app.open(project: need.project, session: need.session)
            }
        } label: {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    Dot(colour: theme.colour(need.kind))
                    Text(line)
                        .font(theme.monoSized(12))
                        .foregroundStyle(theme.muted)
                        .lineLimit(2)
                }
                Text(card.title)
                    .font(theme.sized(14, .medium))
                    .lineLimit(4)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .multilineTextAlignment(.leading)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(!picked ? "Shows its answers" : showsTask ? "Shows \(need.task) in full: how to check it, its PR, steps and notes" : "Shows it in the window")
        .accessibilityLabel([line, card.title].filter { !$0.isEmpty }.joined(separator: ": ").replacingOccurrences(of: " · ", with: ", "))
        .accessibilityAddTraits(picked ? .isSelected : [])
        .accessibilityHint(!picked ? "Shows its answers" : showsTask ? "Shows the task in full" : "Shows it in the window")
    }

    /// What the server said, and what came of a link's or a task's ask.
    @ViewBuilder
    private var outcomes: some View {
        if let said {
            Text(said.text)
                .font(theme.body(.footnote))
                .foregroundStyle(said.failed ? theme.attention : theme.muted)
        }
        if let o = app.linkOutcomes[need.id] {
            LinkOutcomeRow(outcome: o)
        }
        if !need.task.isEmpty, need.kind == .review || need.kind == .blocked {
            TaskOutcome(project: need.project, task: need.task)
        }
    }

    /// The primary answer's style, its words wrapping in the panel.
    private var primary: FlatButtonStyle { FlatButtonStyle(primary: true, lines: 3) }

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
                    TaskActs(project: need.project, task: need.task, status: need.kind == .review ? "review" : "blocked", sendBack: "Send back")
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
                        let button = Button(c.note ? "Send back" : c.title) { peerAct(c) }
                            .accessibilityLabel(named(c.title))
                            .help("Goes to \(p.server) as your answer; it refuses it if the need changed meanwhile")
                        if leads(c, in: p) {
                            button.buttonStyle(primary)
                        } else {
                            button
                        }
                    }
                } else {
                    open("Open")
                }
            case .checks, .held:
                open("Open")
            }
    }

    /// Open is only words beside a need's answers; an Answer… or the
    /// like is the answer.
    @ViewBuilder
    private func open(_ title: String) -> some View {
        let button = Button(title) { app.open(project: need.project, session: need.session) }
            .help("Shows it in the window")
            .accessibilityLabel(named(title))
        if title == "Open" {
            button.buttonStyle(.flatText)
        } else {
            button
        }
    }

    /// Whether a paired server's choice is the primary answer, as this
    /// server's own are: Accept, Answer now, a dialog's first option.
    private func leads(_ c: Need.PeerAct.Choice, in p: Need.PeerAct) -> Bool {
        switch c.params.act {
        case "accept", "questions": true
        case "approve": c == p.choices.first
        default: false
        }
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
                answered()
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

/// What needs the user, under its count, one card each (T1016): the first
/// starts picked, a click picks another, and once the picked one is
/// answered the next is. One whose answer stands (an ask with its
/// coordinator, a link's act) isn't picked again by itself.
struct NeedCards: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let needs: [Need]
    /// The done acts whose needs went: their rows, for a moment.
    var settled: [(id: String, outcome: LinkOutcome)] = []
    var named = false
    @State private var picked: String?
    @State private var answered: Set<String> = []

    var body: some View {
        if !needs.isEmpty || !settled.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text("Needs you · \(needs.count)")
                    .font(theme.sized(12))
                    .foregroundStyle(theme.muted)
                    .accessibilityLabel("Needs you, \(needs.count)")
                    .accessibilityAddTraits(.isHeader)
                ForEach(needs) { n in
                    NeedCard(need: n, picked: picked == n.id, named: named,
                             pick: { picked = n.id }, answered: { answered.insert(n.id) })
                }
                ForEach(settled, id: \.id) { LinkOutcomeRow(outcome: $0.outcome, settled: true) }
            }
            .onChange(of: open, initial: true) { was, now in
                picked = NeedPick.next(picked: picked, was: was, now: now)
                answered.formIntersection(needs.map(\.id))
            }
        }
    }

    /// The ids that can be picked: those with no answer standing.
    private var open: [String] {
        needs.filter { n in
            !answered.contains(n.id)
                && app.linkOutcomes[n.id]?.holdsButtons != true
                && !((n.kind == .review || n.kind == .blocked) && app.asks[TaskAsk.key(n.project, n.task)]?.holdsButtons == true)
        }
        .map(\.id)
    }
}
