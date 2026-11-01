import Foundation

/// A task's priority (T927): must, should or could, "" for none, which
/// ranks it within its board group, as tm task list and the TUI's Tasks
/// tab sort it. task.priority sets it (app API 42).
public enum Priority {
    /// The levels, highest first.
    public static let levels = ["must", "should", "could"]

    /// What the picker offers: the levels, then none ("").
    public static let choices = levels + [""]

    /// The server's capability for task.priority.
    public static let cap = "task-priority"

    /// p's place: must 0, should 1, could 2, none (or one this app
    /// doesn't know) 3.
    public static func rank(_ p: String) -> Int {
        levels.firstIndex(of: p) ?? levels.count
    }

    /// ts by priority, keeping their order within one (the board's, by
    /// id): what a server before app API 42 sends needs no sorting.
    public static func sorted(_ ts: [AppAPI.Task]) -> [AppAPI.Task] {
        ts.enumerated().sorted { a, b in
            let (ra, rb) = (rank(a.element.priority), rank(b.element.priority))
            return ra != rb ? ra < rb : a.offset < b.offset
        }.map(\.element)
    }

    /// p as the picker words it: "none" for none.
    public static func word(_ p: String) -> String { p.isEmpty ? "none" : p }

    /// Whether a task of status takes a priority: not a done or
    /// dropped one.
    public static func settable(_ status: String) -> Bool {
        status != "done" && status != "dropped"
    }

    /// Whether hello's tm sets a priority from the app: its bridge has
    /// task.priority (at this device's level) and its server says so.
    public static func sets(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= Compatibility.priorityAPI && hello.methods.contains(AppAPI.Methods.taskPriority.name)
            && (hello.server?.caps.contains(cap) ?? false)
    }
}
