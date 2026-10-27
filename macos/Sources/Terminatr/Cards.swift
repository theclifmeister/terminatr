import SwiftUI
import TerminatrKit
import TerminatrUI

/// What the session waits on, above the composer: a question menu
/// (session.answer, guarded) and the agent's permission dialog
/// (session.approve, checked first). The server re-checks each answer;
/// these cards only offer what it would take. ⌘↩ (firstChoice bumped)
/// takes the first card's first choice, as a click on it would, with
/// the same confirmations. The agent's own menu (Claude's
/// AskUserQuestion) shows one question at a time, as its screen does.
struct Cards: View {
    let model: SessionModel
    let session: AppAPI.Session?
    var firstChoice = 0

    var body: some View {
        let shown = session?.question.map(QuestionPlan.shown) ?? []
        let first = shown.first
        VStack(spacing: 10) {
            if let session, let q = session.question {
                ForEach(shown, id: \.self) { i in
                    QuestionCard(model: model, menu: q, index: i, item: q.questions[i], firstChoice: i == first ? firstChoice : nil)
                        .id("\(session.id) \(q.since?.timeIntervalSince1970 ?? 0) \(i)")
                }
                if shown.isEmpty, let words = QuestionPlan.allAnswered(q) {
                    AnsweredCard(model: model, words: words, screen: q.screen)
                }
            }
            if let session, session.state == "blocked", session.reason == "permission" {
                ApprovalCard(model: model, session: session, firstChoice: first == nil ? firstChoice : nil)
                    .id(session.id + (model.transcript.pendingTool?.call ?? ""))
            }
        }
        .padding(.horizontal, Page.edge)
    }
}

/// One question of the session's open menu (QuestionPlan). A
/// single-select option goes with the Question guard: the server
/// refuses it unless the menu still asks this, so a menu replaced
/// meanwhile gets no stale answer. Several options, or words of the
/// user's, can't be guarded that way (the guard takes one option's
/// label) and go as the question's answer. An escape's question names
/// its command in a confirmation first. The agent's own menu takes no
/// multi-select from here: the card shows its options and sends the
/// user to the Terminal tab. The first card's options answer ⌥⌘1–⌥⌘9.
struct QuestionCard: View {
    @Environment(\.theme) private var theme
    let model: SessionModel
    let menu: AppAPI.Question
    let index: Int
    let item: AppAPI.QuestionItem
    /// The first card's: ⌘↩ takes its first option.
    let firstChoice: Int?
    @State private var picked: Set<String> = []
    @State private var other = ""
    @State private var otherOpen = false
    @State private var confirm: (answer: String, guarded: Bool)?
    @State private var error: String?
    /// The error is one the Terminal tab answers (QuestionPlan.useTerminal).
    @State private var terminal = false
    /// The answer in flight, then sent, in words.
    @State private var status: String?
    @State private var busy = false

