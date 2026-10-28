import SwiftUI
import TerminatrKit
import TerminatrUI

/// The task the window's task view shows (AppModel.taskShown).
struct TaskShown: Identifiable, Hashable {
    let project: String
    let task: String
    var id: String { TaskAsk.key(project, task) }
}

/// A task in full (T780), as the TUI's task panel shows it: its status
/// and thread, what it is blocked on, for a task in review where its PR
/// stands and how to check it, its steps and its notes, with its
/// actions (TaskActs: Accept and Send back…, or Delegate…) and what
/// came of them. It follows the projects watch; a board task's notes
/// and steps, which the watch leaves out, come from task.get, again
/// whenever the watch says the task changed.
struct TaskSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    let shown: TaskShown
    @State private var full: AppAPI.Task?
    @State private var fetchError: String?
    @State private var asking = false
    @State private var askError: String?

    var body: some View {
        let detail = app.project(shown.project).flatMap { TaskDetail.of(shown.task, in: $0, fetched: full) }
        let watched = app.project(shown.project).flatMap { TaskDetail.of(shown.task, in: $0) }
        VStack(alignment: .leading, spacing: 0) {
            if let detail {
                header(detail)
                theme.line.frame(height: 1)
                ScrollView {
                    content(detail)
                        .padding(20)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(maxHeight: 520)
                .fixedSize(horizontal: false, vertical: true)
            } else if app.canGetTask && fetchError == nil {
                ProgressView().controlSize(.small)
                    .padding(20)
            } else {
                Text("\(shown.task) isn't on \(shown.project)'s board any more: it was archived.")
                    .foregroundStyle(theme.muted)
                    .padding(20)
            }
            theme.line.frame(height: 1)
            footer(detail)
        }
        .frame(width: 560)
        .guardingLinks()
        .task(id: watched.map { "\($0.task.status) \($0.task.updated) \($0.partial)" }) {
            await fetch(watched)
        }
    }

    // MARK: parts

    private func header(_ d: TaskDetail) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(d.task.ref)
                    .font(theme.mono(.title3, .medium))
                    .foregroundStyle(theme.action)
                Text(d.task.title)
                    .font(theme.title(.title2, .heavy))
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                Spacer(minLength: 8)
                Text(Self.words(d.task.status))
                    .font(theme.mono(.caption, .medium))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(theme.cardTint, in: Capsule())
                    .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
            }
            Text(facts(d))
                .font(theme.mono(.footnote))
                .foregroundStyle(theme.muted)
            TaskOutcome(project: d.project, task: d.task.ref)
        }
        .padding(20)
    }

    @ViewBuilder
    private func content(_ d: TaskDetail) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            if let on = d.blockedOn {
                callout(on.isEmpty ? "No note says. Ask the coordinator in its conversation." : on,
                        label: "Blocked on", symbol: "hand.raised.fill", colour: theme.attention, faint: on.isEmpty)
            }
            if let ship = d.ship {
                shipRow(ship, url: prURL(d))
            }
            if let checks = d.checks {
                section("How to check") {
                    if checks.isEmpty {
                        Text("The report doesn't say. Ask the coordinator in its conversation.")
                            .foregroundStyle(theme.muted)
                    } else {
                        ForEach(Array(checks.enumerated()), id: \.offset) { _, c in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text("•").foregroundStyle(theme.muted)
                                MarkdownView(text: c)
                            }
                        }
                    }
                }
            }
            if !d.task.steps.isEmpty {
                section("Steps · \(d.stepsDone)") {
                    ForEach(d.task.steps, id: \.n) { s in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Image(systemName: s.done ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(s.done ? theme.good : theme.muted)
                                .accessibilityLabel(s.done ? "done" : "to do")
                            Text("\(s.n). \(s.text)")
                                .foregroundStyle(s.done ? theme.muted : theme.ink)
                                .fixedSize(horizontal: false, vertical: true)
                                .textSelection(.enabled)
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            }
            if !d.notes.isEmpty {
                section("Notes") { MarkdownView(text: d.notes) }
            } else if d.partial {
                if let fetchError {
                    Text("Its notes and steps didn't load: \(fetchError)")
                        .foregroundStyle(theme.attention)
                } else if app.canGetTask {
                    ProgressView().controlSize(.small)
                } else {
                    Text("This tm doesn't send a board task's notes and steps: update it with tm update.")
                        .foregroundStyle(theme.muted)
                }
            }
        }
        .font(theme.body(.body))
    }

    private func footer(_ d: TaskDetail?) -> some View {
        HStack(spacing: 8) {
            if let d, let th = d.thread {
                Button("Show Thread") {
                    dismiss()
                    app.select(.card(th, in: d.project))
                }
                .help("Shows \(th.record.id) in the window")
            }
            if let d {
                askButton(d)
            }
            Spacer()
            Button("Close") { dismiss() }
                .keyboardShortcut(.cancelAction)
            if let d, TaskActs.acts(d.task.status) {
                TaskActs(project: d.project, task: d.task.ref, status: d.task.status)
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 14)
    }

    /// The TUI's c on a task: the project's coordinator, started when
    /// none runs, its composer holding the task for the user's question.
    private func askButton(_ d: TaskDetail) -> some View {
        let project = app.project(d.project)
        let running = app.coordinatorSession(d.project) != nil
        return VStack(alignment: .leading, spacing: 4) {
            Button(asking ? "Opening…" : "Ask the Coordinator") {
                ask(d)
            }
            .disabled(asking || (!running && project?.active != true))
            .help(running || project?.active == true
                  ? "Opens \(d.project)'s coordinator with \(d.task.ref) in its composer, for your question"
                  : "The project is inactive: activate it to start its coordinator")
            if let askError {
                Text(askError)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
            }
        }
    }

    private func ask(_ d: TaskDetail) {
        asking = true
        askError = nil
        Task {
            do {
                try await app.askCoordinator(about: d.task, project: d.project)
                dismiss()
            } catch {
                askError = Dashboard.words(error)
            }
            asking = false
        }
    }

    private func shipRow(_ ship: TaskDetail.Ship, url: URL?) -> some View {
        let (symbol, colour): (String, Color) = switch ship.state {
        case .merged: ("checkmark.circle.fill", theme.good)
        case .open: ("arrow.triangle.pull", theme.action)
        case .closed: ("xmark.circle.fill", theme.attention)
        case .unknown: ("arrow.triangle.pull", theme.muted)
        }
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: symbol)
                .foregroundStyle(colour)
                .accessibilityHidden(true)
            if let url {
                Link(ship.text, destination: url)
                    .help(url.absoluteString)
            } else {
                Text(ship.text)
            }
        }
        .font(theme.body(.body, .medium))
        .foregroundStyle(ship.state == .closed ? theme.attention : theme.ink)
    }

    private func callout(_ text: String, label: String, symbol: String, colour: Color, faint: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: symbol)
                .foregroundStyle(colour)
                .accessibilityHidden(true)
            (Text("\(label): ").bold() + Text(text))
                .foregroundStyle(faint ? theme.muted : theme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(colour.opacity(0.12), in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(colour.opacity(theme.contrast ? 1 : 0.3), lineWidth: 1))
        .accessibilityElement(children: .combine)
    }

    private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title.uppercased())
                .font(theme.mono(.caption2, .medium))
                .tracking(1)
                .foregroundStyle(theme.muted)
            content()
        }
    }

    // MARK: words

    /// "web · thread t-0713 · owner me".
    private func facts(_ d: TaskDetail) -> String {
        var f = [d.project]
        if !d.task.thread.isEmpty { f.append("thread \(d.task.thread)") }
        if !d.task.owner.isEmpty { f.append("owner \(d.task.owner)") }
        if !d.task.updated.isEmpty { f.append("updated \(d.task.updated)") }
        return f.joined(separator: " · ")
    }

    /// The PR's link: the report's PR line, a web link only.
    private func prURL(_ d: TaskDetail) -> URL? {
        guard let s = d.thread?.reportPr, let u = URL(string: s), SafeMarkdown.allowed(u) else { return nil }
        return u
    }

    static func words(_ status: String) -> String {
        switch status {
        case "ready": "on deck"
        case "started": "in motion"
        case "open": "backlog"
        default: status
        }
    }

    // MARK: task.get

    /// Reads a board task whole (task.get), which the watch sends
    /// without its notes and steps.
    /// Reads the task whole when the watch has it without its notes
    /// and steps, or has it nowhere (a done task, from the board's Done
    /// fold).
    private func fetch(_ d: TaskDetail?) async {
        guard d?.partial ?? true, app.canGetTask else { return }
        do {
            full = try await app.getTask(project: shown.project, task: shown.task)
            fetchError = nil
        } catch {
            full = nil
            fetchError = Dashboard.words(error, task: shown.task)
        }
    }
}

extension AppModel {
    /// Shows task of project in the task view, over the window.
    func showTask(project: String, task: String) {
        palette = false
        taskShown = TaskShown(project: project, task: task)
        showWindow?()
    }

    /// Whether this tm reads a task whole (task.get, app API 14).
    var canGetTask: Bool {
        if case .connected(let h) = phase { return Compatibility.getsTasks(h) }
        return false
    }
}
