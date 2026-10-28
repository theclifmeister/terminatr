import SwiftUI
import TerminatrKit
import TerminatrUI

/// A project in the window's detail (design T746, T748 milestone C):
/// its goal; what needs the user, as cards that act in place (the
/// project's share of the one needs list, the tasks in review as their
/// own group); its coordinator, with Start when none runs; its threads
/// grouped by state, idle and stopped ones folded; its board, on deck
/// shown and the backlog folded (C2), each task with its action; and
/// the inbox items for the user. An empty project offers a next step.
/// A thread's card shows its thread in the window.
struct ProjectDashboard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    /// The folded groups the user opened: "idle", "stopped", "backlog",
    /// "done".
    @State private var unfolded: Set<String> = []
    /// The board's filter (the TUI's / on the Tasks tab): words found in
    /// a task's id or title. While it has some, the folds are open.
    @State private var filter = ""
    /// The board's done tasks (task.done), read when the Done fold
    /// opens and again when the board's done count changes; nil until
    /// read.
    @State private var done: [AppAPI.Task]?
    @State private var doneError: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                let needs = app.needs(of: project.slug)
                let running = app.coordinatorSession(project.slug) != nil
                if Dashboard.isEmpty(project, needs: needs.count) {
                    NextStepCard(project: project, step: Dashboard.next(project, coordinatorRuns: running))
                }
                let settled = app.settledLinks(project.slug)
                if !needs.isEmpty || !settled.isEmpty {
                    section("Needs you · \(needs.count)") {
                        CardGrid { ForEach(needs.filter { !$0.isReview }) { NeedCard(need: $0) } }
                        ForEach(settled, id: \.id) { LinkOutcomeRow(outcome: $0.outcome, settled: true) }
                        let review = needs.filter(\.isReview)
                        if !review.isEmpty {
                            label("Review · \(review.count)")
                            CardGrid { ForEach(review) { NeedCard(need: $0) } }
                        }
                    }
                }
                section("Coordinator") {
                    CardGrid { CoordinatorRow(project: project, attention: app.coordinator(of: project), busy: app.busy(coordinator: project), running: running) }
                }
                let groups = Dashboard.groups(project.threads, attention: app.attention) { t in
                    !t.record.session.isEmpty && app.sessions.contains { $0.id == t.record.session }
                }
                if !groups.isEmpty {
                    section("Threads · \(project.threads.count)") {
                        ForEach(groups, id: \.group) { g in
                            fold(g.group.rawValue, g.threads.count, key: g.group.folded ? "\(g.group)" : nil) {
                                CardGrid { ForEach(g.threads, id: \.record.id) { threadCard($0) } }
                            }
                        }
                    }
                }
                // The TUI's O: an outside agent session, adopted here.
                let outside = app.actsOnSidebar && project.active ? ProjectAsk.adoptable(app.sessions) : []
                if !outside.isEmpty {
                    section("Agent sessions outside the projects · \(outside.count)") {
                        AdoptableSessions(project: project, sessions: outside)
                    }
                }
                let deck = Dashboard.onDeck(project), backlog = Dashboard.backlog(project)
                let doneCount = app.listsDone ? Dashboard.doneCount(project) : 0
                if !deck.isEmpty || !backlog.isEmpty || doneCount > 0 {
                    section("Board") {
                        BoardFilter(text: $filter)
                        boardGroups(deck: deck, backlog: backlog, doneCount: doneCount)
                    }
                }
                let inbox = project.items.filter(\.forYou)
                if !inbox.isEmpty {
                    section("Inbox · \(inbox.count) for you") {
                        ForEach(inbox.prefix(Self.inboxShown)) { it in
                            InboxLine(item: it)
                        }
                        if inbox.count > Self.inboxShown {
                            Text("\(inbox.count - Self.inboxShown) more in tm")
                                .font(theme.body(.subheadline))
                                .foregroundStyle(theme.muted)
                        }
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 18)
            .padding(.bottom, 28)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
    }

    static let inboxShown = 8

    /// The board's groups, as the filter finds their tasks: on deck
    /// shown; the backlog and the done tasks folded (the TUI's b lists
    /// the done ones), open while the filter has words.
    @ViewBuilder
    private func boardGroups(deck: [AppAPI.Task], backlog: [AppAPI.Task], doneCount: Int) -> some View {
        let filtering = !filter.trimmingCharacters(in: .whitespaces).isEmpty
        let deck = Dashboard.filtered(deck, by: filter), backlog = Dashboard.filtered(backlog, by: filter)
        let doneOpen = doneCount > 0 && (filtering || unfolded.contains("done"))
        let doneShown = Dashboard.filtered(done ?? [], by: filter)
        if !deck.isEmpty {
            fold("On deck", deck.count, key: nil) {
                CardGrid { ForEach(deck) { BoardRow(project: project.slug, task: $0) } }
            }
        }
        if !backlog.isEmpty {
            fold("Backlog", backlog.count, key: filtering ? nil : "backlog") {
                CardGrid { ForEach(backlog) { BoardRow(project: project.slug, task: $0) } }
            }
        }
        if doneCount > 0 && (!filtering || !doneShown.isEmpty || done == nil) {
            fold("Done", filtering && done != nil ? doneShown.count : doneCount, key: filtering ? nil : "done") {
                if let doneError {
                    Text(doneError)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.attention)
                } else if done == nil {
                    ProgressView().controlSize(.small)
                } else {
                    CardGrid { ForEach(doneShown) { BoardRow(project: project.slug, task: $0) } }
                }
            }
        }
        if filtering && deck.isEmpty && backlog.isEmpty && (doneCount == 0 || (done != nil && doneShown.isEmpty)) {
            Text("No task on the board matches “\(filter.trimmingCharacters(in: .whitespaces))”.")
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.muted)
        }
        Color.clear.frame(height: 0)
            .task(id: DoneRead(project: project.slug, open: doneOpen, count: doneCount)) {
                guard doneOpen else { return }
                do {
                    done = try await app.doneTasks(project.slug)
                    doneError = nil
                } catch {
                    doneError = Dashboard.words(error)
                }
            }
    }

    /// When the board's done tasks are read again: the fold opens, or
    /// the board's done count changes while it is open.
    private struct DoneRead: Equatable {
        let project: String
        let open: Bool
        let count: Int
    }

    private func threadCard(_ t: AppAPI.Thread) -> some View {
        let sel = Selection.card(t, in: project.slug)
        // A button, so Full Keyboard Access reaches it (Tab, Space).
        return Button { app.select(sel) } label: {
            ThreadCard(thread: t, attention: app.attention(t), busy: app.busy(t))
                .contentShape(RoundedRectangle(cornerRadius: 14))
        }
        .buttonStyle(.plain)
            .help("Show \(t.task?.ref ?? t.record.id)")
            .contextMenu { NewWindowButton(ref: SessionRef(project: project.slug, thread: t.record.id)) }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(project.slug)
                    .font(theme.title(.display, .heavy))
                Spacer()
                // As S in the TUI's sidebar (app API 13).
                if app.controlsProjects {
                    let a = ProjectAsk.toggle(project, sessions: app.sessions)
                    Button(a.verb) { app.ask(a) }
                        .help(project.active ? "Stops its coordinator and threads, to resume when you activate it"
                                             : "Turns it on: its coordinator starts, and its threads resume")
                }
            }
            if !project.goal.isEmpty {
                Text(project.goal)
                    .foregroundStyle(theme.muted)
                    .lineLimit(2)
            }
            if let board = board {
                Text(board)
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
            }
            if !project.error.isEmpty {
                Text(project.error)
                    .foregroundStyle(theme.attention)
            }
        }
        .padding(.bottom, 4)
    }

    /// "2 need you · 3 in motion · 5 on deck", as the TUI's row; what
    /// needs you is the needs list's count, as the sidebar's badge.
    private var board: String? {
        let c = project.counts
        guard !c.isEmpty else { return nil }
        return "\(app.needs(of: project.slug).count) need you · \(c["in_motion"] ?? 0) in motion · \(c["on_deck"] ?? 0) on deck · \(c["backlog"] ?? 0) in the backlog"
    }

    private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title.uppercased())
                .font(theme.mono(.caption2, .medium))
                .tracking(1)
                .foregroundStyle(theme.muted)
                .padding(.top, 6)
            content()
        }
    }

    private func label(_ title: String) -> some View {
        Text(title.uppercased())
            .font(theme.mono(.label, .medium))
            .tracking(1)
            .foregroundStyle(theme.muted)
            .padding(.top, 2)
    }

    /// A group under its label: with a key, folded until the user opens
    /// it (the key is kept while the dashboard shows).
    @ViewBuilder
    private func fold<C: View>(_ title: String, _ n: Int, key: String?, @ViewBuilder _ content: () -> C) -> some View {
        if let key {
            let open = unfolded.contains(key)
            Button {
                if open { unfolded.remove(key) } else { unfolded.insert(key) }
            } label: {
                HStack(spacing: 5) {
                    Image(systemName: "chevron.right")
                        .rotationEffect(.degrees(open ? 90 : 0))
                        .font(theme.body(.glyph, .bold))
                    Text("\(title) · \(n)".uppercased())
                        .font(theme.mono(.label, .medium))
                        .tracking(1)
                }
                .foregroundStyle(theme.muted)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 2)
            .accessibilityValue(open ? "open" : "folded")
            if open { content() }
        } else {
            label("\(title) · \(n)")
            content()
        }
    }
}

