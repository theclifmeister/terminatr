import Foundation

// A working session shows once in the tree (T985), on the nearest row
// you can see: its own while it is listed, else the row folded over it,
// as the TUI's treeRow.busy and foldBusy (internal/tui sidetree.go). The
// shared fixture (internal/tui/testdata/sidebar-app.json) pins busyRows
// to the TUI's.

extension Busy {
    /// A paired server's session's, while it works and doesn't ask. No
    /// time: the mirror doesn't carry its turn's start.
    public static func of(peer s: AppAPI.PeerSession?) -> Busy? {
        guard let s, s.state == "working", !s.asking else { return nil }
        return Busy(since: nil)
    }

    /// Program g's director's, with s its session, as a coordinator's.
    public static func of(director s: AppAPI.Session?, in g: AppAPI.Program) -> Busy? {
        Attention.of(director: s, in: g).waits ? nil : of(s)
    }
}

extension SidebarTree {
    /// Whether a session of this server's project p works, as its rows'
    /// own marks say: its coordinator, or a thread. Nothing of an
    /// inactive project runs.
    public static func busy(_ p: AppAPI.Project, sessions: [AppAPI.Session]) -> Bool {
        guard p.active else { return false }
        let coordinator = sessions.first { $0.role == "coordinator" && $0.project == p.slug && $0.server.isEmpty }
        return Busy.of(coordinator: coordinator, in: p) != nil
            || p.threads.contains { Busy.of(thread: $0, session: session($0, in: sessions)) != nil }
    }

    /// Whether a session of paired server peer's project p works, among
    /// those it lists (sessions(of:)). A stale project's or an offline
    /// server's last values never count.
    public static func busy(_ peer: AppAPI.Peer, _ p: AppAPI.PeerProject, watched: Bool = true) -> Bool {
        guard peer.state == "up" else { return false }
        return sessions(of: p, alias: peer.alias, watched: watched).contains { Busy.of(peer: $0.session) != nil }
    }

    /// Whether a session of any of paired server peer's projects works.
    public static func busy(_ peer: AppAPI.Peer, watched: Bool = true) -> Bool {
        peer.projects.contains { busy(peer, $0, watched: watched) }
    }

    /// Whether program g's director or a session of a member works.
    public static func busy(_ g: AppAPI.Program, projects: [AppAPI.Project], sessions: [AppAPI.Session],
                            peers: [AppAPI.Peer], watched: Bool = true) -> Bool {
        let director = sessions.first { $0.role == "director" && $0.project == g.slug && $0.server.isEmpty }
        if Busy.of(director: director, in: g) != nil { return true }
        return memberRows(g, projects: projects, peers: peers).contains { m in
            switch m {
            case .local(let p): busy(p, sessions: sessions)
            case .paired(_, _, let peer, let p, _):
                if let peer, let p { busy(peer, p, watched: watched) } else { false }
            }
        }
    }

    /// The keys, as rows(…) has them, of the rows folded over a session
    /// that works: the rows that show it in its place.
    public static func busyRows(_ projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer] = [],
                                programs: [AppAPI.Program] = [], nav: Navigation, watched: Bool = true) -> Set<String> {
        let held = members(programs, projects: projects)
        var out: Set<String> = []
        func add(_ p: AppAPI.Project, member: Bool) {
            if !opens(p, nav: nav, member: member), busy(p, sessions: sessions) { out.insert("p:\(p.slug)") }
        }
        for p in projects where held[p.slug] == nil { add(p, member: false) }
        for g in programs {
            guard opens(g, nav: nav) else {
                if busy(g, projects: projects, sessions: sessions, peers: peers, watched: watched) { out.insert("g:\(g.slug)") }
                continue
            }
            for m in memberRows(g, projects: projects, peers: peers) {
                switch m {
                case .local(let p):
                    if held[p.slug] == g.slug { add(p, member: true) }
                case .paired(let alias, let slug, let peer, let p, _):
                    if let peer, let p, !opens(member: alias, slug, of: g, nav: nav), busy(peer, p, watched: watched) {
                        out.insert("g:\(g.slug)/r:\(alias)/\(slug)")
                    }
                }
            }
        }
        for peer in peers {
            guard !nav.collapsed.contains(Navigation.sectionKey(server: peer.alias)) else {
                if busy(peer, watched: watched) { out.insert("g:\(peer.alias)") }
                continue
            }
            for p in peer.projects where nav.collapsed.contains(Navigation.sectionKey(server: peer.alias, project: p.project)) {
                if busy(peer, p, watched: watched) { out.insert("r:\(peer.alias)/\(p.project)") }
            }
        }
        return out
    }
}
