import Foundation

/// The work running on this server (T849), for the phone's live strip
/// above its tab bar: how many of its sessions work now, and the one
/// that has worked longest, which a tap opens. Only state "working"
/// counts, as Busy has it; a paired server's sessions don't, as they
/// have their own server.
public struct LiveWork: Equatable, Sendable {
    /// How many sessions work.
    public let count: Int
    /// The one that has worked longest: the earliest turn; without a
    /// turn's start (a tm before app API 25), the first listed.
    public let lead: AppAPI.Session

    /// What sessions of this server work now; nil when none does.
    public static func of(_ sessions: [AppAPI.Session]) -> LiveWork? {
        let working = sessions.filter { $0.server.isEmpty && $0.state == "working" }
        let lead = working.enumerated().min { a, b in
            let (x, y) = (a.element.turnSince ?? .distantFuture, b.element.turnSince ?? .distantFuture)
            return (x, a.offset) < (y, b.offset)
        }
        return lead.map { LiveWork(count: working.count, lead: $0.element) }
    }

    /// The lead's session, its coordinator's or its thread's.
    public var ref: SessionRef {
        SessionRef(project: lead.project, thread: lead.role == "coordinator" ? "" : lead.thread)
    }

    /// "2 working".
    public var word: String { "\(count) working" }

    /// Who leads: its task ("T16"), a coordinator its project
    /// ("acme-web"), else its thread.
    public var who: String {
        if !lead.task.isEmpty { return lead.task }
        if lead.role == "coordinator" { return lead.project }
        return lead.thread
    }

    /// "T16 reading cart.spec.ts": who leads and what it does now, as
    /// the Working… bar says it; just who without a word of it.
    public var what: String {
        lead.current.isEmpty ? who : "\(who) \(Activity.lowered(lead.current))"
    }
}
