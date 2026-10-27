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
    }

    public struct Result: Sendable, Equatable {
        /// The result's first line.
        public var text: String
        public var lines: Int
        public var error: Bool
    }

    public private(set) var entries: [Entry] = []
    /// Items left out before the first entry: the server's, and the
    /// ones dropped here past limit.
    public private(set) var skipped = 0
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

    /// Takes the stream's result: the status it opened with.
    public mutating func start(_ status: AppAPI.TranscriptStatus) {
        self.status = status
    }

    /// The entries the conversation draws: the last count of them, and
    /// back to entry keep (a find's match) when it is earlier. A long
    /// transcript then costs a switch to it no more than a short one.
    public func tail(_ count: Int, keeping keep: Int? = nil) -> ArraySlice<Entry> {
        var from = max(0, entries.count - count)
        if let keep, let i = entries.firstIndex(where: { $0.id == keep }), i < from {
            from = i
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
                    entries[i].result = Result(text: it.text, lines: it.lines, error: it.error)
                } else {
                    add(.result, it, result: Result(text: it.text, lines: it.lines, error: it.error))
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

    private mutating func add(_ kind: Entry.Kind, _ it: AppAPI.TranscriptItem, result: Result? = nil) {
        entries.append(Entry(id: nextID, kind: kind, text: it.text, truncated: it.truncated, tool: it.tool, call: it.call,
                             result: result, at: it.at, from: it.from, files: it.files, display: it.display))
        nextID += 1
    }
}
