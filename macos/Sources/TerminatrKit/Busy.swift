import Foundation

/// A session whose agent has its turn (T803): its row's and card's
/// activity mark with `working · 3m`, and the conversation's Working…
/// bar with what it does now. Only state "working" is busy: an agent
/// that asks, is blocked or idles has the user's turn, or none.
public struct Busy: Equatable, Sendable {
    /// When the turn began (app API 26's turn_since); nil from an older
    /// tm, which then shows no time.
    public let since: Date?

    public init(since: Date?) {
        self.since = since
    }

    /// s's, while its agent works; nil otherwise.
    public static func of(_ s: AppAPI.Session?) -> Busy? {
        guard let s, s.state == "working" else { return nil }
        return Busy(since: s.turnSince)
    }

    /// Thread t's, with s its session, while it works and nothing of
    /// it waits on the user (Attention): a thread in review or with a
    /// question shows that, not the mark.
    public static func of(thread t: AppAPI.Thread, session s: AppAPI.Session?) -> Busy? {
        Attention.of(thread: t, session: s).waits ? nil : of(s)
    }

    /// Project p's coordinator's, with s its session, as a thread's.
    public static func of(coordinator s: AppAPI.Session?, in p: AppAPI.Project) -> Busy? {
        Attention.of(coordinator: s, in: p).waits ? nil : of(s)
    }

    /// The time the turn has run: "12s", "3m", "1h 5m"; nil without a
    /// start.
    public func elapsed(now: Date) -> String? {
        since.map { Self.short(now.timeIntervalSince($0)) }
    }

    /// The time as the Working… bar counts it: "0:45", "3:12",
    /// "1:05:09"; nil without a start.
    public func clock(now: Date) -> String? {
        guard let since else { return nil }
        let s = max(0, Int(now.timeIntervalSince(since)))
        let (h, m, sec) = (s / 3600, (s % 3600) / 60, s % 60)
        return h > 0 ? String(format: "%d:%02d:%02d", h, m, sec) : String(format: "%d:%02d", m, sec)
    }

    /// "working · 3m", or "working" without a start.
    public func word(now: Date) -> String {
        elapsed(now: now).map { "working · \($0)" } ?? "working"
    }

    /// What VoiceOver hears: "working for 3 minutes".
    public func spoken(now: Date) -> String {
        guard let since else { return "working" }
        let f = DateComponentsFormatter()
        f.unitsStyle = .full
        f.allowedUnits = now.timeIntervalSince(since) < 60 ? [.second] : [.hour, .minute]
        f.maximumUnitCount = 2
        return f.string(from: max(0, now.timeIntervalSince(since))).map { "working for \($0)" } ?? "working"
    }

    /// A duration as the rows show it: seconds under a minute, minutes
    /// under an hour, then hours and minutes.
    public static func short(_ t: TimeInterval) -> String {
        let s = max(0, Int(t))
        if s < 60 { return "\(s)s" }
        if s < 3600 { return "\(s / 60)m" }
        let m = (s % 3600) / 60
        return m == 0 ? "\(s / 3600)h" : "\(s / 3600)h \(m)m"
    }
}

/// What the agent does now, in words, for the Working… bar: its latest
/// tool call still waiting for its result ("running make test",
/// "editing TerminalTab.swift"), else its todo in progress, else
/// "thinking".
public enum Activity {
    public static func of(_ t: Transcript, current: String) -> String {
        if let call = t.pendingTool, !call.asksUser {
            return of(tool: call.tool, argument: call.text)
        }
        let todo = t.todos.first { $0.status == "in_progress" }?.text ?? ""
        if !todo.isEmpty { return lowered(todo) }
        if !current.isEmpty { return lowered(current) }
        return "thinking"
    }

    /// One call in words, by its tool (Claude's and Codex's names) and
    /// main argument.
    public static func of(tool: String, argument: String) -> String {
        let arg = argument.split(separator: "\n", maxSplits: 1).first.map(String.init) ?? ""
        let file = (arg as NSString).lastPathComponent
        func with(_ verb: String, _ what: String) -> String { what.isEmpty ? verb : "\(verb) \(what)" }
        switch tool {
        case "Bash", "shell", "exec_command", "local_shell": return with("running", arg)
        case "BashOutput": return "reading a command's output"
        case "Edit", "MultiEdit", "NotebookEdit", "apply_patch": return with("editing", file)
        case "Write": return with("writing", file)
        case "Read": return with("reading", file)
        case "Grep": return with("searching for", arg)
        case "Glob": return with("finding", arg)
        case "WebFetch": return with("fetching", arg)
        case "WebSearch", "web_search": return with("searching the web for", arg)
        case "Task", "Agent": return with("running an agent:", arg)
        case "TodoWrite", "update_plan": return "updating its todos"
        case "": return with("running a tool", arg)
        default: return with(tool, arg)
        }
    }

    /// A todo's words as the bar's: "Running the tests" reads "running
    /// the tests" after "Working… ·".
    private static func lowered(_ s: String) -> String {
        guard let first = s.first, first.isUppercase, s.dropFirst().first?.isUppercase != true else { return s }
        return first.lowercased() + s.dropFirst()
    }
}
