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
    /// The guard's refusals waiting on the user (guard.refusals, app
    /// API 18): needs whose cards answer them.
    private(set) var refusals: [AppAPI.GuardRefusal] = []
    /// [ui] needs_first, as the last session list said: the sidebar
    /// lists the projects that need the user first, as the TUI's.
    private(set) var needsFirst = false
    /// [ui] context_hint, as the last session list said (app API 13; 0
    /// for never): a coordinator's row warns from it.
    private(set) var contextHint = 0
    /// The bridge sends a paired project's watched threads (app API 10):
    /// the sidebar lists them, not only the ones a session runs.
    var watched: Bool { (hello?.appApi ?? 0) >= AppAPI.watchedAPI }
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
    /// What came of the link needs the window answered, by need id: the
    /// card's outcome row while the need stands, and a done one's for a
    /// moment after it went (T778).
    var linkOutcomes: [String: LinkOutcome] = [:]
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
    /// Text the coordinator's composer of a project takes as it shows,
    /// once (Ask the Coordinator from the task view).
    private(set) var draft: ComposerDraft?
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
    /// Whether this tm takes files for a prompt (session.upload, app API
    /// 15).
    var canAttach: Bool {
        if case .connected(let h) = phase { return Compatibility.takesFiles(h) }
        return false
    }

    /// The project File › New Task… adds to while its sheet shows.
    var newTask: String?
    /// The task the task view shows (TaskSheet), nil for none.
    var taskShown: TaskShown?
    /// What the bridge did: Settings › Advanced shows it.
    private(set) var log = BridgeLog()
    /// The need ⌥⌘↓ or ⌥⌘↑ showed last (Need.step).
    @ObservationIgnored private var lastNeed: String?

    /// Text for project's coordinator's composer; n tells one ask from
    /// the next.
    struct ComposerDraft: Equatable {
        let n: Int
        let project: String
        let text: String
    }

    struct FindRequest: Equatable {
        let n: Int
        /// 0 opens the bar, 1 the next match, -1 the previous.
        let step: Int
    }

    /// Told of every change to needs and notices (Notifier).
    @ObservationIgnored var onNeeds: (@MainActor (AppModel) -> Void)?
    /// The bridge while it runs (the session windows follow it).
    private(set) var bridge: Bridge?
    /// The conversations' files (session.file), while the bridge runs and
    /// its tm sends them: the attachment cards read through it.
    private(set) var files: FileStore?
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
        var projects = false, sessions = false, peers = false, refusals = false
        var all: Bool { projects && sessions && peers && refusals }
    }

    /// The settings key of the configured tm path (TMLocator.configured).
    static let tmPathKey = "tmPath"
    static let navKey = "navigation"
    static let inspectorKey = "inspector"

    /// How often session.list and peer.list are read: neither streams.
    static let sessionsEvery = Duration.seconds(2)
    static let peersEvery = Duration.seconds(5)
    /// How often guard.refusals is read: the bridge reads the guard's
    /// log, a small file, itself.
    static let refusalsEvery = Duration.seconds(3)

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
            files = Compatibility.sendsFiles(hello) ? FileStore { p in try await b.call(AppAPI.Methods.sessionFile, p) } : nil
            phase = .connected(hello)
            log.add(BridgeLog.connected(hello, tm: tm.lastPathComponent))
            // An older tm's bridge has no peer.list: no paired servers.
            loaded.peers = !hello.methods.contains(AppAPI.Methods.peerList.name)
            let peers = !loaded.peers
            // An older tm's bridge has no guard.refusals: none to answer.
            loaded.refusals = !Compatibility.answersRefusals(hello)
            let refusals = !loaded.refusals
            try await withThrowingTaskGroup(of: Void.self) { g in
                g.addTask { try await self.watchProjects(b) }
                g.addTask { try await self.readSessions(b) }
                if peers {
                    g.addTask { try await self.readPeers(b) }
                }
                if refusals {
                    g.addTask { try await self.readRefusals(b) }
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
            files = nil
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
            needsFirst = l.needsFirst
            contextHint = l.contextHint
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

    /// Reads guard.refusals until the bridge ends; a refusal (a bridge
    /// that isn't the user's) leaves them out.
    private func readRefusals(_ b: Bridge) async throws {
        while true {
            let l: AppAPI.GuardRefusals
            do {
                l = try await b.call(AppAPI.Methods.guardRefusals)
            } catch let e as AppAPI.Error {
                log.add("Guard refusals: \(e.message)")
                loaded.refusals = true
                changed()
                return
            }
            if l.refusals != refusals || !loaded.refusals {
                refusals = l.refusals
                loaded.refusals = true
                changed()
            }
            try await Task.sleep(for: Self.refusalsEvery)
        }
    }

    private func changed() {
        guard loaded.all else { return }
        needs = Need.of(projects: projects, sessions: sessions, peers: peers, refusals: refusals)
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
                    needsFirst = l.needsFirst
                    contextHint = l.contextHint
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

    /// What a paired server's conversation allows now
    /// (RemoteConversation).
    func remoteConversation(_ ref: SessionRef) -> RemoteConversation {
        RemoteConversation.of(caps: caps, peer: peer(ref.server), alias: ref.server)
    }

    /// What a paired server's pane allows now (RemotePane).
    func remotePane(_ ref: SessionRef) -> RemotePane {
        RemotePane.of(caps: caps, peer: peer(ref.server), alias: ref.server)
    }

    /// This server's active projects, the pinned ones first, with [ui]
    /// needs_first those that need the user first in each (as the TUI's
    /// sidebar): what ⌘1–⌘9, the Go menu, the menu bar and the first
    /// selection count.
    var activeProjects: [AppAPI.Project] { SidebarTree.active(projects, needsFirst: needsFirst, sessions: sessions) }

    /// The inactive ones, folded at the sidebar's bottom.
    var inactiveProjects: [AppAPI.Project] {
        SidebarTree.inactive(projects, needsFirst: needsFirst, sessions: sessions, nav: nav)
    }

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

    /// t's turn while its agent works: its row's and card's activity
    /// mark (T803).
    func busy(_ t: AppAPI.Thread) -> Busy? {
        Busy.of(thread: t, session: t.record.session.isEmpty ? nil : sessions.first { $0.id == t.record.session })
    }

    /// The coordinator's turn, as busy(_:) a thread's.
    func busy(coordinator p: AppAPI.Project) -> Busy? {
        Busy.of(coordinator: coordinatorSession(p.slug), in: p)
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
        guard let m = selectedSession, !m.ref.isRemote || m.transcript.status != nil else { return }
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
        guard let m = selectedSession, !m.ref.isRemote || remoteConversation(m.ref).sends else { return }
        m.pick(.conversation)
        composerFocus += 1
    }

    /// ⌘⇧T: the other tab.
    func switchTab() {
        guard let m = selectedSession, m.sessionID != nil else { return }
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
        ConversationState.of(caps: caps, agent: s?.agent ?? m.ref.session(in: peers)?.agent ?? "", status: m.transcript.status,
                             failure: m.transcriptFailure, remote: m.ref.isRemote ? remoteConversation(m.ref) : nil)
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
        do {
            return try await bridge.call(AppAPI.Methods.sessionAnswer, params)
        } catch let e as AppAPI.Error where e.code == "no-question" {
            if await questionGone(e, session: params.id, asked: params.question) { throw QuestionGone() }
            throw e
        }
    }

    /// Whether session.answer's refusal e was for a question answered
    /// elsewhere meanwhile (QuestionPlan.gone): the sessions are read
    /// again now, so its card goes at once.
    func questionGone(_ e: AppAPI.Error, session: String, asked: String) async -> Bool {
        guard let bridge, let l = try? await bridge.call(AppAPI.Methods.sessionList) else { return false }
        sessions = l.sessions
        changed()
        return QuestionPlan.gone(e.code, asked: asked, now: l.sessions.first { $0.id == session }?.question)
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

    /// Acts on a paired server's need as this server's user
    /// (peer.need-act, app API 30): that server checks it again, and
    /// refuses it once the need changed (need-changed).
    func actOnNeed(_ params: AppAPI.PeerNeedActParams) async throws -> AppAPI.PeerNeedActResult {
        try await connected().call(AppAPI.Methods.peerNeedAct, params)
    }

    /// Reads task of project whole, its notes and steps too (task.get,
    /// app API 14): a board task comes without them in the watch.
    func getTask(project: String, task: String) async throws -> AppAPI.Task {
        try await connected().call(AppAPI.Methods.taskGet, AppAPI.TaskGetParams(project: project, task: task))
    }

    /// The project's memory files (memory.list, app API 21).
    func memoryFiles(_ project: String) async throws -> [AppAPI.MemoryFile] {
        try await connected().call(AppAPI.Methods.memoryList, AppAPI.ProjectParams(project: project)).files
    }

    /// A memory file's Markdown, by its path in the project (memory.read).
    func memoryText(_ project: String, path: String) async throws -> String {
        try await connected().call(AppAPI.Methods.memoryRead, AppAPI.MemoryReadParams(project: project, path: path)).text
    }

    /// The project's designs, by name, each with its build in after
    /// order (design.list, app API 31).
    func designs(_ project: String) async throws -> [AppAPI.Design] {
        try await connected().call(AppAPI.Methods.designList, AppAPI.ProjectParams(project: project)).designs
    }

    /// The project's library, newest first (library.list).
    func library(_ project: String) async throws -> [AppAPI.LibraryFile] {
        try await connected().call(AppAPI.Methods.libraryList, AppAPI.ProjectParams(project: project)).files
    }

    /// A library file's bytes (library.read, a chunk at a time).
    func libraryData(_ project: String, _ f: AppAPI.LibraryFile) async -> FileOutcome {
        guard let b = bridge else { return .unavailable("tm isn't connected") }
        return await LibraryPage.read({ p in try await b.call(AppAPI.Methods.libraryRead, p) }, project: project, file: f)
    }

    /// The project pages this tm has (Memory and Library need app API 21).
    var projectPages: [ProjectPage] {
        if case .connected(let h) = phase { return ProjectPage.pages(h) }
        return ProjectPage.pages(nil)
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

    /// Whether this tm saves and clears coordinators and activates
    /// projects from the app (app API 13).
    var controlsProjects: Bool {
        if case .connected(let h) = phase { return Compatibility.controlsProjects(h) }
        return false
    }

    /// The project action the window asks about, in its confirmation.
    var asking: ProjectAsk?
    /// Why the last project action failed, or what it couldn't do: the
    /// window's alert.
    var askSaid: (title: String, text: String)?

    /// Whether this tm pins, makes and adopts into projects and writes
    /// on their links from the app (app API 22).
    var actsOnSidebar: Bool {
        if case .connected(let h) = phase { return Compatibility.actsOnSidebar(h) }
        return false
    }

    /// Whether this tm turns a coordinator's remote control on or off
    /// from the app (app API 22).
    var controlsRemote: Bool {
        if case .connected(let h) = phase { return Compatibility.controlsRemote(h) }
        return false
    }

    /// Whether this tm does a.
    func can(_ a: ProjectAsk) -> Bool {
        switch a {
        case .remote: controlsRemote
        case .pin, .adopt, .withdrawAdopt: actsOnSidebar
        default: controlsProjects
        }
    }

    /// Asks about a, or does it when it asks nothing (activating, or
    /// deactivating a project that runs nothing, pinning).
    func ask(_ a: ProjectAsk) {
        guard can(a) else { return }
        if a.asks {
            asking = a
            showWindow?()
        } else {
            act(a)
        }
    }

    /// Does a, once the user said so: coordinator.clear, project.active,
    /// project.pin, session.remote or session.adopt.request.
    func act(_ a: ProjectAsk) {
        Task {
            do {
                switch a {
                case .saveClear(let p):
                    _ = try await connected().call(AppAPI.Methods.coordinatorClear, AppAPI.ProjectParams(project: p))
                case .activate(let p), .deactivate(let p, _):
                    let on: Bool
                    if case .activate = a { on = true } else { on = false }
                    let r = try await connected().call(AppAPI.Methods.projectActive, AppAPI.ProjectActiveParams(project: p, active: on))
                    if let lost = ProjectAsk.words(r, project: p) { askSaid = ("Not everything resumed", lost) }
                    listSessions()
                case .pin(let p, let on):
                    _ = try await connected().call(AppAPI.Methods.projectPin, AppAPI.ProjectPinParams(project: p, pinned: on))
                case .remote(let p, let id, let on):
                    let r = try await connected().call(AppAPI.Methods.sessionRemote, AppAPI.RemoteParams(id: id, on: on))
                    // Said when the coordinator restarted or stays off
                    // against the setting; else the row shows it.
                    if r.how == "restarted" || r.held || r.how == "unchanged" {
                        askSaid = ("Remote control", ProjectAsk.words(r, project: p))
                    }
                    listSessions()
                case .adopt(let p, let id, _):
                    let r = try await connected().call(AppAPI.Methods.sessionAdoptRequest, AppAPI.AdoptRequestParams(id: id, project: p))
                    if !r.asked { askSaid = ("Already asked", "\(id) is already waiting on \(p)'s coordinator to adopt it.") }
                case .withdrawAdopt(let p, let id):
                    _ = try await connected().call(AppAPI.Methods.sessionAdoptRequest,
                                                   AppAPI.AdoptRequestParams(id: id, project: p, withdraw: true))
                }
            } catch {
                askSaid = ("\(a.verb) didn't work", Dashboard.words(error))
            }
        }
    }

    /// The project the window's Save & Clear, Activate and Deactivate act
    /// on: the shown one, a paired server's never.
    var shownProject: AppAPI.Project? { boardProject.flatMap { project($0) } }

    func connected() throws -> Bridge {
        guard let bridge else { throw BridgeError.closed("Not connected to terminatr.") }
        return bridge
    }

    /// The permission dialog on a session's screen, as the human
    /// (session.approve): what it offers (check), or an option sent,
    /// once the server checked the dialog again.
    func approve(_ params: AppAPI.ApproveParams) async throws -> AppAPI.ApproveResult {
        try await connected().call(AppAPI.Methods.sessionApprove, params)
    }

    /// Answers refusal id of the guard as the user (guard.decide): how
    /// is "here" (allow it in its project), "all" (in all projects) or
    /// "keep" (keep refusing). It leaves the needs at once, and so does
    /// one answered elsewhere (gone). Returns what tm says it did.
    func decide(refusal id: String, how: String) async throws -> String {
        do {
            let r = try await connected().call(AppAPI.Methods.guardDecide, AppAPI.GuardDecideParams(id: id, how: how))
            drop(refusal: id)
            return r.message
        } catch let e as AppAPI.Error where e.code == "gone" {
            drop(refusal: id)
            throw e
        }
    }

    private func drop(refusal id: String) {
        refusals.removeAll { $0.id == id }
        changed()
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

    /// File › New Project… (⇧⌘N): its sheet, shown.
    var newProject = false

    /// The project and link the Write to Linked Project sheet writes
    /// from and to.
    var linkMessage: (project: String, peer: String)?

    /// File › New Project…: the sheet.
    func showNewProject() {
        guard actsOnSidebar else { return }
        newProject = true
        showWindow?()
    }

    /// Makes a project as the user (project.new) and shows it.
    func makeProject(_ form: NewProjectForm) async throws -> AppAPI.ProjectNew {
        let r = try await connected().call(AppAPI.Methods.projectNew, form.params)
        select(.project(r.project))
        return r
    }

    /// Write to Linked Project…: the sheet for project p, to first or
    /// the first link that sends.
    func showLinkMessage(_ p: AppAPI.Project, to first: String = "") {
        guard actsOnSidebar, let l = LinkMessageForm.writable(p, first: first).first else { return }
        linkMessage = (p.slug, l.peer)
        showWindow?()
    }

    /// Writes the user's message on project's link (link.send); returns
    /// what tm says it did.
    func sendLinkMessage(_ form: LinkMessageForm, project: String) async throws -> String {
        try await connected().call(AppAPI.Methods.linkSend, form.params(project: project)).words
    }

    /// File › New Task… (⌘N): the sheet for the shown project.
    func showNewTask() {
        guard canAddTask, let p = boardProject else { return }
        newTask = p
        showWindow?()
    }

    /// Whether this tm lists a project's designs (design.list, app API
    /// 31).
    var listsDesigns: Bool {
        if case .connected(let h) = phase { return Compatibility.listsDesigns(h) }
        return false
    }

    /// Whether this tm lists the board's done tasks (task.done, app API
    /// 20).
    var listsDone: Bool {
        if case .connected(let h) = phase { return Compatibility.listsDone(h) }
        return false
    }

    /// Whether this tm offers task templates for New Task (app API 20).
    var offersTemplates: Bool {
        if case .connected(let h) = phase { return Compatibility.offersTemplates(h) }
        return false
    }

    /// The done tasks of project's board, in board order (task.done).
    func doneTasks(_ project: String) async throws -> [AppAPI.Task] {
        try await connected().call(AppAPI.Methods.taskDone, AppAPI.ProjectParams(project: project)).tasks
    }

    /// The task templates of the mods that run in project
    /// (task.templates); none with an older tm.
    func templates(_ project: String) async throws -> [AppAPI.Template] {
        guard offersTemplates else { return [] }
        return try await connected().call(AppAPI.Methods.taskTemplates, AppAPI.ProjectParams(project: project)).templates
    }

    /// The task view's Ask the Coordinator (the TUI's c): shows the
    /// project's coordinator, started when none runs, its composer
    /// focused and holding the task (Dashboard.askAbout).
    func askCoordinator(about task: AppAPI.Task, project: String) async throws {
        if coordinatorSession(project) == nil {
            try await startCoordinator(project)
        }
        draft = ComposerDraft(n: (draft?.n ?? 0) + 1, project: project, text: Dashboard.askAbout(task))
        select(.session(SessionRef(project: project)))
        focusComposer()
    }

    /// Takes the draft for ref's composer: its text, once.
    func takeDraft(for ref: SessionRef) -> String? {
        guard let d = draft, ref.isCoordinator, !ref.isRemote, d.project == ref.project else { return nil }
        draft = nil
        return d.text
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
