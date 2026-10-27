import AppKit
import Observation
import TerminatrKit

/// What the window and the menu bar show: the bridge's state, every
/// project (projects.watch {detail, lean}: an inactive one but the
/// shown as its slug and settings alone), the sessions and the mods' notices
/// (session.list) and the paired servers' shared projects (peer.list),
/// and what of them needs the user; and what the window has selected
/// (nav), which the menu bar, notifications and cards set. The session
/// views reach the server through bridge.
@MainActor
@Observable
final class AppModel {
    enum Phase: Equatable {
        case connecting
        case connected(AppAPI.Hello)
        case failed(String)
    }

    private(set) var phase = Phase.connecting {
        didSet { showBadge() }
    }
    /// The hello of the last bridge that connected: kept while the
    /// bridge is down, so the window keeps what it showed (lost).
    private(set) var hello: AppAPI.Hello?
    /// Whether this app and the bridge's tm fit (their app API and
    /// releases); the toolbar and the menu bar say which is behind.
    private(set) var compatibility = Compatibility.ok
    private(set) var projects: [AppAPI.Project] = []
    private(set) var sessions: [AppAPI.Session] = []
    private(set) var notices: [AppAPI.Notice] = []
    private(set) var peers: [AppAPI.Peer] = []
    /// What needs the user, once projects and sessions both came: the
    /// one list every count shows (the Dock badge too).
    private(set) var needs: [Need] = [] {
        didSet {
            if needs.count != oldValue.count { showBadge() }
        }
    }
    /// The asks the window put to coordinators about tasks, by
    /// TaskAsk.key: every card that shows the task shows its outcome
    /// row, and keeps its buttons away while the ask stands (T769).
    var asks: [String: TaskAsk] = [:]
    /// The Dock icon's badge: how many needs wait, none at 0 or while
    /// the bridge is down (the count may be old).
    func showBadge() {
        NSApp?.dockTile.badgeLabel = needs.isEmpty || !isConnected ? nil : "\(needs.count)"
    }

    var isConnected: Bool {
        if case .connected = phase { return true }
        return false
    }

    /// The bridge is down (or coming back) after one connected: the
    /// window keeps what it showed, dimmed, under a banner with Retry.
    var lost: Bool { hello != nil && !isConnected }

    /// Counts the bridges started: needs of a new one are learned
    /// afresh (NeedTracker.reset).
    @ObservationIgnored private(set) var connection = 0
    /// The window's selection, its history and the sidebar's collapsed
    /// sections; saved on every change, restored at launch.
    var nav = Navigation.restore(UserDefaults.standard.data(forKey: AppModel.navKey)) {
        didSet {
            if nav != oldValue { UserDefaults.standard.set(nav.encoded(), forKey: Self.navKey) }
            if let ref = selectedRef { warm(ref) }
            reshow()
        }
    }
    /// Whether the session view shows its inspector (⌘⌥I).
    var inspector = UserDefaults.standard.object(forKey: AppModel.inspectorKey) as? Bool ?? true {
        didSet { UserDefaults.standard.set(inspector, forKey: Self.inspectorKey) }
    }
    /// Bumped by ⌘L: the shown composer takes the focus.
    private(set) var composerFocus = 0
    /// Bumped by ⌘F, ⌘G and ⇧⌘G: the shown conversation's find bar
    /// opens, or moves to the next or previous match.
    private(set) var find = FindRequest(n: 0, step: 0)
    /// Bumped by ⌘↩: the open card takes its first choice.
    private(set) var firstChoice = 0
    /// Bumped by ⌥⌘F: the sidebar's filter field takes the focus.
    private(set) var filterFocus = 0
    /// The sidebar's filter: what its rows must match.
    var filter = ""
    /// Whether the command palette (⌘K) shows.
    var palette = false
    /// Whether the palette shows a session's cards or composer (its
    /// act): ⌘↩ is its cards' then, bumping paletteChoice.
    var paletteActing = false
    private(set) var paletteChoice = 0
    /// Whether the Keyboard Shortcuts sheet shows.
    var shortcuts = false
    /// Whether the first-run sheet shows (FirstRun; Help › Getting
    /// Started shows it again).
    var welcome = false
    /// The project File › New Task… adds to while its sheet shows.
    var newTask: String?
    /// What the bridge did: Settings › Advanced shows it.
    private(set) var log = BridgeLog()
    /// The need ⌥⌘↓ or ⌥⌘↑ showed last (Need.step).
    @ObservationIgnored private var lastNeed: String?