/// The project's coordinator as the dashboard's row: its state as the
/// dot and tag, what it does or waits on, and Start when none runs
/// (project.open, as tm project open) or Open when it does.
struct CoordinatorRow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    let attention: Attention
    /// Its turn while it works (the activity mark, working · 3m).
    let busy: Busy?
    let running: Bool

    var body: some View {
        let ref = SessionRef(project: project.slug)
        HStack(alignment: .center, spacing: 8) {
            StateDot(tone: attention.tone, busy: busy != nil)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text("Coordinator")
                        .font(theme.title(.headline, .bold))
                    if let busy {
                        BusyLabel(busy: busy, colour: theme.colour(.working))
                    } else {
                        Text(attention.word)
                            .font(theme.mono(.caption))
                            .foregroundStyle(attention.waits ? theme.colour(attention.tone) : theme.muted)
                    }
                    if running, let ctx = ContextUse.of(app.coordinatorSession(project.slug), hint: app.contextHint) {
                        Text("context \(ctx.word)")
                            .font(theme.mono(.caption, ctx.tone == nil ? .regular : .semibold))
                            .foregroundStyle(ctx.tone.map { theme.colour($0) } ?? theme.muted)
                            .help(ctx.spoken)
                    }
                }
                if !attention.why.isEmpty {
                    Text(attention.why)
                        .font(theme.body(.callout, attention.waits ? .medium : .regular))
                        .foregroundStyle(attention.waits ? theme.colour(attention.tone) : theme.muted)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 8)
            if running {
                if app.controlsProjects {
                    Button("Save & Clear") { app.ask(.saveClear(project: project.slug)) }
                        .help("Asks the coordinator to save what it learned, then clears its conversation, as tm clear does")
                }
                Button("Open") { app.select(.session(ref)) }
                    .help("Show the coordinator")
            } else {
                StartButton(project: project)
            }
        }
        .padding(14)
        .frosted(RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        .contextMenu {
            NewWindowButton(ref: ref)
            ProjectMenu(project: project, coordinator: running)
        }
    }
}

