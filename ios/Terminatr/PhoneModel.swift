import Foundation
import Observation
import TerminatrKit
import TerminatrLink

/// The device link is a Bridge's transport as tm app bridge's stdio is.
extension LinkTransport: @retroactive BridgeTransport {}

/// What the phone shows of one server: its projects (projects.watch),
/// sessions (session.list) and paired servers (peer.list), and what of
/// it needs the user (Need.of), as the Mac app's AppModel reads them;
/// and at the answer level the acts on those needs, each signed by
/// the write key (WriteKey). It talks to a Bridge on either transport:
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
    /// The bridge's hello as last read: what its tm has (Compatibility),
    /// its level and methods.
    private(set) var hello: AppAPI.Hello?
    private var watching: Task<Void, Never>?
    /// Where the user is on each screen, which a resize keeps (T825).
    @ObservationIgnored let places = Places()

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
        places.forget()
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
        hello = nil
    }

    private func connect(_ transport: any BridgeTransport, signer: (any WriteSigner)? = nil) async {
        let b = Bridge(transport: transport)
        do {
            hello = try await b.start()
            await b.setSigner(signer)
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
        let s = try await DevicePairing.join(code, as: id, name: name, version: version, writeKey: try WriteKey.forPairing().publicKey)
        try DeviceStore.save(s)
        await disconnect()
        open(s)
    }

    /// Says why something failed, on the screen at hand.
    func failed(_ error: any Error) {
        failure = error.localizedDescription
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
                await connect(t, signer: WriteKey.existing())
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
                hello = nil
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
        hello = await b.hello
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

    // MARK: Acts (the answer level)

    /// Whether the bridge has method m: the device's level allows it.
    func can(_ m: String) -> Bool {
        hello?.methods.contains(m) == true
    }

    /// Whether the phone answers here at all (the answer level).
    var answers: Bool { can(AppAPI.Methods.sessionApprove.name) }

    /// What an approval's dialog offers, as the server reads it off the
    /// screen (session.approve {check}, unsigned: it answers nothing).
    func check(_ session: String) async throws -> AppAPI.ApproveResult {
        try await connected().call(AppAPI.Methods.sessionApprove, AppAPI.ApproveParams(id: session, choice: 1, check: true))
    }

    func approve(_ session: String, choice: Int) async throws {
        _ = try await connected().call(AppAPI.Methods.sessionApprove, AppAPI.ApproveParams(id: session, choice: choice))
        await reread()
    }

    func answer(_ a: Need.Answer, _ option: String) async throws {
        _ = try await connected().call(AppAPI.Methods.sessionAnswer, a.params(option))
        await reread()
    }

    /// Asks the coordinator to accept task: false when it was asked
    /// already.
    func accept(project: String, task: String) async throws -> Bool {
        try await connected().call(AppAPI.Methods.taskAccept, AppAPI.TaskParams(project: project, task: task)).asked
    }

    func sendBack(project: String, task: String, note: String) async throws -> Bool {
        try await connected().call(AppAPI.Methods.taskSendBack, AppAPI.SendBackParams(project: project, task: task, note: note)).asked
    }

    /// Has the coordinator put its open questions to the user: they
    /// come back as its session's question, answered here.
    func openQuestions(_ project: String) async throws {
        _ = try await connected().call(AppAPI.Methods.questionsOpen, AppAPI.ProjectParams(project: project))
        await reread()
    }

    // MARK: Writes (the write level, T821), as the Mac app's

    /// Whether the phone sends prompts here (the write level).
    var writes: Bool { can(AppAPI.Methods.sessionPrompt.name) }

    /// Sends a prompt to session: queued (mode ""), or steered into the
    /// running turn ("now").
    func prompt(_ session: String, text: String, mode: String = "") async throws -> AppAPI.PromptResult {
        let r = try await connected().call(AppAPI.Methods.sessionPrompt, AppAPI.PromptParams(id: session, text: text, mode: mode))
        await reread()
        return r
    }

    /// Writes data to session's terminal as if typed (TerminalKey).
    func keys(_ session: String, _ data: String) async throws {
        _ = try await connected().call(AppAPI.Methods.sessionKeys, AppAPI.KeysParams(id: session, data: data))
    }

    /// Whether this tm offers task templates for New Task (app API 20).
    var offersTemplates: Bool { hello.map(Compatibility.offersTemplates) ?? false }

    /// The task templates of the mods that run in project; none with an
    /// older tm.
    func templates(_ project: String) async throws -> [AppAPI.Template] {
        guard offersTemplates else { return [] }
        return try await connected().call(AppAPI.Methods.taskTemplates, AppAPI.ProjectParams(project: project)).templates
    }

    /// Adds a task to project's board as the user (task.add).
    func addTask(_ form: NewTaskForm, project: String) async throws -> AppAPI.TaskAdded {
        try await connected().call(AppAPI.Methods.taskAdd, form.params(project: project))
    }

    /// Asks project's coordinator to delegate task: false when it was
    /// asked already.
    func delegate(project: String, task: String) async throws -> Bool {
        try await connected().call(AppAPI.Methods.taskDelegate, AppAPI.TaskParams(project: project, task: task)).asked
    }

    /// Starts project's coordinator unless it runs (project.open, at the
    /// Mac's size), and lists the sessions again so it shows.
    func startCoordinator(_ project: String) async throws {
        _ = try await connected().call(AppAPI.Methods.projectOpen, AppAPI.ProjectOpenParams(project: project, cols: 120, rows: 36))
        await reread()
    }

    /// The sessions again, after an act: their needs change at once.
    private func reread() async {
        guard let b = bridge, let l = try? await b.call(AppAPI.Methods.sessionList) else { return }
        sessions = l.sessions
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

    /// t's turn while its agent works: its row's activity mark (T803).
    func busy(_ t: AppAPI.Thread) -> Busy? {
        Busy.of(thread: t, session: session(t))
    }

    func project(_ slug: String) -> AppAPI.Project? {
        projects.first { $0.slug == slug }
    }

    /// The session a ref's conversation is read from: this server's from
    /// its project, a paired server's as that server last listed it.
    func sessionID(_ ref: SessionRef) -> String? {
        ref.isRemote ? ref.session(in: peers)?.id : ref.session(in: projects, sessions: sessions)
    }

    /// What the server serves beside its protocol (pane-vt …).
    var caps: [String] { hello?.server?.caps ?? [] }

    /// Whether a paired server's pane opens here, and takes keys, as the
    /// Mac's Terminal tab reads it.
    func remotePane(_ ref: SessionRef) -> RemotePane {
        RemotePane.of(caps: caps, peer: peers.first { $0.alias == ref.server }, alias: ref.server)
    }

    /// A ref's title: its thread's, else "Coordinator".
    func title(_ ref: SessionRef) -> String {
        if ref.isCoordinator { return "Coordinator" }
        return project(ref.project)?.threads.first { $0.record.id == ref.thread }?.record.title ?? ref.thread
    }

    // MARK: A project's pages, read only, as the Mac app reads them

    /// The pages this tm has (Memory and Library need app API 21).
    var pages: [ProjectPage] { ProjectPage.pages(hello) }

    /// Whether this tm lists the board's done tasks (task.done, app API 20).
    var listsDone: Bool { hello.map(Compatibility.listsDone) ?? false }

    /// Whether this tm reads a task whole (task.get, app API 14).
    var getsTasks: Bool { hello.map(Compatibility.getsTasks) ?? false }

    private func connected() throws -> Bridge {
        guard let bridge else { throw LinkError.closed }
        return bridge
    }

    /// The project's memory files (memory.list).
    func memoryFiles(_ project: String) async throws -> [AppAPI.MemoryFile] {
        try await connected().call(AppAPI.Methods.memoryList, AppAPI.ProjectParams(project: project)).files
    }

    /// A memory file's Markdown, by its path in the project (memory.read).
    func memoryText(_ project: String, path: String) async throws -> String {
        try await connected().call(AppAPI.Methods.memoryRead, AppAPI.MemoryReadParams(project: project, path: path)).text
    }

    /// The project's library, newest first (library.list).
    func library(_ project: String) async throws -> [AppAPI.LibraryFile] {
        try await connected().call(AppAPI.Methods.libraryList, AppAPI.ProjectParams(project: project)).files
    }

    /// A library file's bytes (library.read, a chunk at a time).
    func libraryData(_ project: String, _ f: AppAPI.LibraryFile) async -> FileOutcome {
        guard let b = bridge else { return .unavailable("the link is down") }
        return await LibraryPage.read({ p in try await b.call(AppAPI.Methods.libraryRead, p) }, project: project, file: f)
    }

    /// The done tasks of project's board, in board order (task.done).
    func doneTasks(_ project: String) async throws -> [AppAPI.Task] {
        try await connected().call(AppAPI.Methods.taskDone, AppAPI.ProjectParams(project: project)).tasks
    }

    /// Reads task of project whole, its notes and steps too (task.get).
    func getTask(project: String, task: String) async throws -> AppAPI.Task {
        try await connected().call(AppAPI.Methods.taskGet, AppAPI.TaskGetParams(project: project, task: task))
    }
}
