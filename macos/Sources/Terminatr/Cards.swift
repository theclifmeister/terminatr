import SwiftUI
import TerminatrKit

/// What the session waits on, above the composer: tm's question menu
/// (session.answer, guarded) and the agent's permission dialog
/// (session.approve, checked first). The server re-checks each answer;
/// these cards only offer what it would take. ⌘↩ (firstChoice bumped)
/// takes the first card's first choice, as a click on it would, with
/// the same confirmations.
struct Cards: View {
    let model: SessionModel
    let session: AppAPI.Session?
    var firstChoice = 0

    var body: some View {
        let first = session?.question?.questions.firstIndex { !$0.answered }
        VStack(spacing: 10) {
            if let session, let q = session.question {
                ForEach(Array(q.questions.enumerated()), id: \.offset) { i, item in
                    if !item.answered {
                        QuestionCard(model: model, menu: q, index: i, item: item, firstChoice: i == first ? firstChoice : nil)
                    }
                }
            }
            if let session, session.state == "blocked", session.reason == "permission" {
                ApprovalCard(model: model, session: session, firstChoice: first == nil ? firstChoice : nil)
                    .id(session.id + (model.transcript.pendingTool?.call ?? ""))
            }
        }
        .padding(.horizontal, 22)
        .frame(maxWidth: 820)
    }
}

/// A card's frame: its tag, a wash and an edge in the card's colour.
struct CardFrame<Content: View>: View {
    @Environment(\.theme) private var theme
    let tag: String
    let colour: Color
    let tint: Color
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Text(tag.uppercased())
                .font(theme.mono(10.5, .medium))
                .tracking(1)
                .foregroundStyle(colour)
            content
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
        .background(tint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(colour.opacity(0.4), lineWidth: 1))
        .shadow(color: colour.opacity(0.25), radius: 12, y: 6)
    }
}

/// One question of tm's open menu. A single-select option goes with the
/// Question guard: the server refuses it unless the menu still asks
/// this, so a menu replaced meanwhile gets no stale answer. Several
/// options, or words of the user's, can't be guarded that way (the
/// guard takes one option's label) and go as the question's answer.
/// An escape's question names its command in a confirmation first.
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
    @State private var busy = false

    var body: some View {
        let tag = menu.escape ? "Question · outside the sandbox" : (item.header.isEmpty ? "Question" : item.header)
        CardFrame(tag: firstChoice == nil ? tag : "\(tag) · ⌘↩ answer", colour: theme.question, tint: theme.questionTint) {
            Text(item.question)
                .font(theme.title(15, .bold))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            if item.multiSelect {
                ForEach(item.options, id: \.label) { o in
                    Toggle(isOn: Binding(get: { picked.contains(o.label) }, set: { on in if on { picked.insert(o.label) } else { picked.remove(o.label) } })) {
                        option(o)
                    }
                    .toggleStyle(.checkbox)
                }
                Button("Answer") { send(item.options.map(\.label).filter(picked.contains).joined(separator: ", "), guarded: false) }
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
                    .disabled(picked.isEmpty || busy)
            } else {
                FlowButtons(options: item.options, busy: busy) { send($0, guarded: true) }
            }
            if otherOpen {
                HStack {
                    TextField("Your answer", text: $other)
                        .textFieldStyle(.roundedBorder)
                        .onSubmit { sendOther() }
                    Button("Send") { sendOther() }
                        .disabled(other.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                }
            } else {
                Button("Other…") { otherOpen = true }
                    .buttonStyle(.link)
                    .foregroundStyle(theme.action)
            }
            if let error {
                Text(error)
                    .font(.system(size: 12))
                    .foregroundStyle(theme.attention)
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

    /// ⌘↩: the first option, or the options picked of a multi-select.
    private func takeFirst() {
        guard firstChoice != nil, !busy else { return }
        if item.multiSelect {
            if !picked.isEmpty { send(item.options.map(\.label).filter(picked.contains).joined(separator: ", "), guarded: false) }
        } else if let o = item.options.first {
            send(o.label, guarded: true)
        }
    }

    private func option(_ o: AppAPI.QuestionOption) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(o.label)
            if !o.description.isEmpty {
                Text(o.description)
                    .font(.system(size: 11.5))
                    .foregroundStyle(theme.muted)
            }
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
        Task {
            do {
                _ = try await model.answer(index: index, answer: answer, question: item.question, guarded: guarded)
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
        }
    }
}

/// A menu's options as buttons, wrapped onto lines.
struct FlowButtons: View {
    @Environment(\.theme) private var theme
    let options: [AppAPI.QuestionOption]
    let busy: Bool
    let pick: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(options, id: \.label) { o in
                Button {
                    pick(o.label)
                } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
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
                .font(theme.title(15, .bold))
            if let tool, !tool.text.isEmpty {
                Text(tool.text)
                    .font(theme.mono(12.5))
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
                        .font(.system(size: 12))
                        .foregroundStyle(theme.muted)
                }
            } else if error == nil {
                ProgressView().controlSize(.small)
            }
            HStack {
                if let error {
                    Text(error)
                        .font(.system(size: 12))
                        .foregroundStyle(theme.attention)
                }
                Spacer()
                Button("Esc") { Task { try? await model.keys("\u{1b}") } }
                    .help("Sends Esc to the agent: it dismisses the dialog")
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