/// The project's actions in a context menu, as the TUI's prefix+C and S
/// (app API 13): Save & Clear while its coordinator runs, and Activate
/// or Deactivate; and as its P, prefix+r and M (app API 22): Pin to Top
/// or Unpin, remote control on or off while its coordinator runs, and
/// Write to Linked Project… while a link of it sends. Nothing with an
/// older tm.
struct ProjectMenu: View {
    @Environment(AppModel.self) private var app
    let project: AppAPI.Project
    var coordinator: Bool
    /// Under a divider, after the menu's other items.
    var divided = true

    var body: some View {
        if app.controlsProjects || app.actsOnSidebar {
            if divided { Divider() }
        }
        if app.controlsProjects {
            if coordinator {
                Button("Save & Clear Coordinator…") { app.ask(.saveClear(project: project.slug)) }
            }
            let a = ProjectAsk.toggle(project, sessions: app.sessions)
            Button(a.asks ? "\(a.verb) \(project.slug)…" : "\(a.verb) \(project.slug)") { app.ask(a) }
        }
        if app.actsOnSidebar {
            Button(ProjectAsk.pinToggle(project).verb) { app.ask(.pinToggle(project)) }
        }
        if app.controlsRemote, coordinator, let c = app.coordinatorSession(project.slug) {
            let r = ProjectAsk.remoteToggle(c)
            Button("\(r.verb)…") { app.ask(r) }
        }
        if app.actsOnSidebar, !LinkMessageForm.writable(project).isEmpty {
            Button("Write to Linked Project…") { app.showLinkMessage(project) }
        }
    }
}

