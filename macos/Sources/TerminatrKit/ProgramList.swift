import Foundation

/// The programs as the iPhone's Projects screen lists them (programs
/// design §11.1, §12; B19): a program is one row, sorted with the
/// projects that aren't pinned by slug, as the TUI's buildTree places
/// it, its local members under it alone; its needs are its director's
/// and its members', a paired member's as that server's mirror has
/// them, each counted once, so the row, the badge and Needs you agree.
/// Only while the server's hello lists the capability programs: without
/// it there are no programs to list.
public enum ProgramList {
    /// The server capability that says programs are on.
    public static let cap = "programs"

    /// A member as a need's project names it: "slug" for this server's,
    /// "alias/slug" for a paired server's.
    public static func key(_ m: AppAPI.ProgramMember) -> String {
        m.server.isEmpty ? m.project : m.server + "/" + m.project
    }

    /// This server's projects programs hold, each to the first program
    /// that lists it (a project is in one program at most).
    public static func members(_ programs: [AppAPI.Program]) -> [String: String] {
        var out: [String: String] = [:]
        for g in programs {
            for m in g.members where m.server.isEmpty && out[m.project] == nil {
                out[m.project] = g.slug
            }
        }
        return out
    }

    /// projects less the programs' local members, which are listed under
    /// their program only.
    public static func standalone(_ projects: [AppAPI.Project], programs: [AppAPI.Program]) -> [AppAPI.Project] {
        let held = members(programs)
        return held.isEmpty ? projects : projects.filter { held[$0.slug] == nil }
    }

    /// The needs of program g among needs: its own (its director's,
    /// under the program's slug) and its members', in needs' order, each
    /// once.
    public static func needs(of g: AppAPI.Program, in needs: [Need]) -> [Need] {
        let keys = Set([g.slug] + g.members.map(key))
        var seen = Set<String>()
        return needs.filter { keys.contains($0.project) && seen.insert($0.id).inserted }
    }

    /// needs by their task's priority (must, should, could, then none),
    /// keeping their order within one, as the TUI's program dashboard
    /// ranks its Needs you; priority is a need's task's, "" unknown.
    public static func ranked(_ needs: [Need], priority: (Need) -> String) -> [Need] {
        needs.enumerated().map { (Priority.rank(priority($0.element)), $0.offset, $0.element) }
            .sorted { ($0.0, $0.1) < ($1.0, $1.1) }.map(\.2)
    }

    /// A row of the Projects screen: a program or a project.
    public enum Entry: Hashable, Sendable {
        case program(AppAPI.Program)
        case project(AppAPI.Project)

        public var slug: String {
            switch self {
            case .program(let g): g.slug
            case .project(let p): p.slug
            }
        }
    }

    /// The Projects screen's rows: active, this server's standalone
    /// projects in their order (pinned first, SidebarTree.active), with
    /// each program placed before the first project that isn't pinned
    /// and sorts after it by slug, the rest at the end (buildTree). With
    /// needsFirst, those that need the user (needs) come first among
    /// those that aren't pinned, each part in its order.
    public static func entries(_ active: [AppAPI.Project], programs: [AppAPI.Program], needsFirst: Bool = false,
                               needs: (Entry) -> Bool = { _ in false }) -> [Entry] {
        var out: [Entry] = []
        var progs = programs[...]
        for p in active {
            while let g = progs.first, !p.pinned, g.slug < p.slug {
                out.append(.program(g))
                progs = progs.dropFirst()
            }
            out.append(.project(p))
        }
        out += progs.map(Entry.program)
        guard needsFirst else { return out }
        let pinned = out.prefix { if case .project(let p) = $0 { p.pinned } else { false } }
        let rest = out.dropFirst(pinned.count)
        return Array(pinned) + rest.filter(needs) + rest.filter { !needs($0) }
    }

    /// The paired server's project member m names, as that server's
    /// mirror (peer.list) lists it: never copied, so the program's row
    /// and the server's agree; nil while it isn't listed (invited, not
    /// shared, or that server isn't shown).
    public static func peerProject(_ m: AppAPI.ProgramMember, in peers: [AppAPI.Peer]) -> AppAPI.PeerProject? {
        guard !m.server.isEmpty else { return nil }
        return peers.first { $0.alias == m.server }?.projects.first { $0.project == m.project }
    }
}
