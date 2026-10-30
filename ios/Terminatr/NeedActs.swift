import LocalAuthentication
import SwiftUI
import TerminatrKit
import TerminatrUI

/// A need's buttons on the phone, at the answer level (T791): an
/// approval's options that allow once, as the server reads the dialog
/// (session.approve {check}); a question's options (session.answer, the
/// question its guard); the coordinator's open questions (Answer now); a
/// task in review (Accept, Send back…). Each act is signed by the
/// write key, with no prompt, and the server checks every
/// answer as it checks the Mac app's. An approval that runs a command
/// outside the sandbox is never the phone's: the server refuses it
/// (device-escape-off), and the card says to answer it on the computer.
/// Nothing shows at view, or for a need the phone can't answer.
///
/// The buttons are the redesign's (T847): an approval's two in its
/// tone, Y and N on their keycaps; a question's options as numbered
/// rows (AnswerOption), 1 to 9; the rest a tone's primary and a
/// secondary. Keys answer only where the environment's answerKeys says
/// (the deck's lead card), never while the Send back note is asked. A
/// server's answer is a success or warning haptic. laidOut lays the
/// parts out: Needs you's card (NeedCard), or under a conversation's
/// need (NeedActsColumn). An answer the agent's screen didn't confirm
/// (QuestionPlan.useTerminal) offers its terminal, in a session's
/// screen (showTerminal), to answer there (T884).
struct NeedActs: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.showTerminal) private var showTerminal
    @Environment(\.dynamicTypeSize) private var typeSize
    let need: Need
    let laidOut: (NeedParts) -> AnyView
    @State private var dialog: AppAPI.ApproveResult?
    @State private var escape = false
    /// The last answer failed as one the terminal answers instead.
    @State private var terminal = false
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var sendingBack = false
    @State private var note = ""
    @State private var peerSendBack: Need.PeerAct.Choice?
    @State private var answered = 0
    @State private var refused = 0

    var body: some View {
        laidOut(NeedParts(buttons: AnyView(answers), command: dialog.map { $0.escape ? "" : $0.question } ?? "",
                      note: shownNote, acts: acts))
        .feedback(.success, trigger: answered)
        .feedback(.warning, trigger: refused)
        .task(id: need.id) { await check() }
        .kept($note, "send back \(need.id)")
        .kept($sendingBack, "sending back \(need.id)")
        .alert("Send \(need.task) back", isPresented: $sendingBack) {
            TextField("What's missing", text: $note, axis: .vertical)
            Button("Send Back") {
                if let c = peerSendBack { peerSendBack = nil; peerAct(c.params(note: note.trimmingCharacters(in: .whitespacesAndNewlines))) } else { sendBack() }
            }
                .disabled(note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            Button("Cancel", role: .cancel) { peerSendBack = nil }
        } message: {
            Text("The coordinator hands your note to the thread.")
        }
    }

    private var answers: some View {
        buttons
            .disabled(busy)
            .transformEnvironment(\.answerKeys) { if sendingBack { $0 = false } }
    }

    @ViewBuilder
    private var buttons: some View {
        switch need.kind {
        case .approval where model.can(AppAPI.Methods.sessionApprove.name):
            if let d = dialog, !d.escape, let first = d.options.first, once(d, 1) {
                pair {
                    Button { approve(1) } label: { keyed("Y", first, inverted: true) }
                        .buttonStyle(.primary(.approval))
                        .answerKey("y")
                        .accessibilityLabel(named(first))
                    if d.options.count > 1, let last = d.options.last, once(d, d.options.count) {
                        Button { approve(d.options.count) } label: { keyed("N", last, inverted: false) }
                            .buttonStyle(.secondary)
                            .answerKey("n")
                            .accessibilityLabel(named(last))
                    }
                }
            }
        case .question where need.id.hasPrefix("questions:") && model.can(AppAPI.Methods.questionsOpen.name):
            Button("Answer now") { openQuestions() }
                .buttonStyle(.primary(.question))
                .accessibilityLabel(named("Answer now"))
                .accessibilityHint("The coordinator puts its questions to you; they show here")
        case .question where model.can(AppAPI.Methods.sessionAnswer.name):
            if let a = need.answer {
                VStack(spacing: Spacing.item) {
                    ForEach(Array(a.options.enumerated()), id: \.offset) { i, o in
                        AnswerOption(key: i + 1, label: o, detail: i < a.descriptions.count ? a.descriptions[i] : "") {
                            answer(a, o)
                        }
                        .accessibilityLabel(named(o))
                    }
                    if terminal, let showTerminal {
                        Button("Open the terminal", systemImage: "apple.terminal", action: showTerminal.go)
                            .buttonStyle(.secondary)
                            .accessibilityHint("Shows the agent's menu in its terminal, to answer there")
                    }
                }
            }
        case .review where !need.task.isEmpty && model.can(AppAPI.Methods.taskAccept.name):
            pair {
                Button("Accept") { accept() }
                    .buttonStyle(.primary(.review))
                    .accessibilityLabel(named("Accept"))
                if model.can(AppAPI.Methods.taskSendBack.name) {
                    Button("Send Back…") { note = ""; sendingBack = true }
                        .buttonStyle(.secondary)
                        .accessibilityLabel(named("Send back"))
                }
            }
        case .network where need.peer != nil && model.can(AppAPI.Methods.peerNeedAct.name):
            // A paired server's need at its answer level, answered there
            // through this server (app API 30), in the same buttons as
            // this server's own.
            if let p = need.peer {
                peerButtons(p)
            }
        default:
            EmptyView()
        }
    }

    /// The buttons as VoiceOver actions on the card, as buttons shows them.
    private var acts: [NeedAct] {
        switch need.kind {
        case .approval where model.can(AppAPI.Methods.sessionApprove.name):
            guard let d = dialog, !d.escape, let first = d.options.first, once(d, 1), !busy else { return [] }
            var out = [NeedAct(first) { approve(1) }]
            if d.options.count > 1, let last = d.options.last, once(d, d.options.count) {
                out.append(NeedAct(last) { approve(d.options.count) })
            }
            return out
        case .question where need.id.hasPrefix("questions:") && model.can(AppAPI.Methods.questionsOpen.name):
            return busy ? [] : [NeedAct("Answer now") { openQuestions() }]
        case .question where model.can(AppAPI.Methods.sessionAnswer.name):
            guard let a = need.answer, !busy else { return [] }
            return a.options.map { o in NeedAct(o) { answer(a, o) } }
        case .review where !need.task.isEmpty && model.can(AppAPI.Methods.taskAccept.name):
            guard !busy else { return [] }
            var out = [NeedAct("Accept") { accept() }]
            if model.can(AppAPI.Methods.taskSendBack.name) {
                out.append(NeedAct("Send back") { note = ""; sendingBack = true })
            }
            return out
        case .network where need.peer != nil && model.can(AppAPI.Methods.peerNeedAct.name):
            guard let p = need.peer, !busy else { return [] }
            return p.choices.map { c in NeedAct(c.title) { peerPick(c) } }
        default:
            return []
        }
    }

    /// A paired server's need's buttons: a question's or a dialog's
    /// choices as numbered rows, accept and send back as a review's pair,
    /// the coordinator's questions as Answer now.
    @ViewBuilder
    private func peerButtons(_ p: Need.PeerAct) -> some View {
        let tone: Need.Kind = p.choices.first.map { $0.params.act == "approve" ? .approval : .question } ?? .question
        switch p.choices.first?.params.act {
        case "answer", "approve":
            VStack(spacing: Spacing.item) {
                ForEach(Array(p.choices.enumerated()), id: \.offset) { i, c in
                    AnswerOption(key: i + 1, label: c.title) { peerPick(c) }
                        .accessibilityLabel(named(c.title))
                }
            }
            .tint(theme.colour(tone))
        case "questions":
            Button("Answer now") { peerPick(p.choices[0]) }
                .buttonStyle(.primary(.question))
                .accessibilityLabel(named("Answer now"))
        default:
            pair {
                ForEach(p.choices, id: \.self) { c in
                    if c.note {
                        Button(c.title) { peerPick(c) }
                            .buttonStyle(.secondary)
                            .accessibilityLabel(named(c.title))
                    } else {
                        Button(c.title) { peerPick(c) }
                            .buttonStyle(.primary(.review))
                            .accessibilityLabel(named(c.title))
                    }
                }
            }
        }
    }

    /// Sends choice c of a paired server's need; send back asks for its
    /// note first.
    private func peerPick(_ c: Need.PeerAct.Choice) {
        if c.note {
            note = ""
            peerSendBack = c
            sendingBack = true
        } else {
            peerAct(c.params)
        }
    }

    /// The card's line under the buttons: what the server said, else why
    /// the phone can't answer an escape.
    private var shownNote: (text: String, failed: Bool)? {
        if let said { return said }
        if escape { return ("It runs a command outside its sandbox: answer it on your computer.", false) }
        return nil
    }

    /// An answer's label after its keycap.
    private func keyed(_ key: String, _ title: String, inverted: Bool) -> some View {
        HStack(spacing: 7) {
            Keycap(key, tone: .approval, inverted: inverted)
            Text(title)
        }
    }

    /// Two answers side by side, the primary taking the room; stacked at
    /// accessibility sizes, so their words fit.
    private func pair(@ViewBuilder _ content: () -> some View) -> some View {
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(spacing: Spacing.item))
            : AnyLayout(HStackLayout(spacing: Spacing.item))
        return layout { content() }
    }

    private func named(_ title: String) -> String { "\(title): \(need.what)" }

    /// An option that allows once only: one for the session, or always,
    /// stays the user's on the computer.
    private func once(_ d: AppAPI.ApproveResult, _ choice: Int) -> Bool {
        choice > d.scopes.count || d.scopes[choice - 1] == "once"
    }

    /// Reads the dialog on screen (unsigned: it answers nothing); an
    /// escape's the server refuses to the phone at once.
    private func check() async {
        guard need.kind == .approval, !need.session.isEmpty, model.can(AppAPI.Methods.sessionApprove.name) else { return }
        do {
            dialog = try await model.check(need.session)
            escape = dialog?.escape == true
        } catch let e as AppAPI.Error where e.code == "device-escape-off" {
            escape = true
        } catch {
            dialog = nil
        }
    }

    private func approve(_ choice: Int) {
        act { try await model.approve(need.session, choice: choice); return nil }
    }

    private func answer(_ a: Need.Answer, _ option: String) {
        act { try await model.answer(a, option); return nil }
    }

    private func openQuestions() {
        act { try await model.openQuestions(need.project); return "The coordinator asks them now; they show here." }
    }

    private func accept() {
        act {
            try await model.accept(project: need.project, task: need.task)
                ? "The coordinator was asked to accept \(need.task)." : "The coordinator was asked already."
        }
    }

    private func peerAct(_ p: AppAPI.PeerNeedActParams) {
        let server = need.peer?.server ?? "the paired server"
        act {
            let r = try await model.actOnNeed(p)
            switch r.act {
            case "accept", "send-back": return r.asked ? "Sent to \(server)'s coordinator." : "\(server)'s coordinator was asked already."
            case "questions": return "\(server)'s coordinator asks them now, there."
            default: return "Answered on \(server)."
            }
        }
    }

    private func sendBack() {
        let text = note.trimmingCharacters(in: .whitespacesAndNewlines)
        act {
            try await model.sendBack(project: need.project, task: need.task, note: text)
                ? "The coordinator was asked to send \(need.task) back." : "The coordinator was asked already."
        }
    }

    private func act(_ f: @escaping () async throws -> String?) {
        busy = true
        said = nil
        terminal = false
        Task {
            do {
                if let s = try await f() { said = (s, false) }
                answered += 1
            } catch {
                if let w = Self.words(error) {
                    said = (w, true)
                    refused += 1
                }
                if (error as? AppAPI.Error)?.code == "device-escape-off" { escape = true }
                if let e = error as? AppAPI.Error, need.kind == .question, QuestionPlan.useTerminal(e.code) { terminal = true }
            }
            busy = false
        }
    }

    /// An act's failure in words; nil when the user cancelled the system's prompt (an older key asks), or the question was answered elsewhere.
    static func words(_ error: any Error) -> String? {
        if let e = error as? LAError, [.userCancel, .appCancel, .systemCancel].contains(e.code) { return nil }
        if error is QuestionGone { return nil } // answered elsewhere: the card goes
        if let e = error as? AppAPI.Error {
            switch e.code {
            case "device-escape-off": return nil // the card says so
            case "device-signature": return "Your server didn't take this phone's signature: \(e.message)"
            case "device-level": return "This phone may only view: tm device access on your computer raises it."
            case "always-allow": return "That option allows more than once: only you pick it, on your computer."
            case "need-changed": return "That changed meanwhile: look again."
            case "no-question", "screen-only": return "\(e.message). Answer it in the terminal view."
            case DemoTransport.refused: return "The demo shows recorded data: pair your server to do this."
            default: return e.errorDescription
            }
        }
        return error.localizedDescription
    }
}

extension NeedActs {
    init(need: Need, laidOut: @escaping (NeedParts) -> some View) {
        self.need = need
        self.laidOut = { AnyView(laidOut($0)) }
    }

    init(need: Need) {
        self.init(need: need) { NeedActsColumn(parts: $0) }
    }
}

/// What NeedActs lays out: its buttons, the dialog an approval asks,
/// what the server said (or why the phone can't answer), and the
/// buttons as VoiceOver actions.
struct NeedParts {
    let buttons: AnyView
    let command: String
    let note: (text: String, failed: Bool)?
    let acts: [NeedAct]
}

/// NeedActs' parts one under the other: the buttons and the server's
/// line, under a conversation's need.
struct NeedActsColumn: View {
    @Environment(\.theme) private var theme
    let parts: NeedParts

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            parts.buttons
            if let note = parts.note {
                Text(note.text)
                    .font(theme.body(.footnote))
                    .foregroundStyle(note.failed ? theme.attention : theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}
