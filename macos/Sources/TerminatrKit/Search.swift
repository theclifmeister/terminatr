import Foundation
import Observation

/// Search over a project's history (T924, design agent-recall T914), as
/// tm search and the TUI's search popup ask it: the hits (search.query),
/// a hit's timeline (search.timeline) and its text (search.show), the
/// three layers, from app API 41 when the server's caps say search. The
/// human's scope: the coordinator's notes and the inbox too. This
/// server's projects only; nothing here writes.
public enum Search {
    /// A kind of chunk, as the index names it, with the words and the
    /// symbol a hit shows.
    public struct Kind: Identifiable, Hashable, Sendable {
        public let id: String
        public let label: String
        public let symbol: String
    }

    /// The kinds, in tm search --in's order; a kind the app doesn't know
    /// shows as its own name (kind(_:)).
    public static let kinds: [Kind] = [
        Kind(id: "notes", label: "Notes", symbol: "note.text"),
        Kind(id: "tasks", label: "Tasks", symbol: "checklist"),
        Kind(id: "threads", label: "Threads", symbol: "bubble.left.and.text.bubble.right"),
        Kind(id: "journal", label: "Journal", symbol: "clock.arrow.circlepath"),
        Kind(id: "designs", label: "Designs", symbol: "square.on.square.dashed"),
        Kind(id: "library", label: "Library", symbol: "books.vertical"),
        Kind(id: "context", label: "Context", symbol: "doc.text"),
        Kind(id: "inbox", label: "Inbox", symbol: "tray"),
    ]

    public static func kind(_ id: String) -> Kind {
        kinds.first { $0.id == id } ?? Kind(id: id, label: id.capitalized, symbol: "doc")
    }

    /// The day a hit was written, as tm search prints it (2026-11-01),
    /// in the user's time zone.
    public static func day(_ d: Date, in zone: TimeZone = .current) -> String {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = zone
        let c = cal.dateComponents([.year, .month, .day], from: d)
        return String(format: "%04d-%02d-%02d", c.year ?? 0, c.month ?? 0, c.day ?? 0)
    }

    /// What a hit opens: its task, its thread, or the memory file it is
    /// a section of.
    public enum Target: Hashable, Identifiable, Sendable {
        case task(String)
        case thread(String)
        case note(path: String)

        public var id: String {
            switch self {
            case .task(let t): "task:" + t
            case .thread(let t): "thread:" + t
            case .note(let p): "note:" + p
            }
        }

        /// The button's words.
        public var label: String {
            switch self {
            case .task(let t): "Open \(t)"
            case .thread(let t): "Open \(t)"
            case .note: "Open Note"
            }
        }

        public var symbol: String {
            switch self {
            case .task: "checklist"
            case .thread: "bubble.left.and.text.bubble.right"
            case .note: "note.text"
            }
        }
    }

    /// What hit h opens, task first: its task (the hit's, or a task's
    /// own id), its thread, and for a note or the project's context a
    /// file memory.read reads (CONTEXT.md, MEMORY.md, memory/*.md).
    public static func targets(_ h: AppAPI.SearchHit) -> [Target] {
        var out: [Target] = []
        if !h.task.isEmpty {
            out.append(.task(h.task))
        } else if h.kind == "tasks", isTaskRef(h.id) {
            out.append(.task(h.id))
        }
        if !h.thread.isEmpty { out.append(.thread(h.thread)) }
        if readsAsMemory(h.path), h.member.isEmpty { out.append(.note(path: h.path)) }
        return out
    }

    /// Whether s is a task's id: T and digits.
    public static func isTaskRef(_ s: String) -> Bool {
        s.count > 1 && s.first == "T" && s.dropFirst().allSatisfy(\.isASCIIDigit)
    }

    /// Whether path is one memory.read reads.
    public static func readsAsMemory(_ path: String) -> Bool {
        path == "CONTEXT.md" || path == "MEMORY.md" || (path.hasPrefix("memory/") && path.hasSuffix(".md") && !path.dropFirst(7).contains("/"))
    }

    /// The line under the query: "3 hits", "20 hits of 41", "No hits".
    public static func summary(_ r: AppAPI.SearchResult) -> String {
        switch (r.hits.count, r.total) {
        case (0, _): "No hits"
        case (let n, let t) where t > n: "\(n) hits of \(t)"
        case (1, _): "1 hit"
        case (let n, _): "\(n) hits"
        }
    }

    /// A hit's facts after its title: its task and thread, and the
    /// journal lines folded into it ("T914 · t-0833 · +3 journal").
    public static func facts(_ h: AppAPI.SearchHit) -> String {
        var out: [String] = []
        if !h.task.isEmpty, h.task != h.id { out.append(h.task) }
        if !h.thread.isEmpty, h.thread != h.id { out.append(h.thread) }
        if h.more > 0 { out.append(h.more == 1 ? "+1 journal line" : "+\(h.more) journal lines") }
        return out.joined(separator: " · ")
    }

    /// The query a search asks: words, the kind picked ("" for all) and
    /// how many hits.
    public static func params(project: String, words: String, kind: String = "", limit: Int = 50) -> AppAPI.SearchQueryParams {
        AppAPI.SearchQueryParams(project: project, words: words.trimmingCharacters(in: .whitespacesAndNewlines),
                                 in: kind.isEmpty ? [] : [kind], limit: limit)
    }

