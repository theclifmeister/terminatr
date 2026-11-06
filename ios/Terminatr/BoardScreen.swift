import SwiftUI
import TerminatrKit
import TerminatrUI

/// A project's board, as the Mac dashboard's: on deck shown, the backlog
/// and the done tasks folded in one section (task.done, read when the
/// fold opens), and a filter (the TUI's / on the Tasks tab) that opens
/// the folds while it has words. A task opens its detail. At the write level + adds a
/// task (NewTaskSheet; -newtask YES opens it at launch). -filter WORDS fills
/// the filter at launch (the Simulator's screenshots).
struct BoardScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let slug: String
    @State private var filter = UserDefaults.standard.string(forKey: "filter") ?? ""
    @State private var unfolded: Set<String> = []
    /// The board's done tasks, read when the Done fold opens and again
    /// when the board's done count changes; nil until read.
    @State private var done: [AppAPI.Task]?
    @State private var doneError: String?
    @State private var adding = UserDefaults.standard.bool(forKey: "newtask")

    var body: some View {
        PanelPage {
            if let p = model.project(slug) {
                groups(p)
            }
        }
        // In the screen, under its title (T928, FilterField).
        .safeAreaInset(edge: .top, spacing: 0) { FilterField(prompt: "Filter by id or title", text: $filter) }
        .refreshable { await model.refresh() }
        .pageBar("Board") {
            if model.can(AppAPI.Methods.taskAdd.name) {
                Button("New Task", systemImage: "plus") { adding = true }
            }
        }
        .keepsPlace(route: Route.board(slug))
        .kept($filter, "filter \(slug)")
        .kept($unfolded, "folds \(slug)")
        .kept($adding, "adding \(slug)")
        .sheet(isPresented: $adding) { NewTaskSheet(project: slug) }
        .backdrop()
    }

    @ViewBuilder
    private func groups(_ p: AppAPI.Project) -> some View {
        let filtering = !filter.trimmingCharacters(in: .whitespaces).isEmpty
        let deck = Dashboard.filtered(Dashboard.onDeck(p), by: filter)
        let backlog = Dashboard.filtered(Dashboard.backlog(p), by: filter)
        let doneCount = model.listsDone ? Dashboard.doneCount(p) : 0
        let doneOpen = doneCount > 0 && (filtering || unfolded.contains("done"))
        let doneShown = Dashboard.filtered(done ?? [], by: filter)
        if !deck.isEmpty {
            SectionHeading("On deck · \(deck.count)")
            PanelList { rows(deck) }
        }
        // The two folds share a section: they read as a pair, a row
        // apart, not a section's gap (T856).
        let doneFold = doneCount > 0 && (!filtering || !doneShown.isEmpty || done == nil)
        if !backlog.isEmpty || doneFold {
            PanelList {
                if !backlog.isEmpty {
                    FoldRow(title: "Backlog · \(backlog.count)", open: open("backlog", filtering))
                    if open("backlog", filtering).wrappedValue { rows(backlog) }
                }
                if doneFold {
                    FoldRow(title: "Done · \(filtering && done != nil ? doneShown.count : doneCount)", open: open("done", filtering))
                    .task(id: DoneRead(project: slug, open: doneOpen, count: doneCount, generation: model.generation)) {
                        guard doneOpen else { return }
                        do {
                            done = try await model.doneTasks(slug)
                            doneError = nil
                        } catch {
                            doneError = Dashboard.words(error)
                        }
                    }
                    if open("done", filtering).wrappedValue {
                        if let doneError {
                            ListRow { Failure(text: doneError) }
                        } else if done == nil {
                            ListRow { ProgressView() }
                        } else {
                            rows(doneShown)
                        }
                    }
                }
            }
            .padding(.top, Spacing.item)
        }
        if filtering && deck.isEmpty && backlog.isEmpty && (doneCount == 0 || (done != nil && doneShown.isEmpty)) {
            Text("No task on the board matches “\(filter.trimmingCharacters(in: .whitespaces))”.")
                .font(theme.sized(Flat.text))
                .foregroundStyle(theme.muted)
        } else if !filtering && deck.isEmpty && backlog.isEmpty && doneCount == 0 {
            Text("Nothing on the board: on deck, backlog and done tasks show here.")
                .font(theme.sized(Flat.text))
                .foregroundStyle(theme.muted)
        }
    }

    /// A fold's state: open while the filter has words, else as the user
    /// left it.
    private func open(_ key: String, _ filtering: Bool) -> Binding<Bool> {
        Binding {
            filtering || unfolded.contains(key)
        } set: { on in
            if on { unfolded.insert(key) } else { unfolded.remove(key) }
        }
    }

    private func rows(_ ts: [AppAPI.Task]) -> some View {
        ForEach(ts) { t in
            RowLink(value: Route.task(slug, ref: t.ref)) {
                let line = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 2))
                    : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 8))
                VStack(alignment: .leading, spacing: 2) {
                    line {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(t.ref)
                                .font(theme.monoSized(12.5))
                                .foregroundStyle(theme.muted)
                                .frame(minWidth: 34 * theme.scale, alignment: .leading)
                            if t.status == "done" {
                                FinishedTag(task: t)
                            } else {
                                PriorityTag(t.priority)
                            }
                        }
                        Text(t.title)
                            .font(theme.sized(Flat.text))
                            .foregroundStyle(theme.ink)
                            .lines(2)
                    }
                    // Waits-for (T1049): muted while a link is unmet, and on
                    // the task they wait for; nothing once it is met.
                    let waits = Waits.words(t)
                    if !waits.isEmpty {
                        Text(waits)
                            .font(theme.sized(Flat.label))
                            .foregroundStyle(theme.muted)
                            .lines(1)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    /// When the done tasks are read again: the fold opens, the board's
    /// done count changes while it is open, or the link comes back.
    private struct DoneRead: Equatable {
        let project: String
        let open: Bool
        let count: Int
        let generation: Int
    }
}

