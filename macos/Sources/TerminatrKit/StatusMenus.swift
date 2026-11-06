import Foundation

// What the status bar's server and working menus list (T1032). Words as
// the TUI's Network popup has them (internal/tui sidepeers.go linkWords,
// networktree.go connLook).

/// One server in the status bar's menu: this Mac (alias "") or a paired one.
public struct ServerRow: Identifiable, Equatable, Sendable {
    public let alias: String
    /// "mini · connected 38 ms · tm 0.82.1".
    public let text: String
    public var id: String { alias }

    public init(alias: String, text: String) {
        self.alias = alias
        self.text = text
    }
}

public enum ServerList {
    /// This Mac, then the paired servers in the sidebar's order. rtt is
    /// the latest ping by alias, which a peer.list with the same gen
    /// carries though peers keeps the older listing.
    public static func of(connected: Bool, version: String, peers: [AppAPI.Peer], rtt: [String: Double], now: Date) -> [ServerRow] {
        var out = [row("", name: "This Mac", link: connected ? "connected" : "not connected", version: connected ? version : "")]
        for p in peers {
            out.append(row(p.alias, name: p.name.isEmpty ? p.alias : p.name,
                           link: link(p, rtt: rtt[p.alias] ?? p.rttMs, now: now), version: p.version))
        }
        return out
    }

    private static func row(_ alias: String, name: String, link: String, version: String) -> ServerRow {
        let tm = version.hasPrefix("v") ? String(version.dropFirst()) : version
        return ServerRow(alias: alias, text: ([name, link] + (tm.isEmpty ? [] : ["tm \(tm)"])).joined(separator: " · "))
    }

    /// "connected 38 ms", "connecting", "offline 4m · relay".
    static func link(_ p: AppAPI.Peer, rtt: Double, now: Date) -> String {
        switch p.state {
        case "up":
            return rtt > 0 ? "connected \(ping(rtt))" : "connected"
        case "connecting":
            return "connecting"
        default:
            let w = p.since.map { "offline \(Busy.short(now.timeIntervalSince($0)))" } ?? "offline"
            return reasons[p.reason].map { "\(w) · \($0)" } ?? w
        }
    }

    /// A round trip in milliseconds: "38 ms", "<1 ms".
    public static func ping(_ ms: Double) -> String {
        ms < 1 ? "<1 ms" : "\(Int(ms.rounded())) ms"
    }

    private static let reasons = ["relay": "relay", "peer": "peer", "refused": "key refused", "version": "old tm"]

    /// Where choosing a server lands: its first project, unless the
    /// window already shows something of it, or it lists none.
    public static func landing(_ alias: String, shown: Selection?, entries: [SidebarEntry], peers: [AppAPI.Peer]) -> Selection? {
        if let shown, shown.server == alias { return nil }
        if alias.isEmpty {
            switch entries.first {
            case .project(let p): return .project(p.slug)
            case .program(let g): return .program(g.slug)
            case nil: return nil
            }
        }
        return peers.first { $0.alias == alias }?.projects.first.map { .project($0.project, server: alias) }
    }
}

/// One working session in the status bar's menu.
public struct WorkingRow: Identifiable, Equatable, Sendable {
    public let selection: Selection
    /// "T1032 the title", or a coordinator's or director's name.
    public let title: String
    public let busy: Busy
    public var id: Selection { selection }

    /// The most of a title a menu row shows: a menu is as wide as its
    /// longest row.
    static let titleMax = 72

    /// "T1032 the title · 3m", or without a time when tm has none.
    public func text(now: Date) -> String {
        var short = title
        if title.count > Self.titleMax {
            short = title.prefix(Self.titleMax - 1).trimmingCharacters(in: .whitespaces) + "…"
        }
        return [short, busy.elapsed(now: now)].compactMap { $0 }.joined(separator: " · ")
    }
}

/// What the status bar's working menu lists: the threads it counts, by
/// project in the sidebar's order, and the coordinators and directors at
/// work, which it doesn't count.
public struct WorkingList: Equatable, Sendable {
    public struct Group: Identifiable, Equatable, Sendable {
        /// The project, "mini › acme" on a paired server.
        public let name: String
        public let rows: [WorkingRow]
        public var id: String { name }
    }

    public let threads: [Group]
    public let roles: [WorkingRow]

    public var isEmpty: Bool { threads.isEmpty && roles.isEmpty }

    /// Thread rows as their sidebar rows count them (Busy), and a paired
    /// server's while it is up and its listing fresh.
    public static func of(projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer],
                          programs: [AppAPI.Program] = []) -> WorkingList {
        var groups: [Group] = [], roles: [WorkingRow] = []
        for p in projects {
            let rows = p.threads.compactMap { t -> WorkingRow? in
                guard !t.record.session.isEmpty,
                      let busy = Busy.of(thread: t, session: sessions.first(where: { $0.id == t.record.session })) else { return nil }
                let title = [t.task?.ref ?? "", t.record.title.isEmpty ? t.record.id : t.record.title]
                    .filter { !$0.isEmpty }.joined(separator: " ")
                return WorkingRow(selection: .session(SessionRef(project: p.slug, thread: t.record.id)), title: title, busy: busy)
            }
            if !rows.isEmpty { groups.append(Group(name: p.slug, rows: rows)) }
            let coordinator = sessions.first { $0.role == "coordinator" && $0.project == p.slug && $0.server.isEmpty }
            if p.active, let busy = Busy.of(coordinator: coordinator, in: p) {
                roles.append(WorkingRow(selection: .session(SessionRef(project: p.slug)), title: "\(p.slug) · \(SessionRole.coordinator.word)", busy: busy))
            }
        }
        for g in programs {
            let director = sessions.first { $0.role == "director" && $0.project == g.slug && $0.server.isEmpty }
            if let busy = Busy.of(director: director, in: g) {
                roles.append(WorkingRow(selection: .session(.director(g.slug)), title: "\(g.slug) · \(SessionRole.director.word)", busy: busy))
            }
        }
        for peer in peers where peer.state == "up" {
            let server = peer.name.isEmpty ? peer.alias : peer.name
            for p in peer.projects where !p.stale {
                var rows: [WorkingRow] = []
                for s in p.sessions {
                    guard let ref = SessionRef(s, of: peer.alias), let busy = Busy.of(peer: s) else { continue }
                    if ref.isCoordinator {
                        roles.append(WorkingRow(selection: .session(ref), title: "\(server) › \(p.project) · \(SessionRole.coordinator.word)", busy: busy))
                    } else if !ref.director {
                        let t = p.watched.first { $0.id == s.thread }
                        let title = [t?.task.isEmpty == false ? t!.task : s.task, t?.title.isEmpty == false ? t!.title : s.thread]
                            .filter { !$0.isEmpty }.joined(separator: " ")
                        rows.append(WorkingRow(selection: .session(ref), title: title, busy: busy))
                    }
                }
                if !rows.isEmpty { groups.append(Group(name: "\(server) › \(p.project)", rows: rows)) }
            }
        }
        return WorkingList(threads: groups, roles: roles)
    }
}
