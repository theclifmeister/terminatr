import SwiftUI
import TerminatrKit
import TerminatrUI

/// A project's board, as the Mac dashboard's: on deck shown, the backlog
/// and the done tasks folded (task.done, read when the fold opens), and
/// a filter (the TUI's / on the Tasks tab) that opens the folds while it
/// has words. A task opens its detail. Read only. -filter WORDS fills
/// the filter at launch (the Simulator's screenshots).
struct BoardScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    @State private var filter = UserDefaults.standard.string(forKey: "filter") ?? ""
    @State private var unfolded: Set<String> = []
    /// The board's done tasks, read when the Done fold opens and again
    /// when the board's done count changes; nil until read.
    @State private var done: [AppAPI.Task]?
    @State private var doneError: String?

    var body: some View {
        List {
            if let p = model.project(slug) {
                groups(p)
            }
        }
        .searchable(text: $filter, prompt: "Filter by id or title")
        .refreshable { await model.refresh() }
        .navigationTitle("Board")
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
            Section("On deck · \(deck.count)") { rows(deck) }
        }
        if !backlog.isEmpty {
            Section {
                DisclosureGroup("Backlog · \(backlog.count)", isExpanded: open("backlog", filtering)) { rows(backlog) }
            }
        }
        if doneCount > 0 && (!filtering || !doneShown.isEmpty || done == nil) {
            Section {
                DisclosureGroup("Done · \(filtering && done != nil ? doneShown.count : doneCount)", isExpanded: open("done", filtering)) {
                    if let doneError {
                        Failure(text: doneError)
                    } else if done == nil {
                        ProgressView()
                    } else {
                        rows(doneShown)
                    }
                }
            }
            .task(id: DoneRead(project: slug, open: doneOpen, count: doneCount, generation: model.generation)) {
                guard doneOpen else { return }
                do {
                    done = try await model.doneTasks(slug)
                    doneError = nil
                } catch {
                    doneError = Dashboard.words(error)
                }
            }
        }
        if filtering && deck.isEmpty && backlog.isEmpty && (doneCount == 0 || (done != nil && doneShown.isEmpty)) {
            Text("No task on the board matches “\(filter.trimmingCharacters(in: .whitespaces))”.")
                .foregroundStyle(theme.muted)
        } else if !filtering && deck.isEmpty && backlog.isEmpty && doneCount == 0 {
            Text("Nothing on the board: on deck, backlog and done tasks show here.")
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
            NavigationLink(value: Route.task(slug, ref: t.ref)) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(t.ref)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(theme.action)
                        .frame(minWidth: 40, alignment: .leading)
                    Text(t.title)
                        .font(theme.body(.body))
                        .lineLimit(2)
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
/// and a done task as a whole, come from task.get. Read only: its
/// thread's conversation opens from here.
struct TaskScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    let ref: String
    @State private var full: AppAPI.Task?
    @State private var fetchError: String?

    var body: some View {
        let project = model.project(slug)
        let detail = project.flatMap { TaskDetail.of(ref, in: $0, fetched: full) }
        let watched = project.flatMap { TaskDetail.of(ref, in: $0) }
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if let detail {
                    header(detail)
                    if let th = detail.thread {
                        NavigationLink(value: Route.session(SessionRef(project: slug, thread: th.record.id))) {
                            Label("Show thread \(th.record.id)", systemImage: "bubble.left.and.text.bubble.right")
                                .font(theme.body(.body, .medium))
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
        .navigationTitle(ref)
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
    }

    private func header(_ d: TaskDetail) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(d.task.ref)
                    .font(theme.mono(.title3, .medium))
                    .foregroundStyle(theme.action)
                Spacer()
                Text(TaskDetail.words(d.task.status))
                    .font(theme.mono(.caption, .medium))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(theme.cardTint, in: Capsule())
                    .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
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