/// The agent sessions outside the projects (the TUI dashboard's OTHER
/// SESSIONS), on a project's dashboard, each with Adopt… (app API 22):
/// the TUI's O, asking this project's coordinator to adopt it as a
/// thread, or Withdraw… once asked.
struct AdoptableSessions: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    /// ProjectAsk.adoptable's.
    let sessions: [AppAPI.Session]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
                ForEach(sessions, id: \.id) { s in
                    let a = ProjectAsk.adoptToggle(s, project: project.slug, items: project.items)
                    HStack(spacing: 8) {
                        Text(s.id)
                            .font(theme.mono(.footnote, .medium))
                            .foregroundStyle(theme.action)
                        Text(ProjectAsk.adoptWhat(s))
                            .font(theme.body(.callout))
                            .lineLimit(1)
                        Spacer(minLength: 4)
                        Button("\(a.verb)…") { app.ask(a) }
                            .help(a.message)
                    }
                    .accessibilityElement(children: .combine)
                }
        }
    }
}

/// Starts the project's coordinator (project.open) and shows it; an
/// inactive project is refused, since opening never activates.
struct StartButton: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    var title = "Start"
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        VStack(alignment: .trailing, spacing: 4) {
            Button {
                start()
            } label: {
                if busy {
                    ProgressView().controlSize(.small)
                } else {
                    Text(title)
                }
            }
            .buttonStyle(.borderedProminent)
            .tint(theme.action)
            .disabled(busy || !project.active)
            .help(project.active ? "Starts the coordinator with the project's agent and model, as tm project open does"
                                 : app.controlsProjects ? "The project is inactive: Activate turns it on"
                                 : "The project is inactive: tm project activate \(project.slug) turns it on")
            if let error {
                Text(error)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
                    .multilineTextAlignment(.trailing)
            }
        }
    }

    private func start() {
        busy = true
        error = nil
        Task {
            do {
                try await app.startCoordinator(project.slug)
            } catch {
                self.error = Dashboard.words(error)
            }
            busy = false
        }
    }
}

/// The board's filter field: words that a task's id or title has (the
/// TUI's / on the Tasks tab); esc clears it.
private struct BoardFilter: View {
    @Environment(\.theme) private var theme
    @Binding var text: String

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "line.3.horizontal.decrease")
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            TextField("Filter tasks", text: $text)
                .textFieldStyle(.plain)
                .onExitCommand { text = "" }
                .accessibilityLabel("Filter the board's tasks")
            if !text.isEmpty {
                Button { text = "" } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.borderless)
                    .foregroundStyle(theme.muted)
                    .help("Clear the filter")
                    .accessibilityLabel("Clear the filter")
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .frame(maxWidth: 320)
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.edge, lineWidth: 1))
        .help("Shows the tasks whose id or title has these words, the done ones too")
    }
}