    struct FindRequest: Equatable {
        let n: Int
        /// 0 opens the bar, 1 the next match, -1 the previous.
        let step: Int
    }

    /// Told of every change to needs and notices (Notifier).
    @ObservationIgnored var onNeeds: (@MainActor (AppModel) -> Void)?
    /// The bridge while it runs (the session windows follow it).
    private(set) var bridge: Bridge?
    @ObservationIgnored private var run: Task<Void, Never>?
    @ObservationIgnored private var loaded = Loaded()
    @ObservationIgnored private var listing: Task<Void, Never>?
    /// The lean projects watch (app API 8): the inactive project it
    /// sends whole (watchedShown), and its events' task, which reshow
    /// ends so the watch opens again for the newly shown one.
    @ObservationIgnored private var lean = false
    @ObservationIgnored private var watchedShown = ""
    @ObservationIgnored private var projectEvents: Task<Void, any Error>?
    @ObservationIgnored private var reshowing = false
    @ObservationIgnored private var listAgain = false

    /// The server's hello capabilities (pane-vt, transcript): the last
    /// bridge's while it is down, so the views stay as they were.
    var caps: [String] { hello?.server?.caps ?? [] }

    /// What came since the bridge started: needs wait for all of it, so
    /// the first set is whole.
    private struct Loaded {
        var projects = false, sessions = false, peers = false
        var all: Bool { projects && sessions && peers }
    }

    /// The settings key of the configured tm path (TMLocator.configured).
    static let tmPathKey = "tmPath"
    static let navKey = "navigation"
    static let inspectorKey = "inspector"

    /// How often session.list and peer.list are read: neither streams.
    static let sessionsEvery = Duration.seconds(2)
    static let peersEvery = Duration.seconds(5)

    /// Starts the bridge unless it runs.
    func start() {
        if run == nil { connect() }
    }

    /// (Re)starts the bridge: with the configured tm, or the first one
    /// found. A bridge already running is ended first.
    func connect() {
        if run != nil { log.add("Reconnecting") }
        run?.cancel()
        run = Task { await watch(tmPath: UserDefaults.standard.string(forKey: Self.tmPathKey)) }
    }

    private func watch(tmPath: String?) async {
        phase = .connecting
        compatibility = .ok
        loaded = Loaded()
        connection += 1
        var started: Bridge?
        do {
            let tm = try TMLocator(configured: tmPath).locate()
            log.add("Starting \(tm.path) app bridge")
            let b = try await Bridge.launch(tm: tm)
            started = b
            bridge = b
            guard let hello = await b.hello else { throw BridgeError.closed("") }
            let fit = Compatibility.check(hello, appVersion: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String)
            compatibility = fit
            if fit.blocks { throw BridgeError.closed(fit.message ?? "") }
            self.hello = hello
            phase = .connected(hello)
            log.add(BridgeLog.connected(hello, tm: tm.lastPathComponent))
            // An older tm's bridge has no peer.list: no paired servers.
            loaded.peers = !hello.methods.contains(AppAPI.Methods.peerList.name)
            let peers = !loaded.peers
            try await withThrowingTaskGroup(of: Void.self) { g in
                g.addTask { try await self.watchProjects(b) }
                g.addTask { try await self.readSessions(b) }
                if peers {
                    g.addTask { try await self.readPeers(b) }
                }
                // Until one throws: the projects' stream ends only so.
                for try await _ in g {}
            }
        } catch {
            if !Task.isCancelled {
                // The needs stay for the window (dimmed); the menu bar and
                // the Dock badge count none while it is down.
                log.add("Failed: \(error.localizedDescription)")
                phase = .failed(error.localizedDescription)
            }
        }
        await started?.close()
        if bridge === started {
            bridge = nil
        }
    }