    var body: some View {
        let plan = QuestionPlan(menu, index: index)
        let tag = QuestionPlan.tag(menu, index: index)
        CardFrame(tag: firstChoice == nil || plan.paneOnly ? tag : "\(tag) · ⌘↩ answer", colour: theme.question, tint: theme.questionTint) {
            Text(item.question)
                .font(theme.title(.headline, .bold))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            if plan.checkboxes {
                ForEach(Array(item.options.enumerated()), id: \.offset) { i, o in
                    Toggle(isOn: Binding(get: { picked.contains(o.label) }, set: { on in if on { picked.insert(o.label) } else { picked.remove(o.label) } })) {
                        OptionLabel(o)
                    }
                    .toggleStyle(.checkbox)
                    .disabled(plan.paneOnly || busy)
                    .accessibilityLabel(QuestionPlan.spoken(item, option: i))
                }
                if !plan.paneOnly {
                    Button("Submit") { send(QuestionPlan.joined(item, picked: picked), guarded: false) }
                        .buttonStyle(.borderedProminent)
                        .tint(theme.action)
                        .disabled(picked.isEmpty || busy)
                        .accessibilityHint("Answers with the options checked")
                }
            } else {
                FlowButtons(item: item, busy: busy, numbered: firstChoice != nil) { send($0, guarded: true) }
            }
            if let note = plan.note {
                terminalNote(note)
            }
            if plan.words {
                if otherOpen {
                    HStack {
                        TextField("Your answer", text: $other)
                            .textFieldStyle(.roundedBorder)
                            .onSubmit { sendOther() }
                            .accessibilityLabel("Your own answer to: \(item.question)")
                        Button("Send") { sendOther() }
                            .disabled(other.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                    }
                } else {
                    Button("Other…") { otherOpen = true }
                        .buttonStyle(.link)
                        .foregroundStyle(theme.action)
                        .accessibilityHint("Answers in your own words")
                }
            }
            if let status, error == nil {
                Text(status)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
            }
            if let error {
                if terminal {
                    terminalNote(error)
                } else {
                    Text(error)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.attention)
                }
            }
        }
        .alert("Answer this question?", isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }), presenting: confirm) { c in
            Button("Answer “\(c.answer)”") { go(c.answer, guarded: c.guarded) }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("This answer lets a command run outside the thread's sandbox:\n\n\(item.question)")
        }
        .onChange(of: firstChoice) { old, new in
            // A card that just became the first takes nothing by itself.
            if let old, let new, new > old { takeFirst() }
        }
    }

    /// Why the Terminal tab answers this, with its button.
    private func terminalNote(_ words: String) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(words)
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 8)
            Button("Open in Terminal") { model.pick(.terminal) }
                .help("Shows the agent's menu in the Terminal tab, to answer there")
        }
    }

    /// ⌘↩: the first option, or the options picked of a multi-select.
    private func takeFirst() {
        let plan = QuestionPlan(menu, index: index)
        guard firstChoice != nil, !busy, !plan.paneOnly else { return }
        if plan.checkboxes {
            if !picked.isEmpty { send(QuestionPlan.joined(item, picked: picked), guarded: false) }
        } else if let o = item.options.first {
            send(o.label, guarded: true)
        }
    }

    private func sendOther() {
        let t = other.trimmingCharacters(in: .whitespacesAndNewlines)
        if !t.isEmpty { send(t, guarded: false) }
    }

    private func send(_ answer: String, guarded: Bool) {
        if menu.escape {
            confirm = (answer, guarded)
        } else {
            go(answer, guarded: guarded)
        }
    }

    private func go(_ answer: String, guarded: Bool) {
        busy = true
        error = nil
        terminal = false
        status = QuestionPlan.sending(answer, screen: menu.screen)
        Task {
            do {
                _ = try await model.answer(index: index, answer: answer, question: item.question, guarded: guarded)
                status = "Answered “\(answer)”."
            } catch let e as AppAPI.Error where menu.screen && QuestionPlan.useTerminal(e.code) {
                self.error = "\(e.message). Answer it in the Terminal tab."
                terminal = true
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
        }
    }
}

/// Every question of the menu has its answer, but the menu is still
/// open: says so in words, with the Terminal tab to finish it there.
struct AnsweredCard: View {
    @Environment(\.theme) private var theme
    let model: SessionModel
    let words: String
    let screen: Bool

    var body: some View {
        CardFrame(tag: "Question · answered", colour: theme.question, tint: theme.questionTint) {
            HStack(alignment: .firstTextBaseline) {
                Text(words)
                    .font(theme.body(.subheadline))
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                if screen {
                    Button("Open in Terminal") { model.pick(.terminal) }
                }
            }
        }
    }
}

/// A question's options as buttons, one per line, each with its
/// description; numbered (⌥⌘1–⌥⌘9) on the first card.
struct FlowButtons: View {
    @Environment(\.theme) private var theme
    let item: AppAPI.QuestionItem
    let busy: Bool
    var numbered = false
    let pick: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(item.options.enumerated()), id: \.offset) { i, o in
                Button {
                    pick(o.label)
                } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        if numbered && i < 9 {
                            Text("\(i + 1)")
                                .font(theme.mono(.caption, .medium))
                                .foregroundStyle(theme.muted)
                        }
                        Text(o.label).fontWeight(.semibold)
                        if !o.description.isEmpty {
                            Text(o.description)
                                .foregroundStyle(theme.muted)
                                .lineLimit(2)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .buttonStyle(.bordered)
                .disabled(busy)
                .keyboardShortcut(numbered && i < 9 ? KeyboardShortcut(KeyEquivalent(Character("\(i + 1)")), modifiers: [.command, .option]) : nil)
                .accessibilityLabel(QuestionPlan.spoken(item, option: i))
                .help(numbered && i < 9 ? "Answers “\(o.label)” (⌥⌘\(i + 1))" : "Answers “\(o.label)”")
            }
        }
    }
}

