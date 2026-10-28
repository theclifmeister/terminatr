import Foundation

/// A task as the window's task view shows it (T780), what the TUI's task
/// panel shows (enter on a task): what it is blocked on, for a task in
/// review where its PR stands and how to check it, its steps and its
/// notes. It reads the projects watch: a task that needs the user or
/// that a thread works on comes whole; one on deck or in the backlog
/// comes without its notes and steps (partial), which task.get reads.
public struct TaskDetail: Equatable, Sendable {
    public let project: String
    public let task: AppAPI.Task
    /// Its unresolved thread, nil for none.
    public let thread: AppAPI.Thread?
    /// Whether task lacks its notes and steps (a board task's copy).
    public let partial: Bool

    public init(project: String, task: AppAPI.Task, thread: AppAPI.Thread? = nil, partial: Bool = false) {
        self.project = project
        self.task = task
        self.thread = thread
        self.partial = partial
    }

    /// Task ref ("T12") of project as the watch has it, nil when the
    /// watch has it nowhere (done, archived, or another project's).
    public static func of(_ ref: String, in project: AppAPI.Project) -> TaskDetail? {
        let threads = project.threads
        func thread(of t: AppAPI.Task) -> AppAPI.Thread? {
            threads.first { !t.thread.isEmpty && $0.record.id == t.thread } ?? threads.first { $0.record.task == t.ref }
        }
        if let t = project.needsYou.first(where: { $0.ref == ref }) ?? threads.lazy.compactMap(\.task).first(where: { $0.ref == ref }) {
            return TaskDetail(project: project.slug, task: t, thread: thread(of: t))
        }
        if let t = project.board.first(where: { $0.ref == ref }) {
            return TaskDetail(project: project.slug, task: t, thread: thread(of: t), partial: true)
        }
        return nil
    }

    /// Task ref of project as the watch has it, filled with task.get's
    /// copy (filled); one the watch has nowhere, a done task's, is
    /// task.get's copy alone, without a thread. nil when neither has it.
    public static func of(_ ref: String, in project: AppAPI.Project, fetched: AppAPI.Task?) -> TaskDetail? {
        if let d = of(ref, in: project) { return d.filled(fetched) }
        guard let fetched, fetched.ref == ref else { return nil }
        return TaskDetail(project: project.slug, task: fetched)
    }

    /// The same with full, task.get's copy of the task, unless the
    /// watch's has moved on since (another status or update).
    public func filled(_ full: AppAPI.Task?) -> TaskDetail {
        guard partial, let full, full.ref == task.ref, full.status == task.status, full.updated == task.updated else { return self }
        return TaskDetail(project: project, task: full, thread: thread)
    }

    /// What a blocked task is blocked on: its latest "blocked (date):"
    /// note, "" when no note says. nil for a task that isn't blocked.
    public var blockedOn: String? {
        guard task.status == "blocked" else { return nil }
        return Self.blockedOn(task.notes)
    }

    /// For a task in review, how to check it: its thread's report's
    /// ## Check, then the notes that say so ("Check:", "To check:",
    /// "How to check:"). nil for a task that isn't in review.
    public var checks: [String]? {
        guard task.status == "review" else { return nil }
        return (thread?.check ?? []) + Self.noteChecks(task.notes)
    }

    /// Where a task in review's PR stands: "PR #12 merged", "PR #12
    /// open, not merged yet", "PR #12 closed without merging", or the
    /// report's PR link while the ticker has no number. nil for none.
    public var ship: Ship? {
        guard task.status == "review", let th = thread else { return nil }
        if th.pr > 0 {
            let pr = "PR #\(th.pr)"
            switch th.prState {
            case "MERGED": return Ship(text: "\(pr) merged", state: .merged)
            case "OPEN": return Ship(text: "\(pr) open, not merged yet", state: .open)
            case "CLOSED": return Ship(text: "\(pr) closed without merging", state: .closed)
            default: return Ship(text: pr, state: .unknown)
            }
        }
        return th.reportPr.isEmpty ? nil : Ship(text: th.reportPr, state: .unknown)
    }

    public struct Ship: Equatable, Sendable {
        public enum State: Sendable { case open, merged, closed, unknown }
        public let text: String
        public let state: State
    }

    /// The steps done, as the TUI says them: "2 of 5 steps done".
    public var stepsDone: String {
        let done = task.steps.filter(\.done).count
        return "\(done) of \(task.steps.count) step\(task.steps.count == 1 ? "" : "s") done"
    }

    /// The notes' paragraphs, blank lines between them dropped.
    public var notes: String {
        task.notes.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The latest "blocked (date): why" paragraph of notes, as why on
    /// one line; "" when none says.
    static func blockedOn(_ notes: String) -> String {
        for p in paragraphs(notes).reversed() {
            guard p.hasPrefix("blocked ("), let close = p.range(of: "): ") else { continue }
            return oneLine(String(p[close.upperBound...]))
        }
        return ""
    }

    /// The paragraphs of notes that say how to check the task, past a
    /// status note's "review (date): ", each on one line.
    static func noteChecks(_ notes: String) -> [String] {
        var out: [String] = []
        for var p in paragraphs(notes) {
            if let m = p.range(of: #"^[a-z]+ \([0-9-]+\): "#, options: .regularExpression) {
                p = String(p[m.upperBound...])
            }
            for pre in ["check:", "to check:", "how to check:"] where p.lowercased().hasPrefix(pre) {
                let s = oneLine(String(p.dropFirst(pre.count)))
                if !s.isEmpty { out.append(s) }
                break
            }
        }
        return out
    }

    private static func paragraphs(_ notes: String) -> [String] {
        notes.trimmingCharacters(in: .whitespacesAndNewlines)
            .components(separatedBy: "\n\n")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
    }

    private static func oneLine(_ s: String) -> String {
        s.split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }.joined(separator: " ")
    }
}