    /// The timeline's rows, in time order: those before, the chunk, those
    /// after; at marks the chunk.
    public struct Row: Identifiable, Hashable, Sendable {
        public let hit: AppAPI.SearchHit
        public let at: Bool
        public var id: String { hit.id }
    }

    public static func rows(_ t: AppAPI.SearchTimeline) -> [Row] {
        t.before.map { Row(hit: $0, at: false) } + [Row(hit: t.at, at: true)] + t.after.map { Row(hit: $0, at: false) }
    }

    /// The calls a search makes, as a bridge answers them; a test's own.
    public struct Calls: Sendable {
        public var query: @Sendable (AppAPI.SearchQueryParams) async throws -> AppAPI.SearchResult
        public var timeline: @Sendable (AppAPI.SearchTimelineParams) async throws -> AppAPI.SearchTimeline
        public var show: @Sendable (AppAPI.SearchShowParams) async throws -> AppAPI.SearchShown

        public init(query: @escaping @Sendable (AppAPI.SearchQueryParams) async throws -> AppAPI.SearchResult,
                    timeline: @escaping @Sendable (AppAPI.SearchTimelineParams) async throws -> AppAPI.SearchTimeline,
                    show: @escaping @Sendable (AppAPI.SearchShowParams) async throws -> AppAPI.SearchShown) {
            self.query = query
            self.timeline = timeline
            self.show = show
        }

        public init(_ b: Bridge) {
            self.init(query: { try await b.call(AppAPI.Methods.searchQuery, $0) },
                      timeline: { try await b.call(AppAPI.Methods.searchTimeline, $0) },
                      show: { try await b.call(AppAPI.Methods.searchShow, $0) })
        }
    }
}

extension Character {
    fileprivate var isASCIIDigit: Bool { isASCII && isNumber }
}

/// One search, as a search window or screen shows it: the query and
/// its hits, then the hit shown, with its text and its timeline. A
/// later search or hit wins over one still answering.
@MainActor @Observable
public final class SearchModel {
    public var project: String
    public var words = ""
    /// The kind searched, "" for all.
    public var kind = ""
    public private(set) var result: AppAPI.SearchResult?
    public private(set) var searching = false
    public private(set) var failure: String?

    /// The hit shown, its text and its timeline (nil while they come),
    /// and what went wrong reading them.
    public private(set) var shown: AppAPI.SearchHit?
    public private(set) var text: AppAPI.SearchText?
    public private(set) var timeline: AppAPI.SearchTimeline?
    public private(set) var shownFailure: String?

    @ObservationIgnored private let calls: Search.Calls
    @ObservationIgnored private var asked = 0
    @ObservationIgnored private var opened = 0

    public init(project: String, calls: Search.Calls) {
        self.project = project
        self.calls = calls
    }

    /// Whether the words would search.
    public var canSearch: Bool { !words.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    /// Asks the words; the hit shown goes.
    public func search() async {
        guard canSearch else { return }
        asked += 1
        let n = asked
        searching = true
        failure = nil
        close()
        do {
            let r = try await calls.query(Search.params(project: project, words: words, kind: kind))
            guard n == asked else { return }
            result = r
        } catch {
            guard n == asked else { return }
            result = nil
            failure = Self.words(error)
        }
        searching = false
    }

    /// Shows hit h: its text and its timeline, read together.
    public func show(_ h: AppAPI.SearchHit) async {
        opened += 1
        let n = opened
        shown = h
        text = nil
        timeline = nil
        shownFailure = nil
        let p = project, calls = calls
        async let t = Result(awaiting: { try await calls.show(AppAPI.SearchShowParams(project: p, ids: [h.id])) })
        async let l = Result(awaiting: { try await calls.timeline(AppAPI.SearchTimelineParams(project: p, id: h.id)) })
        let (shownR, lineR) = await (t, l)
        guard n == opened else { return }
        switch shownR {
        case .success(let s):
            if let it = s.items.first {
                if it.error.isEmpty { text = it } else { shownFailure = it.error }
            }
        case .failure(let e): shownFailure = Self.words(e)
        }
        if case .success(let tl) = lineR { timeline = tl }
    }

    /// Shows the chunk named id, as a screen opened by its id alone (the
    /// phone's hit screen): what it is comes with its text.
    public func show(id: String) async {
        await show(AppAPI.SearchHit(id: id, kind: "", title: "", time: .distantPast, path: ""))
    }

    /// The hit shown, as its text or its timeline gave it once they came.
    public var hit: AppAPI.SearchHit? {
        guard let shown else { return nil }
        if let h = text?.hit, h.id == shown.id, !h.kind.isEmpty { return h }
        if let h = timeline?.at, h.id == shown.id { return h }
        return shown
    }

    /// Back to the hits.
    public func close() {
        opened += 1
        shown = nil
        text = nil
        timeline = nil
        shownFailure = nil
    }

    /// Another project's history: the hits go.
    public func switchProject(_ slug: String) {
        guard slug != project else { return }
        project = slug
        asked += 1
        result = nil
        failure = nil
        searching = false
        close()
    }

    static func words(_ e: any Error) -> String {
        if let e = e as? AppAPI.Error { return e.message }
        return e.localizedDescription
    }
}

extension Result where Failure == any Error {
    fileprivate init(awaiting body: () async throws -> Success) async {
        do { self = .success(try await body()) } catch { self = .failure(error) }
    }
}
