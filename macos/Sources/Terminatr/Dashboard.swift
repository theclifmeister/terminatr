import SwiftUI
import TerminatrKit
import TerminatrUI

/// A project in the window's detail (design T746, T748 milestone C, flat
/// since T1002): its tile, name and goal; its coordinator as the main row,
/// with Start when none runs; a slim strip of its board's counts; its threads, idle
/// and stopped ones folded; its designs, each opening in place to its
/// answers, questions and build (DesignsSection); the first of on deck,
/// the Board tab having the rest; and the inbox items for the user. Under
/// them its repositories. What needs the user, and answering it, is the
/// inspector's (ProjectFacts). An empty project offers a next step. A
/// thread's row shows its thread in the window.
struct ProjectDashboard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    /// The Board tab: the tasks alone. Otherwise the Overview: all else.
    var board = false
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
            VStack(alignment: .leading, spacing: 22) {
                if board {
                    boardPage
                } else {
                    overview
                }
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 22)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
    }

    /// The Board tab: the board's filter and its groups.
    @ViewBuilder
    private var boardPage: some View {
        let deck = Dashboard.onDeck(project), backlog = Dashboard.backlog(project)
        let doneCount = app.listsDone ? Dashboard.doneCount(project) : 0
        if !deck.isEmpty || !backlog.isEmpty || doneCount > 0 {
            BoardFilter(text: $filter)
            boardGroups(deck: deck, backlog: backlog, doneCount: doneCount)
        } else {
            Text("Nothing on the board yet: + adds a task.")
                .foregroundStyle(theme.muted)
        }
    }

    /// The Overview tab: what the page draws, with what acts in place.
    @ViewBuilder
    private var overview: some View {
        header
        let needs = app.needs(of: project.slug)
        let running = app.coordinatorSession(project.slug) != nil
        CoordinatorRow(project: project, attention: app.coordinator(of: project), busy: app.busy(coordinator: project), running: running)
        StatStrip(stats)
        if Dashboard.isEmpty(project, needs: needs.count) {
            NextStepCard(project: project, step: Dashboard.next(project, coordinatorRuns: running))
        }
        threads
        // The TUI's O: an outside agent session, adopted here.
        let outside = app.actsOnSidebar && project.active ? ProjectAsk.adoptable(app.sessions) : []
        if !outside.isEmpty {
            section("Agent sessions outside the projects · \(outside.count)") {
                AdoptableSessions(project: project, sessions: outside)
            }
        }
        if app.listsDesigns {
            DesignsSection(project: project)
        }
        onDeck
        let inbox = project.items.filter(\.forYou)
        if !inbox.isEmpty {
            section("Inbox · \(inbox.count) for you") {
                HairlineList {
                    ForEach(inbox.prefix(Self.inboxShown)) { it in
                        InboxLine(item: it)
                    }
                }
                if inbox.count > Self.inboxShown {
                    Text("\(inbox.count - Self.inboxShown) more in tm")
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.muted)
                }
            }
        }
        let details = ProjectOverview(project, coordinator: app.coordinatorSession(project.slug), needs: needs.count)
        if !details.repos.isEmpty {
            section("Repositories · \(details.repos.count)") {
                HairlineList {
                    ForEach(details.repos, id: \.path) { r in
                        ListRow {
                            HStack(alignment: .firstTextBaseline, spacing: 10) {
                                Text(r.path)
                                    .font(theme.mono(.subheadline))
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                                    .textSelection(.enabled)
                                    .help(r.path)
                                Spacer(minLength: 8)
                                if !r.note.isEmpty {
                                    Text(r.note)
                                        .font(theme.sized(Flat.label))
                                        .foregroundStyle(theme.attention)
                                        .lineLimit(1)
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    /// The strip's counts, as the inspector names them.
    private var stats: [(value: String, word: String)] {
        var s: [(value: String, word: String)] = [("\(Dashboard.onDeck(project).count)", "on deck"), ("\(Dashboard.backlog(project).count)", "backlog")]
        if app.listsDone { s.append(("\(Dashboard.doneCount(project))", "done")) }
        return s
    }

    /// Its threads in one list, by state in the groups' order, each
    /// saying its own; the idle and the stopped folded under it.
    @ViewBuilder
    private var threads: some View {
        let groups = Dashboard.groups(project.threads, attention: app.attention) { t in
            !t.record.session.isEmpty && app.sessions.contains { $0.id == t.record.session }
        }
        if !groups.isEmpty {
            section("Threads · \(project.threads.count)") {
                let shown = groups.filter { !$0.group.folded }.flatMap(\.threads)
                if !shown.isEmpty {
                    HairlineList { ForEach(shown, id: \.record.id) { threadCard($0) } }
                }
                ForEach(groups.filter(\.group.folded), id: \.group) { g in
                    fold(g.group.rawValue, g.threads.count, key: "\(g.group)") {
                        HairlineList { ForEach(g.threads, id: \.record.id) { threadCard($0) } }
                    }
                }
            }
        }
    }

    /// The first of on deck: the Board tab has them all, with their acts.
    @ViewBuilder
    private var onDeck: some View {
        let deck = Dashboard.onDeck(project)
        if !deck.isEmpty {
            section("On deck · \(deck.count)") {
                HairlineList {
                    ForEach(deck.prefix(Self.deckShown)) { BoardRow(project: project.slug, task: $0, acts: false) }
                }
                if deck.count > Self.deckShown {
                    Button("\(deck.count - Self.deckShown) more on the Board") { app.projectTab = .board }
                        .buttonStyle(.link)
                        .foregroundStyle(theme.action)
                        .font(theme.sized(Flat.label))
                }
            }
        }
    }

    static let inboxShown = 8
    static let deckShown = 3

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
                HairlineList { ForEach(deck) { BoardRow(project: project.slug, task: $0) } }
            }
        }
        if !backlog.isEmpty {
            fold("Backlog", backlog.count, key: filtering ? nil : "backlog") {
                HairlineList { ForEach(backlog) { BoardRow(project: project.slug, task: $0) } }
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
                    HairlineList { ForEach(doneShown) { BoardRow(project: project.slug, task: $0) } }
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
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
            .help("Show \(t.task?.ref ?? t.record.id) · \(t.record.id)\(t.record.agent.isEmpty ? "" : " · \(t.record.agent)")")
            .contextMenu { NewWindowButton(ref: SessionRef(project: project.slug, thread: t.record.id)) }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                NameTile(project.slug)
                Text(project.slug)
                    .font(theme.title(.display, .semibold))
                    .lineLimit(1)
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
                    .lineSpacing(3)
                    .frame(maxWidth: 560, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !project.error.isEmpty {
                Text(project.error)
                    .foregroundStyle(theme.attention)
            }
        }
    }

    private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            PaneLabel(title)
            content()
        }
    }

    /// A group under its label: with a key, folded until the user opens
    /// it (the key is kept while the dashboard shows).
    private func fold<C: View>(_ title: String, _ n: Int, key: String?, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            if let key {
                let open = unfolded.contains(key)
                Button {
                    if open { unfolded.remove(key) } else { unfolded.insert(key) }
                } label: {
                    HStack(spacing: 5) {
                        Image(systemName: "chevron.right")
                            .rotationEffect(.degrees(open ? 90 : 0))
                            .font(theme.body(.glyph, .semibold))
                        Text("\(title) · \(n)")
                            .font(theme.sized(Flat.label))
                    }
                    .foregroundStyle(theme.muted)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityValue(open ? "open" : "folded")
                if open { content() }
            } else {
                PaneLabel("\(title) · \(n)")
                content()
            }
        }
    }
}

/// The project's coordinator as the dashboard's main row (T1006, the
/// phone's LeadRow): its state, what it does or waits on, its context, and
/// Open while it runs, which a click on the row does too, else Wake or Start
/// (project.open, as tm project open). Save & Clear is the inspector's
/// footer's, and this row's menu's.
struct CoordinatorRow: View {
    @Environment(AppModel.self) private var app
    let project: AppAPI.Project
    let attention: Attention
    /// Its turn while it works (the activity mark, working · 3m).
    let busy: Busy?
    let running: Bool

    var body: some View {
        let ref = SessionRef(project: project.slug)
        LeadRow(.coordinator, state: attention, busy: busy,
                context: ContextUse.of(app.coordinatorSession(project.slug), hint: app.contextHint), dormant: !project.active) {
            if running {
                Button("Open") { app.select(.session(ref)) }
                    .help("Show the coordinator")
            } else if app.sleep(of: project.slug) != .awake {
                WakeButton(project: project.slug, title: "Wake")
            } else {
                StartButton(project: project)
            }
        }
        .onTapGesture { if running { app.select(.session(ref)) } }
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
        HairlineList {
            ForEach(sessions, id: \.id) { s in
                let a = ProjectAsk.adoptToggle(s, project: project.slug, items: project.items)
                ListRow {
                    HStack(spacing: 10) {
                        Text(s.id)
                            .font(theme.mono(.subheadline))
                            .foregroundStyle(theme.muted)
                        Text(ProjectAsk.adoptWhat(s))
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
            .buttonStyle(.flatPrimary)
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
        .font(theme.sized(Flat.label))
        .padding(.horizontal, 11)
        .frame(maxWidth: 320, minHeight: 34)
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1))
        .help("Shows the tasks whose id or title has these words, the done ones too")
    }
}

/// A task on the board: its id, title, priority and its action
/// (Delegate…); its words show it in the task view.
private struct BoardRow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    let task: AppAPI.Task
    /// With its action: the Board's, not the Overview's short list.
    var acts = true

    var body: some View {
        ListRow {
            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .center, spacing: 10) {
                    Button { app.showTask(project: project, task: task.ref) } label: {
                        HStack(alignment: .center, spacing: 10) {
                            Text(task.ref)
                                .font(theme.mono(.subheadline))
                                .foregroundStyle(theme.muted)
                            Text(task.title)
                                .lineLimit(1)
                            Spacer(minLength: 8)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help("Shows \(task.ref) in full: its steps and notes")
                    PriorityTag(task.priority)
                    if acts {
                        TaskActs(project: project, task: task.ref, status: task.status)
                    }
                }
                if acts {
                    TaskOutcome(project: project, task: task.ref)
                }
            }
        }
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
                .font(theme.title(.headline, .semibold))
            Text(detail)
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            switch step {
            case .activate:
                if app.controlsProjects {
                    Button("Activate") { app.ask(.activate(project: project.slug)) }
                        .buttonStyle(.flatPrimary)
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
                .buttonStyle(.flatPrimary)
            }
            if step != .activate {
                // The New Task sheet with a tm that adds tasks (app API
                // 7); before it, the command that does.
                if app.canAddTask {
                    HStack(spacing: 8) {
                        Text("Or add a task to its board yourself, for the coordinator to start:")
                            .font(theme.sized(Flat.label))
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
        .padding(.horizontal, 13)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1))
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
        ListRow {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Dot(colour: item.needsUser ? theme.attention : theme.idle)
                Text(item.subject.isEmpty ? item.kind : item.subject)
                    .lineLimit(1)
                Text(item.shownSummary)
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                Spacer(minLength: 6)
                Text(item.created, format: .relative(presentation: .named))
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .fixedSize()
            }
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
    /// Its pages (T886): the board shows under its sessions when that
    /// server shares them.
    var pages: PeerPages.State<AppAPI.PeerPages> = .notShared
    /// The Board tab: the board alone. Otherwise the Overview.
    var board = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                if board {
                    boardPart
                } else {
                    overviewPart
                }
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 22)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
        .opacity(project.stale ? 0.7 : 1)
    }

    @ViewBuilder
    private var overviewPart: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                NameTile(project.project)
                Text(project.project)
                    .font(theme.title(.display, .semibold))
                    .lineLimit(1)
                Chip(peer.name.isEmpty ? peer.alias : peer.name)
            }
            if !project.goal.isEmpty {
                Text(project.goal)
                    .foregroundStyle(theme.muted)
                    .lineSpacing(3)
                    .frame(maxWidth: 560, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Text(facts)
                .font(theme.sized(Flat.label))
                .foregroundStyle(theme.muted)
        }
        let sessions = project.sessions.compactMap { s in SessionRef(s, of: peer.alias).map { (s, $0) } }
        let others = sessions.filter { !$0.1.isCoordinator }
        // Its coordinator as the main row, as this server's project's.
        ForEach(sessions.filter(\.1.isCoordinator), id: \.0.id) { s, ref in
            LeadRow(ref.role ?? .coordinator, state: .of(peer: s)) {
                Button("Open") { app.select(.session(ref)) }
                    .help("Show the coordinator")
            }
            .onTapGesture { app.select(.session(ref)) }
            .contextMenu { NewWindowButton(ref: ref) }
        }
        if case .shared(let p) = pages {
            StatStrip(PeerPages.stats(p).map { (value: "\($0.0 ?? 0)", word: $0.1) })
        }
        if !others.isEmpty || sessions.isEmpty {
            VStack(alignment: .leading, spacing: 7) {
                PaneLabel("Sessions · \(others.count)")
                if others.isEmpty {
                    Text("Nothing runs there now.")
                        .foregroundStyle(theme.muted)
                } else {
                    HairlineList {
                        ForEach(others, id: \.0.id) { s, ref in
                            Button { app.select(.session(ref)) } label: {
                                PeerSessionCard(session: s, ref: ref)
                                    .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .contextMenu { NewWindowButton(ref: ref) }
                        }
                    }
                }
            }
        }
        // Its designs (T888), read-only, under the same switch.
        if app.listsDesigns {
            DesignsSection(peer: peer, project: project)
        }
        if case .shared(let p) = pages {
            VStack(alignment: .leading, spacing: 7) {
                PaneLabel("Details")
                OverviewView(ProjectOverview(p, project: project), showsGoal: false)
            }
        }
    }

    /// The Board tab: what it shares of its board.
    @ViewBuilder
    private var boardPart: some View {
        switch pages {
        case .shared(let p):
            StatStrip(PeerPages.stats(p).map { (value: "\($0.0 ?? 0)", word: $0.1) })
            PeerBoard(pages: p)
        case .loading:
            ProgressView().controlSize(.small)
        case .failed(let why):
            Text(why).foregroundStyle(theme.attention)
        case .notShared:
            Text(PeerPages.notShared(by: PeerPages.server(peer)))
                .foregroundStyle(theme.muted)
        }
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
        ListRow {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                if !session.task.isEmpty {
                    Text(session.task)
                        .font(theme.mono(.subheadline))
                        .foregroundStyle(theme.muted)
                }
                if let role = ref.role {
                    RoleLabel(role, points: Flat.text)
                } else {
                    Text(ref.thread)
                        .lineLimit(1)
                }
                if !session.current.isEmpty {
                    Text(session.current)
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 8)
                Text(session.asking ? "asks" : session.state)
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(session.asking || session.state == "blocked" ? theme.approval : theme.muted)
            }
        }
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
    /// A program's row (the programs design §11.1): its mark before its
    /// name, and its director's state in place of a coordinator's.
    var program = false
    /// A dim word after its name: a paired member's program ("· acme"),
    /// or why a member has no listing ("invited").
    var tag = ""
    /// Folded over a session that works (T985): the activity mark on its
    /// right; unfolded, the session's own row has it.
    var busy = false

    var body: some View {
        HStack(spacing: 9) {
            Text(ProjectTile.letters(name.split(separator: "/").last.map(String.init) ?? name))
                .font(theme.sized(10, .semibold))
                .foregroundStyle(theme.onAction)
                .frame(width: 20, height: 20)
                .background(theme.action, in: RoundedRectangle(cornerRadius: 4))
                .accessibilityHidden(true)
            Text(name)
                .font(theme.sized(13.5))
                .lineLimit(1)
            if program || !tag.isEmpty {
                Text(program ? (tag.isEmpty ? "program" : "program · \(tag)") : tag)
                    .font(theme.sized(12))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .layoutPriority(-1)
            }
            Spacer(minLength: 4)
            if busy {
                BusyMark(size: 6, colour: selected ? theme.ink : nil)
                    .frame(width: 11, height: 11)
            }
            // The sidebar's percent column (SidebarGrid), ending at its rows'.
            if needs > 0 || busy {
                Group {
                    if needs > 0 {
                        Text("\(needs)")
                            .font(theme.sized(12, .medium))
                            .padding(.horizontal, 5)
                            .background(theme.attention, in: RoundedRectangle(cornerRadius: 4))
                            .foregroundStyle(theme.onAttention)
                            .fixedSize()
                            .accessibilityLabel("\(needs) need you")
                    } else {
                        Color.clear.frame(height: 1)
                    }
                }
                .frame(width: CGFloat(SidebarGrid.percent) * theme.scale, alignment: .trailing)
            }
            Dot(colour: dot)
        }
        .padding(.horizontal, 8)
        .foregroundStyle(theme.ink)
        .help("\(program ? "Director" : "Coordinator"): \(state)\(busy ? " · working under it" : "")")
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(name), \(program ? "program, director" : "coordinator") \(state)\(busy ? ", working under it" : "")")
    }
}

/// A thread on the dashboard, one row: its dot, task, title and at its
/// end what it does, how far along and its PR. Its dot and words come
/// from its attention, so one that waits on you says so. Its id and agent
/// are its tooltip's, and the inspector's.
struct ThreadCard: View {
    @Environment(\.theme) private var theme
    let thread: AppAPI.Thread
    let attention: Attention
    /// Its turn while its agent works: the activity mark for its dot and
    /// how long the turn has run.
    var busy: Busy?

    var body: some View {
        let r = thread.record
        ListRow {
            HStack(spacing: 10) {
                StateDot(tone: attention.tone, busy: busy != nil, size: 7)
                if let task = thread.task {
                    Text(task.ref)
                        .font(theme.mono(.subheadline))
                        .foregroundStyle(theme.muted)
                }
                Text(r.title)
                    .lineLimit(1)
                Spacer(minLength: 8)
                HStack(spacing: 4) {
                    if let busy {
                        BusyLabel(busy: busy, capital: true)
                            .fixedSize()
                    }
                    Text(facts)
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(attention.waits ? theme.ink : theme.muted)
                        .lineLimit(1)
                }
                .frame(maxWidth: 340, alignment: .trailing)
                .layoutPriority(1)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// "Asks to run a command · 5 of 8 · PR #12".
    private var facts: String {
        var f = [busy == nil ? (attention.why.isEmpty ? attention.word : attention.why) : ""]
        if let s = thread.status {
            f.append(s.stepsTotal > 0 ? "\(min(s.stepsDone, s.stepsTotal)) of \(s.stepsTotal)" : "\(s.percent)%")
        }
        if thread.pr > 0 {
            f.append("PR #\(thread.pr)" + (thread.checks.isEmpty ? "" : " · \(thread.checks)"))
        }
        let words = f.filter { !$0.isEmpty }.joined(separator: " · ")
        return busy != nil && !words.isEmpty ? "· \(words)" : words.prefix(1).uppercased() + words.dropFirst()
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
                    .font(theme.title(.headline, .semibold))
                Text(why)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(theme.muted)
                    .textSelection(.enabled)
                    .frame(maxWidth: 440)
                HStack {
                    Button("Retry") { model.connect() }
                    .accessibilityLabel("Retry the connection to terminatr")
                        .buttonStyle(.flatPrimary)
                    SettingsLink { Text("Settings…") }
                }
            case .connected:
                if empty {
                    NoProjects()
                } else {
                    Text("Pick a project")
                        .font(theme.title(.headline, .semibold))
                }
            }
        }
        .padding(28)
    }
}
