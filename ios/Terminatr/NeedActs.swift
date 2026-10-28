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
struct NeedActs: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let need: Need
    @State private var dialog: AppAPI.ApproveResult?
    @State private var escape = false
    @State private var busy = false
    @State private var said: (text: String, failed: Bool)?
    @State private var sendingBack = false
    @State private var note = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            buttons
                .controlSize(.small)
                .disabled(busy)
            if escape {
                Label("It runs a command outside its sandbox: answer it on your computer.", systemImage: "desktopcomputer")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
            if let said {
                Text(said.text)
                    .font(theme.body(.footnote))
                    .foregroundStyle(said.failed ? theme.attention : theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .task(id: need.id) { await check() }
        .kept($note, "send back \(need.id)")
        .kept($sendingBack, "sending back \(need.id)")
        .alert("Send \(need.task) back", isPresented: $sendingBack) {
            TextField("What's missing", text: $note, axis: .vertical)
            Button("Send Back") { sendBack() }
                .disabled(note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The coordinator hands your note to the thread.")
        }
    }

    @ViewBuilder
    private var buttons: some View {
        switch need.kind {
        case .approval where model.can(AppAPI.Methods.sessionApprove.name):
            if let d = dialog, !d.escape, let first = d.options.first, once(d, 1) {
                row {
                    Button(first) { approve(1) }
                        .buttonStyle(.borderedProminent)
                        .tint(theme.action)
                        .accessibilityLabel(named(first))
                    if d.options.count > 1, let last = d.options.last, once(d, d.options.count) {
                        Button(last) { approve(d.options.count) }
                            .buttonStyle(.bordered)
                            .accessibilityLabel(named(last))
                    }
                }
            }
        case .question where need.id.hasPrefix("questions:") && model.can(AppAPI.Methods.questionsOpen.name):
            Button("Answer now") { openQuestions() }
                .buttonStyle(.borderedProminent)
                .tint(theme.action)
                .accessibilityLabel(named("Answer now"))
                .accessibilityHint("The coordinator puts its questions to you; they show here")
        case .question where model.can(AppAPI.Methods.sessionAnswer.name):
            if let a = need.answer {
                row {
                    ForEach(a.options, id: \.self) { o in
                        Button(o) { answer(a, o) }
                            .buttonStyle(.bordered)
                            .accessibilityLabel(named(o))
                    }
                }
            }
        case .review where !need.task.isEmpty && model.can(AppAPI.Methods.taskAccept.name):
            row {
                Button("Accept") { accept() }
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
                    .accessibilityLabel(named("Accept"))
                if model.can(AppAPI.Methods.taskSendBack.name) {
                    Button("Send Back…") { note = ""; sendingBack = true }
                        .buttonStyle(.bordered)
                        .accessibilityLabel(named("Send back"))
                }
            }
        default:
            EmptyView()
        }
    }

    /// Buttons side by side, wrapping to the next line when they don't
    /// fit: a short screen keeps them in a line or two (T826).
    private func row(@ViewBuilder _ content: () -> some View) -> some View {
        Flow(spacing: 8, lineSpacing: 6) { content() }
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
        Task {
            do {
                if let s = try await f() { said = (s, false) }
            } catch {
                if let w = Self.words(error) { said = (w, true) }
                if (error as? AppAPI.Error)?.code == "device-escape-off" { escape = true }
            }
            busy = false
        }
    }

    /// An act's failure in words; nil when the user cancelled the system's prompt (an older key asks).
    static func words(_ error: any Error) -> String? {
        if let e = error as? LAError, [.userCancel, .appCancel, .systemCancel].contains(e.code) { return nil }
        if let e = error as? AppAPI.Error {
            switch e.code {
            case "device-escape-off": return nil // the card says so
            case "device-signature": return "Your server didn't take this phone's signature: \(e.message)"
            case "device-level": return "This phone may only view: tm device access on your computer raises it."
            case "always-allow": return "That option allows more than once: only you pick it, on your computer."
            case DemoTransport.refused: return "The demo shows recorded data: pair your server to do this."
            default: return e.errorDescription
            }
        }
        return error.localizedDescription
    }
}

/// Views left to right, onto the next line when the next doesn't fit.
struct Flow: Layout {
    var spacing: CGFloat
    var lineSpacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let places = place(subviews, width: proposal.width ?? .infinity)
        return CGSize(width: places.map(\.maxX).max() ?? 0, height: places.map(\.maxY).max() ?? 0)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for (v, p) in zip(subviews, place(subviews, width: bounds.width)) {
            v.place(at: CGPoint(x: bounds.minX + p.minX, y: bounds.minY + p.minY), proposal: ProposedViewSize(p.size))
        }
    }

    private func place(_ subviews: Subviews, width: CGFloat) -> [CGRect] {
        var out: [CGRect] = []
        var x: CGFloat = 0, y: CGFloat = 0, line: CGFloat = 0
        for v in subviews {
            var size = v.sizeThatFits(.unspecified)
            size.width = min(size.width, width)
            if x > 0, x + size.width > width {
                x = 0
                y += line + lineSpacing
                line = 0
            }
            out.append(CGRect(origin: CGPoint(x: x, y: y), size: size))
            x += size.width + spacing
            line = max(line, size.height)
        }
        return out
    }
}