/// A task on the board: its id, title and its action (Delegate…); its
/// words show it in the task view.
private struct BoardRow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    let task: AppAPI.Task

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .center, spacing: 8) {
                Button { app.showTask(project: project, task: task.ref) } label: {
                    HStack(alignment: .center, spacing: 8) {
                        Text(task.ref)
                            .font(theme.mono(.subheadline, .medium))
                            .foregroundStyle(theme.action)
                            .frame(minWidth: 44, alignment: .leading)
                        Text(task.title)
                            .font(theme.body(.body))
                            .lineLimit(1)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Shows \(task.ref) in full: its steps and notes")
                Spacer(minLength: 8)
                TaskActs(project: project, task: task.ref, status: task.status, compact: true)
            }
            TaskOutcome(project: project, task: task.ref)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

/// An empty project's next step: turn it on, start its coordinator, or
/// tell the coordinator what to build.
private struct NextStepCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    let step: Dashboard.NextStep

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(headline)
                .font(theme.title(.title2, .heavy))
            Text(detail)
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            switch step {
            case .activate:
                if app.controlsProjects {
                    Button("Activate") { app.ask(.activate(project: project.slug)) }
                        .buttonStyle(.borderedProminent)
                        .tint(theme.action)
                } else {
                    CommandText("tm project activate \(project.slug)")
                }
            case .start:
                StartButton(project: project, title: "Start the Coordinator")
            case .message:
                Button("Message the Coordinator") {
                    app.select(.session(SessionRef(project: project.slug)))
                    app.focusComposer()
                }
                .buttonStyle(.borderedProminent)
                .tint(theme.action)
            }
            if step != .activate {
                // The New Task sheet with a tm that adds tasks (app API
                // 7); before it, the command that does.
                if app.canAddTask {
                    HStack(spacing: 8) {
                        Text("Or add a task to its board yourself, for the coordinator to start:")
                            .font(theme.body(.callout))
                            .foregroundStyle(theme.muted)
                        Button("New Task…") { app.showNewTask() }
                            .accessibilityLabel("New task for \(project.slug)")
                    }
                    .padding(.top, 6)
                } else {
                    Text("Or add a task to its board yourself, for the coordinator to start:")
                        .font(theme.body(.callout))
                        .foregroundStyle(theme.muted)
                        .padding(.top, 6)
                    CommandText(FirstRun.addTask(project.slug))
                }
            }
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .frosted(RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
    }

    private var headline: String {
        switch step {
        case .activate: "This project is inactive"
        case .start, .message: "Nothing here yet"
        }
    }

    private var detail: String {
        switch step {
        case .activate: app.controlsProjects ? "Its coordinator and threads don't run while it is inactive. Turn it on to start them:"
            : "Its coordinator and threads don't run while it is inactive. Turn it on in a terminal:"
        case .start: "Start the coordinator and tell it what to build: it fills the board and starts threads for the tasks."
        case .message: "Tell the coordinator what to build: it fills the board and starts threads for the tasks."
        }
    }
}

/// An item of the project's inbox: a thread's report, a link's message.
private struct InboxLine: View {
    @Environment(\.theme) private var theme
    let item: AppAPI.Item

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle()
                .fill(item.needsUser ? theme.attention : theme.idle)
                .frame(width: 6, height: 6)
                .accessibilityHidden(true)
            Text(item.subject.isEmpty ? item.kind : item.subject)
                .font(theme.body(.callout, .medium))
                .lineLimit(1)
            Text(item.shownSummary)
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
            Spacer(minLength: 6)
            Text(item.created, format: .relative(presentation: .named))
                .font(theme.mono(.caption))
                .foregroundStyle(theme.muted)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A paired server's shared project in the detail: what peer.list says
/// of it, and its coordinator and threads, each shown in the window.
struct RemoteProjectDashboard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let peer: AppAPI.Peer
    let project: AppAPI.PeerProject

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(peer.name.isEmpty ? peer.alias : peer.name)
                            .font(theme.mono(.subheadline, .medium))
                            .padding(.horizontal, 7)
                            .padding(.vertical, 1)
                            .background(theme.cardTint, in: Capsule())
                        Text(project.project)
                            .font(theme.title(.display, .heavy))
                    }
                    if !project.goal.isEmpty {
                        Text(project.goal)
                            .foregroundStyle(theme.muted)
                            .lineLimit(2)
                    }
                    Text(facts)
                        .font(theme.mono(.footnote))
                        .foregroundStyle(theme.muted)
                }
                if project.sessions.isEmpty {
                    Text("Nothing runs there now.")
                        .foregroundStyle(theme.muted)
                }
                CardGrid {
                    ForEach(project.sessions.compactMap { s in SessionRef(s, of: peer.alias).map { (s, $0) } }, id: \.0.id) { s, ref in
                        Button { app.select(.session(ref)) } label: {
                            PeerSessionCard(session: s, ref: ref)
                                .contentShape(RoundedRectangle(cornerRadius: 14))
                        }
                        .buttonStyle(.plain)
                            .contextMenu { NewWindowButton(ref: ref) }
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 18)
            .padding(.bottom, 28)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
        .opacity(project.stale ? 0.7 : 1)
    }

    private var facts: String {
        var f = ["paired", project.threads == 1 ? "1 thread" : "\(project.threads) threads"]
        if project.needsYou > 0 { f.append("\(project.needsYou) need you") }
        if project.paused { f.append("paused") }
        if project.stale { f.append("stale") }
        if peer.state != "up" { f.append("offline") }
        return f.joined(separator: " · ")
    }
}

