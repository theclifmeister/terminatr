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
    @State private var prioritizing = false
    @State private var priorityError: String?

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
                PriorityTag(d.task.priority)
                Text(TaskDetail.words(d.task.status))
                    .font(theme.mono(.caption, .medium))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(theme.cardTint, in: Capsule())
                    .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
            }
            Text(d.facts)
                .font(theme.mono(.footnote))
                .foregroundStyle(theme.muted)
            if let priorityError {
                Text(priorityError)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
            }
            TaskOutcome(project: d.project, task: d.task.ref)
        }
        .padding(20)
    }

    private func content(_ d: TaskDetail) -> some View {
        TaskDetailView(d) {
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
            if let d, app.canSetPriority, Priority.settable(d.task.status) {
                PriorityMenu(task: d.task.ref, current: d.task.priority, busy: prioritizing) { p in
                    setPriority(d, p)
                }
                .fixedSize()
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

    /// Sets the task's priority (task.priority, the TUI's P); the
    /// watch brings the board's new order.
    private func setPriority(_ d: TaskDetail, _ p: String) {
        prioritizing = true
        priorityError = nil
        Task {
            do {
                _ = try await app.setPriority(project: d.project, task: d.task.ref, priority: p)
            } catch {
                priorityError = Dashboard.words(error, task: d.task.ref)
            }
            prioritizing = false
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

    /// Whether this tm sets a task's priority (task.priority, app API 42).
    var canSetPriority: Bool {
        if case .connected(let h) = phase { return Priority.sets(h) }
        return false
    }

    /// Sets project's task's priority: must, should, could, or "" for
    /// none (task.priority, the TUI's P). False when it had it already.
    func setPriority(project: String, task: String, priority: String) async throws -> Bool {
        try await connected().call(AppAPI.Methods.taskPriority,
                                   AppAPI.TaskPriorityParams(project: project, task: task, priority: priority)).changed
    }
}
