import Foundation
import Observation
import TerminatrKit

/// What the windows and the menu bar show: the bridge's state, every
/// project (projects.watch {detail}), the sessions and the mods' notices
/// (session.list) and the paired servers' shared projects (peer.list),
/// and what of them needs the user. The session windows reach the
/// server through bridge.
@MainActor
@Observable
final class AppModel {
    enum Phase: Equatable {
        case connecting
        case connected(AppAPI.Hello)
        case failed(String)
    }

    private(set) var phase = Phase.connecting
    private(set) var projects: [AppAPI.Project] = []
    private(set) var sessions: [AppAPI.Session] = []
    private(set) var notices: [AppAPI.Notice] = []
    private(set) var peers: [AppAPI.Peer] = []
    /// What needs the user, once projects and sessions both came.
    private(set) var needs: [Need] = []
    /// Counts the bridges started: needs of a new one are learned
    /// afresh (NeedTracker.reset).
    @ObservationIgnored private(set) var connection = 0
    /// The project the projects window shows; the menu bar sets it.
    var focus: String?

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
        loaded = Loaded()
        connection += 1
        var started: Bridge?
        do {
            let tm = try TMLocator(configured: tmPath).locate()
            let b = try await Bridge.launch(tm: tm)
            started = b
            bridge = b
            guard let hello = await b.hello else { throw BridgeError.closed("") }
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
        onNeeds?(self)
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

    /// The session ref names now.
    func sessionID(_ ref: SessionRef) -> String? {
        ref.session(in: projects, sessions: sessions)
    }

    func project(_ slug: String) -> AppAPI.Project? {
        projects.first { $0.slug == slug }
    }

    func thread(_ ref: SessionRef) -> AppAPI.Thread? {
        project(ref.project)?.threads.first { $0.record.id == ref.thread }
    }

    /// Opens the projects window, from the menu bar or a notification:
    /// set by a view that has the scene's openWindow.
    @ObservationIgnored var openWindow: (@MainActor () -> Void)?
    /// Opens a coordinator's or thread's window, likewise.
    @ObservationIgnored var openSession: (@MainActor (SessionRef) -> Void)?

    /// Opens what a need or a notification names: session's window when
    /// it is a coordinator's or a thread's, else project (none: as it
    /// was) in the projects window, in front.
    func open(project: String? = nil, session: String = "") {
        if let s = sessions.first(where: { $0.id == session }), let ref = SessionRef(s), let openSession {
            openSession(ref)
            return
        }
        if let project, projects.contains(where: { $0.slug == project }) {
            focus = project
        }
        openWindow?()
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
