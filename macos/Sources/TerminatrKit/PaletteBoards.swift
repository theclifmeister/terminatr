import Foundation

/// What the command palette knows of paired servers' boards: the open
/// tasks of each shared project whose pages switch is on, read in the
/// background (peer.pages-read) when the palette opens and now and then
/// while it stays open, never per keystroke. Searching reads only this, so
/// a slow or offline server just shows fewer rows. Keyed by server: the
/// same project and task id on two servers are two boards.
public struct PaletteBoards: Equatable, Sendable {
    public struct Key: Hashable, Sendable {
        public let alias: String
        public let project: String

        public init(alias: String, project: String) {
            self.alias = alias
            self.project = project
        }
    }

    /// Seconds before a board is read again.
    public static let readEvery: TimeInterval = 120
    /// The most boards kept, and open tasks kept of each, in board order.
    public static let maxBoards = 16
    public static let maxTasks = 200

    private struct Board: Equatable, Sendable {
        var asked: Date
        var tasks: [AppAPI.Task] = []
    }

    private var boards: [Key: Board] = [:]

    public init() {}

    /// The boards the palette may read: an up server's fresh projects with
    /// the switch on, while this tm reads pages. An older tm, either side,
    /// has none: the same as the switch being off.
    public static func readable(peers: [AppAPI.Peer], reads: Bool) -> [Key] {
        guard reads else { return [] }
        let keys = peers.filter { $0.state == "up" }.flatMap { peer in
            peer.projects.filter { $0.pages && !$0.stale }.map { Key(alias: peer.alias, project: $0.project) }
        }
        return Array(keys.prefix(maxBoards))
    }

    /// The open tasks kept of a board.
    public func tasks(_ key: Key) -> [AppAPI.Task] { boards[key]?.tasks ?? [] }

    /// The boards to read now, marked as asked so that none is read twice
    /// at once; forgets those no longer readable first.
    public mutating func due(peers: [AppAPI.Peer], reads: Bool, now: Date) -> [Key] {
        prune(peers: peers, reads: reads)
        let keys = Self.readable(peers: peers, reads: reads).filter { key in
            boards[key].map { now.timeIntervalSince($0.asked) >= Self.readEvery } ?? true
        }
        for key in keys { boards[key, default: Board(asked: now)].asked = now }
        return keys
    }

    /// A read's answer: its open tasks, cut down to what a row shows. One
    /// that lands after its board was forgotten stays out.
    public mutating func store(_ pages: AppAPI.PeerPages, for key: Key) {
        guard boards[key] != nil else { return }
        boards[key]?.tasks = pages.tasks.lazy.filter { $0.status != "done" }.prefix(Self.maxTasks).map {
            AppAPI.Task(id: $0.id, ref: $0.ref, title: $0.title, status: $0.status)
        }
    }

    /// A read that failed, or found the board no longer shared: its rows
    /// go, and it isn't read again before readEvery.
    public mutating func failed(_ key: Key) {
        boards[key]?.tasks = []
    }

    /// Forgets every board no longer readable: its server went off, the
    /// project stopped being shared, or its switch went off.
    public mutating func prune(peers: [AppAPI.Peer], reads: Bool) {
        let keep = Set(Self.readable(peers: peers, reads: reads))
        boards = boards.filter { keep.contains($0.key) }
    }
}
