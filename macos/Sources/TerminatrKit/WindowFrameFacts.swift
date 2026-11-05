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
        var working = 0
        for p in projects {
            for t in p.threads where !t.record.session.isEmpty {
                if Busy.of(thread: t, session: sessions.first { $0.id == t.record.session }) != nil { working += 1 }
            }
        }
        for peer in peers where peer.state == "up" {
            for p in peer.projects where !p.stale {
                for s in p.sessions {
                    guard let ref = SessionRef(s, of: peer.alias), !ref.isCoordinator, !ref.director else { continue }
                    if Busy.of(peer: s) != nil { working += 1 }
                }
            }
        }
        return StatusFacts(servers: 1 + peers.count, working: working, needs: needs)
    }

    public var server: String { servers > 1 ? "This Mac · \(servers) servers" : "This Mac" }
    public var workingWord: String { "\(working) working" }
    public var needsWord: String { Self.needsWord(needs) }

    /// "1 needs you", "2 need you".
    public static func needsWord(_ n: Int) -> String { n == 1 ? "1 needs you" : "\(n) need you" }
}

/// What the inspector's header says beside its name (T1000): its chip and
/// its count at the far end.
public struct InspectorHead: Equatable, Sendable {
    public let chip: String
    public let count: String

    public init(chip: String, count: String) {
        self.chip = chip
        self.count = count
    }

    /// A thread's: its task, or its id without one, and its steps done.
    public static func thread(ref: String?, id: String, steps: [AppAPI.Step]) -> InspectorHead {
        InspectorHead(chip: ref ?? id, count: steps.isEmpty ? "" : "\(steps.filter(\.done).count) of \(steps.count)")
    }

    /// A project's or a coordinator's: what needs the user, and how much
    /// of its context the coordinator has used, while it runs.
    public static func project(needs: Int, context: Int?) -> InspectorHead {
        InspectorHead(chip: StatusFacts.needsWord(needs), count: context.map { "\($0)% context" } ?? "")
    }
}