/// The agent's permission dialog. The card asks the server what the
/// dialog on screen offers (session.approve with check) and shows the
/// tool call from the conversation; a click sends that option, once
/// the server has checked again. An escape's dialog (a command outside
/// the sandbox) needs the user's confirmation naming the command.
struct ApprovalCard: View {
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session
    /// The first card's: ⌘↩ takes the dialog's first option.
    let firstChoice: Int?
    @State private var dialog: AppAPI.ApproveResult?
    @State private var error: String?
    @State private var confirm: Int?
    @State private var busy = false

    var body: some View {
        let tool = model.transcript.pendingTool
        let tag = session.escape ? "Approval · outside the sandbox" : "Approval"
        CardFrame(tag: firstChoice == nil ? tag : "\(tag) · ⌘↩ allow", colour: theme.approval, tint: theme.approvalTint) {
            Text(tool.map { "Run \($0.tool.isEmpty ? "a tool" : $0.tool)?" } ?? "The agent asks for permission")
                .font(theme.title(.headline, .bold))
            if let tool, !tool.text.isEmpty {
                Text(tool.text)
                    .font(theme.mono(.callout))
                    .textSelection(.enabled)
                    .padding(9)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(theme.command, in: RoundedRectangle(cornerRadius: 8))
            }
            if let dialog {
                if !dialog.question.isEmpty {
                    Text(dialog.question)
                        .foregroundStyle(theme.muted)
                }
                HStack(spacing: 8) {
                    ForEach(Array(dialog.options.enumerated()), id: \.offset) { i, o in
                        if i == 0 {
                            Button(o) { pick(i + 1) }
                                .buttonStyle(.borderedProminent)
                                .tint(theme.action)
                                .disabled(busy || (session.escape && command == nil))
                        } else {
                            Button(o) { pick(i + 1) }
                                .disabled(busy || (session.escape && command == nil))
                        }
                    }
                }
                .lineLimit(1)
                if session.escape && command == nil {
                    Text("The conversation doesn't show the command: read it in the Terminal tab and answer there, or press Esc.")
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                }
            } else if error == nil {
                ProgressView().controlSize(.small)
            }
            HStack {
                if let error {
                    Text(error)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.attention)
                }
                Spacer()
                Button("Esc") { Task { try? await model.keys("\u{1b}") } }
                    .help("Sends Esc to the agent: it dismisses the dialog")
                    .accessibilityLabel("Dismiss the dialog (Esc)")
                    .controlSize(.small)
            }
        }
        .task(id: session.question == nil) { await check() }
        .alert("A command outside the sandbox", isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }), presenting: confirm) { choice in
            Button(choice == 1 ? "Allow \(command ?? "it")" : "Answer “\(option(choice))”") { send(choice, escape: true) }
            Button("Cancel", role: .cancel) {}
        } message: { choice in
            Text("\(command ?? "")\n\nThis command runs with your access, not the thread's sandbox: only you answer for it. Your answer: \(option(choice)).")
        }
        .onChange(of: firstChoice) { old, new in
            // As a click on the first button: only once the dialog is
            // read, and never for a card that just became the first.
            guard let old, let new, new > old, let dialog, !dialog.options.isEmpty, !busy,
                  !(session.escape && command == nil) else { return }
            pick(1)
        }
    }

    /// The command the dialog asks about, as the conversation shows it.
    private var command: String? {
        guard let t = model.transcript.pendingTool, !t.text.isEmpty else { return nil }
        return t.text
    }

    private func check() async {
        do {
            dialog = try await model.approve(choice: 1, check: true)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func option(_ choice: Int) -> String {
        dialog.flatMap { choice <= $0.options.count ? $0.options[choice - 1] : nil } ?? "option \(choice)"
    }

    /// An escape's dialog takes any answer only with the user's
    /// confirmation naming the command; the server refuses it without.
    private func pick(_ choice: Int) {
        if session.escape {
            confirm = choice
        } else {
            send(choice, escape: false)
        }
    }

    private func send(_ choice: Int, escape: Bool) {
        busy = true
        error = nil
        Task {
            do {
                _ = try await model.approve(choice: choice, check: false, escape: escape)
            } catch let e as AppAPI.Error where e.code == "always-allow" {
                error = "That option allows more than once: only you pick it, in the Terminal tab."
            } catch let e as AppAPI.Error where e.code == "escape-unconfirmed" {
                confirm = choice
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
        }
    }
}
