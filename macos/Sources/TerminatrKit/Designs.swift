import Foundation

/// A project's designs as its dashboard lists them (T864), from
/// design.list (app API 31): the records the TUI's Designs tab and the
/// mod API's designs.list show, each with its status from its build,
/// its design task, its build in after order, its decided answers and
/// its design task's open questions. Nothing here changes a design: the
/// coordinator records them (tm design set).
public enum Designs {
    /// The statuses, from a design's build.
    public enum Status: String, Sendable {
        case designing, planned, building, done

        /// A status the app doesn't know reads as planned.
        public init(_ s: String) { self = Status(rawValue: s) ?? .planned }
    }

    /// A design's tone on a tile: a question while its design task has
    /// open ones, else by its status: designing and planned idle (the
    /// badge's word tells them apart), building working, done done.
    public static func tone(_ d: AppAPI.Design) -> Attention.Tone {
        if !d.questions.isEmpty { return .question }
        switch Status(d.status) {
        case .designing, .planned: return .idle
        case .building: return .working
        case .done: return .done
        }
    }

    /// A build task's tone, from its status.
    public static func tone(task t: AppAPI.DesignTask) -> Attention.Tone {
        switch t.status {
        case "done": .done
        case "started": .working
        case "review": .review
        case "blocked": .attention
        default: .idle
        }
    }

    /// "3/5 built", "no build tasks".
    public static func built(_ d: AppAPI.Design) -> String {
        d.build.isEmpty && d.tasks.isEmpty ? "no build tasks" : "\(d.tasksDone)/\(max(d.build.count, d.tasks.count)) built"
    }

    /// The share of the build done, 0 without one.
    public static func progress(_ d: AppAPI.Design) -> Double {
        let n = max(d.build.count, d.tasks.count)
        return n == 0 ? 0 : Double(d.tasksDone) / Double(n)
    }

    /// The design task and its status: "T714 · review", "T714" when it
    /// isn't on the board, "" for none.
    public static func designTask(_ d: AppAPI.Design) -> String {
        guard !d.task.isEmpty else { return "" }
        return d.taskStatus.isEmpty ? d.task : "\(d.task) · \(d.taskStatus)"
    }

    /// A design's facts on its row: its design task, its open
    /// questions (only when it has some) and its answers.
    public static func facts(_ d: AppAPI.Design) -> [String] {
        var out: [String] = d.archived ? ["archived"] : []
        let t = designTask(d)
        if !t.isEmpty { out.append("design task \(t)") }
        if !d.questions.isEmpty { out.append(count(d.questions.count, "open question")) }
        out.append(count(d.answers.count, "answer"))
        return out
    }

    /// The design's page, when it is an https URL.
    public static func page(_ d: AppAPI.Design) -> URL? {
        guard let u = URL(string: d.artifact), u.scheme == "https", u.host != nil else { return nil }
        return u
    }

    /// "after T10, T11", "" for a task that comes after none.
    public static func after(_ t: AppAPI.DesignTask) -> String {
        t.after.isEmpty ? "" : "after " + t.after.joined(separator: ", ")
    }

    /// The designs a list shows: without the done and the archived ones
    /// (tm design archive, app API 33) unless it shows all, as tm design
    /// list --all --archived does.
    public static func shown(_ ds: [AppAPI.Design], all: Bool) -> [AppAPI.Design] {
        all ? ds : ds.filter { !$0.archived && Status($0.status) != .done }
    }

    /// The toggle's words: "Show 2 done or archived", "Hide done and
    /// archived"; "" with none hidden.
    public static func toggle(_ ds: [AppAPI.Design], all: Bool) -> String {
        let hidden = ds.count - shown(ds, all: false).count
        if hidden == 0 { return "" }
        return all ? "Hide done and archived" : "Show \(hidden) done or archived"
    }

    /// The designs that need the user first (open questions), then the
    /// ones being built, planned, designed, done, and archived last; by
    /// name within.
    public static func ordered(_ ds: [AppAPI.Design]) -> [AppAPI.Design] {
        func rank(_ d: AppAPI.Design) -> Int {
            if d.archived { return 5 }
            if !d.questions.isEmpty { return 0 }
            switch Status(d.status) {
            case .building: return 1
            case .planned: return 2
            case .designing: return 3
            case .done: return 4
            }
        }
        return ds.sorted { (rank($0), $0.name) < (rank($1), $1.name) }
    }

    static func count(_ n: Int, _ word: String) -> String { n == 1 ? "1 \(word)" : "\(n) \(word)s" }
}
