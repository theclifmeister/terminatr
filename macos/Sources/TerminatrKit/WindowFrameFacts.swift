import Foundation

/// A project's tabs in the title bar (T1000), in the flat look's order.
/// Overview is its dashboard; Board its tasks, which the dashboard no
/// longer lists. iOS keeps ProjectPage's own list.
public enum ProjectTab: String, CaseIterable, Identifiable, Sendable {
    case overview = "Overview"
    case board = "Board"
    case memory = "Memory"
    case library = "Library"
    case mods = "Mods"

    public var id: String { rawValue }

    /// The tabs of a project of this server: those hello's tm has.
    public static func tabs(_ hello: AppAPI.Hello?) -> [ProjectTab] {
        guard let hello else { return [.overview, .board] }
        var out: [ProjectTab] = [.overview, .board]
        if Compatibility.readsProjectFiles(hello) { out += [.memory, .library] }
        if Compatibility.listsMods(hello) { out.append(.mods) }
        return out
    }

    /// A paired server's project's: its mods are its own, never here.
    public static let paired: [ProjectTab] = [.overview, .board, .memory, .library]
}

/// A project's tile: two letters of its name, a hyphenated name's first
/// two parts' initials, so terminatr and terminatr-mods differ.
public enum ProjectTile {
    public static func letters(_ slug: String) -> String {
        let parts = slug.split { $0 == "-" || $0 == "_" }
        if parts.count > 1, let a = parts[0].first, let b = parts[1].first {
            return String([a, b]).lowercased()
        }
        return String(slug.prefix(2)).lowercased()
    }
}

/// The window's status bar (T1000): the server, the threads at work and
/// what needs the user, in numbers the sidebar's rows already show.
public struct StatusFacts: Equatable, Sendable {
    /// This server and its paired ones.
    public let servers: Int
    /// Threads whose agents have their turn.
    public let working: Int
    public let needs: Int

    public init(servers: Int, working: Int, needs: Int) {
        self.servers = servers
        self.working = working
        self.needs = needs
    }

    /// Counts a thread as its row does (Busy), and a paired server's as
    /// its row does while that server is up and its listing fresh.
    public static func of(projects: [AppAPI.Project], sessions: [AppAPI.Session], peers: [AppAPI.Peer], needs: Int) -> StatusFacts {
        let working = WorkingList.of(projects: projects, sessions: sessions, peers: peers).threads
        return StatusFacts(servers: 1 + peers.count, working: working.reduce(0) { $0 + $1.rows.count }, needs: needs)
    }

    public var server: String { servers > 1 ? "This Mac · \(servers) servers" : "This Mac" }
    public var workingWord: String { "\(working) working" }
    public var needsWord: String { Self.needsWord(needs) }

    /// "1 needs you", "2 need you".
    public static func needsWord(_ n: Int) -> String { n == 1 ? "1 needs you" : "\(n) need you" }
}

/// What the inspector's header says (T1016): what the panel shows, not
/// the panel's name, with its state at the far end. A role's mark and
/// name, or a thread's task and thread ids.
public struct InspectorHead: Equatable, Sendable {
    public enum Name: Equatable, Sendable {
        case role(SessionRole, String)
        case ids(task: String, thread: String)
    }

    public let name: Name?
    public let state: Attention?
    /// Who it is in words, for VoiceOver and the tooltip, naming the
    /// project the mark and its word leave out.
    public let who: String

    public init(name: Name?, state: Attention?, who: String) {
        self.name = name
        self.state = state
        self.who = who
    }

    /// Nothing to name: a selection the server no longer lists.
    public static let empty = InspectorHead(name: nil, state: nil, who: "")

