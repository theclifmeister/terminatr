import Foundation

/// An action on a project's coordinator or the project itself, as the
/// TUI's prefix+C and S (app API 13): save and clear the coordinator
/// (coordinator.clear), activate the project or deactivate it
/// (project.active). The window asks first, in the TUI's words: always
/// for a save and clear, and for a deactivation while anything of the
/// project runs, which it stops.
public enum ProjectAsk: Identifiable, Hashable, Sendable {
    case saveClear(project: String)
    case activate(project: String)
    /// running names what the deactivation stops: "its coordinator", a
    /// thread's task or id.
    case deactivate(project: String, running: [String])

    public var id: String {
        switch self {
        case .saveClear(let p): "saveclear:\(p)"
        case .activate(let p): "activate:\(p)"
        case .deactivate(let p, _): "deactivate:\(p)"
        }
    }

    public var project: String {
        switch self {
        case .saveClear(let p), .activate(let p), .deactivate(let p, _): p
        }
    }

    /// Activate or deactivate project p, active now, with sessions:
    /// what the window does for it.
    public static func toggle(_ p: AppAPI.Project, sessions: [AppAPI.Session]) -> ProjectAsk {
        p.active ? .deactivate(project: p.slug, running: running(in: sessions, project: p.slug)) : .activate(project: p.slug)
    }

    /// Whether the window asks before it acts.
    public var asks: Bool {
        switch self {
        case .saveClear: true
        case .activate: false
        case .deactivate(_, let running): !running.isEmpty
        }
    }

    /// The menu's and the confirmation's button.
    public var verb: String {
        switch self {
        case .saveClear: "Save & Clear"
        case .activate: "Activate"
        case .deactivate: "Deactivate"
        }
    }

    /// The confirmation's title.
    public var title: String {
        switch self {
        case .saveClear(let p): "Save and clear \(p)'s coordinator?"
        case .activate(let p): "Activate \(p)?"
        case .deactivate(let p, _): "Deactivate \(p)?"
        }
    }

    /// The confirmation's message: what the action does.
    public var message: String {
        switch self {
        case .saveClear:
            "The coordinator is asked to save what it learned, then its conversation is cleared at its next idle moment, as tm clear does. The project's files keep its state."
        case .activate:
            "Its coordinator starts, and its threads resume."
        case .deactivate(_, let running) where running.isEmpty:
            "Nothing of it runs now; it stays inactive until you activate it."
        case .deactivate(_, let running):
            "This stops \(Self.joined(running)) now; they resume when you activate it."
        }
    }

    /// What the running sessions of project are, as the TUI names them:
    /// "its coordinator", a thread's task, else its id. A paired
    /// server's never count.
    public static func running(in sessions: [AppAPI.Session], project: String) -> [String] {
        sessions.compactMap { s in
            guard s.project == project, s.server.isEmpty else { return nil }
            switch s.role {
            case "coordinator": return "its coordinator"
            case "thread": return s.task.isEmpty ? s.thread : s.task
            default: return nil
            }
        }
    }

    /// "a", "a and b", "a, b and c".
    static func joined(_ s: [String]) -> String {
        guard s.count > 1 else { return s.first ?? "" }
        return s.dropLast().joined(separator: ", ") + " and " + s[s.count - 1]
    }

    /// What came of a deactivation or activation worth saying: the
    /// sessions that couldn't be resumed. Nil when there is nothing.
    public static func words(_ r: AppAPI.ProjectActive, project: String) -> String? {
        guard !r.lost.isEmpty else { return nil }
        return "\(project) is active, but \(joined(r.lost)) couldn't be resumed: their records were dropped."
    }
}

/// A coordinator's context use as its sidebar row shows it, as the
/// TUI's: the percent of its window its latest turn read, warn from
/// [ui] context_hint, high from 80%.
public struct ContextUse: Equatable, Sendable {
    public enum Level: Equatable, Sendable { case plain, hint, high }

    public let percent: Int
    public let level: Level

    /// The percent from which the use is high, whatever the hint.
    public static let high = 80

    /// s's use, against hint (0: never); nil when s has no window or
    /// use yet.
    public static func of(_ s: AppAPI.Session?, hint: Int) -> ContextUse? {
        guard let s, s.context > 0, s.contextWindow > 0 else { return nil }
        let pct = s.context * 100 / s.contextWindow
        let level: Level = pct >= high ? .high : hint > 0 && pct >= hint ? .hint : .plain
        return ContextUse(percent: pct, level: level)
    }

    /// The colour the row's words take, as the TUI's warn and bad: the
    /// approvals' amber at the hint, the attention red when high; nil
    /// for muted.
    public var tone: Attention.Tone? {
        switch level {
        case .plain: nil
        case .hint: .approval
        case .high: .attention
        }
    }

    /// The row's words: "62%".
    public var word: String { "\(percent)%" }

    /// What VoiceOver and the tooltip say.
    public var spoken: String {
        switch level {
        case .plain: "context \(percent)% used"
        case .hint: "context \(percent)% used, past the hint: Save & Clear frees it"
        case .high: "context \(percent)% used, nearly full: Save & Clear frees it"
        }
    }
}
