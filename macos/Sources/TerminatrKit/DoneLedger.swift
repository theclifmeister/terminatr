import Foundation

extension AppAPI.Methods {
    /// task.done as a tm before app API 57 takes it, the project alone: its
    /// bridge refuses params it doesn't know, which TaskDoneParams sends.
    public static let taskDoneAll = AppAPI.Call<AppAPI.ProjectParams, AppAPI.DoneTasks>("task.done", write: false)
    /// task.done as a tm of app API 57 to 67 takes it, without a repo.
    public static let taskDoneNoRepo = AppAPI.Call<AppAPI.TaskDoneNoRepoParams, AppAPI.DoneTasks>("task.done", write: false)
}

extension AppAPI {
    /// TaskDoneParams without repo, which the bridge of a tm before app API
    /// 68 refuses.
    public struct TaskDoneNoRepoParams: Codable, Sendable, Hashable {
        public var project: String
        public var groups: Bool
        public var from: String
        public var to: String
        public var archived: Bool
        public var offset: Int
        public var limit: Int
        public var tz: String

        public init(_ p: TaskDoneParams) {
            project = p.project
            groups = p.groups
            from = p.from
            to = p.to
            archived = p.archived
            offset = p.offset
            limit = p.limit
            tz = p.tz
        }
    }
}

extension AppAPI.DoneGroup {
    /// Names the group among its project's: its days don't overlap another's.
    public var key: String { "\(from):\(to)" }
}

/// What a repo filter calls its entries: tm's name for a repo, and none and
/// all in words.
public enum DoneRepoWords {
    public static func title(_ repo: String) -> String {
        switch repo {
        case "": "All repos"
        case "none": "No repo"
        default: repo
        }
    }

    /// A release count beside a filter: never a sum, only one repo's.
    public static func releases(_ count: Int) -> String {
        count == 1 ? "1 release" : "\(count) releases"
    }
}

/// What a board shows of Done by day (app API 57; design board-library,
/// B3): the groups tm heads it with, which of them the user opened or
/// folded, and the rows read of each open one. tm decides the groups, so
/// every surface says the same days in the same words.
public struct DoneLedger: Equatable, Sendable {
    /// The rows read of an open group; next is the offset to read on
    /// from, 0 when they are all there.
    public struct Fold: Equatable, Sendable {
        public var tasks: [AppAPI.Task]
        public var next: Int

        public init(tasks: [AppAPI.Task], next: Int) {
            self.tasks = tasks
            self.next = next
        }
    }

    /// A fold's page: how many rows a read asks for.
    public static let page = 50

    /// The headings, nil until read.
    public private(set) var groups: [AppAPI.DoneGroup]?
    /// How many done tasks the groups hold, the archive's too.
    public private(set) var total = 0
    /// What a filter offers, with several repos (app API 68).
    public private(set) var repos: [AppAPI.DoneRepo] = []
    private var rows: [String: Fold] = [:]
    /// The groups the user opened or folded from how they came.
    private var toggled: Set<String> = []

    public init() {}

    /// Whether group shows its rows: as it came, unless the user pressed it.
    public func isOpen(_ group: AppAPI.DoneGroup) -> Bool {
        group.open != toggled.contains(group.key)
    }

    /// The rows read of group, while it is open.
    public func tasks(in group: AppAPI.DoneGroup) -> [AppAPI.Task] {
        isOpen(group) ? rows[group.key]?.tasks ?? [] : []
    }

    /// One row of an open group: a task, or a release's line.
    public enum Entry: Hashable, Sendable, Identifiable {
        case task(AppAPI.Task)
        case release(AppAPI.DoneRelease)

        public var id: String {
            switch self {
            case .task(let t): "t:\(t.id)"
            case .release(let r): "r:\(r.repo):\(r.tag)"
            }
        }
    }

    /// The rows read of group with the release lines at their times, tm
    /// having said how many rows each comes after. A line past the rows
    /// read waits for them.
    public func entries(in group: AppAPI.DoneGroup) -> [Entry] {
        guard isOpen(group) else { return [] }
        let rows = tasks(in: group)
        var out: [Entry] = []
        var lines = group.releases[...]
        for (i, t) in rows.enumerated() {
            while let l = lines.first, l.offset <= i {
                out.append(.release(l))
                lines = lines.dropFirst()
            }
            out.append(.task(t))
        }
        while let l = lines.first, l.offset <= rows.count {
            out.append(.release(l))
            lines = lines.dropFirst()
        }
        return out
    }

    /// Forgets what was read of another repo, keeping which folds are open.
    public mutating func refilter() {
        groups = nil
        rows = [:]
    }

    /// How many more rows group has to read.
    public func more(in group: AppAPI.DoneGroup) -> Int {
        guard isOpen(group), let f = rows[group.key], f.next > 0 else { return 0 }
        return max(group.count - f.tasks.count, 0)
    }

    /// The offset to read group on from.
    public func next(in group: AppAPI.DoneGroup) -> Int {
        rows[group.key]?.next ?? 0
    }

    /// Takes a groups read, and answers the open groups whose rows must be
    /// read again, each with how many: a fold the user opened, or read on.
    public mutating func take(_ done: AppAPI.DoneTasks) -> [(group: AppAPI.DoneGroup, limit: Int)] {
        let was = rows
        groups = done.groups
        total = done.total
        repos = done.repos
        rows = [:]
        var again: [(group: AppAPI.DoneGroup, limit: Int)] = []
        for g in done.groups where isOpen(g) {
            let read = was[g.key]?.tasks.count ?? 0
            if g.open && read <= g.tasks.count {
                rows[g.key] = Fold(tasks: g.tasks, next: g.next)
            } else {
                if let old = was[g.key] { rows[g.key] = old }
                again.append((g, max(read, Self.page)))
            }
        }
        return again
    }

    /// Presses group's heading, and says whether it needs a read now.
    public mutating func press(_ group: AppAPI.DoneGroup) -> Bool {
        if toggled.contains(group.key) { toggled.remove(group.key) } else { toggled.insert(group.key) }
        guard isOpen(group), rows[group.key] == nil else { return false }
        if group.open {
            rows[group.key] = Fold(tasks: group.tasks, next: group.next)
            return false
        }
        return true
    }

    /// Replaces group's rows with a read from its start.
    public mutating func set(_ group: AppAPI.DoneGroup, _ page: AppAPI.DoneTasks) {
        rows[group.key] = Fold(tasks: page.tasks, next: page.next)
    }

    /// Adds a read further on to group's rows.
    public mutating func append(_ group: AppAPI.DoneGroup, _ page: AppAPI.DoneTasks) {
        let had = rows[group.key]?.tasks ?? []
        rows[group.key] = Fold(tasks: had + page.tasks, next: page.next)
    }
}
