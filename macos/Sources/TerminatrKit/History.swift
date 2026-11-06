import Foundation

/// When to read a project's daily numbers (project.history, app API 57)
/// again: when the board's done count changed, as the Done fold reads
/// task.done, never on a timer. The server keeps the numbers in memory,
/// but a project's journal moves every few seconds while agents work, and
/// nothing a client shows of them moves until a task finishes.
public struct HistoryReads: Equatable, Sendable {
    private var read: [String: Int] = [:]

    public init() {}

    /// Whether project's numbers need a read at this done count; it
    /// counts as read from here on.
    public mutating func needsRead(_ project: String, done: Int) -> Bool {
        if read[project] == done { return false }
        read[project] = done
        return true
    }

    /// Forgets project's read, one that failed, so the next asks again.
    public mutating func forget(_ project: String) {
        read[project] = nil
    }
}
