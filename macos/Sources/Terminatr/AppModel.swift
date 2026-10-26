import Foundation
import Observation
import TerminatrKit

/// What the window and the menu bar show: the bridge's state, every
/// project (projects.watch {detail}), the sessions and the mods' notices
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

    private(set) var phase = Phase.connecting
    /// Whether this app and the bridge's tm fit (their app API and
    /// releases); the status line and the menu bar say which is behind.
    private(set) var compatibility = Compatibility.ok
    private(set) var projects: [AppAPI.Project] = []
    private(set) var sessions: [AppAPI.Session] = []
    private(set) var notices: [AppAPI.Notice] = []
    private(set) var peers: [AppAPI.Peer] = []
    /// What needs the user, once projects and sessions both came.
    private(set) var needs: [Need] = []
    /// Counts the bridges started: needs of a new one are learned
    /// afresh (NeedTracker.reset).
    @ObservationIgnored private(set) var connection = 0
    /// The window's selection, its history and the sidebar's collapsed
    /// sections; saved on every change, restored at launch.
    var nav = Navigation.restore(UserDefaults.standard.data(forKey: AppModel.navKey)) {
        didSet {
            if nav != oldValue { UserDefaults.standard.set(nav.encoded(), forKey: Self.navKey) }
            if let ref = selectedRef { warm(ref) }
        }
    }
    /// Whether the session view shows its inspector (⌘⌥I).
    var inspector = UserDefaults.standard.object(forKey: AppModel.inspectorKey) as? Bool ?? true {
        didSet { UserDefaults.standard.set(inspector, forKey: Self.inspectorKey) }
    }
    /// Bumped by ⌘L: the shown composer takes the focus.
    private(set) var composerFocus = 0

    /// Told of every change to needs and notices (Notifier).
    @ObservationIgnored var onNeeds: (@MainActor (AppModel) -> Void)?
    /// The bridge while it runs (the session windows follow it).
    private(set) var bridge: Bridge?
    @ObservationIgnored private var run: Task<Void, Never>?
    @ObservationIgnored private var loaded = Loaded()
    @ObservationIgnored private var listing: Task<Void, Never>?
    @ObservationIgnored private var listAgain = false

    /// The server's hello capabilities (pane-vt, transcript).
    var caps: [String] {
        if case .connected(let h) = phase { return h.server?.caps ?? [] }
        return []
    }

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
            let b = try await Bridge.launch(tm: tm)
            started = b
            bridge = b
            guard let hello = await b.hello else { throw BridgeError.closed("") }
            let fit = Compatibility.check(hello, appVersion: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String)
            compatibility = fit
            if fit.blocks { throw BridgeError.closed(fit.message ?? "") }
            phase = .connected(hello)
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
                phase = .failed(error.localizedDescription)
                // Not known any more: the menu bar counts nothing.
                needs = []
            }
        }
        await started?.close()
        if bridge === started {
            bridge = nil
        }
    }

    private func watchProjects(_ b: Bridge) async throws {
        let sub = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true))
        projects = sub.result.projects
        loaded.projects = true
        changed()
        for try await e in sub.events {
            projects = e.data.projects
            changed()
        }
        throw BridgeError.closed("The server stopped sending the projects.")
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
    /// the first one, and so does none.
    private func settleSelection() {
        var n = nav
        n.prune(fallback: projects.first.map { .project($0.slug) }) { [projects, peers] in
            $0.exists(projects: projects, peers: peers)
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

    func project(_ slug: String) -> AppAPI.Project? {
        projects.first { $0.slug == slug }
    }

    func thread(_ ref: SessionRef) -> AppAPI.Thread? {
        project(ref.project)?.threads.first { $0.record.id == ref.thread }
    }

    // MARK: the window

    /// Brings the window to the front, opening it when it was closed:
    /// set by a view that has the scene's openWindow (MenuBarLabel).
    @ObservationIgnored var showWindow: (@MainActor () -> Void)?

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

    func goBack() { nav.goBack() }
    func goForward() { nav.goForward() }

    /// The n-th project of this server (⌘1–⌘9, from 0).
    func selectProject(_ n: Int) {
        guard n < projects.count else { return }
        select(.project(projects[n].slug))
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
