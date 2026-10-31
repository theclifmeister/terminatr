import Foundation
import Network
import Observation
import TerminatrKit
import TerminatrLink

/// The device link is a Bridge's transport as tm app bridge's stdio is.
extension LinkTransport: @retroactive BridgeTransport {}

/// What the phone shows of one server (Phone holds one per pairing,
/// T852): its projects (projects.watch),
/// sessions (session.list) and paired servers (peer.list), and what of
/// it needs the user (Need.of), as the Mac app's AppModel reads them;
/// and at the answer level the acts on those needs, each signed by
/// the write key (WriteKey). It talks to a Bridge on either transport:
/// the demo's recording (DemoTransport), or the device link to the
/// paired server (LinkTransport), which it keeps while the app is in
/// front and for a short grace behind another app (LinkKeep): redialled
/// when it drops, at once when the network changes, and when the app
/// comes back, its streams afresh each time but what it shows kept
/// meanwhile (T859). Each drop goes in the link's log (LinkLog), which
/// Settings' Connection shows.
@MainActor @Observable
final class PhoneModel {
    /// The pairing it reads, by its server's id (DeviceStore): what its
    /// screens' routes carry (Route.on); "demo" for the demo, "" for
    /// none.
    let key: String
    /// The servers the phone pairs with directly (Phone), which this
    /// one's paired servers' lists then leave out (shownPeers).
    @ObservationIgnored var direct: @MainActor () -> [Pairings.Direct] = { [] }

    init(key: String = "", places: Places = Places()) {
        self.key = key
        self.places = places
        log = key == "demo" || key.isEmpty ? LinkLog() : LinkLog.load(pairing: key)
    }

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
    /// The link's drops, kept across launches; the demo's is made up.
    private(set) var log: LinkLog
    /// The link while it is up, for its ping (LinkTransport.check).
    private var transport: LinkTransport?
    /// Dials failed in a row: the next one waits LinkKeep.backoff of it.
    private var fails = 0
    /// The wait before the next dial, which a network change cuts short.
    private var napping: Task<Void, Never>?
    /// The next dial goes at once: the network changed under the link.
    private var rush = false
    /// The link was up since the server was opened: only then is a
    /// failure a drop.
    private var wasUp = false
    /// The app is behind another one, the link still up until this ends.
    private var grace: Task<Void, Never>?
    private var paths: NWPathMonitor?
    private var path: LinkKeep.Path?
    /// Told when projects.watch ends by itself: the link dropped.
    private var dropped: (@MainActor (any Error) -> Void)?
    private(set) var projects: [AppAPI.Project] = []
    private(set) var sessions: [AppAPI.Session] = []
    private(set) var peers: [AppAPI.Peer] = []
    private(set) var needsFirst = false
    /// [ui] context_hint, as the last session list said (app API 13; 0
    /// never warns): from it the coordinator's context gauge warns.
    private(set) var contextHint = 0
    /// Each project's Save & Clear on its way or just done (T868).
    private(set) var clears: [String: SaveClear] = [:]
    /// Reads session.list while the bridge is up: the context gauges
    /// and a Save & Clear's progress follow it.
    private var polling: Task<Void, Never>?
    /// Why the last load failed, in words.
    private(set) var failure: String?
    private(set) var bridge: Bridge?
    /// The bridge's hello as last read: what its tm has (Compatibility),
    /// its level and methods.
    private(set) var hello: AppAPI.Hello?
    private var watching: Task<Void, Never>?
    /// Where the user is on each screen, which a resize keeps (T825):
    /// the Phone's, one for every server.
    @ObservationIgnored let places: Places
    /// Each session's composer files (T860), by its ref: kept across the
    /// screens and the link's drops, as its draft is.
    @ObservationIgnored private var trays: [SessionRef: ComposerTray] = [:]
    /// The conversation's files (session.file), the bridge's: nil when
    /// its tm sends none.
    private(set) var files: FileStore?

