import Foundation

// Programs in the sidebar (the programs design §11.1, §12), as the
// TUI's internal/tui sideprograms.go lists them: a program's row, its
// director's under it, then its members in its table's order, each with
// its own rows as a standalone project has them. A program sorts with
// the projects that aren't pinned, by slug, and folds as a project does.
// A member on a paired server shows twice: under its program, and under
// its server's group as ever, tagged with its program; both rows come
// from the one peer.list entry, so they never drift. Need counts come
// from the needs, never from the rows, so a need counts once. With
// programs off the server sends none, and the tree is the one without.

/// A top-level entry of this server's part of the tree: a project, or a
/// program with its members under it.
public enum SidebarEntry: Hashable, Sendable {
    case project(AppAPI.Project)
    case program(AppAPI.Program)

    /// Its slug: a project's or a program's (they share one namespace).
    public var slug: String {
        switch self {
        case .project(let p): p.slug
        case .program(let g): g.slug
        }
    }
}

/// A program's member as the sidebar lists it under the program.
public enum ProgramMemberRow: Hashable, Sendable {
    /// This server's project.
    case local(AppAPI.Project)
    /// A paired server's: its peer.list entry, nil while the mirror has
    /// none (its invite waits, or it isn't shared with this server).
    case paired(alias: String, slug: String, peer: AppAPI.Peer?, project: AppAPI.PeerProject?, state: String)

    /// Its name under the program: a paired one's "alias/slug" (the
    /// program mixes servers), with the server's name when it has one.
    public var name: String {
        switch self {
        case .local(let p): p.slug
        case .paired(let alias, let slug, let peer, _, _):
            "\((peer?.name.isEmpty ?? true) ? alias : peer!.name)/\(slug)"
        }
    }
}

extension SidebarTree {
    /// The projects of this server that a program holds, by slug, with
    /// its program's slug: the first program that lists one, and only
    /// projects listed here (a missing one has no row).
    public static func members(_ programs: [AppAPI.Program], projects: [AppAPI.Project]) -> [String: String] {
        guard !programs.isEmpty else { return [:] }
        let here = Set(projects.map(\.slug))
        var out: [String: String] = [:]
        for g in programs {
            for m in g.members where m.server.isEmpty && here.contains(m.project) && out[m.project] == nil {
                out[m.project] = g.slug
            }
        }
        return out
    }

    /// The program each paired member ("alias/slug") is in: the tag of
    /// its row under its server's group, the first program listing it.
    public static func pairedMembers(_ programs: [AppAPI.Program]) -> [String: String] {
        var out: [String: String] = [:]
        for g in programs {
            for m in g.members where !m.server.isEmpty && out["\(m.server)/\(m.project)"] == nil {
                out["\(m.server)/\(m.project)"] = g.slug
            }
        }
        return out
    }

    /// This server's active entries as the TUI's buildTree lists them:
    /// the pinned projects first, then the programs and the other
    /// projects, a program before the first project whose slug sorts
    /// after its own; a program's members only under it. With
    /// needs_first, in each part those that need the user first, a
    /// program with all its rows.
    public static func entries(_ projects: [AppAPI.Project], programs: [AppAPI.Program], needsFirst: Bool = false,
                               sessions: [AppAPI.Session] = [], peers: [AppAPI.Peer] = [], nav: Navigation = Navigation()) -> [SidebarEntry] {
        let held = members(programs, projects: projects)
        let active = projects.filter { $0.active && held[$0.slug] == nil }
        var progs = programs[...]
        var rest: [SidebarEntry] = []
        for p in active where !p.pinned {
            while let g = progs.first, g.slug < p.slug {
                rest.append(.program(g))
                progs.removeFirst()
            }
            rest.append(.project(p))
        }
        rest += progs.map(SidebarEntry.program)
        let pinned = active.filter(\.pinned).map(SidebarEntry.project)
        guard needsFirst else { return pinned + rest }
        let first = { (es: [SidebarEntry]) -> [SidebarEntry] in
            let need = es.map { e in
                switch e {
                case .project(let p): needs(p, sessions: sessions, nav: nav)
                case .program(let g): needs(g, projects: projects, sessions: sessions, peers: peers, nav: nav)
                }
            }
            return zip(es, need).filter { $1 }.map { $0.0 } + zip(es, need).filter { !$1 }.map { $0.0 }
        }
        return first(pinned) + first(rest)
    }

