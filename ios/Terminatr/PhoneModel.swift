import Foundation
import Observation
import TerminatrKit
import TerminatrLink

/// The device link is a Bridge's transport as tm app bridge's stdio is.
extension LinkTransport: @retroactive BridgeTransport {}

/// What the phone shows of one server, read only: its projects
/// (projects.watch), sessions (session.list) and paired servers
/// (peer.list), and what of it needs the user (Need.of), as the Mac
/// app's AppModel reads them. It talks to a Bridge on either transport:
/// the demo's recording (DemoTransport), or the device link to the
/// paired server (LinkTransport), which it keeps while the app is in
/// front: redialled when it drops and when the app comes back, its
/// hello and streams afresh each time.
@MainActor @Observable
final class PhoneModel {
    enum Source: Equatable {
        /// Not connected: the welcome screen.
        case none
        /// The built-in recording (DemoTransport).
        case demo
        /// The paired server, over its device link.
        case server(DevicePairing.Server)
    }

    /// Where the device link is.
    enum Link: Equatable {
        /// Dialing the relay, or the TLS with the server.
        case connecting
        /// On the relay, but the server isn't there: asleep, offline or
        /// stopped. The link comes up when it is back.
        case asleep
        case up
        /// It dropped or failed: why; the next dial is on its way.
        case lost(String)
    }

    private(set) var source = Source.none
    private(set) var link = Link.connecting
    /// Counts the bridges: a view whose stream is the bridge's restarts
    /// it when this changes (a redial).
    private(set) var generation = 0
    private var linking: Task<Void, Never>?
    /// Told when projects.watch ends by itself: the link dropped.
    private var dropped: (@MainActor (any Error) -> Void)?
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
        await drop()
        source = .none
        projects = []
        sessions = []
        peers = []
    }

    /// Ends the bridge and the link, and the redials.
    private func drop() async {
        linking?.cancel()
        linking = nil
        dropped = nil
        watching?.cancel()
        watching = nil
        await bridge?.close()
        bridge = nil
    }

    private func connect(_ transport: any BridgeTransport) async {
        let b = Bridge(transport: transport)
        do {
            _ = try await b.start()
            bridge = b
            generation += 1
            await refresh()
        } catch {
            failure = error.localizedDescription
        }
    }

    // MARK: The paired server

    /// Opens the server the Keychain keeps, if there is one.
    func openPaired() {
        guard source == .none, let s = DeviceStore.server() else { return }
        open(s)
    }

    /// Pairs with the server code names, keeps it and opens it. It
    /// throws why not, for the pairing screen to say.
    func pair(_ code: PairingCode, name: String) async throws {
        let id = try DeviceStore.identity()
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        let s = try await DevicePairing.join(code, as: id, name: name, version: version)
        try DeviceStore.save(s)
        await disconnect()
        open(s)
    }

    /// Forgets the server and the device key, back to the welcome screen.
    func forget() async {
        await disconnect()
        DeviceStore.forget()
    }

    private func open(_ s: DevicePairing.Server) {
        source = .server(s)
        resume()
    }

    /// The app came to the front: link again (redial, hello, streams).
    func resume() {
        guard case .server(let s) = source, linking == nil else { return }
        linking = Task { await self.keep(s) }
    }

    /// The app went to the back: the link closes (the server sees the
    /// device go), and resume opens it again.
    func suspend() async {
        guard case .server = source else { return }
        await drop()
        link = .connecting
    }

    /// Keeps the link to s until cancelled: dial, hello, read it all,
    /// follow projects.watch; when that ends, dial again after a wait (1 s
    /// doubling to 30 s). While the server is asleep the relay's room
    /// waits for it; when the relay ends that wait, the next one starts.
    private func keep(_ s: DevicePairing.Server) async {
        var fails = 0
        while !Task.isCancelled {
            if link != .asleep { link = .connecting }
            var wait = 1
            do {
                let id = try DeviceStore.identity()
                let t = try await LinkTransport.connect(to: s.endpoint, as: id) { [weak self] in
                    Task { @MainActor in self?.link = .asleep }
                }
                let lost = AsyncStream<any Error>.makeStream()
                dropped = { lost.continuation.yield($0) }
                await connect(t)
                guard bridge != nil else { throw LinkError.closed }
                link = .up
                fails = 0
                for await e in lost.stream {
                    throw e
                }
            } catch {
                if Task.isCancelled { break }
                dropped = nil
                watching?.cancel()
                watching = nil
                await bridge?.close()
                bridge = nil
                if link != .asleep {
                    link = .lost(error.localizedDescription)
                    fails += 1
                    wait = min(30, 1 << min(fails - 1, 5))
                }
            }
            try? await Task.sleep(for: .seconds(wait))
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
                    self?.dropped?(LinkError.closed)
                } catch {
                    if !Task.isCancelled { self?.dropped?(error) }
                }
            }
        } catch {
            failure = error.localizedDescription
            // The bridge ended under it: the link dropped.
            if error is LinkError || error is BridgeError { dropped?(error) }
        }
    }

    /// The paired server's name, for the screens to say.
    var serverName: String {
        if case .server(let s) = source { return s.name.isEmpty ? "your server" : s.name }
        return ""
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
