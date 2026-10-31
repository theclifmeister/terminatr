import Foundation

/// A session's conversation as the window shows it, built from
/// transcript.watch's events (docs/SPEC.md §3.3, Transcript): the
/// prompts, the agent's text, each tool call with its result folded in,
/// files a call sends (an attachment card, never folded into a run of
/// tool calls) and notes. The todo list is kept apart, for the inspector.
public struct Transcript: Sendable, Equatable {
    public struct Entry: Sendable, Equatable, Identifiable {
        public enum Kind: Sendable, Equatable {
            case prompt
            case text
            case tool
            /// A result whose call isn't in the conversation (it was
            /// before the items the server sent).
            case result
            case note
            /// A call that sends the user files (Claude's SendUserFile):
            /// text is its caption, files what it sends.
            case files
        }

        public let id: Int
        public var kind: Kind
        public var text: String
        public var truncated: Bool
        /// A tool call's tool and id.
        public var tool: String
        public var call: String
        /// A tool call's result, once it came.
        public var result: Result?
        /// When the agent wrote it, when its transcript says.
        public var at: Date?
        /// Who sent a prompt: "tm", "coordinator", or empty for the
        /// user (Entry.who).
        public var from: String = ""
        /// The files a prompt's pasted images or a files call put in
        /// front of the user, and how a call shows them: "attach" draws
        /// no preview (Entry.attachments).
        public var files: [AppAPI.TranscriptFile] = []
        public var display: String = ""

        /// The same item, whatever its id and its result so far.
        func same(as o: Entry) -> Bool {
            kind == o.kind && text == o.text && tool == o.tool && call == o.call && at == o.at && from == o.from
        }
    }

    public struct Result: Sendable, Equatable {
        /// The result's first line.
        public var text: String
        public var lines: Int
        public var error: Bool
        /// An answered question menu's: each question and what the user
        /// picked (app API 41; Entry.questionOutcome).
        public var answers: [AppAPI.TranscriptAnswer] = []
    }

    public private(set) var entries: [Entry] = []
    /// Items left out before the first entry: the server's, and the
    /// ones dropped here past limit.
    public private(set) var skipped = 0
    /// The cursor transcript.page reads the items before the first entry
    /// from (a tail's reset, app API 19, then each page's); empty when
    /// there are none, or the server pages none.
    public private(set) var before = ""
    public private(set) var todos: [AppAPI.TranscriptTodo] = []
    public private(set) var status: AppAPI.TranscriptStatus?
    /// Why the stream ended (exited, stopping), once it did.
    public private(set) var closed: String?

    private var nextID = 0
    private let limit: Int

    public init(limit: Int = 2000) {
        self.limit = limit
    }

    /// Applies one of the stream's events.
    public mutating func apply(_ event: String, _ data: AppAPI.TranscriptEvent) {
        switch event {
        case "transcript.reset":
            entries = []
            todos = []
            skipped = data.skipped
            before = data.before
            if let s = data.status { status = s }
            append(data.items)
        case "transcript.items":
            append(data.items)
        case "transcript.status":
            if let s = data.status { status = s }
        case "transcript.closed":
            closed = data.reason.isEmpty ? "closed" : data.reason
        default:
            break
        }
    }

    /// Puts a transcript.page's items (the ones just before the first
    /// entry) in front, and takes its cursor. A result whose call was in
    /// the page folds into that call, as it would have in order.
    public mutating func prepend(_ page: AppAPI.TranscriptPage) {
        var older = Transcript(limit: .max)
        older.append(page.items)
        var mine = entries[...]
        while let r = mine.first, r.kind == .result, !r.call.isEmpty,
              let i = older.entries.lastIndex(where: { ($0.kind == .tool || $0.kind == .files) && $0.call == r.call && $0.result == nil }) {
            older.entries[i].result = r.result
            mine = mine.dropFirst()
        }
        // Ids below the first entry's, so the ones the view has keep theirs.
        let low = (entries.map(\.id).min() ?? nextID) - older.entries.count
        let moved = older.entries.enumerated().map { i, e in
            Entry(id: low + i, kind: e.kind, text: e.text, truncated: e.truncated, tool: e.tool, call: e.call,
                  result: e.result, at: e.at, from: e.from, files: e.files, display: e.display)
        }
        entries = moved + mine
        skipped = max(0, skipped - page.items.count)
        before = page.before
        if todos.isEmpty { todos = older.todos }
    }