    /// Program g's members in its order, as its rows list them: this
    /// server's that are listed here, and every paired one.
    public static func memberRows(_ g: AppAPI.Program, projects: [AppAPI.Project], peers: [AppAPI.Peer]) -> [ProgramMemberRow] {
        g.members.compactMap { m in
            guard !m.server.isEmpty else {
                return projects.first { $0.slug == m.project }.map(ProgramMemberRow.local)
            }
            let peer = peers.first { $0.alias == m.server }
            return .paired(alias: m.server, slug: m.project, peer: peer, project: peer?.projects.first { $0.project == m.project },
                           state: m.state)
        }
    }

    /// Whether program g needs the user, as its row's hint in the TUI
    /// (programRows), which needs_first orders by: its open questions,
    /// or a member that needs the user.
    static func needs(_ g: AppAPI.Program, projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer],
                      nav: Navigation) -> Bool {
        if g.questions > 0 { return true }
        return memberRows(g, projects: projects, peers: peers).contains { m in
            switch m {
            case .local(let p): needs(p, sessions: sessions, nav: nav)
            case .paired(_, _, _, let p, _): p.map(needs) ?? false
            }
        }
    }

    /// Whether a paired server's project needs the user, as its row's
    /// hint in the TUI (peerProjectRows): its Needs you, or a thread
    /// that waits on the user.
    static func needs(_ p: AppAPI.PeerProject) -> Bool {
        p.needsYou > 0 || p.watched.contains(where: \.needsYou)
            || p.sessions.contains { $0.role == "thread" && ($0.state == "blocked" || $0.asking) }
    }

    /// Whether program g lists its director and members: unless ← folded
    /// it; and, the app's own, always while its director's session or a
    /// member's of this server is shown, so the List's selection has its
    /// row.
    public static func opens(_ g: AppAPI.Program, nav: Navigation) -> Bool {
        if case .session(let ref) = nav.selection, ref.server.isEmpty,
           ref.director ? ref.project == g.slug : g.members.contains(where: { $0.server.isEmpty && $0.project == ref.project }) {
            return true
        }
        return !nav.collapsed.contains(Navigation.programKey(g.slug))
    }

    /// Whether program g's paired member alias/slug lists its rows under
    /// the program: only once → unfolded it there, as a member starts
    /// folded (its own fold, apart from its server's group's), and never
    /// while it is stale or not shared.
    public static func opens(member alias: String, _ slug: String, of g: AppAPI.Program, nav: Navigation) -> Bool {
        nav.unfolded.contains(Navigation.memberKey(program: g.slug, server: alias, project: slug))
    }

    /// The key of a program member's fold (Navigation.unfolded): a local
    /// member's is its project's own (sectionKey, as view.Tree.Unfolded
    /// keys it), a paired one's (alias not empty) is apart from its
    /// server's group's (memberKey).
    public static func foldKey(member slug: String, alias: String = "", of g: AppAPI.Program) -> String {
        alias.isEmpty ? Navigation.sectionKey(server: "", project: slug) : Navigation.memberKey(program: g.slug, server: alias, project: slug)
    }

    /// The needs of program g, its badge: its own (its director's, its
    /// questions) and its members', each once.
    public static func needs(of g: AppAPI.Program, in needs: [Need]) -> [Need] {
        var keys: Set<String> = [g.slug]
        for m in g.members {
            keys.insert(m.server.isEmpty ? m.project : "\(m.server)/\(m.project)")
        }
        return needs.filter { keys.contains($0.project) }
    }

    /// Program g's director's session, nil when none runs.
    public static func director(_ g: AppAPI.Program, sessions: [AppAPI.Session]) -> AppAPI.Session? {
        sessions.first { $0.role == "director" && $0.project == g.slug && $0.server.isEmpty }
    }
}

