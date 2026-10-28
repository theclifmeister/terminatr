import Foundation
import Observation
import TerminatrKit

/// What the phone shows of one server, read only: its projects
/// (projects.watch), sessions (session.list) and paired servers
/// (peer.list), and what of it needs the user (Need.of), as the Mac
/// app's AppModel reads them. It talks to a Bridge on any transport:
/// the demo's recording (DemoTransport) now, a device link (LinkTransport,
/// from the TLS spike) once pairing lands.
@MainActor @Observable
final class PhoneModel {
    enum Source: Equatable {
        /// Not connected: the welcome screen.
        case none
        /// The built-in recording (DemoTransport).
        case demo
    }

    private(set) var source = Source.none
    private(set) var projects: [AppAPI.Project] = []
    private(set) var sessions: [AppAPI.Session] = []
    private(set) var peers: [AppAPI.Peer] = []
    private(set) var needsFirst = false
    /// Why the last load failed, in words.
    private(set) var failure: String?
    private(set) var bridge: Bridge?
    private var watching: Task<Void, Never>?

    /// Every need, as the Mac app's Needs you lists them.
    var needs: [Need] { Need.of(projects: projects, sessions: sessions, peers: peers) }

    var activeProjects: [AppAPI.Project] { SidebarTree.active(projects, needsFirst: needsFirst, sessions: sessions) }

    var inactiveProjects: [AppAPI.Project] { SidebarTree.inactive(projects, needsFirst: needsFirst, sessions: sessions) }

    /// The demo's recording, in the app's bundle.
    static let recording = Bundle.main.url(forResource: "demo", withExtension: "json")

    /// Opens the demo: the recording through a Bridge, as a server's
    /// lines would come.
    func startDemo() async {
        guard let url = Self.recording else {
            failure = "The demo's recording is missing from the app."
            return
        }
        do {
            await connect(try DemoTransport(recording: Data(contentsOf: url)))
            source = .demo
        } catch {
            failure = error.localizedDescription
        }
    }

    /// Back to the welcome screen.
    func disconnect() async {
        watching?.cancel()
        watching = nil
        await bridge?.close()
        bridge = nil
        source = .none
        projects = []
        sessions = []
        peers = []
    }

    private func connect(_ transport: any BridgeTransport) async {
        let b = Bridge(transport: transport)
        do {
            _ = try await b.start()
            bridge = b
            await refresh()
        } catch {
            failure = error.localizedDescription
        }
    }

    /// Reads it all again (pull to refresh): the session and peer lists,
    /// and projects.watch opened afresh.
    func refresh() async {
        guard let b = bridge else { return }
        failure = nil
        do {
            let l = try await b.call(AppAPI.Methods.sessionList)
            sessions = l.sessions
            needsFirst = l.needsFirst
            if await b.hello?.methods.contains(AppAPI.Methods.peerList.name) == true {
                peers = try await b.call(AppAPI.Methods.peerList).peers
            }
            watching?.cancel()
            let sub = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true, lean: true))
            projects = sub.result.projects
            watching = Task { [weak self] in
                do {
                    for try await e in sub.events {
                        self?.projects = e.data.projects
                    }
                } catch {}
            }
        } catch {
            failure = error.localizedDescription
        }
    }

    func coordinatorSession(_ project: String) -> AppAPI.Session? {
        sessions.first { $0.role == "coordinator" && $0.project == project && $0.server.isEmpty }
    }

    func session(_ t: AppAPI.Thread) -> AppAPI.Session? {
        t.record.session.isEmpty ? nil : sessions.first { $0.id == t.record.session }
    }

    /// What t's row says of it, with its session's state.
    func attention(_ t: AppAPI.Thread) -> Attention {
        Attention.of(thread: t, session: session(t))
    }

    func project(_ slug: String) -> AppAPI.Project? {
        projects.first { $0.slug == slug }
    }

    /// The session a ref's conversation is read from: this server's from
    /// its project, a paired server's as that server last listed it.
    func sessionID(_ ref: SessionRef) -> String? {
        ref.isRemote ? ref.session(in: peers)?.id : ref.session(in: projects, sessions: sessions)
    }

    /// A ref's title: its thread's, else "Coordinator".
    func title(_ ref: SessionRef) -> String {
        if ref.isCoordinator { return "Coordinator" }
        return project(ref.project)?.threads.first { $0.record.id == ref.thread }?.record.title ?? ref.thread
    }
}