    /// Takes the stream's result: the status it opened with.
    public mutating func start(_ status: AppAPI.TranscriptStatus) {
        self.status = status
    }

    /// The entries the conversation draws: the last count of them, and
    /// back to entry keep (a find's match) when it is earlier. A long
    /// transcript then costs a switch to it no more than a short one.
    /// It never starts within a run of tool calls, so the run's group
    /// (rows) keeps its first call, its id.
    public func tail(_ count: Int, keeping keep: Int? = nil) -> ArraySlice<Entry> {
        var from = max(0, entries.count - count)
        if let keep, let i = entries.firstIndex(where: { $0.id == keep }), i < from {
            from = i
        }
        while from > 0, from < entries.count, entries[from].kind == .tool, entries[from - 1].kind == .tool {
            from -= 1
        }
        return entries[from...]
    }

    /// The last tool call still waiting for its result: what a
    /// permission dialog asks about, while one is open.
    public var pendingTool: Entry? {
        guard let last = entries.last(where: { $0.kind == .tool }), last.result == nil else { return nil }
        return last
    }

    private mutating func append(_ items: [AppAPI.TranscriptItem]) {
        for it in items {
            switch it.kind {
            case "todos":
                todos = it.todos
            case "result":
                if !it.call.isEmpty, let i = entries.lastIndex(where: { ($0.kind == .tool || $0.kind == .files) && $0.call == it.call }) {
                    entries[i].result = Result(text: it.text, lines: it.lines, error: it.error, answers: it.answers)
                } else {
                    add(.result, it, result: Result(text: it.text, lines: it.lines, error: it.error, answers: it.answers))
                }
            case "prompt": add(.prompt, it)
            case "text": add(.text, it)
            case "tool": add(it.files.isEmpty ? .tool : .files, it)
            case "note": add(.note, it)
            default:
                break // a kind this app doesn't know yet
            }
        }
        if entries.count > limit {
            let drop = entries.count - limit
            entries.removeFirst(drop)
            skipped += drop
        }
    }

    /// Takes fresh, the same session's transcript read again after a
    /// redial, without starting over (T859): when fresh's first entry is
    /// one this one has, the entries before it stay (the pages read
    /// earlier too), the ones both have keep their ids (so the view keeps
    /// its place), with fresh's results, and the new ones follow. Without
    /// such an entry it is fresh, as a new watch would have it.
    public mutating func rejoin(_ fresh: Transcript) {
        guard let first = fresh.entries.first,
              let at = entries.lastIndex(where: { $0.same(as: first) }),
              zip(entries[at...], fresh.entries).allSatisfy({ $0.same(as: $1) }) else {
            self = fresh
            return
        }
        var kept = Array(entries[..<at])
        for (i, e) in fresh.entries.enumerated() {
            var e = e
            if at + i < entries.count {
                e = Entry(id: entries[at + i].id, kind: e.kind, text: e.text, truncated: e.truncated, tool: e.tool, call: e.call,
                          result: e.result ?? entries[at + i].result, at: e.at, from: e.from, files: e.files, display: e.display)
            } else {
                e = Entry(id: nextID, kind: e.kind, text: e.text, truncated: e.truncated, tool: e.tool, call: e.call,
                          result: e.result, at: e.at, from: e.from, files: e.files, display: e.display)
                nextID += 1
            }
            kept.append(e)
        }
        entries = kept
        if at == 0 {
            skipped = fresh.skipped
            before = fresh.before
        }
        todos = fresh.todos
        status = fresh.status ?? status
        closed = fresh.closed
    }

    private mutating func add(_ kind: Entry.Kind, _ it: AppAPI.TranscriptItem, result: Result? = nil) {
        entries.append(Entry(id: nextID, kind: kind, text: it.text, truncated: it.truncated, tool: it.tool, call: it.call,
                             result: result, at: it.at, from: it.from, files: it.files, display: it.display))
        nextID += 1
    }
}
