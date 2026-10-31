import Foundation

/// A paired server's project pages (T886, docs/SPEC.md §16.17): the same
/// pages as a project of this server's, read through this server's link
/// (peer.pages-read, peer.library-list, peer.library-read), while that
/// server's project pages switch for this one is on for the project.
/// Read only: nothing here writes, there or here. Its memory is never
/// shared, nor are its designs (yet): those pages always show
/// "Not shared by <server>".
public enum PeerPages {
    /// One of a paired server's project's pages as the screens show it.
    public enum Page: String, CaseIterable, Identifiable, Sendable {
        case overview = "Overview"
        case board = "Board"
        case memory = "Memory"
        case library = "Library"
        case designs = "Designs"

        public var id: String { rawValue }

        /// Whether the page can be shared at all: memory never is, and
        /// designs aren't yet.
        public var shareable: Bool { self != .memory && self != .designs }
    }

    /// What a page has to show.
    public enum State<Value: Equatable & Sendable>: Equatable, Sendable {
        case loading
        case shared(Value)
        /// The server doesn't share it: its switch is off, its tm
        /// shares no pages, or the project isn't shared any more.
        case notShared
        case failed(String)
    }

    /// The error codes that mean "not shared": the switch is off
    /// (pages-off), the server's tm shares no pages (no-pages), the
    /// project isn't shared any more (not-shared), or this tm reads no
    /// pages (unknown-method).
    public static let notSharedCodes: Set<String> = ["pages-off", "no-pages", "not-shared", "unknown-method"]

    /// The state a read's error makes.
    public static func state<V>(_ error: any Error) -> State<V> {
        if let e = error as? AppAPI.Error, notSharedCodes.contains(e.code) { return .notShared }
        return .failed(error.localizedDescription)
    }

    /// A page's state as it starts: not shared without a tm that reads
    /// pages, or for a page that can't be shared; else loading.
    public static func initial<V>(_ page: Page, reads: Bool) -> State<V> {
        reads && page.shareable ? .loading : .notShared
    }

    /// "Not shared by studio": the empty state of a page the server
    /// doesn't share.
    public static func notShared(by server: String) -> String { "Not shared by \(server)" }

    /// What the empty state says under it.
    public static func notSharedDetail(_ page: Page, server: String) -> String {
        switch page {
        case .memory: return "A paired server's memory never crosses."
        case .designs: return "Designs stay on \(server) for now."
        default: return "On \(server), tm peer pages <this server> <project> on shares the board, overview and library."
        }
    }

    /// The server's name as its project's pages say it: its display
    /// name, else its alias.
    public static func server(_ peer: AppAPI.Peer) -> String { peer.name.isEmpty ? peer.alias : peer.name }

    /// The statuses in motion, as the overview counts them.
    public static let inMotion: Set<String> = ["started", "blocked", "review"]

    /// The board's on-deck tasks, in board order.
    public static func onDeck(_ p: AppAPI.PeerPages) -> [AppAPI.Task] { p.tasks.filter { $0.status == "ready" } }

    /// The board's backlog (open tasks), in board order.
    public static func backlog(_ p: AppAPI.PeerPages) -> [AppAPI.Task] { p.tasks.filter { $0.status == "open" } }

    /// The board's done tasks, in board order.
    public static func done(_ p: AppAPI.PeerPages) -> [AppAPI.Task] { p.tasks.filter { $0.status == "done" } }

    /// The board's tasks in motion, in board order.
    public static func moving(_ p: AppAPI.PeerPages) -> [AppAPI.Task] { p.tasks.filter { inMotion.contains($0.status) } }

    /// The stat tiles' counts and words, as a project of this server's
    /// shows them; nil counts while not shared (the tiles show –).
    public static func stats(_ p: AppAPI.PeerPages?) -> [(Int?, String)] {
        guard let p else { return [(nil, "on deck"), (nil, "backlog"), (nil, "done")] }
        return [(onDeck(p).count, "on deck"), (backlog(p).count, "backlog"), (done(p).count, "done")]
    }

    /// Reads a library file of a paired server's project, as
    /// LibraryPage.read reads one of this server's.
    public static func read(_ fetch: @Sendable (AppAPI.PeerLibraryReadParams) async throws -> AppAPI.LibraryChunk,
                            alias: String, project: String, file f: AppAPI.LibraryFile) async -> FileOutcome {
        await LibraryPage.read({ p in
            try await fetch(AppAPI.PeerLibraryReadParams(alias: alias, project: project, id: p.id, name: p.name, offset: p.offset))
        }, project: project, file: f)
    }
}
