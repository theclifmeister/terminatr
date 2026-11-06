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
    /// The programs (projects.watch, app API 44), while the server's
    /// caps say programs (the programs design §12): none while
    /// programs are off, and the sidebar is the one without.
    private(set) var programs: [AppAPI.Program] = []
    private(set) var sessions: [AppAPI.Session] = []
    private(set) var notices: [AppAPI.Notice] = []
    private(set) var peers: [AppAPI.Peer] = []
    /// Each paired server's latest ping, on its own: a ping alone doesn't
    /// change peer.list's gen, and only the status bar's menu reads it, so
    /// the sidebar doesn't draw again for it.
    private(set) var peerRTT: [String: Double] = [:]
    /// [ui] needs_first, as the last session list said: the sidebar
    /// lists the projects that need the user first, as the TUI's.
    private(set) var needsFirst = false
    /// [ui] context_hint, as the last session list said (app API 13; 0
    /// for never): a coordinator's row warns from it.
    private(set) var contextHint = 0
    /// The projects whose coordinator sleeps (app API 47's asleep), as
    /// the last session list said: opening one wakes it (Sleep).
    private(set) var asleep: Set<String> = []
    /// The projects whose coordinator the app is waking (project.open):
    /// its row and pane spin until session.list has it.
    private(set) var waking: Set<String> = []
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
    /// What the right panel was left showing, Files (⌘⌥E) or the Inspector
    /// (⌘⌥I), nil hidden.
    var panel = RightPanel.saved(UserDefaults.standard.string(forKey: AppModel.panelKey),
                                 inspector: UserDefaults.standard.object(forKey: AppModel.inspectorKey) as? Bool) {
        didSet { UserDefaults.standard.set(panel?.rawValue ?? "", forKey: Self.panelKey) }
    }
    /// Whether the window shows its sidebar (⌃⌘S).
    var sidebarShown = UserDefaults.standard.object(forKey: AppModel.sidebarKey) as? Bool ?? true {
        didSet { UserDefaults.standard.set(sidebarShown, forKey: Self.sidebarKey) }
    }
    /// The sidebar's Pair a server: its sheet, shown.
    var pairServer = false
    /// The server the sidebar should scroll to, each time anew.
    struct ServerReveal: Equatable {
        var key = ""
        var n = 0
    }
    var serverReveal = ServerReveal()
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
    /// ⌘K and the buttons: opens or closes the palette. It lives on the main
    /// window, which takes the keys for it: an ask from a session window
    /// would leave its field with no keys until a click (T1076).
    func togglePalette() {
        palette.toggle()
        if palette { showWindow?() }
    }

    /// Whether the palette shows a session's cards or composer (its
    /// act): ⌘↩ is its cards' then, bumping paletteChoice.
    var paletteActing = false
    private(set) var paletteChoice = 0
    /// Whether the Keyboard Shortcuts sheet shows.
    var shortcuts = false
    /// Whether the first-run sheet shows (FirstRun; Help › Getting
    /// Started shows it again).
    var welcome = false
    /// Whether this tm takes files for a prompt to ref's session
    /// (session.upload, app API 15; a director's from app API 46; a paired
    /// server's while it says so, at the write level).
    func attaches(_ ref: SessionRef) -> Bool {
        if case .connected(let h) = phase { return Compatibility.takesFiles(h, for: ref, peer: peer(ref.server)) }
        return false
    }

    /// The project File › New Task… adds to while its sheet shows.
    var newTask: String?
    /// The task the task view shows (TaskSheet), nil for none.
    var taskShown: TaskShown?
    /// A paired server's task the window shows, read only.
    var peerTaskShown: PeerTaskShown?
    /// The paired boards the command palette searches.
    var paletteBoards = PaletteBoards()
    /// What the bridge did: Settings › Advanced shows it.
    private(set) var log = BridgeLog()
    /// The need ⌥⌘↓ or ⌥⌘↑ showed last (Need.step).
    @ObservationIgnored private var lastNeed: String?

    /// Text for project's coordinator's composer; n tells one ask from
    /// the next.
    struct ComposerDraft: Equatable {
        let n: Int
        /// The session whose composer takes it.
        let ref: SessionRef
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
    /// sessions.watch sends the list as it changes: listSessions has
    /// nothing to add.
    @ObservationIgnored private var sessionsPushed = false

    /// The server's hello capabilities (pane-vt, transcript): the last
    /// bridge's while it is down, so the views stay as they were.
    var caps: [String] { hello?.server?.caps ?? [] }

    /// Whether the server has programs on (its caps say programs): the
    /// app draws no program row, screen or badge without.
    var showsPrograms: Bool { caps.contains("programs") }

    /// What Settings › Advanced says about tm's signature.
    private(set) var tmCheck = TMCheckState.none

    /// What came since the bridge started: needs wait for all of it, so
    /// the first set is whole.
    private struct Loaded {
        var projects = false, sessions = false, peers = false
        var all: Bool { projects && sessions && peers }
    }

    /// The settings key of the configured tm path (TMLocator.configured).
    static let tmPathKey = "tmPath"
    /// The settings key of the path the app last ran tm from (TMAsk).
    static let tmLastPathKey = "tmLastPath"
    static let navKey = "navigation"
    static let inspectorKey = "inspector"
    static let panelKey = "rightPanel"
    static let sidebarKey = "sidebar"

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
            try await checkSignature(tm)
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
            // The app's crash and hang reports go to this computer's tm
            // server and nowhere else (T957).
            if hello.methods.contains(AppAPI.Methods.appreportAdd.name) {
                CrashReporter.shared.setSender({ p in try await b.call(AppAPI.Methods.appreportAdd, p) }, for: "mac")
            }
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
            CrashReporter.shared.setSender(nil, for: "mac")
            bridge = nil
            files = nil
        }
    }

    /// Before a signed app runs tm: compares it with the app's own Team
    /// ID and asks when it fails, or passes at another path than last
    /// time (TMAsk). A development build has no Team ID to compare and
    /// skips it, so throwaway copies can run any -tmPath. Throws to run
    /// nothing.
    private func checkSignature(_ tm: URL) async throws {
        guard let team = TMSignature.ownTeam else {
            tmCheck = .skipped
            log.add("Not checking tm's signature: this build of the app isn't signed")
            return
        }
        tmCheck = .none
        let signature = await Task.detached { TMSignature.check(tm, team: team) }.value
        try Task.checkCancellation()
        var check = TMCheckState.passed
        if let ask = TMAsk(path: tm.path, signature: signature, last: UserDefaults.standard.string(forKey: Self.tmLastPathKey)) {
            log.add("Asking before running \(tm.path): \(ask.why)")
            tmCheck = .asked
            let yes = await TMAskPanel.ask(ask)
            try Task.checkCancellation()
            guard yes else { throw TMNotRun(path: tm.path) }
            check = .ranAnyway
            log.add("You chose to run \(tm.path)")
        }
        UserDefaults.standard.set(tm.path, forKey: Self.tmLastPathKey)
        tmCheck = check
    }

    /// Watches the projects until the bridge ends. A lean watch (app
    /// API 8) leaves the inactive projects but the shown one out, so it
    /// opens again whenever the window shows another inactive one.
    private func watchProjects(_ b: Bridge) async throws {
        lean = (await b.hello?.appApi ?? 0) >= AppAPI.leanAPI
        guard lean else {
            let sub = try await b.subscribe(AppAPI.projectsWatchDetail, AppAPI.DetailParams(detail: true))
            took(sub.result)
            for try await e in sub.events {
                took(e.data)
            }
            throw BridgeError.closed("The server stopped sending the projects.")
        }
        while true {
            let shown = nav.selection?.shownInactive(projects) ?? ""
            watchedShown = shown
            let sub = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true, lean: true, shown: shown))
            took(sub.result)
            let events = Task { @MainActor in
                for try await e in sub.events {
                    self.took(e.data)
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

    private func took(_ ps: AppAPI.Projects) {
        projects = ps.projects
        programs = showsPrograms ? ps.programs : []
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

    /// Follows the sessions until the bridge ends: sessions.watch's
    /// lists as they change (app API 32), so a question's card shows as
    /// its dialog opens and goes once it is answered anywhere; from an
    /// older tm, or once the watch ends, session.list every
    /// sessionsEvery.
    private func readSessions(_ b: Bridge) async throws {
        if let h = await b.hello, Compatibility.watchesSessions(h) {
            let sub = try await b.subscribe(AppAPI.Methods.sessionsWatch, AppAPI.NoParams())
            listed(sub.result)
            sessionsPushed = true
            defer { sessionsPushed = false }
            for try await e in sub.events {
                listed(e.data)
            }
        }
        while true {
            listed(try await b.call(AppAPI.Methods.sessionList))
            try await Task.sleep(for: Self.sessionsEvery)
        }
    }

    /// Takes a session list: the sessions, the mods' notices and the
    /// settings it carries.
    private func listed(_ l: AppAPI.SessionList) {
        sessions = l.sessions
        notices = l.notices
        needsFirst = l.needsFirst
        contextHint = l.contextHint
        asleep = Sleep.projects(l)
        waking = Sleep.settle(waking, sessions: sessions)
        loaded.sessions = true
        changed()
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
            let rtt = Dictionary(l.peers.map { ($0.alias, $0.rttMs) }, uniquingKeysWith: { a, _ in a })
            if rtt != peerRTT { peerRTT = rtt }
            if l.gen != gen || !loaded.peers {
                gen = l.gen
                peers = l.peers
                paletteBoards.prune(peers: peers, reads: readsPeerPages)
                loaded.peers = true
                changed()
            }
            try await Task.sleep(for: Self.peersEvery)
        }
    }

    private func changed() {
        guard loaded.all else { return }
        needs = Need.of(projects: projects, sessions: sessions, peers: peers, programs: programs)
        settleSelection()
        onNeeds?(self)
    }

    /// Once everything is listed: a selection (restored, or kept from
    /// before) that names nothing any more falls back to its project or
    /// the first active one, and so does none.
    private func settleSelection() {
        var n = nav
        n.prune(fallback: activeProjects.first.map { .project($0.slug) }) { [projects, peers, programs] in
            $0.exists(projects: projects, peers: peers, programs: programs) || self.leftOut($0)
        }
        if n != nav { nav = n }
    }

    /// Lists the sessions now, between readSessions' rounds (a session
    /// window's watch changed: its question or escape may have): once
    /// at a time, and once more after it when asked meanwhile.
    func listSessions() {
        guard !sessionsPushed else { return }
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
                    asleep = Sleep.projects(l)
                    waking = Sleep.settle(waking, sessions: sessions)
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
        SidebarTree.inactive(projects, needsFirst: needsFirst, sessions: sessions, nav: nav, programs: programs)
    }

    /// This server's part of the sidebar: its active projects and its
    /// programs, in the TUI's order (SidebarTree.entries).
    var entries: [SidebarEntry] {
        SidebarTree.entries(projects, programs: programs, needsFirst: needsFirst, sessions: sessions, peers: peers, nav: nav)
    }

    /// The program each paired member ("alias/slug") is in: its tag
    /// under its server's group.
    var pairedMembers: [String: String] { SidebarTree.pairedMembers(programs) }

    func program(_ slug: String) -> AppAPI.Program? {
        programs.first { $0.slug == slug }
    }

    /// Program slug's director's session, when one runs.
    func directorSession(_ slug: String) -> AppAPI.Session? {
        sessions.first { $0.role == "director" && $0.project == slug && $0.server.isEmpty }
    }

    /// The needs of a program, its badge: its own and its members',
    /// each once (SidebarTree.needs(of:in:)).
    func needs(of g: AppAPI.Program) -> [Need] {
        SidebarTree.needs(of: g, in: needs)
    }

    /// The human acts of the program screen (the programs design §7.2,
    /// app API 44): each the user's own, never the director's.
    func programAnswer(_ slug: String, ref: String, choices: [String], other: String) async throws -> AppAPI.ProgramAnswered {
        guard let b = bridge else { throw BridgeError.closed("") }
        return try await b.call(AppAPI.Methods.programAnswer, AppAPI.ProgramAnswerParams(program: slug, ref: ref, choices: choices, other: other))
    }

    /// Accepts the director's recommendation on a question of program
    /// slug's Needs you (app API 48): answered as the user's own.
    func programRecommendAccept(_ slug: String, question: String) async throws -> AppAPI.RecommendAccepted {
        guard let b = bridge else { throw BridgeError.closed("") }
        return try await b.call(AppAPI.Methods.programRecommendAccept, AppAPI.ProgramRecommendAcceptParams(program: slug, question: question))
    }

    /// Accepts umbrella task of program slug (accept), or sends it back
    /// with the user's note.
    func programUmbrella(_ slug: String, task: String, accept: Bool, note: String = "") async throws -> AppAPI.ProgramUmbrellaDone {
        guard let b = bridge else { throw BridgeError.closed("") }
        let p = AppAPI.ProgramUmbrellaParams(program: slug, task: task, note: note)
        return try await b.call(accept ? AppAPI.Methods.programUmbrellaAccept : AppAPI.Methods.programUmbrellaSendBack, p)
    }

    /// Renames program from to to; the window follows it.
    func programRename(_ from: String, to: String) async throws -> AppAPI.ProgramRenamed {
        guard let b = bridge else { throw BridgeError.closed("") }
        let r = try await b.call(AppAPI.Methods.programRename, AppAPI.ProgramRenameParams(from: from, to: to))
        if nav.selection?.isProgram == true, nav.selection?.project == from { nav.select(.program(to)) }
        return r
    }

    /// Whether this tm starts a program's director (program.open, app
    /// API 45): the director pane's Start.
    var opensDirector: Bool {
        if case .connected(let h) = phase { return Compatibility.opensDirector(h) }
        return false
    }

    /// Whether this tm takes Accept on the director's recommendation.
    var acceptsRecommendations: Bool {
        if case .connected(let h) = phase { return Compatibility.acceptsRecommendations(h) }
        return false
    }

    /// Starts program slug's director unless it runs (program.open, as
    /// the TUI's enter on its row), and shows it once session.list has
    /// it.
    func startDirector(_ slug: String) async throws {
        _ = try await connected().call(AppAPI.Methods.programOpen, AppAPI.ProgramOpenParams(program: slug, cols: 120, rows: 36))
        listSessions()
        select(.session(.director(slug)))
    }

    /// Deletes program slug, confirm its name typed again; the window
    /// goes to the first project.
    func programDelete(_ slug: String, confirm: String) async throws -> AppAPI.ProgramDeleted {
        guard let b = bridge else { throw BridgeError.closed("") }
        let r = try await b.call(AppAPI.Methods.programDelete, AppAPI.ProgramDeleteParams(program: slug, confirm: confirm))
        if nav.selection?.isProgram == true, nav.selection?.project == slug, let first = activeProjects.first {
            nav.select(.project(first.slug))
        }
        return r
    }

    /// The program screen's detail (program.get) and its Needs you
    /// (program.needs), as the server reads them now.
    func programDetail(_ slug: String) async throws -> (AppAPI.ProgramDetail, AppAPI.ProgramNeeds?) {
        guard let b = bridge else { throw BridgeError.closed("") }
        let d = try await b.call(AppAPI.Methods.programGet, AppAPI.ProgramParams(program: slug))
        let n = try? await b.call(AppAPI.Methods.programNeeds, AppAPI.ProgramParams(program: slug))
        return (d, n)
    }

    /// The project's coordinator's session, when one runs.
    func coordinatorSession(_ project: String) -> AppAPI.Session? {
        sessions.first { $0.role == "coordinator" && $0.project == project && $0.server.isEmpty }
    }

    /// What p's coordinator says of itself: its dot, its card.
    func coordinator(of p: AppAPI.Project) -> Attention {
        Attention.of(coordinator: coordinatorSession(p.slug), in: p, sleep: sleep(of: p.slug))
    }

    /// Whether project's coordinator sleeps, wakes or neither (Sleep).
    func sleep(of project: String) -> Sleep {
        Sleep.of(project, running: coordinatorSession(project) != nil, asleep: asleep, waking: waking)
    }

    /// Wakes project's sleeping coordinator (project.open resumes it
    /// under its session id, its conversation kept), as enter on its row
    /// in the TUI: its row and pane spin until it runs. Opening it (the
    /// sidebar's selection) does this; a refusal is the pane's to say.
    func wake(_ project: String) async throws {
        guard sleep(of: project) == .asleep else { return }
        waking.insert(project)
        do {
            _ = try await connected().call(AppAPI.Methods.projectOpen, AppAPI.ProjectOpenParams(project: project, cols: 120, rows: 36))
        } catch {
            waking.remove(project)
            throw error
        }
        listSessions()
        // Never a spinner for good: a coordinator not listed by then is
        // asleep again, or failed, and its pane says so.
        Task {
            try? await Task.sleep(for: .seconds(30))
            waking.remove(project)
        }
    }

    /// Opening a sleeping coordinator wakes it, as enter on its row in
    /// the TUI (T980): sel is the window's new selection.
    func opened(_ sel: Selection?) {
        guard case .session(let r) = sel, r.isCoordinator, !r.isRemote, sleep(of: r.project) == .asleep else { return }
        Task { try? await wake(r.project) }
    }

    func project(_ slug: String) -> AppAPI.Project? {
        projects.first { $0.slug == slug }
    }

    /// ref's thread on this server; nil for a paired server's.
    func thread(_ ref: SessionRef) -> AppAPI.Thread? {
        ref.ownThread(in: projects)
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
        if let s = Selection.route(project: project, session: session, projects: projects, sessions: sessions, peers: peers, programs: programs) {
            nav.select(s)
        }
        showWindow?()
    }

    /// The status bar's server: unfolds it in the sidebar, scrolled to,
    /// and shows its first project unless it shows something of it already.
    func go(toServer alias: String) {
        nav.collapsed.remove(Navigation.sectionKey(server: alias))
        filter = ""
        sidebarShown = true
        if let s = ServerList.landing(alias, shown: nav.selection, entries: entries, peers: peers) {
            nav.select(s)
        }
        serverReveal = ServerReveal(key: Navigation.sectionKey(server: alias), n: serverReveal.n + 1)
        showWindow?()
    }

    /// Where a need takes the window (Selection.route).
    func route(_ n: Need) -> Selection? {
        Selection.route(project: n.project, session: n.session, projects: projects, sessions: sessions, peers: peers, programs: programs)
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
        filesModels[.session(m.ref)]?.pane.shown = nil
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
        case .program(let slug): "\(slug) · program"
        }
    }

    func title(_ ref: SessionRef) -> String {
        if ref.isRemote {
            let task = ref.session(in: peers)?.task ?? ""
            return "\(ref.server) › \(ref.project) · " + (ref.isCoordinator ? "coordinator" : [task, ref.thread].filter { !$0.isEmpty }.joined(separator: " "))
        }
        if ref.isCoordinator { return "\(ref.project) · coordinator" }
        if ref.director { return "\(ref.project) · director" }
        let t = thread(ref)
        return [t?.task?.ref, t?.record.title ?? ref.thread].compactMap { $0 }.joined(separator: " ")
    }

    // MARK: sessions' streams

    /// The session models with their streams: the shown one and the
    /// few shown before it (warmth), so switching back to one finds
    /// its conversation loaded.
    @ObservationIgnored private var models: [SessionRef: SessionModel] = [:]
    @ObservationIgnored private var warmth = WarmSet<SessionRef>(limit: 3)

    /// The Files panels' models, the shown selection's and the few before
    /// it, with the file tabs they opened.
    @ObservationIgnored private var filesModels: [Selection: FilesModel] = [:]
    @ObservationIgnored private var filesWarmth = WarmSet<Selection>(limit: 8)

    /// selection's Files model, made when it has none.
    func filesModel(_ selection: Selection) -> FilesModel {
        for gone in filesWarmth.touch(selection) {
            filesModels.removeValue(forKey: gone)
        }
        if let m = filesModels[selection] { return m }
        let m = FilesModel()
        filesModels[selection] = m
        return m
    }

    /// What Files browses for selection; nil leaves its button off.
    func filesPlace(_ selection: Selection?) -> FilesPlace? {
        var session: String?
        var done = false
        var task: String?
        switch selection {
        case .project(let slug, let server):
            session = server.isEmpty ? coordinatorSession(slug)?.id : nil
        case .session(let ref):
            session = sessionID(ref)
            if ref.isRemote {
                task = ref.session(in: peers)?.task
            } else if !ref.isCoordinator {
                let t = thread(ref)
                done = t?.record.done ?? true
                task = t?.task?.ref
            }
        case .program, nil:
            break
        }
        return FilesPlace.of(selection, sessionID: session, threadDone: done, task: task,
                             readsWorktree: caps.contains("worktree-files"), hasLibrary: projectTabs.contains(.library),
                             readsProgramLibrary: readsProgramLibrary)
    }

    /// Whether this tm lists a program's library: a director's Files view.
    var readsProgramLibrary: Bool {
        if case .connected(let h) = phase { return Compatibility.readsProgramLibrary(h) }
        return false
    }

    /// What the right panel shows for selection.
    func panelShown(_ selection: Selection?) -> RightPanel? {
        selection == nil ? nil : RightPanel.shown(panel, hasFiles: filesPlace(selection) != nil)
    }

    /// ⌘⌥E and ⌘⌥I, and their buttons: that view, or hidden when it shows.
    func press(_ view: RightPanel) {
        panel = RightPanel.pressed(view, now: panelShown(nav.selection))
        if panel == .files, let s = nav.selection { filesModel(s).askFocus() }
    }

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
    /// The project's mods (mods.list, T869); consent names the mod whose
    /// consent's lines to read with them.
    func mods(_ project: String, consent: String = "") async throws -> [AppAPI.ProjectMod] {
        try await connected().call(AppAPI.Methods.modsList, AppAPI.ModsListParams(project: project, consent: consent)).mods
    }

    /// Turns mod m on or off for project as its button says, with the key
    /// of what it asked (mods.set): what came of it.
    func setMod(_ m: AppAPI.ProjectMod, project: String) async throws -> String {
        try await connected().call(AppAPI.Methods.modsSet, ProjectMods.set(m, project: project)).words
    }

    /// Sets key of mod m for project, nil to follow all projects again
    /// (mods.setting).
    func setModSetting(_ m: AppAPI.ProjectMod, project: String, key: String, _ value: AppAPI.ModValue?) async throws -> String {
        try await connected().call(AppAPI.Methods.modsSetting, ProjectMods.setting(m, project: project, key: key, value)).words
    }

    /// Whether this tm changes a project's mods here (mods.set, app API
    /// 38).
    var changesMods: Bool {
        if case .connected(let h) = phase { return Compatibility.changesMods(h) }
        return false
    }

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
        try await connected().call(AppAPI.Methods.libraryList, AppAPI.LibraryListParams(project: project)).files
    }

    /// Program slug's library: its director's own files, then its members'
    /// (library.list's program form).
    func programLibrary(_ slug: String) async throws -> AppAPI.Library {
        try await connected().call(AppAPI.Methods.libraryList, AppAPI.LibraryListParams(project: "", program: AppAPI.LibraryProgram(slug: slug)))
    }

    /// The bytes of a file of program slug's library: a paired member's over
    /// the link, a member's as any file of its project, the director's own by
    /// the program.
    func programLibraryData(_ slug: String, _ f: AppAPI.LibraryFile) async -> FileOutcome {
        if !f.server.isEmpty { return await peerLibraryData(f.server, f.project, f) }
        if !f.project.isEmpty { return await libraryData(f.project, f) }
        guard let b = bridge else { return .unavailable("tm isn't connected") }
        return await LibraryPage.read({ p in try await b.call(AppAPI.Methods.libraryRead, p) }, project: "", program: slug, file: f)
    }

    /// A library file's bytes (library.read, a chunk at a time).
    func libraryData(_ project: String, _ f: AppAPI.LibraryFile) async -> FileOutcome {
        guard let b = bridge else { return .unavailable("tm isn't connected") }
        return await LibraryPage.read({ p in try await b.call(AppAPI.Methods.libraryRead, p) }, project: project, file: f)
    }

    /// Whether a Library draws thumbnails: this server's, or a paired
    /// server's (which may still answer no-thumbnail, and then has rows).
    func drawsThumbnails(peer: String?) -> Bool {
        guard case .connected(let h) = phase else { return false }
        return peer == nil ? Compatibility.readsThumbnails(h) : Compatibility.readsPeerThumbnails(h)
    }

    /// A library file's thumbnail, nil for a plain row.
    func libraryThumbnail(_ project: String, peer: String?, _ f: AppAPI.LibraryFile) async -> AppAPI.LibraryThumbnail? {
        guard let b = bridge else { return nil }
        if let peer {
            return await PeerPages.thumbnail({ p in try await b.call(AppAPI.Methods.peerLibraryThumbnail, p) }, alias: peer, project: project, file: f)
        }
        return await LibraryPage.thumbnail({ p in try await b.call(AppAPI.Methods.libraryThumbnail, p) }, project: project, file: f)
    }

    /// Whether this tm reads a paired server's project pages
    /// (peer.pages-read, app API 35).
    var readsPeerPages: Bool {
        if case .connected(let h) = phase { return Compatibility.readsPeerPages(h) }
        return false
    }

    /// Reads the paired boards due (PaletteBoards), each on its own so a
    /// slow server holds up only its own rows.
    func readPaletteBoards() {
        for key in paletteBoards.due(peers: peers, reads: readsPeerPages, now: Date()) {
            Task {
                do {
                    paletteBoards.store(try await peerPages(key.alias, key.project), for: key)
                } catch {
                    paletteBoards.failed(key)
                }
            }
        }
    }

    /// A paired server's project's board and overview (peer.pages-read):
    /// its "not shared" errors as AppAPI.Error (PeerPages.notSharedCodes).
    func peerPages(_ alias: String, _ project: String) async throws -> AppAPI.PeerPages {
        try await connected().call(AppAPI.Methods.peerPagesRead, AppAPI.PeerPagesParams(alias: alias, project: project))
    }

    /// Whether this tm reads a paired server's project's designs
    /// (peer.design-list, app API 36).
    var readsPeerDesigns: Bool {
        if case .connected(let h) = phase { return Compatibility.readsPeerDesigns(h) }
        return false
    }

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

    /// A paired server's library file's bytes (peer.library-read, a chunk
    /// at a time).
    func peerLibraryData(_ alias: String, _ project: String, _ f: AppAPI.LibraryFile) async -> FileOutcome {
        guard let b = bridge else { return .unavailable("tm isn't connected") }
        return await PeerPages.read({ p in try await b.call(AppAPI.Methods.peerLibraryRead, p) }, alias: alias, project: project, file: f)
    }

    /// The project tabs this tm has (Memory and Library need app API 21).
    var projectTabs: [ProjectTab] {
        if case .connected(let h) = phase { return ProjectTab.tabs(h) }
        return ProjectTab.tabs(nil)
    }

    /// The tabs of the project s shows, this server's or a paired one's.
    func projectTabs(_ s: Selection) -> [ProjectTab] {
        guard !s.server.isEmpty else { return projectTabs }
        if case .connected(let h) = phase { return ProjectTab.paired(h) }
        return ProjectTab.paired(nil)
    }

    /// The shown project's tab in the title row: the one it was left on,
    /// across a relaunch too (nav), while this tm has it; else Overview.
    var projectTab: ProjectTab {
        get {
            guard let s = nav.selection, let t = nav.projectTabs[s].flatMap(ProjectTab.init(rawValue:)),
                  projectTabs(s).contains(t) else { return .overview }
            return t
        }
        set {
            if let s = nav.selection { nav.projectTabs[s] = newValue.rawValue }
        }
    }

    /// ⌃⌘S: the sidebar folds away, or back.
    func toggleSidebar() {
        sidebarShown.toggle()
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
                    _ = try await connected().call(AppAPI.Methods.coordinatorClear, AppAPI.CoordinatorClearParams(project: p))
                case .saveClearDirector(let g):
                    _ = try await connected().call(AppAPI.Methods.coordinatorClear, AppAPI.CoordinatorClearParams(program: g))
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
        case .session(let ref) where !ref.isRemote && !ref.director: ref.project
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

    /// Search Project… (⇧⌘F, T924): whether its sheet shows, and the
    /// search it shows, kept while it is closed so ⇧⌘F comes back to its
    /// hits; a new bridge starts a new one.
    var searchShown = false
    private(set) var search: SearchModel?
    @ObservationIgnored private var searchBridge: ObjectIdentifier?

    /// The memory file the project's Memory page shows next (a search
    /// hit's Open Note); n tells one ask from the next.
    struct NoteShown: Equatable {
        let n: Int
        let project: String
        let path: String
    }
    private(set) var noteShown: NoteShown?

    /// The note to show on project's Memory page, once: nil when none
    /// waits for it.
    func takeNote(_ project: String) -> String? {
        guard let n = noteShown, n.project == project else { return nil }
        noteShown = nil
        return n.path
    }

    /// Whether this tm searches a project's history (search.query, app
    /// API 41, the server's caps say search).
    var searches: Bool {
        if case .connected(let h) = phase { return Compatibility.searches(h) }
        return false
    }

    /// Search Project… (⇧⌘F): the sheet, on the shown project of this
    /// server, else the one searched last, else the first.
    func showSearch() {
        guard searches, let b = bridge else { return }
        let shown = boardProject
        if search == nil || searchBridge != ObjectIdentifier(b) {
            guard let p = shown ?? search?.project ?? activeProjects.first?.slug ?? projects.first?.slug else { return }
            search = SearchModel(project: p, calls: Search.Calls(b))
            searchBridge = ObjectIdentifier(b)
        } else if let shown {
            search?.switchProject(shown)
        }
        searchShown = true
        showWindow?()
    }

    /// Whether project lists thread id (an open thread: a resolved one
    /// is no longer in the sidebar).
    func listsThread(_ id: String, in project: String) -> Bool {
        self.project(project)?.threads.contains { $0.record.id == id } == true
    }

    /// Shows what a search hit opens: its task in the task view, its
    /// thread, or its note on the project's Memory page. The search sheet
    /// closes first; the task view opens once it has.
    func openSearchTarget(_ t: Search.Target, project: String) {
        searchShown = false
        switch t {
        case .task(let id):
            Task {
                try? await Task.sleep(for: .milliseconds(350))
                taskShown = TaskShown(project: project, task: id)
            }
        case .thread(let id):
            select(.session(SessionRef(project: project, thread: id)))
        case .note(let path):
            noteShown = NoteShown(n: (noteShown?.n ?? 0) + 1, project: project, path: path)
            select(.project(project))
        }
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

    /// Whether this tm answers Done's groups and ranges (app API 57).
    var groupsDone: Bool {
        if case .connected(let h) = phase { return Compatibility.groupsDone(h) }
        return false
    }

    /// The done tasks of project's board as task.done answers: newest
    /// first, or board order from an older tm. With groupsDone, the
    /// archive's too, read a page at a time, as the board's filter finds
    /// them.
    func doneTasks(_ project: String) async throws -> [AppAPI.Task] {
        let bridge = try connected()
        guard groupsDone else {
            return try await bridge.call(AppAPI.Methods.taskDoneAll, AppAPI.ProjectParams(project: project)).tasks
        }
        var out: [AppAPI.Task] = []
        var offset = 0
        repeat {
            let page = try await bridge.call(AppAPI.Methods.taskDone, AppAPI.TaskDoneParams(project: project, archived: true, offset: offset, limit: 500, tz: TimeZone.current.identifier))
            out += page.tasks
            offset = page.next
        } while offset > 0
        return out
    }

    /// Done's headings for project, in the viewer's time zone.
    func doneGroups(_ project: String) async throws -> AppAPI.DoneTasks {
        try await connected().call(AppAPI.Methods.taskDone, AppAPI.TaskDoneParams(project: project, groups: true, tz: TimeZone.current.identifier))
    }

    /// limit of group's rows from offset, the archive read only when the
    /// group holds archived tasks.
    func doneRows(_ project: String, _ group: AppAPI.DoneGroup, offset: Int = 0, limit: Int = DoneLedger.page) async throws -> AppAPI.DoneTasks {
        try await connected().call(AppAPI.Methods.taskDone, AppAPI.TaskDoneParams(project: project, from: group.from, to: group.to, archived: group.archived > 0, offset: offset, limit: limit, tz: TimeZone.current.identifier))
    }

    /// Whether this tm answers a project's daily numbers (app API 58).
    var readsHistory: Bool {
        if case .connected(let h) = phase { return Compatibility.readsHistory(h) }
        return false
    }

    /// Whether this tm asks a paired server for its numbers (app API 58).
    var readsPeerHistory: Bool {
        if case .connected(let h) = phase { return Compatibility.readsPeerHistory(h) }
        return false
    }

    /// A project's daily numbers (project.history); none from an older
    /// tm, so what draws them hides.
    func history(_ project: String) async throws -> AppAPI.History? {
        guard readsHistory else { return nil }
        return try await connected().call(AppAPI.Methods.projectHistory, AppAPI.ProjectParams(project: project))
    }

    /// A paired server's project's daily numbers (peer.history); none
    /// from an older tm on either side, or when it shares no pages.
    func peerHistory(_ alias: String, _ project: String) async throws -> AppAPI.History? {
        guard readsPeerHistory else { return nil }
        do {
            return try await connected().call(AppAPI.Methods.peerHistory, AppAPI.PeerPagesParams(alias: alias, project: project))
        } catch {
            let state: PeerPages.State<Int> = PeerPages.state(error)
            if state == .notShared { return nil }
            throw error
        }
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
        draft = ComposerDraft(n: (draft?.n ?? 0) + 1, ref: SessionRef(project: project), text: Dashboard.askAbout(task))
        select(.session(SessionRef(project: project)))
        focusComposer()
    }

    /// Takes the draft for ref's composer: its text, once.
    func takeDraft(for ref: SessionRef) -> String? {
        guard let d = draft, d.ref == ref else { return nil }
        draft = nil
        return d.text
    }

    /// Files' Mention in the Message: text in ref's composer, shown and
    /// focused. A coordinator is started when none runs, as Ask the
    /// Coordinator does; a thread has a session or offers no Mention.
    func mention(_ text: String, in ref: SessionRef, own: SessionModel?) async throws {
        if ref.isCoordinator, coordinatorSession(ref.project) == nil {
            try await startCoordinator(ref.project)
        }
        draft = ComposerDraft(n: (draft?.n ?? 0) + 1, ref: ref, text: text)
        if let own {
            own.pick(.conversation)
            composerFocus += 1
        } else {
            select(.session(ref))
            filesModels[.session(ref)]?.pane.shown = nil
            focusComposer()
        }
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