/// A task in full, as the Mac's task view shows it (TaskDetail): its
/// status and thread, what it is blocked on, its PR and how to check it
/// in review, its steps and its notes. A board task's notes and steps,
/// and a done task as a whole, come from task.get. Its thread's
/// conversation opens from here; at the write level an open, on-deck or
/// blocked task has Delegate… (DelegateButton), and one not done or
/// dropped its Priority menu.
struct TaskScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    let ref: String
    @State private var full: AppAPI.Task?
    @State private var fetchError: String?
    @State private var prioritizing = false
    @State private var priorityError: String?

    var body: some View {
        let project = model.project(slug)
        let detail = project.flatMap { TaskDetail.of(ref, in: $0, fetched: full) }
        let watched = project.flatMap { TaskDetail.of(ref, in: $0) }
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if let detail {
                    header(detail)
                    if let th = detail.thread {
                        PlainRouteLink(value: Route.session(SessionRef(project: slug, thread: th.record.id))) {
                            Label("Show thread \(th.record.id)", systemImage: "bubble.left.and.text.bubble.right")
                                .font(theme.body(.body, .medium))
                        }
                    }
                    if model.can(AppAPI.Methods.taskDelegate.name), Palette.delegable(detail.task.status) {
                        DelegateButton(project: slug, task: ref, status: detail.task.status)
                    }
                    if model.setsPriority, Priority.settable(detail.task.status) {
                        PriorityMenu(task: ref, current: detail.task.priority, busy: prioritizing) { setPriority($0) }
                            .font(theme.body(.body, .medium))
                            .buttonStyle(.secondary)
                        if let priorityError {
                            Failure(text: priorityError)
                        }
                    }
                    TaskDetailView(detail) {
                        if let fetchError {
                            Failure(text: "Its notes and steps didn't load: \(fetchError)")
                        } else if model.getsTasks {
                            ProgressView()
                        } else {
                            Text("This tm doesn't send a board task's notes and steps: update it with tm update.")
                                .foregroundStyle(theme.muted)
                        }
                    }
                } else if let fetchError {
                    Failure(text: fetchError)
                } else if model.getsTasks {
                    ProgressView()
                } else {
                    Text("\(ref) isn't on \(slug)'s board any more: it was archived.")
                        .foregroundStyle(theme.muted)
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task(id: TaskRead(watched: watched.map { "\($0.task.status) \($0.task.updated) \($0.partial)" }, generation: model.generation)) {
            await fetch(watched)
        }
        .refreshable { await model.refresh() }
        .pageBar(ref)
        .backdrop()
    }

    private func header(_ d: TaskDetail) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(d.task.ref)
                    .font(theme.mono(.title3, .medium))
                    .foregroundStyle(theme.action)
                Spacer()
                PriorityTag(d.task.priority)
                Text(TaskDetail.words(d.task.status))
                    .font(theme.mono(.caption, .medium))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .overlay(RoundedRectangle(cornerRadius: Radius.chip).strokeBorder(theme.line, lineWidth: 1))
            }
            Text(d.task.title)
                .font(theme.title(.title2, .heavy))
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            Text(d.facts)
                .font(theme.mono(.footnote))
                .foregroundStyle(theme.muted)
        }
    }

    /// Sets the task's priority (task.priority); the watch brings the
    /// board's new order.
    private func setPriority(_ p: String) {
        prioritizing = true
        priorityError = nil
        Task {
            do {
                _ = try await model.setPriority(project: slug, task: ref, priority: p)
            } catch {
                priorityError = Dashboard.words(error, task: ref)
            }
            prioritizing = false
        }
    }

    private struct TaskRead: Equatable {
        let watched: String?
        let generation: Int
    }

    /// Reads the task whole when the watch has it without its notes and
    /// steps, or has it nowhere (a done task, from the Done fold).
    private func fetch(_ d: TaskDetail?) async {
        guard d?.partial ?? true, model.getsTasks else { return }
        do {
            full = try await model.getTask(project: slug, task: ref)
            fetchError = nil
        } catch {
            full = nil
            fetchError = Dashboard.words(error, task: ref)
        }
    }
}

/// When a done task finished, in a row where a live task has its priority
/// tag; nothing when nothing dates it.
struct FinishedTag: View {
    @Environment(\.theme) private var theme
    let task: AppAPI.Task

    var body: some View {
        let label = Dashboard.finished(task.doneAt)
        if !label.isEmpty {
            Text(label)
                .font(theme.mono(.caption2))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .fixedSize()
                .accessibilityLabel("finished \(label)")
        }
    }
}