    /// What VoiceOver says of the header.
    public var spoken: String {
        [who, state?.spoken].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: ", ")
    }

    public static func coordinator(project: String, server: String = "", state: Attention?) -> InspectorHead {
        InspectorHead(name: .role(.coordinator, SessionRole.coordinator.word), state: state,
                      who: "Coordinator of \(place(project, server))")
    }

    public static func director(program: String, state: Attention?) -> InspectorHead {
        InspectorHead(name: .role(.director, SessionRole.director.word), state: state, who: "Director of \(program)")
    }

    /// A project's page: its coordinator's mark and state, its own name.
    public static func project(_ slug: String, server: String = "", state: Attention?) -> InspectorHead {
        InspectorHead(name: .role(.coordinator, slug), state: state,
                      who: "Project \(place(slug, server))" + (state == nil ? "" : ", coordinator"))
    }

    /// A program's page: its director's mark and state, its own name.
    public static func program(_ slug: String, state: Attention?) -> InspectorHead {
        InspectorHead(name: .role(.director, slug), state: state, who: "Program \(slug)" + (state == nil ? "" : ", director"))
    }

    public static func thread(task: String?, id: String, server: String = "", state: Attention?) -> InspectorHead {
        let ids = [task ?? "", id].filter { !$0.isEmpty }.joined(separator: " ")
        return InspectorHead(name: .ids(task: task ?? "", thread: id), state: state,
                             who: server.isEmpty ? ids : "\(ids) on \(server)")
    }

    private static func place(_ project: String, _ server: String) -> String {
        server.isEmpty ? project : "\(server) › \(project)"
    }
}

extension Attention {
    /// A session's own state, for a panel with nothing surer to say of it
    /// (a paired server's session, a coordinator without its project).
    public static func session(state: String, reason: String, asking: Bool = false) -> Attention {
        if asking { return Attention(word: "asks", tone: .approval, why: "asks a question", waits: true) }
        if state == "blocked" {
            let word = reason == "permission" ? "needs approval" : reason.isEmpty ? "blocked" : reason
            return Attention(word: word, tone: .approval, why: word, waits: true)
        }
        return Attention(word: state, tone: state == "working" ? .working : .idle, why: "", waits: false)
    }
}

/// The one-line summaries of the inspector's folds (T1016).
public enum InspectorSummary {
    /// "2 threads · 5 on deck".
    public static func project(threads: Int, onDeck: Int) -> String {
        "\(count(threads, "thread")) · \(onDeck) on deck"
    }

    /// "3 threads", a paired project's, with what stands out of it.
    public static func peerProject(threads: Int, paused: Bool, stale: Bool) -> String {
        ([count(threads, "thread")] + (paused ? ["paused"] : []) + (stale ? ["stale"] : [])).joined(separator: " · ")
    }

    /// "3 projects", with the questions open.
    public static func program(projects: Int, questions: Int) -> String {
        ([count(projects, "project")] + (questions > 0 ? [count(questions, "question")] : [])).joined(separator: " · ")
    }

    /// "3 of 4 done".
    public static func done(_ done: Int, of total: Int) -> String {
        "\(done) of \(total) done"
    }

    /// "claude · opus", with what waits in its queue.
    public static func agent(_ agent: String, model: String, queued: Int = 0) -> String {
        ([agent, model] + (queued > 0 ? [count(queued, "queued prompt")] : []))
            .filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// "1 prompt", "2 prompts": what waits in a session's queue.
    public static func prompts(_ n: Int) -> String {
        count(n, "prompt")
    }

    /// "mini · online", a paired server's, with a stale read.
    public static func paired(server: String, link: String, stale: Bool = false) -> String {
        ([server, link] + (stale ? ["stale"] : [])).filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private static func count(_ n: Int, _ word: String) -> String {
        "\(n) \(word)\(n == 1 ? "" : "s")"
    }
}

/// Which need's answers show (T1016): the first starts picked, a click
/// picks another, and when the picked one is answered or goes, the one
/// after it is picked.
public enum NeedPick {
    /// What's picked once the needs that can be picked went from was to
    /// now: picked while it stays, else the next of was still there,
    /// else the nearest before it, else the first.
    public static func next(picked: String?, was: [String], now: [String]) -> String? {
        if let picked, now.contains(picked) { return picked }
        if let picked, let i = was.firstIndex(of: picked) {
            if let after = was[(i + 1)...].first(where: now.contains) { return after }
            if let before = was[..<i].last(where: now.contains) { return before }
        }
        return now.first
    }
}