/// What a program's screen says in words (the TUI's program dashboard,
/// internal/tui programdash.go).
public enum ProgramFacts {
    /// Its line under its name: "propose · slots 1/4 · paused".
    public static func line(_ g: AppAPI.Program, working: Int?) -> String {
        var bits = [g.authority.isEmpty ? "propose" : g.authority]
        if g.parallelThreads > 0 {
            bits.append("slots \(working ?? 0)/\(g.parallelThreads)")
        } else if let working, working > 0 {
            bits.append("\(working) working")
        }
        if g.paused { bits.append("paused") }
        if g.unread > 0 { bits.append(g.unread == 1 ? "1 in its inbox" : "\(g.unread) in its inbox") }
        return bits.joined(separator: " · ")
    }

    /// A member's place: its slug, a paired one's "alias/slug", as
    /// needs and routes name it.
    public static func place(_ project: String, server: String) -> String {
        server.isEmpty ? project : "\(server)/\(project)"
    }

    /// A program need's words: its task and what it is.
    public static func need(_ n: AppAPI.ProgramNeed) -> String {
        let what = n.need.title.isEmpty ? n.need.text : n.need.title
        return [n.need.task, what].filter { !$0.isEmpty }.joined(separator: " ")
    }

    /// A child's line, as the dashboard's CHILDREN has it: this
    /// server's, its threads and needs; a paired one's, its membership
    /// and its server's state.
    public static func child(_ m: AppAPI.ProgramMember, project p: AppAPI.Project?, peer: AppAPI.Peer?, needs: Int?) -> String {
        if !m.error.isEmpty { return m.error }
        let need = needs.map { $0 == 0 ? nil : $0 == 1 ? "1 needs you" : "\($0) need you" } ?? nil
        guard !m.server.isEmpty else {
            guard let p else { return "not on this server" }
            let open = p.threads.filter { !$0.record.done }.count
            return ([p.active ? nil : "inactive", open == 1 ? "1 thread" : "\(open) threads", need].compactMap { $0 }).joined(separator: " · ")
        }
        if m.dormant { return "dormant: programs are off on \(m.server)" }
        let words = m.words.isEmpty ? m.state : m.words
        guard let peer else { return [words, "not paired"].filter { !$0.isEmpty }.joined(separator: " · ") }
        let link = peer.state == "up" ? nil : "offline"
        let shared = peer.projects.contains { $0.project == m.project } ? nil : "not shared"
        return ([words.isEmpty ? nil : words, link, shared, need].compactMap { $0 }).joined(separator: " · ")
    }
}

extension ProgramFacts {
    /// What a click on a program need does on the program screen.
    public enum Act: Equatable, Sendable {
        /// One of the program's own questions ("Q2"): answered once here
        /// for the member questions it covers (program.answer).
        case answer(ref: String, question: AppAPI.QuestionItem?)
        /// An umbrella of the program's board in review ("T4"): accept,
        /// or send back with a note (program.umbrella.*).
        case umbrella(task: String)
        /// A member's need: its project, where its own cards answer it.
        case open(project: String, session: String)
    }

    public static func act(_ n: AppAPI.ProgramNeed) -> Act {
        if n.program, n.need.kind == "umbrella-review" { return .umbrella(task: n.need.task) }
        if n.program, n.need.id.hasPrefix("ask:") {
            return .answer(ref: String(n.need.id.dropFirst(4)), question: n.need.question?.questions.first)
        }
        return .open(project: n.program ? n.project : place(n.project, server: n.server), session: n.server.isEmpty ? n.need.session : "")
    }

    /// A program answer's outcome, in words: where it went and what
    /// came of each.
    public static func answered(_ a: AppAPI.ProgramAnswered) -> String {
        guard !a.fanned.isEmpty else { return "Answered." }
        return a.fanned.map { f in
            let what = f.result == "failed" && !f.error.isEmpty ? "failed: \(f.error)" : f.result
            return "\(f.project) \(f.question): \(what)"
        }.joined(separator: "\n")
    }
}
