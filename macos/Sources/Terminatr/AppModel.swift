import Foundation
import Observation
import TerminatrKit

/// What the windows show: the bridge's state and every project, kept
/// current by projects.watch {detail}, and the sessions, listed again
/// whenever the projects change. The session windows reach the server
/// through bridge.
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
    /// The bridge while it runs.
    private(set) var bridge: Bridge?

    @ObservationIgnored private var run: Task<Void, Never>?
    @ObservationIgnored private var listing: Task<Void, Never>?
    @ObservationIgnored private var listAgain = false

    /// The server's hello capabilities (pane-vt, transcript).
    var caps: [String] {
        if case .connected(let h) = phase { return h.server?.caps ?? [] }
        return []
    }

    /// The settings key of the configured tm path (TMLocator.configured).
    static let tmPathKey = "tmPath"

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
        var bridge: Bridge?
        do {
            let tm = try TMLocator(configured: tmPath).locate()
            let b = try await Bridge.launch(tm: tm)
            bridge = b
            guard let hello = await b.hello else { throw BridgeError.closed("") }
            phase = .connected(hello)
            self.bridge = b
            let sub = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true))
            projects = sub.result.projects
            listSessions()
            for try await e in sub.events {
                projects = e.data.projects
                listSessions()
            }
            if !Task.isCancelled {
                phase = .failed("The server stopped sending the projects.")
            }
        } catch {
            if !Task.isCancelled {
                phase = .failed(error.localizedDescription)
            }
        }
        if self.bridge === bridge {
            self.bridge = nil
        }
        await bridge?.close()
    }

    /// Lists the sessions again: once at a time, and once more after
    /// it when asked meanwhile.
    func listSessions() {
        guard listing == nil else {
            listAgain = true
            return
        }
        listing = Task {
            repeat {
                listAgain = false
                if let b = bridge, let list = try? await b.call(AppAPI.Methods.sessionList) {
                    sessions = list.sessions
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
}
