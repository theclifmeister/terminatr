import Foundation

/// What the window's sidebar lists: the rows of the TUI's sidebar
/// (internal/tui buildTree, docs/SPEC.md §4), in its order and folded as
/// it folds them, under the app's own Needs you, cut to a few.
public enum SidebarTree {
    /// The needs the sidebar's Needs you shows before "+N more" (A3).
    public static let needsShown = 3

    /// The active projects as the TUI lists them (SPEC §5.1): the
    /// pinned ones first, each group in the server's order; with [ui]
    /// needs_first (SessionList.needsFirst), in each group those that
    /// need the user (needs) first. ⌘1–⌘9, the Go menu, the menu bar
    /// and the first selection count only these.
    public static func active(_ projects: [AppAPI.Project], needsFirst: Bool = false, sessions: [AppAPI.Session] = []) -> [AppAPI.Project] {
        let active = projects.filter(\.active)
        // An active project's threads count, folded or not: nav is moot.
        let first = { (ps: [AppAPI.Project]) in needsFirst ? needing(ps, sessions: sessions, nav: Navigation()) : ps }
        return first(active.filter(\.pinned)) + first(active.filter { !$0.pinned })
    }

    /// The inactive projects, folded under "Inactive (n)" at the bottom,
    /// pinned or not; with needs_first, those that need the user first.
    public static func inactive(_ projects: [AppAPI.Project], needsFirst: Bool = false, sessions: [AppAPI.Session] = [],
                                nav: Navigation = Navigation()) -> [AppAPI.Project] {
        let gone = projects.filter { !$0.active }
        return needsFirst ? needing(gone, sessions: sessions, nav: nav) : gone
    }

    /// ps, those that need the user first, each part in its order.
    static func needing(_ ps: [AppAPI.Project], sessions: [AppAPI.Session], nav: Navigation) -> [AppAPI.Project] {
        let need = ps.map { needs($0, sessions: sessions, nav: nav) }
        return zip(ps, need).filter { $1 }.map { $0.0 } + zip(ps, need).filter { !$1 }.map { $0.0 }
    }

    /// Whether this server's project p needs the user, as the TUI's
    /// project row's hint (projectRows), which needs_first orders by:
    /// tasks in its board's Needs you, its coordinator's open questions,
    /// or a thread waiting on the user (group 0); an inactive project's
    /// threads only once → expanded it.
    static func needs(_ p: AppAPI.Project, sessions: [AppAPI.Session], nav: Navigation) -> Bool {
        if (p.counts["needs_you"] ?? 0) > 0 || p.questions > 0 { return true }
        guard p.active || nav.unfolded.contains(Navigation.sectionKey(server: "", project: p.slug)) else { return false }
        return p.threads.contains { group($0, session($0, in: sessions)) == 0 }
    }

    /// Thread t's session as session.list has it, nil when none runs.
    static func session(_ t: AppAPI.Thread, in sessions: [AppAPI.Session]) -> AppAPI.Session? {
        t.record.session.isEmpty ? nil : sessions.first { $0.id == t.record.session }
    }

    /// The threads a project lists under its coordinator, as the TUI's
    /// sidebar has them (internal/tui projectRows, orderedThreads): every
    /// unresolved one, done ones too, ordered as §7.4 orders them:
    /// waiting on the user, a report to read, working, running, then the
    /// rest, each group in the server's order. session is a thread's
    /// session as session.list has it (nil when none runs).
    public static func threads(of p: AppAPI.Project, session: (AppAPI.Thread) -> AppAPI.Session?) -> [AppAPI.Thread] {
        let ranked = p.threads.enumerated().map { (group($0.element, session($0.element)), $0.offset, $0.element) }
        return ranked.sorted { ($0.0, $0.1) < ($1.0, $1.1) }.map { $0.2 }
    }

    /// A thread's group in that order, as the TUI's threadGroup.
    static func group(_ t: AppAPI.Thread, _ s: AppAPI.Session?) -> Int {
        if s?.state == "blocked" || !(t.status?.needsYou ?? "").isEmpty { return 0 }
        if t.record.reportAcked < t.record.reports { return 1 }
        if s?.state == "working" { return 2 }
        return s == nil ? 4 : 3
    }

    /// Whether this server's project p lists its coordinator and threads,
    /// as the TUI's tree folds (view.Tree): an active project unless ←
    /// folded it, an inactive one only once → expanded it; and, the
    /// app's own, always while one of its sessions is shown, so the
    /// List's selection has its row.
    public static func opens(_ p: AppAPI.Project, nav: Navigation) -> Bool {
        if case .session(let ref) = nav.selection, ref.server.isEmpty, ref.project == p.slug { return true }
        let key = Navigation.sectionKey(server: "", project: p.slug)
        return p.active ? !nav.collapsed.contains(key) : nav.unfolded.contains(key)
    }

