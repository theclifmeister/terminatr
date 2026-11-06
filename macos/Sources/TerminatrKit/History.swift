import Foundation

/// When to read a project's daily numbers (project.history, app API 58)
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

/// A project's releases in words: each repo's count, a monorepo's by part,
/// never a total, as one repo's versions say nothing of another's. Empty
/// with no releases, where what shows them leaves them out.
public enum ReleaseCounts {
    public static func words(_ repos: [AppAPI.HistoryRepo]) -> String {
        repos.flatMap { r -> [String] in
            guard !r.parts.isEmpty else { return ["\(r.repo) \(r.count)"] }
            return r.parts.map { $0.part.isEmpty ? "\(r.repo) \($0.count)" : "\(r.repo) \($0.part) \($0.count)" }
        }.joined(separator: " · ")
    }
}

/// The Release Tags sheet: which of a project's tags count (release.tags,
/// app API 65), as the TUI's Release tags row sets them.
public struct ReleaseTagsForm: Equatable, Sendable {
    public enum Mode: String, CaseIterable, Sendable {
        case follow, rule, patterns, none
    }

    public var mode: Mode
    /// What the person typed: patterns split on spaces and commas.
    public var text: String
    public let allProjects: String

    /// The form as the project has it now.
    public init(_ tags: AppAPI.ReleaseTags) {
        allProjects = tags.allProjects
        text = tags.patterns.joined(separator: " ")
        if !tags.own {
            mode = .follow
        } else {
            mode = Mode(rawValue: tags.mode) ?? .rule
        }
    }

    public var patterns: [String] {
        text.split(whereSeparator: { $0 == "," || $0.isWhitespace }).map(String.init)
    }

    /// Patterns need one, and "none" is a mode of its own.
    public var canSave: Bool {
        mode != .patterns || !(patterns.isEmpty || patterns == ["none"])
    }

    public func title(_ m: Mode) -> String {
        switch m {
        case .follow: "As all projects (\(allProjects))"
        case .rule: "Version tags"
        case .patterns: "These patterns"
        case .none: "None"
        }
    }

    public func params(project: String) -> AppAPI.ReleaseTagsSetParams {
        AppAPI.ReleaseTagsSetParams(project: project, mode: mode.rawValue, patterns: mode == .patterns ? patterns : [])
    }
}
