import Foundation

/// What the detail shows of a project below its needs, and how it
/// words what it does (T748 milestone C): its threads grouped by state,
/// its board (on deck shown, the backlog folded, C2), the next step an
/// empty project offers, and the task actions' refusals.
public enum Dashboard {
    /// A group of the dashboard's threads, in the order it shows them:
    /// what waits on the user first, idle and stopped ones folded.
    public enum ThreadGroup: String, CaseIterable, Sendable {
        case waiting = "Waiting on you"
        case working = "Working"
        case idle = "Idle"
        case stopped = "Stopped"

        /// Whether the dashboard folds it until the user opens it.
        public var folded: Bool { self == .idle || self == .stopped }
    }

    /// The group of a thread with attention a; running says its session
    /// runs.
    public static func group(_ a: Attention, done: Bool, running: Bool) -> ThreadGroup {
        if a.waits { return .waiting }
        if done || !running { return .stopped }
        return a.tone == .working ? .working : .idle
    }

    /// threads in their groups, the groups in ThreadGroup's order, each
    /// keeping the threads' order; empty groups left out.
    public static func groups(_ threads: [AppAPI.Thread], attention: (AppAPI.Thread) -> Attention,
                              running: (AppAPI.Thread) -> Bool) -> [(group: ThreadGroup, threads: [AppAPI.Thread])] {
        var by: [ThreadGroup: [AppAPI.Thread]] = [:]
        for t in threads {
            by[group(attention(t), done: t.record.done, running: running(t)), default: []].append(t)
        }
        return ThreadGroup.allCases.compactMap { g in by[g].map { (g, $0) } }
    }

    /// The board's tasks on deck (ready), in board order.
    public static func onDeck(_ p: AppAPI.Project) -> [AppAPI.Task] {
        p.board.filter { $0.status == "ready" }
    }

    /// The board's backlog (open tasks), in board order.
    public static func backlog(_ p: AppAPI.Project) -> [AppAPI.Task] {
        p.board.filter { $0.status == "open" }
    }

    /// How many of the board's tasks are done (its counts'), which the
    /// board's Done fold lists through task.done (app API 20).
    public static func doneCount(_ p: AppAPI.Project) -> Int {
        p.counts["done"] ?? 0
    }

    /// The tasks of ts that filter finds, in their order: each of its
    /// words is in the task's id or title, ignoring case and accents, as
    /// the TUI's Tasks tab filter (/) finds them. An empty filter finds
    /// them all.
    public static func filtered(_ ts: [AppAPI.Task], by filter: String) -> [AppAPI.Task] {
        let words = filter.split(whereSeparator: \.isWhitespace)
        guard !words.isEmpty else { return ts }
        return ts.filter { t in
            words.allSatisfy { w in
                t.ref.range(of: w, options: [.caseInsensitive, .diacriticInsensitive]) != nil
                    || t.title.range(of: w, options: [.caseInsensitive, .diacriticInsensitive]) != nil
            }
        }
    }

    /// What a coordinator's composer starts with when the user asks it
    /// about task t (the task view's Ask the Coordinator, as the TUI's c
    /// opens the coordinator about a task): the task, then the user's
    /// question.
    public static func askAbout(_ t: AppAPI.Task) -> String {
        "About \(t.ref) (\(t.title)): "
    }

    /// Whether p has nothing to show: no thread, no task on its board
    /// but done ones, nothing that needs the user, nothing in its inbox.
    public static func isEmpty(_ p: AppAPI.Project, needs: Int) -> Bool {
        let open = p.counts.filter { $0.key != "done" }.values.reduce(0, +)
        return p.threads.isEmpty && p.board.isEmpty && open == 0 && needs == 0 && !p.items.contains(where: \.forYou)
    }

    /// The next step an empty project offers.
    public enum NextStep: Equatable, Sendable {
        /// The project is inactive: Activate (or tm project activate)
        /// turns it on.
        case activate
        /// No coordinator runs: start it.
        case start
        /// Tell the coordinator what to do.
        case message
    }

    public static func next(_ p: AppAPI.Project, coordinatorRuns: Bool) -> NextStep {
        if !p.active { return .activate }
        return coordinatorRuns ? .message : .start
    }

    /// A task action's refusal (task.accept, task.send-back,
    /// task.delegate, project.open), in the window's words; the server's
    /// own message for the rest.
    public static func words(_ error: Swift.Error, task: String = "") -> String {
        guard let e = error as? AppAPI.Error else { return error.localizedDescription }
        let t = task.isEmpty ? "The task" : task
        switch e.code {
        case "already-asked": return "Another ask about \(t) waits on the coordinator."
        case "not-accept", "not-send-back": return "\(t) isn't in review any more."
        case "not-delegate": return "\(t) can't be delegated now: it's in motion, in review or done."
        case "unknown-task": return "\(t) isn't on the board any more."
        case "project-inactive": return "The project is inactive: activate it first (Activate on its page, or tm project activate)."
        default: return e.localizedDescription
        }
    }
}

extension Transcript.Entry {
    /// Who wrote the entry, as its turn names it: "tm", "coordinator"
    /// or "you" for a prompt, agent for the agent's text; empty for the
    /// rest.
    public func who(agent: String) -> String {
        switch kind {
        case .prompt: from.isEmpty ? "you" : from
        case .text, .files: agent
        default: ""
        }
    }

    /// Whether the conversation folds it to one line: tm's prompts (the
    /// thread's kickoff, the server's [tm] notes), which the user opens.
    public var folds: Bool { kind == .prompt && from == "tm" }

    /// A long prompt of the user's or the coordinator's as it shows
    /// folded: its first lines (at most lines of them, about chars
    /// characters), the rest a click away; nil for one that isn't long,
    /// or not a prompt (tm's fold whole, folds).
    public func preview(lines: Int = 10, chars: Int = 800) -> String? {
        guard kind == .prompt, !folds else { return nil }
        let all = text.split(separator: "\n", omittingEmptySubsequences: false)
        if all.count > lines + 2 {
            return all.prefix(lines).joined(separator: "\n")
        }
        if text.count > chars + chars / 2 {
            return String(text.prefix(chars)) + "…"
        }
        return nil
    }

    /// How many lines the text has.
    public var lineCount: Int { text.reduce(1) { $1 == "\n" ? $0 + 1 : $0 } }

    /// A folded prompt's one line: what tm did, or its first line
    /// without the [tm] mark.
    public var headline: String {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if t.hasPrefix("Start on your task.") { return "started the thread · brief and rules" }
        if t.hasPrefix("You are now thread ") { return "adopted this session · brief and rules" }
        if t.hasPrefix("You are this project's coordinator.") { return "started the coordinator" }
        var line = t.split(separator: "\n", maxSplits: 1).first.map(String.init) ?? ""
        if line.hasPrefix("[tm]") { line = String(line.dropFirst(4)).trimmingCharacters(in: .whitespaces) }
        return line
    }
}
