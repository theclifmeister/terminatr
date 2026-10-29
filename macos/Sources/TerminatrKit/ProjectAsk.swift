import Foundation

/// An action on a project's coordinator or the project itself, as the
/// TUI's prefix+C and S (app API 13): save and clear the coordinator
/// (coordinator.clear), activate the project or deactivate it
/// (project.active). The window asks first, in the TUI's words: always
/// for a save and clear, and for a deactivation while anything of the
/// project runs, which it stops. And, as the TUI's P, prefix+r and O
/// (app API 22): pin the project to the top of the sidebar or unpin it
/// (project.pin, never asked), turn its coordinator's remote control on
/// or off (session.remote), ask its coordinator to adopt an agent
/// session outside the projects as a thread or take that back
/// (session.adopt.request), each asked first.
public enum ProjectAsk: Identifiable, Hashable, Sendable {
    case saveClear(project: String)
    case activate(project: String)
    /// running names what the deactivation stops: "its coordinator", a
    /// thread's task or id.
    case deactivate(project: String, running: [String])
    case pin(project: String, on: Bool)
    /// session is the coordinator's; on what remote control becomes.
    case remote(project: String, session: String, on: Bool)
    /// session is the outside agent session, what it runs and where.
    case adopt(project: String, session: String, what: String)
    case withdrawAdopt(project: String, session: String)

    public var id: String {
        switch self {
        case .saveClear(let p): "saveclear:\(p)"
        case .activate(let p): "activate:\(p)"
        case .deactivate(let p, _): "deactivate:\(p)"
        case .pin(let p, let on): "pin:\(p):\(on)"
        case .remote(let p, _, let on): "remote:\(p):\(on)"
        case .adopt(let p, let s, _): "adopt:\(p):\(s)"
        case .withdrawAdopt(let p, let s): "withdraw:\(p):\(s)"
        }
    }

    public var project: String {
        switch self {
        case .saveClear(let p), .activate(let p), .deactivate(let p, _), .pin(let p, _), .remote(let p, _, _),
             .adopt(let p, _, _), .withdrawAdopt(let p, _): p
        }
    }

    /// Pin project p to the top of the sidebar, or unpin it.
    public static func pinToggle(_ p: AppAPI.Project) -> ProjectAsk { .pin(project: p.slug, on: !p.pinned) }

    /// Turn coordinator session c's remote control around.
    public static func remoteToggle(_ c: AppAPI.Session) -> ProjectAsk {
        .remote(project: c.project, session: c.id, on: !c.remoteControl)
    }

    /// Ask project's coordinator to adopt session s, or, when an inbox
    /// item already asks it (items, the project's), take that back: O
    /// again, as the TUI's.
    public static func adoptToggle(_ s: AppAPI.Session, project: String, items: [AppAPI.Item]) -> ProjectAsk {
        if items.contains(where: { $0.kind == "adopt" && $0.subject == s.id }) {
            return .withdrawAdopt(project: project, session: s.id)
        }
        return .adopt(project: project, session: s.id, what: adoptWhat(s))
    }

    /// The agent sessions outside the projects that a coordinator can
    /// adopt, as the TUI's O takes them: a plain session of this
    /// server's, not a project's, in which an agent runs.
    public static func adoptable(_ sessions: [AppAPI.Session]) -> [AppAPI.Session] {
        sessions.filter { $0.project.isEmpty && $0.role == "shell" && $0.server.isEmpty && !$0.agent.isEmpty && $0.state != "exited" }
    }

    /// What runs in s, for its row and the question: "claude · Fix the
    /// login", or the agent alone.
    public static func adoptWhat(_ s: AppAPI.Session) -> String {
        s.title.isEmpty ? s.agent : "\(s.agent) · \(s.title)"
    }

    /// Activate or deactivate project p, active now, with sessions:
    /// what the window does for it.
    public static func toggle(_ p: AppAPI.Project, sessions: [AppAPI.Session]) -> ProjectAsk {
        p.active ? .deactivate(project: p.slug, running: running(in: sessions, project: p.slug)) : .activate(project: p.slug)
    }

    /// Whether the window asks before it acts.
    public var asks: Bool {
        switch self {
        case .saveClear, .remote, .adopt, .withdrawAdopt: true
        case .activate, .pin: false
        case .deactivate(_, let running): !running.isEmpty
        }
    }

    /// Whether the action is one app API 22 added (side-acts).
    public var isSideAct: Bool {
        switch self {
        case .pin, .remote, .adopt, .withdrawAdopt: true
        default: false
        }
    }

    /// The menu's and the confirmation's button.
    public var verb: String {
        switch self {
        case .saveClear: "Save & Clear"
        case .activate: "Activate"
        case .deactivate: "Deactivate"
        case .pin(_, let on): on ? "Pin to Top" : "Unpin"
        case .remote(_, _, let on): on ? "Turn Remote Control On" : "Turn Remote Control Off"
        case .adopt: "Adopt"
        case .withdrawAdopt: "Withdraw"
        }
    }