    /// The tree as the TUI keys its rows (treeRow.key): "p:" a project,
    /// "c:" its coordinator, "t:<slug>/<id>" a thread, and "d:inactive"
    /// the inactive divider, the inactive projects under it while
    /// nav.showInactive; with paired servers, "g:" this server's group
    /// first, then each paired one's, "g:<alias>", with its projects,
    /// "r:<alias>/<slug>", their coordinators, "rc:", and threads,
    /// "rt:<alias>/<slug>/<id>" (sessions(of:)), above the divider.
    /// watched is the bridge's PeerProject.watched (app API 9). The
    /// sidebar draws these rows; the shared fixture
    /// (internal/tui/testdata/sidebar-app.json) pins them to the TUI's
    /// buildTree.
    public static func rows(_ projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer] = [],
                            nav: Navigation, needsFirst: Bool = false, watched: Bool = true) -> [String] {
        func add(_ p: AppAPI.Project) -> [String] {
            guard opens(p, nav: nav) else { return ["p:\(p.slug)"] }
            return ["p:\(p.slug)", "c:\(p.slug)"] + threads(of: p) { session($0, in: sessions) }.map { "t:\(p.slug)/\($0.record.id)" }
        }
        var remote: [String] = []
        for peer in peers {
            remote.append("g:\(peer.alias)")
            guard !nav.collapsed.contains(Navigation.sectionKey(server: peer.alias)) else { continue }
            for p in peer.projects {
                remote.append("r:\(peer.alias)/\(p.project)")
                guard !nav.collapsed.contains(Navigation.sectionKey(server: peer.alias, project: p.project)) else { continue }
                remote += SidebarTree.sessions(of: p, alias: peer.alias, watched: watched).map { s in
                    s.ref.isCoordinator ? "rc:\(peer.alias)/\(p.project)" : "rt:\(peer.alias)/\(p.project)/\(s.ref.thread)"
                }
            }
        }
        let gone = inactive(projects, needsFirst: needsFirst, sessions: sessions, nav: nav)
        return (peers.isEmpty ? [] : ["g:"]) + active(projects, needsFirst: needsFirst, sessions: sessions).flatMap(add) + remote
            + (gone.isEmpty ? [] : ["d:inactive"]) + (nav.showInactive ? gone.flatMap(add) : [])
    }

    /// What a paired server's shared project lists under it, as the
    /// TUI's peerProjectRows: nothing while stale; else its coordinator,
    /// running there or not, then its threads, each with its session
    /// there (nil when none runs) and its task. With watched (a bridge
    /// of app API 9 or later) those are the watched ones, running or
    /// not, in their order; an older bridge lists only the ones a
    /// session runs.
    public static func sessions(of p: AppAPI.PeerProject, alias: String, watched: Bool = true)
        -> [(session: AppAPI.PeerSession?, ref: SessionRef, task: String)] {
        guard !p.stale else { return [] }
        let all = p.sessions.compactMap { s in SessionRef(s, of: alias).map { (session: Optional(s), ref: $0, task: s.task) } }
        let coordinator = all.first { $0.ref.isCoordinator } ?? (nil, SessionRef(project: p.project, server: alias), "")
        guard watched else { return [coordinator] + all.filter { !$0.ref.isCoordinator } }
        return [coordinator] + p.watched.map { t in
            let s = t.session.isEmpty ? nil : p.sessions.first { $0.id == t.session }
            return (s, SessionRef(project: p.project, thread: t.id, server: alias), t.task.isEmpty ? s?.task ?? "" : t.task)
        }
    }

    /// "+4 more: 2 review, 2 blocked": the needs past the shown ones,
    /// by kind in the order they come.
    public static func moreNeeds(_ rest: [Need]) -> String {
        var kinds: [Need.Kind] = [], n: [Need.Kind: Int] = [:]
        for r in rest {
            if n[r.kind] == nil { kinds.append(r.kind) }
            n[r.kind, default: 0] += 1
        }
        let parts = kinds.map { "\(n[$0]!) \(word($0))" }
        return "+\(rest.count) more" + (parts.isEmpty ? "" : ": " + parts.joined(separator: ", "))
    }

    static func word(_ k: Need.Kind) -> String {
        switch k {
        case .question: "asking"
        case .approval: "to allow"
        case .review: "review"
        case .blocked: "blocked"
        case .checks: "CI failing"
        case .held: "held"
        case .network: "paired"
        }
    }
}

extension Attention {
    /// What a project's coordinator says of itself, its project's dot
    /// and its row's: with s its session as session.list has it (nil
    /// when none runs). Its open questions (tm ask) wait even while none
    /// runs.
    public static func of(coordinator s: AppAPI.Session?, in p: AppAPI.Project, now: Date = Date()) -> Attention {
        if let s {
            let menu = s.question.map { q in q.questions.contains { !$0.answered } } ?? false
            if menu || (s.state == "blocked" && s.reason == "question") {
                return Attention(word: "asks", tone: .question, why: menu ? "asks a question" : "asks a question · answer in Terminal", waits: true)
            }
            if s.state == "blocked" && s.reason == "permission" {
                return Attention(word: "allow?", tone: .approval,
                                 why: s.escape ? "asks to run outside the sandbox" : "needs approval", waits: true)
            }
            if Need.held(s, now: now) {
                return Attention(word: "held", tone: .attention, why: "prompt held: \(s.queueHeld)", waits: true)
            }
        }
        if p.questions > 0 {
            return Attention(word: "asks", tone: .question,
                             why: p.questions == 1 ? "has a question" : "has \(p.questions) questions", waits: true)
        }
        guard let s else {
            return Attention(word: "not running", tone: .idle, why: "no coordinator runs", waits: false)
        }
        switch s.state {
        case "working":
            return Attention(word: "working", tone: .working, why: s.current, waits: false)
        case "":
            return Attention(word: "running", tone: .working, why: s.current, waits: false)
        case "blocked":
            return Attention(word: s.reason.isEmpty ? "blocked" : s.reason, tone: .attention, why: s.current, waits: false)
        default:
            return Attention(word: s.state, tone: .idle, why: s.current, waits: false)
        }
    }
}