private struct PeerSessionCard: View {
    @Environment(\.theme) private var theme
    let session: AppAPI.PeerSession
    let ref: SessionRef

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if !session.task.isEmpty {
                Text(session.task)
                    .font(theme.mono(.callout, .medium))
                    .foregroundStyle(theme.action)
            }
            Text(ref.isCoordinator ? "Coordinator" : ref.thread)
                .font(theme.title(.headline, .bold))
            if !session.current.isEmpty {
                Text(session.current)
                    .font(theme.body(.callout))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            Text(session.asking ? "asks" : session.state)
                .font(theme.mono(.caption))
                .foregroundStyle(session.asking || session.state == "blocked" ? theme.approval : theme.muted)
        }
        .padding(14)
        .frosted(RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

/// A project's one line in the sidebar: its coordinator's state as the
/// dot, its slug, and what of the needs list is its as the badge.
struct ProjectRow: View {
    @Environment(\.theme) private var theme
    /// Its slug: this server's project's, or a paired server's.
    let name: String
    let dot: Color
    /// The coordinator's state, for VoiceOver and the tooltip.
    let state: String
    let needs: Int
    let selected: Bool

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(dot)
                .overlay(Circle().strokeBorder(selected ? theme.onAction.opacity(0.8) : .clear, lineWidth: 1))
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            Text(name)
                .font(theme.body(.body, .semibold))
                .lineLimit(1)
            Spacer(minLength: 4)
            if needs > 0 {
                Text("\(needs)")
                    .font(theme.mono(.caption, .medium))
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(theme.attention, in: Capsule())
                    .foregroundStyle(theme.onAttention)
                    .accessibilityLabel("\(needs) need you")
            }
        }
        .padding(.vertical, 2)
        .foregroundStyle(selected ? theme.onAction : theme.ink)
        .help("Coordinator: \(state)")
        .accessibilityElement(children: .combine)
        .accessibilityValue("coordinator \(state)")
    }
}