    /// Watches the projects until the bridge ends. A lean watch (app
    /// API 8) leaves the inactive projects but the shown one out, so it
    /// opens again whenever the window shows another inactive one.
    private func watchProjects(_ b: Bridge) async throws {
        lean = (await b.hello?.appApi ?? 0) >= AppAPI.leanAPI
        guard lean else {
            let sub = try await b.subscribe(AppAPI.projectsWatchDetail, AppAPI.DetailParams(detail: true))
            took(sub.result.projects)
            for try await e in sub.events {
                took(e.data.projects)
            }
            throw BridgeError.closed("The server stopped sending the projects.")
        }
        while true {
            let shown = nav.selection?.shownInactive(projects) ?? ""
            watchedShown = shown
            let sub = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true, lean: true, shown: shown))
            took(sub.result.projects)
            let events = Task { @MainActor in
                for try await e in sub.events {
                    self.took(e.data.projects)
                }
            }
            projectEvents = events
            reshowing = false
            reshow()
            try await withTaskCancellationHandler {
                try await events.value
            } onCancel: {
                events.cancel()
            }
            projectEvents = nil
            guard reshowing, !Task.isCancelled else {
                throw BridgeError.closed("The server stopped sending the projects.")
            }
        }
    }

    private func took(_ ps: [AppAPI.Project]) {
        projects = ps
        loaded.projects = true
        changed()
        reshow()
    }

    /// Ends the lean watch's events when the window shows an inactive
    /// project it doesn't send whole, or no longer shows the one it
    /// does: watchProjects opens it again.
    private func reshow() {
        guard lean, !reshowing, let events = projectEvents,
              (nav.selection?.shownInactive(projects) ?? "") != watchedShown else { return }
        reshowing = true
        events.cancel()
    }

    /// Whether the lean watch leaves s's project out: an inactive one
    /// it doesn't send whole, so its threads aren't known.
    private func leftOut(_ s: Selection) -> Bool {
        lean && s.server.isEmpty && s.project != watchedShown && project(s.project)?.active == false
    }

    private func readSessions(_ b: Bridge) async throws {
        while true {
            let l = try await b.call(AppAPI.Methods.sessionList)
            sessions = l.sessions
            notices = l.notices
            loaded.sessions = true
            changed()
            try await Task.sleep(for: Self.sessionsEvery)
        }
    }

    /// Reads peer.list until the bridge ends; a refusal (a bridge inside
    /// an agent's session) leaves the paired servers out.
    private func readPeers(_ b: Bridge) async throws {
        var gen: Int?
        while true {
            let l: AppAPI.PeerList
            do {
                l = try await b.call(AppAPI.Methods.peerList)
            } catch is AppAPI.Error {
                loaded.peers = true
                changed()
                return
            }
            if l.gen != gen || !loaded.peers {
                gen = l.gen
                peers = l.peers
                loaded.peers = true
                changed()
            }
            try await Task.sleep(for: Self.peersEvery)
        }
    }

    private func changed() {
        guard loaded.all else { return }
        needs = Need.of(projects: projects, sessions: sessions, peers: peers)
        settleSelection()
        onNeeds?(self)
    }

    /// Once everything is listed: a selection (restored, or kept from
    /// before) that names nothing any more falls back to its project or
    /// the first active one, and so does none.
    private func settleSelection() {
        var n = nav
        n.prune(fallback: activeProjects.first.map { .project($0.slug) }) { [projects, peers] in
            $0.exists(projects: projects, peers: peers) || self.leftOut($0)
        }
        if n != nav { nav = n }
    }

    /// Lists the sessions now, between readSessions' rounds (a session
    /// window's watch changed: its question or escape may have): once
    /// at a time, and once more after it when asked meanwhile.
    func listSessions() {
        guard listing == nil else {
            listAgain = true
            return
        }
        listing = Task {
            repeat {
                listAgain = false
                if let b = bridge, let l = try? await b.call(AppAPI.Methods.sessionList) {
                    sessions = l.sessions
                    notices = l.notices
                    changed()
                }
            } while listAgain && !Task.isCancelled
            listing = nil
        }
    }

    /// The session ref names now: a paired server's as its pane's id
    /// here, "<alias>/<session>".
    func sessionID(_ ref: SessionRef) -> String? {
        if ref.isRemote { return ref.session(in: peers)?.id }
        return ref.session(in: projects, sessions: sessions)
    }

    func peer(_ alias: String) -> AppAPI.Peer? {
        peers.first { $0.alias == alias }
    }

    /// What a paired server's pane allows now (RemotePane).
    func remotePane(_ ref: SessionRef) -> RemotePane {
        RemotePane.of(caps: caps, peer: peer(ref.server), alias: ref.server)
    }

    /// This server's active projects, the pinned ones first (as the
    /// TUI's sidebar): what ⌘1–⌘9, the Go menu, the menu bar and the
    /// first selection count.
    var activeProjects: [AppAPI.Project] { SidebarTree.active(projects) }

    /// The inactive ones, folded at the sidebar's bottom.
    var inactiveProjects: [AppAPI.Project] { SidebarTree.inactive(projects) }

    /// The project's coordinator's session, when one runs.
    func coordinatorSession(_ project: String) -> AppAPI.Session? {
        sessions.first { $0.role == "coordinator" && $0.project == project && $0.server.isEmpty }
    }

    /// What p's coordinator says of itself: its dot, its card.
    func coordinator(of p: AppAPI.Project) -> Attention {
        Attention.of(coordinator: coordinatorSession(p.slug), in: p)
    }

    func project(_ slug: String) -> AppAPI.Project? {
        projects.first { $0.slug == slug }
    }

    func thread(_ ref: SessionRef) -> AppAPI.Thread? {
        project(ref.project)?.threads.first { $0.record.id == ref.thread }
    }

    /// The needs of a project (its badge, its dashboard's Needs you).
    func needs(of project: String) -> [Need] {
        needs.filter { $0.project == project }
    }

    /// What t's card and row say of it, with its session's state.
    func attention(_ t: AppAPI.Thread) -> Attention {
        let s = t.record.session.isEmpty ? nil : sessions.first { $0.id == t.record.session }
        return Attention.of(thread: t, session: s)
    }

    // MARK: the window

    /// Brings the window to the front, opening it when it was closed:
    /// set by a view that has the scene's openWindow (MenuBarLabel).
    @ObservationIgnored var showWindow: (@MainActor () -> Void)?

    /// The session shown in a key window while the app is in front
    /// (SessionDetail says): its needs don't notify.
    @ObservationIgnored private var front: SessionRef?

    /// Says whether ref is shown in the key window now (on) or not.
    func shown(_ ref: SessionRef, inFront on: Bool) {
        if on {
            front = ref
        } else if front == ref {
            front = nil
        }
    }

    /// The id of the session shown in front, when one is.
    var frontSession: String? {
        guard NSApp?.isActive == true, let front else { return nil }
        return sessionID(front)
    }

    /// Shows s in the window, in front.
    func select(_ s: Selection) {
        nav.select(s)
        showWindow?()
    }

    /// Shows what a need, a notification or a menu bar row names (see
    /// Selection.route): nothing listed leaves the selection as it was.
    func open(project: String? = nil, session: String = "") {
        if let s = Selection.route(project: project, session: session, projects: projects, sessions: sessions, peers: peers) {
            nav.select(s)
        }
        showWindow?()
    }

    /// Where a need takes the window (Selection.route).
    func route(_ n: Need) -> Selection? {
        Selection.route(project: n.project, session: n.session, projects: projects, sessions: sessions, peers: peers)
    }

    /// ⌥⌘↓ (by 1) and ⌥⌘↑ (by -1): shows the next or the previous need.
    func stepNeed(_ by: Int) {
        guard let n = Need.step(needs, by: by, last: lastNeed, shown: nav.selection, route: route) else { return }
        lastNeed = n.id
        open(project: n.project, session: n.session)
    }

    /// Whether the shown session waits on a card ⌘↩ answers: a
    /// question tm read, or a permission dialog.
    var openCard: Bool {
        selectedSession.map { Self.waitsOnCard(session(of: $0)) } ?? false
    }

    /// Whether s waits on one of the cards (Cards): a question tm read,
    /// or a permission dialog.
    static func waitsOnCard(_ s: AppAPI.Session?) -> Bool {
        guard let s else { return false }
        if s.question.map({ q in q.questions.contains { !$0.answered } }) == true { return true }
        return s.state == "blocked" && s.reason == "permission"
    }

    /// ⌘↩: the open card's first choice: the palette's while it acts.
    func takeFirstChoice() {
        if palette {
            if paletteActing { paletteChoice += 1 }
        } else if openCard {
            firstChoice += 1
        }
    }

    /// ⌘F (step 0), ⌘G (1), ⇧⌘G (-1): the conversation's find bar.
    func findInConversation(_ step: Int = 0) {
        guard let m = selectedSession, !m.ref.isRemote else { return }
        m.pick(.conversation)
        find = FindRequest(n: find.n + 1, step: step)
    }

    /// ⌥⌘F: the sidebar's filter field.
    func focusFilter() {
        filterFocus += 1
    }

    func goBack() { nav.goBack() }
    func goForward() { nav.goForward() }

    /// The n-th active project of this server (⌘1–⌘9, from 0).
    func selectProject(_ n: Int) {
        let active = activeProjects
        guard n < active.count else { return }
        select(.project(active[n].slug))
    }

    var selectedRef: SessionRef? {
        if case .session(let ref) = nav.selection { return ref }
        return nil
    }

    /// The shown session's model, when one is shown.
    var selectedSession: SessionModel? {
        selectedRef.flatMap { models[$0] }
    }

    /// ⌘L: the conversation, its composer focused.
    func focusComposer() {
        guard let m = selectedSession, !m.ref.isRemote else { return }
        m.pick(.conversation)
        composerFocus += 1
    }

    /// ⌘⇧T: the other tab.
    func switchTab() {
        guard let m = selectedSession, !m.ref.isRemote, m.sessionID != nil else { return }
        m.pick(m.tab == .conversation ? .terminal : .conversation)
    }

    /// A Terminal tab is shown, for View › Repaint Terminal.
    var terminalShown: Bool {
        guard let m = selectedSession else { return false }
        return m.tab == .terminal && m.sessionID != nil
    }

    /// ⌥⌘R: the shown terminal asks its server for a fresh repaint.
    func repaintTerminal() {
        guard terminalShown, let m = selectedSession else { return }
        m.askRepaint()
    }

    /// The session model's session as session.list has it.
    func session(of m: SessionModel) -> AppAPI.Session? {
        m.sessionID.flatMap { id in sessions.first { $0.id == id } }
    }

    func conversationState(_ m: SessionModel, _ s: AppAPI.Session?) -> ConversationState {
        ConversationState.of(caps: caps, agent: s?.agent ?? "", status: m.transcript.status,
                             failure: m.transcriptFailure, server: m.ref.server)
    }

    /// The window's title for a selection.
    func title(_ sel: Selection?) -> String {
        switch sel {
        case nil: "terminatr"
        case .project(let slug, let server): server.isEmpty ? slug : "\(server) › \(slug)"
        case .session(let ref): title(ref)
        }
    }

    /// The title bar's second line: a thread's id and its project.
    func subtitle(_ sel: Selection?) -> String {
        guard case .session(let ref) = sel else { return "" }
        return subtitle(ref)
    }

    func subtitle(_ ref: SessionRef) -> String {
        ref.isRemote || ref.isCoordinator ? "" : "\(ref.thread) · \(ref.project)"
    }

    func title(_ ref: SessionRef) -> String {
        if ref.isRemote {
            let task = ref.session(in: peers)?.task ?? ""
            return "\(ref.server) › \(ref.project) · " + (ref.isCoordinator ? "coordinator" : [task, ref.thread].filter { !$0.isEmpty }.joined(separator: " "))
        }
        if ref.isCoordinator { return "\(ref.project) · coordinator" }
        let t = thread(ref)
        return [t?.task?.ref, t?.record.title ?? ref.thread].compactMap { $0 }.joined(separator: " ")
    }

    // MARK: sessions' streams

    /// The session models with their streams: the shown one and the
    /// few shown before it (warmth), so switching back to one finds
    /// its conversation loaded.
    @ObservationIgnored private var models: [SessionRef: SessionModel] = [:]
    @ObservationIgnored private var warmth = WarmSet<SessionRef>(limit: 3)

    /// ref's model, made when it has none.
    func model(_ ref: SessionRef) -> SessionModel {
        if let m = models[ref] { return m }
        let m = SessionModel(ref: ref)
        models[ref] = m
        warm(ref)
        return m
    }

    private func warm(_ ref: SessionRef) {
        for gone in warmth.touch(ref) {
            models.removeValue(forKey: gone)?.stop()
        }
    }

    /// Answers a question as the human, through the guarded
    /// session.answer: the server refuses it when the menu changed.
    func answer(_ params: AppAPI.AnswerParams) async throws -> AppAPI.AnswerResult {
        guard let bridge else { throw BridgeError.closed("Not connected to terminatr.") }
        return try await bridge.call(AppAPI.Methods.sessionAnswer, params)
    }

    /// Asks project's coordinator to delegate task (task.delegate, as
    /// the dashboard's D in the TUI): false when it was asked already.
    func delegate(project: String, task: String) async throws -> Bool {
        try await connected().call(AppAPI.Methods.taskDelegate, AppAPI.TaskParams(project: project, task: task)).asked
    }

    /// Asks project's coordinator to accept task, in review
    /// (task.accept, the TUI's A): false when it was asked already.
    func accept(project: String, task: String) async throws -> Bool {
        try await connected().call(AppAPI.Methods.taskAccept, AppAPI.TaskParams(project: project, task: task)).asked
    }

    /// Asks project's coordinator to send task back with the user's
    /// note (task.send-back, the TUI's X).
    func sendBack(project: String, task: String, note: String) async throws -> Bool {
        try await connected().call(AppAPI.Methods.taskSendBack, AppAPI.SendBackParams(project: project, task: task, note: note)).asked
    }

    /// Starts project's coordinator unless it runs (project.open, as tm
    /// project open), and shows it once session.list has it.
    func startCoordinator(_ project: String) async throws {
        _ = try await connected().call(AppAPI.Methods.projectOpen, AppAPI.ProjectOpenParams(project: project, cols: 120, rows: 36))
        listSessions()
        select(.session(SessionRef(project: project)))
    }

    /// Has project's coordinator put its open questions to the user
    /// (questions.open): its question card then answers them.
    func openQuestions(_ project: String) async throws {
        _ = try await connected().call(AppAPI.Methods.questionsOpen, AppAPI.ProjectParams(project: project))
        listSessions()
    }

    private func connected() throws -> Bridge {
        guard let bridge else { throw BridgeError.closed("Not connected to terminatr.") }
        return bridge
    }

    /// The permission dialog on a session's screen, as the human
    /// (session.approve): what it offers (check), or an option sent,
    /// once the server checked the dialog again.
    func approve(_ params: AppAPI.ApproveParams) async throws -> AppAPI.ApproveResult {
        try await connected().call(AppAPI.Methods.sessionApprove, params)
    }

    /// Whether this tm adds tasks from the app (task.add, app API 7).
    var canAddTask: Bool {
        if case .connected(let h) = phase { return Compatibility.addsTasks(h) }
        return false
    }

    /// The project File › New Task… adds to: the shown one, a paired
    /// server's never (its board is its own).
    var boardProject: String? {
        switch nav.selection {
        case .project(let slug, let server) where server.isEmpty: slug
        case .session(let ref) where !ref.isRemote: ref.project
        default: nil
        }
    }

    /// File › New Task… (⌘N): the sheet for the shown project.
    func showNewTask() {
        guard canAddTask, let p = boardProject else { return }
        newTask = p
        showWindow?()
    }

    /// Adds a task to project's board as the user (task.add).
    func addTask(_ form: NewTaskForm, project: String) async throws -> AppAPI.TaskAdded {
        guard let bridge else { throw BridgeError.closed("Not connected to terminatr.") }
        return try await bridge.call(AppAPI.Methods.taskAdd, form.params(project: project))
    }

    /// Claims mod notice id for this console: true when the app is the
    /// first, and so draws it.
    func claim(_ id: String) async -> Bool {
        guard let bridge else { return false }
        return (try? await bridge.call(AppAPI.Methods.notifyClaim, AppAPI.NoticeParams(id: id)).ok) ?? false
    }

    /// Takes mod notice id out of the tray.
    func dismiss(_ id: String) async {
        _ = try? await bridge?.call(AppAPI.Methods.notifyDismiss, AppAPI.NoticeParams(id: id))
    }
}