    /// The confirmation's title.
    public var title: String {
        switch self {
        case .saveClear(let p): "Save and clear \(p)'s coordinator?"
        case .activate(let p): "Activate \(p)?"
        case .deactivate(let p, _): "Deactivate \(p)?"
        case .pin(let p, let on): on ? "Pin \(p) to the top?" : "Unpin \(p)?"
        case .remote: "Remote control"
        case .adopt(_, let s, _): "Adopt \(s)"
        case .withdrawAdopt(_, let s): "Withdraw \(s)"
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
        case .pin(_, let on):
            on ? "It comes first among the active projects." : "It goes back to its place among the active projects."
        case .remote(let p, _, let on):
            on ? "Turn remote control on for \(p), so it can be continued from another device?" : "Turn remote control off for \(p)?"
        case .adopt(let p, let s, let what):
            "Adopt \(s) as a thread of \(p)? The coordinator links it to a task and briefs it.\n\n\(what)"
        case .withdrawAdopt(let p, let s):
            "Withdraw your go-ahead to adopt \(s) as a thread of \(p)? The coordinator can't adopt it then."
        }
    }

    /// What came of turning project's remote control around, as the
    /// TUI's footer says it.
    public static func words(_ r: AppAPI.Remote, project p: String) -> String {
        let state = r.remoteControl ? "on" : "off"
        switch r.how {
        case "unchanged":
            return r.held ? "Remote control is already off for \(p); it stays off until the coordinator is started anew."
                : "Remote control is already \(state) for \(p)."
        case "restarted":
            return r.held
                ? "Remote control off for \(p): the coordinator restarted and continues its conversation; it stays off until it is started anew, then the project's settings turn it on again."
                : "Remote control \(state) for \(p): the coordinator restarted and continues its conversation; until it is started anew."
        default:
            if r.remoteControl { return "Remote control on for \(p): listed there as \"\(p)\"; until the coordinator is started anew." }
            if r.held {
                return "Remote control off for \(p); it stays off until the coordinator is started anew, then the project's settings turn it on again."
            }
            return "Remote control off for \(p); until the coordinator is started anew."
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

    public init(percent: Int, level: Level) {
        self.percent = percent
        self.level = level
    }

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

extension ContextUse {
    /// How full the window is, for a gauge: 0 to 1.
    public var fraction: Double { Double(min(max(percent, 0), 100)) / 100 }
}

/// A save and clear of a coordinator as the phone follows it (T868):
/// asked (coordinator.clear), the coordinator saving what it learned
/// (its turn), its conversation cleared at its next idle moment, done
/// once its context use drops below what it was when asked (the server
/// forgets the use at the clear). Each session.list read moves it on.
public struct SaveClear: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        /// coordinator.clear is on its way.
        case asking
        case saving
        case clearing
        /// Cleared; the use after, nil until the next turn reports one.
        case done(after: Int?)
        case failed(String)
    }

    public private(set) var phase = Phase.asking
    /// The use when asked, in percent: done once it drops below.
    public let before: Int?
    /// The tokens its latest turn read when asked: more, and the save
    /// turn ran, however short it was between two reads.
    private let tokens: Int
    public let since: Date
    /// The coordinator was seen working since the ask: its save turn.
    private var worked = false
    /// When it was done or failed.
    private var ended: Date?

    /// How long it is followed at most: the ticker may wait on a long
    /// turn before it asks for the save.
    public static let longest: TimeInterval = 15 * 60

    /// How long done or failed stays said.
    public static let shown: TimeInterval = 12

    /// Asked at now of coordinator session s.
    public init(coordinator s: AppAPI.Session?, at now: Date) {
        before = ContextUse.of(s, hint: 0)?.percent
        tokens = s?.context ?? 0
        since = now
    }

    /// The server took it.
    public mutating func asked() {
        if phase == .asking { phase = .saving }
    }

    public mutating func fail(_ why: String, at now: Date) {
        phase = .failed(why)
        ended = now
    }

    /// Moves on, at now, from the coordinator's session as the latest
    /// list has it (nil: none runs).
    public mutating func follow(_ s: AppAPI.Session?, at now: Date) {
        guard phase == .saving || phase == .clearing else { return }
        guard let s, s.state != "exited" else {
            fail("The coordinator stopped before it was cleared.", at: now)
            return
        }
        let use = ContextUse.of(s, hint: 0)?.percent
        if let before, use.map({ $0 < before }) ?? true {
            phase = .done(after: use)
            ended = now
            return
        }
        if s.state == "working" || s.context > tokens {
            worked = true
        }
        if worked, s.state != "working" {
            // The save turn ended: the clear goes now. With no use to
            // watch drop, that is as far as it can be seen.
            if before == nil {
                phase = .done(after: nil)
                ended = now
            } else {
                phase = .clearing
            }
        }
    }

    /// Whether it is still on its way.
    public var running: Bool {
        switch phase {
        case .asking, .saving, .clearing: true
        case .done, .failed: false
        }
    }

    /// Whether it is no longer worth showing at now: said long enough,
    /// or followed too long.
    public func over(at now: Date) -> Bool {
        if let ended { return now.timeIntervalSince(ended) > Self.shown }
        return now.timeIntervalSince(since) > Self.longest
    }

    /// What the screen says of it.
    public var words: String {
        switch phase {
        case .asking: "Asking…"
        case .saving: "Saving what it learned…"
        case .clearing: "Clearing…"
        case .done(let after):
            switch (before, after) {
            case (let b?, let a?): "Cleared: context \(b)% → \(a)%"
            // The server forgets the use until the next turn's.
            case (let b?, nil): "Cleared: context was \(b)%"
            default: "Cleared"
            }
        case .failed(let why): why
        }
    }
}