/// A thread on the dashboard: its dot, tag and one line of why come
/// from its attention, so one that waits on you says so.
struct ThreadCard: View {
    @Environment(\.theme) private var theme
    let thread: AppAPI.Thread
    let attention: Attention
    /// Its turn while its agent works: the activity mark for its dot and
    /// how long the turn has run.
    var busy: Busy?

    var body: some View {
        let r = thread.record, s = thread.status
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                StateDot(tone: attention.tone, busy: busy != nil)
                if let task = thread.task {
                    Text(task.ref)
                        .font(theme.mono(.callout, .medium))
                        .foregroundStyle(theme.action)
                }
                Text(r.title)
                    .font(theme.title(.headline, .bold))
                    .lineLimit(1)
                Spacer(minLength: 8)
                if let busy { BusyLabel(busy: busy, colour: theme.colour(.working)).fixedSize() }
                Text(r.id)
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
                    .textSelection(.enabled)
            }
            if attention.waits {
                Label(attention.why, systemImage: "exclamationmark.bubble")
                    .font(theme.body(.callout, .medium))
                    .foregroundStyle(theme.colour(attention.tone))
                    .lineLimit(2)
            } else if !attention.why.isEmpty {
                Text(attention.why)
                    .font(theme.body(.callout))
                    .foregroundStyle(theme.muted)
                    .lineLimit(2)
            }
            HStack(spacing: 10) {
                if let s {
                    Meter(fraction: Double(s.percent) / 100)
                        .frame(width: 120, height: 5)
                    Text(progress(s))
                        .font(theme.mono(.caption))
                        .foregroundStyle(theme.muted)
                }
                Spacer(minLength: 0)
                Text(facts)
                    .font(theme.mono(.caption))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
        }
        .padding(14)
        .frosted(RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }

    private func progress(_ s: AppAPI.ThreadStatus) -> String {
        s.stepsTotal > 0 ? "\(s.percent)% · step \(min(s.stepsDone + 1, s.stepsTotal)) of \(s.stepsTotal)" : "\(s.percent)%"
    }

    private var facts: String {
        var f = [busy == nil ? attention.word : "", thread.record.agent]
        if thread.pr > 0 {
            f.append("PR #\(thread.pr)" + (thread.checks.isEmpty ? "" : " · \(thread.checks)"))
        }
        return f.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

struct Meter: View {
    @Environment(\.theme) private var theme
    let fraction: Double

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(theme.line)
                Capsule()
                    .fill(LinearGradient(colors: theme.meter, startPoint: .leading, endPoint: .trailing))
                    .frame(width: g.size.width * min(max(fraction, 0), 1))
            }
        }
        .accessibilityHidden(true)
    }
}

/// The detail pane with no project to show: connecting, failed (with
/// why), or a server with none.
struct Placeholder: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let phase: AppModel.Phase
    let empty: Bool

    var body: some View {
        VStack(spacing: 12) {
            switch phase {
            case .connecting:
                ProgressView()
                Text("Starting tm app bridge…").foregroundStyle(theme.muted)
            case .failed(let why):
                Text("Can't reach terminatr")
                    .font(theme.title(.largeTitle, .heavy))
                Text(why)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(theme.muted)
                    .textSelection(.enabled)
                    .frame(maxWidth: 440)
                HStack {
                    Button("Retry") { model.connect() }
                    .accessibilityLabel("Retry the connection to terminatr")
                        .buttonStyle(.borderedProminent)
                        .tint(theme.action)
                    SettingsLink { Text("Settings…") }
                }
            case .connected:
                if empty {
                    NoProjects()
                } else {
                    Text("Pick a project")
                        .font(theme.title(.largeTitle, .heavy))
                }
            }
        }
        .padding(28)
        .frosted(RoundedRectangle(cornerRadius: 18))
        .padding(40)
    }
}