    /// Every need, as the Mac app's Needs you lists them; a paired
    /// server's only when the phone isn't paired with it directly too.
    var needs: [Need] { Need.of(projects: projects, sessions: sessions, peers: shownPeers) }

    /// The paired servers the screens list: those the phone pairs with
    /// directly show as servers of their own, so not here again (T852).
    var shownPeers: [AppAPI.Peer] { Pairings.shown(peers, direct()) }

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
            log = .demo
        } catch {
            failure = error.localizedDescription
        }
    }

    /// Closes it all, and forgets what it showed (the places are the
    /// Phone's, which forgets them with the last server).
    func disconnect() async {
        await bye("the user disconnected")
        await drop()
        source = .none
        projects = []
        sessions = []
        peers = []
        clears = [:]
        for t in trays.values { t.clear() }
        trays = [:]
    }

    /// Ends the bridge and the link, and the redials.
    private func drop() async {
        await unlink()
        hello = nil
        paths?.cancel()
        paths = nil
        path = nil
        wasUp = false
    }

    /// Closes the link and stops the redials, what it showed kept.
    private func unlink() async {
        linking?.cancel()
        linking = nil
        napping?.cancel()
        grace?.cancel()
        grace = nil
        dropped = nil
        watching?.cancel()
        watching = nil
        polling?.cancel()
        polling = nil
        await bridge?.close()
        bridge = nil
        transport = nil
    }

    private func connect(_ transport: any BridgeTransport, signer: (any WriteSigner)? = nil) async {
        let b = Bridge(transport: transport)
        do {
            let h = try await b.start()
            hello = h
            await b.setSigner(signer)
            bridge = b
            files = Compatibility.sendsFiles(h) ? FileStore { p in try await b.call(AppAPI.Methods.sessionFile, p) } : nil
            generation += 1
            await refresh()
        } catch {
            failure = error.localizedDescription
        }
    }

    // MARK: The paired server

    /// Says why something failed, on the screen at hand.
    func failed(_ error: any Error) {
        failure = error.localizedDescription
    }

    /// Opens the server s, this pairing's: its link kept from now on.
    func open(_ s: DevicePairing.Server) {
        source = .server(s)
        watchPaths()
        resume()
    }

    /// The server's name changed in a pairing again: what the screens say.
    func renamed(_ s: DevicePairing.Server) {
        if case .server = source { source = .server(s) }
    }

    /// The app came to the front: back from its grace, the link kept is
    /// pinged (is it still there?); else link again (redial, hello,
    /// streams), what the screens show kept until it is up.
    func resume() {
        if let g = grace {
            g.cancel()
            grace = nil
            transport?.check()
        }
        guard case .server(let s) = source, linking == nil else { return }
        linking = Task { await self.keep(s) }
    }

    /// The app went to the back: the link stays up for LinkKeep's grace,
    /// so a glance at another app keeps it, then suspends.
    func toBack() async {
        guard case .server = source, linking != nil, grace == nil else { return }
        let g = Task { _ = try? await Task.sleep(for: .seconds(LinkKeep.backgroundGrace)) }
        grace = g
        await g.value
        guard grace == g, !g.isCancelled else { return }
        await suspend()
    }

    /// The link closes (the server sees the device go), and resume opens
    /// it again. What the screens show stays, the hello too (T857).
    func suspend() async {
        guard case .server = source else { return }
        if link == .up { note(.background) }
        await bye("the app went to the background")
        await unlink()
        link = .connecting
    }

    /// Keeps the link to s until cancelled: dial, hello, read it all,
    /// follow projects.watch; when that ends, dial again after a wait
    /// (LinkKeep.backoff, cut short by a network change). While the
    /// server is asleep the relay's room waits for it; when the relay
    /// ends that wait, the next one starts.
    private func keep(_ s: DevicePairing.Server) async {
        fails = 0
        while !Task.isCancelled {
            if link != .asleep { link = .connecting }
            do {
                let id = try DeviceStore.identity(key)
                let t = try await LinkTransport.connect(to: s.endpoint, as: id) { [weak self] in
                    Task { @MainActor in self?.asleep() }
                }
                let lost = AsyncStream<any Error>.makeStream()
                dropped = { lost.continuation.yield($0) }
                transport = t
                await connect(t, signer: WriteKey.existing(key))
                guard bridge != nil else { throw LinkError.closed }
                link = .up
                wasUp = true
                for t in trays.values { t.resumeDropped() }
                fails = 0
                rush = false
                log.up(at: .now)
                log.save(pairing: key)
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
                transport = nil
                // The hello stays until the redial's: the same server
                // at the same level, so what its level shows (the
                // composer, the terminal's keys) stays up, and the
                // keyboard with it (T857). Writes fail meanwhile.
                if link != .asleep {
                    let (cause, detail) = Self.cause(error)
                    note(cause, detail)
                    link = .lost(error.localizedDescription)
                    fails += 1
                }
            }
            let wait = rush ? 0 : link == .asleep ? 1 : LinkKeep.backoff(fails)
            rush = false
            await nap(wait)
        }
    }

    /// Waits seconds before the next dial, unless redial cuts it short.
    private func nap(_ seconds: Int) async {
        guard seconds > 0 else { return }
        let t = Task { _ = try? await Task.sleep(for: .seconds(seconds)) }
        napping = t
        await t.value
        napping = nil
    }

    /// The relay says the server isn't in the room.
    private func asleep() {
        if link != .asleep { note(.asleep) }
        link = .asleep
    }

    /// A drop for the log, once the link was up (a first dial failing is
    /// none).
    private func note(_ cause: LinkLog.Cause, _ detail: String = "") {
        guard wasUp, source != .demo else { return }
        log.dropped(at: .now, cause, detail: detail)
        log.save(pairing: key)
    }

    /// Why the link ended, as the log keeps it.
    static func cause(_ error: any Error) -> (LinkLog.Cause, String) {
        let words = error.localizedDescription
        switch error as? LinkError {
        case .pingUnanswered?: return (.ping, words)
        case .timedOut?: return (.idle, words)
        // The server's TLS closing, or the relay saying it left.
        case .closed?, .relayClosed(_, "peer-gone")?: return (.server, words)
        case .relayClosed?: return (.relay, words)
        default:
            if case .closed? = error as? BridgeError { return (.server, words) }
            return (.error, words)
        }
    }

    /// Tells the server why the link closes, for its log (device.bye,
    /// app API 37, T892), when the link is up and its tm has it: the
    /// line goes before the close, which follows at once.
    private func bye(_ reason: String) async {
        guard link == .up, let b = bridge, await b.hello?.methods.contains(AppAPI.Methods.deviceBye.name) == true else { return }
        await b.post(AppAPI.Methods.deviceBye, AppAPI.ByeParams(reason: reason))
    }

    /// Whether error is the link's dropping, which the screens don't
    /// show: they keep what they have until the redial (T859).
    static func isDrop(_ error: any Error) -> Bool {
        if error is LinkError { return true }
        if case .closed? = error as? BridgeError { return true }
        return false
    }

    /// Whether the link is on its way back: the screens keep what they
    /// show, and say nothing of the streams that ended.
    var relinking: Bool {
        if case .server = source { return link != .up }
        return false
    }

    // MARK: The network (NWPathMonitor)

    /// Follows the phone's network while a server is open: a change
    /// redials at once, or checks the link (LinkKeep.onPath).
    private func watchPaths() {
        guard paths == nil else { return }
        let m = NWPathMonitor()
        m.pathUpdateHandler = { [weak self] p in
            let now = LinkKeep.Path(online: p.status == .satisfied, via: p.availableInterfaces.first.map { Self.name($0.type) } ?? "")
            Task { @MainActor in self?.pathChanged(now) }
        }
        m.start(queue: DispatchQueue(label: "dev.terminatr.ios.path"))
        paths = m
    }

    private nonisolated static func name(_ t: NWInterface.InterfaceType) -> String {
        switch t {
        case .wifi: "wifi"
        case .cellular: "cellular"
        case .wiredEthernet: "wired"
        case .loopback: "loopback"
        default: "other"
        }
    }

    private func pathChanged(_ now: LinkKeep.Path) {
        let act = LinkKeep.onPath(from: path, to: now, up: link == .up)
        path = now
        switch act {
        case .nothing: break
        case .check: transport?.check()
        case .redial: redial()
        case .reconnect:
            note(.path, "now on \(now.via)")
            redial()
            let t = transport
            Task {
                await bye("the network changed: now on \(now.via)")
                t?.close()
            }
        }
    }

    /// The next dial goes now, the backoff from the start.
    private func redial() {
        fails = 0
        rush = true
        napping?.cancel()
    }

    /// Reads it all again (pull to refresh): the session and peer lists,
    /// and projects.watch opened afresh.
    func refresh() async {
        guard let b = bridge else { return }
        failure = nil
        hello = await b.hello
        do {
            let l = try await b.call(AppAPI.Methods.sessionList)
            listed(l)
            needsFirst = l.needsFirst
            follow(b)
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
                } catch let e as AppAPI.Error where e.code == AppAPI.tooBig {
                    // The link is up: a redial would ask the same (T892).
                    self?.failure = e.localizedDescription
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

    /// Answers a question; one answered elsewhere meanwhile throws
    /// QuestionGone, its card gone with the sessions read again.
    func answer(_ a: Need.Answer, _ option: String) async throws {
        do {
            _ = try await connected().call(AppAPI.Methods.sessionAnswer, a.params(option))
        } catch let e as AppAPI.Error where e.code == "no-question" {
            await reread()
            if QuestionPlan.gone(e.code, asked: a.question, now: sessions.first { $0.id == a.session }?.question) { throw QuestionGone() }
            throw e
        }
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

    /// Acts on a paired server's need through this server (peer.need-act,
    /// app API 30), signed as the phone's other answers: that server
    /// journals it with this phone's name, and refuses it once the need
    /// changed or when it is an escape's.
    func actOnNeed(_ params: AppAPI.PeerNeedActParams) async throws -> AppAPI.PeerNeedActResult {
        let r = try await connected().call(AppAPI.Methods.peerNeedAct, params)
        await reread()
        return r
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
    /// running turn ("now"); files are the refs its tray's uploads
    /// answered, which tm names after the text.
    func prompt(_ session: String, text: String, mode: String = "", files: [String] = []) async throws -> AppAPI.PromptResult {
        let r = try await connected().call(AppAPI.Methods.sessionPrompt,
                                           AppAPI.PromptParams(id: session, text: text, mode: mode, files: files))
        await reread()
        return r
    }

    /// Whether the composer takes files for ref's session (session.upload,
    /// app API 15, at the write level): this server's sessions only.
    func attaches(_ ref: SessionRef) -> Bool {
        !ref.isRemote && hello.map(Compatibility.takesFiles) == true && can(AppAPI.Methods.sessionUpload.name)
    }

    /// ref's composer files, sent in phone-sized chunks to whichever
    /// session runs it when each chunk goes; one the link's drop cut
    /// goes on as the link comes back. The demo's shows a file in.
    func tray(_ ref: SessionRef) -> ComposerTray {
        if let t = trays[ref] { return t }
        let t = ComposerTray(chunk: ComposerFiles.phoneChunk) { [weak self] p in
            guard let self else { throw LinkError.closed }
            return try await self.upload(ref, p)
        }
        t.dropped = { Self.isDrop($0) }
        if source == .demo { t.keep(ComposerFile(name: "login-dark.png", size: 182_406, state: .ready(ref: "demo-login-dark.png"))) }
        trays[ref] = t
        return t
    }

    /// A chunk of a file for ref's session (session.upload), signed.
    private func upload(_ ref: SessionRef, _ p: AppAPI.UploadParams) async throws -> AppAPI.Upload {
        guard let id = sessionID(ref) else { throw AppAPI.Error(code: "no-session", message: "No session runs it now") }
        var p = p
        p.session = id
        return try await connected().call(AppAPI.Methods.sessionUpload, p)
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
        listed(l)
    }

    /// How often the sessions are read while the bridge is up, from a
    /// tm without sessions.watch.
    static let sessionsEvery = Duration.seconds(4)

    /// Follows the sessions until the bridge ends: sessions.watch's
    /// lists as they change (app API 32), so a question's card shows as
    /// its dialog opens and goes once it is answered anywhere; from an
    /// older tm, session.list every sessionsEvery.
    private func follow(_ b: Bridge) {
        polling?.cancel()
        polling = Task { [weak self] in
            if let h = await b.hello, Compatibility.watchesSessions(h),
               let sub = try? await b.subscribe(AppAPI.Methods.sessionsWatch, AppAPI.NoParams()) {
                self?.listed(sub.result)
                // Its end is the link's, which projects.watch tells.
                do {
                    for try await e in sub.events { self?.listed(e.data) }
                } catch {}
                return
            }
            while !Task.isCancelled {
                try? await Task.sleep(for: Self.sessionsEvery)
                guard !Task.isCancelled else { return }
                await self?.reread()
            }
        }
    }

    /// Takes a session list: the sessions, the hint, and each Save &
    /// Clear moved on from its coordinator.
    private func listed(_ l: AppAPI.SessionList) {
        sessions = l.sessions
        contextHint = l.contextHint
        let now = Date.now
        for (p, var c) in clears {
            c.follow(coordinatorSession(p), at: now)
            clears[p] = c.over(at: now) ? nil : c
        }
    }

    // MARK: Save & Clear (the write level, T868), as the Mac's

    /// Whether the phone saves and clears coordinators here.
    var clearsCoordinators: Bool {
        (hello?.appApi ?? 0) >= Compatibility.projectControlsAPI && can(AppAPI.Methods.coordinatorClear.name)
    }

    /// ref's context use: a coordinator's warns from the hint, a
    /// thread's only when nearly full; a paired server's none.
    func context(_ ref: SessionRef) -> ContextUse? {
        guard !ref.isRemote else { return nil }
        if ref.isCoordinator { return ContextUse.of(coordinatorSession(ref.project), hint: contextHint) }
        return sessionID(ref).flatMap { id in ContextUse.of(sessions.first { $0.id == id }, hint: 0) }
    }

    /// Saves and clears project's coordinator (coordinator.clear), as
    /// tm clear does, and follows it in clears.
    func saveClear(_ project: String) async {
        guard clears[project]?.running != true else { return }
        var c = SaveClear(coordinator: coordinatorSession(project), at: .now)
        clears[project] = c
        do {
            _ = try await connected().call(AppAPI.Methods.coordinatorClear, AppAPI.ProjectParams(project: project))
            c.asked()
        } catch {
            c.fail(NeedActs.words(error) ?? error.localizedDescription, at: .now)
        }
        clears[project] = c
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

    /// A ref's thread as it is named: its task's ref ("T14"), else its id.
    func tag(_ ref: SessionRef) -> String {
        if ref.isCoordinator { return "the coordinator" }
        if ref.isRemote { return ref.thread }
        return project(ref.project)?.threads.first { $0.record.id == ref.thread }?.task?.ref ?? ref.thread
    }

    /// Where a ref's session is, under its title (T848): "T14 · acme-web",
    /// a coordinator's project, and a paired server's alias after.
    func place(_ ref: SessionRef) -> String {
        [ref.isCoordinator ? "" : tag(ref), ref.project, ref.server].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// A ref's state, as its row says it: its pill in the header.
    func attention(_ ref: SessionRef) -> Attention? {
        if ref.isRemote { return Attention.of(peer: ref.session(in: peers)) }
        guard let p = project(ref.project) else { return nil }
        if ref.isCoordinator { return Attention.of(coordinator: coordinatorSession(ref.project), in: p) }
        return p.threads.first { $0.record.id == ref.thread }.map(attention)
    }

    // MARK: A project's pages, read only, as the Mac app reads them

    /// The pages this tm has (Memory and Library need app API 21).
    var pages: [ProjectPage] { ProjectPage.pages(hello) }

    /// Whether this tm lists the board's done tasks (task.done, app API 20).
    var listsDone: Bool { hello.map(Compatibility.listsDone) ?? false }

    /// Whether this tm lists a project's designs (design.list, app API 31).
    var listsDesigns: Bool { hello.map(Compatibility.listsDesigns) ?? false }

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

    /// The project's designs, by name, each with its build in after
    /// order (design.list).
    func designs(_ project: String) async throws -> [AppAPI.Design] {
        try await connected().call(AppAPI.Methods.designList, AppAPI.ProjectParams(project: project)).designs
    }

    /// Whether this tm reads a paired server's project pages
    /// (peer.pages-read, app API 35).
    var readsPeerPages: Bool { hello.map(Compatibility.readsPeerPages) ?? false }

    /// A paired server's project's board and overview (peer.pages-read).
    func peerPages(_ alias: String, _ project: String) async throws -> AppAPI.PeerPages {
        try await connected().call(AppAPI.Methods.peerPagesRead, AppAPI.PeerPagesParams(alias: alias, project: project))
    }

    /// A paired server's project's pages as a screen shows them: not
    /// shared without a tm that reads them, or when that server says so.
    func peerPagesState(_ alias: String, _ project: String) async -> PeerPages.State<AppAPI.PeerPages> {
        guard readsPeerPages else { return .notShared }
        do {
            return .shared(try await peerPages(alias, project))
        } catch {
            return PeerPages.state(error)
        }
    }

    /// Whether this tm reads a paired server's project's designs
    /// (peer.design-list, app API 36).
    var readsPeerDesigns: Bool { hello.map(Compatibility.readsPeerDesigns) ?? false }

    /// A paired server's project's designs, without their pages or open
    /// questions (peer.design-list).
    func peerDesigns(_ alias: String, _ project: String) async throws -> [AppAPI.Design] {
        try await connected().call(AppAPI.Methods.peerDesignList, AppAPI.PeerPagesParams(alias: alias, project: project)).designs
    }

    /// A paired server's project's library, its threads' files
    /// (peer.library-list).
    func peerLibrary(_ alias: String, _ project: String) async throws -> [AppAPI.LibraryFile] {
        try await connected().call(AppAPI.Methods.peerLibraryList, AppAPI.PeerPagesParams(alias: alias, project: project)).files
    }

    /// A paired server's library file's bytes (peer.library-read).
    func peerLibraryData(_ alias: String, _ project: String, _ f: AppAPI.LibraryFile) async -> FileOutcome {
        guard let b = bridge else { return .unavailable("the link is down") }
        return await PeerPages.read({ p in try await b.call(AppAPI.Methods.peerLibraryRead, p) }, alias: alias, project: project, file: f)
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

extension LinkLog {
    /// The demo's Connection: a day's drops as a phone sees them.
    static var demo: LinkLog {
        let now = Date.now
        var l = LinkLog()
        let made: [(TimeInterval, LinkLog.Cause, String, TimeInterval)] = [
            (-7 * 3600, .background, "", 540),
            (-5 * 3600, .path, "now on cellular", 2),
            (-3 * 3600, .ping, "the relay didn't answer a ping: the network went away", 6),
            (-40 * 60, .background, "", 75),
        ]
        for (ago, cause, detail, down) in made {
            l.dropped(at: now.addingTimeInterval(ago), cause, detail: detail)
            l.up(at: now.addingTimeInterval(ago + down))
        }
        return l
    }
}
