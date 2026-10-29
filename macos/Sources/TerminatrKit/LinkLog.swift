import Foundation

/// The phone's device link drops (T859): when each one came, why, and
/// how long until the link was up again, the last few kept across
/// launches (UserDefaults) for Settings' Connection to show.
public struct LinkLog: Codable, Equatable, Sendable {
    public enum Cause: String, Codable, Sendable {
        /// Nothing came from the relay for its idle time: the network
        /// went away under the link.
        case idle
        /// The phone's own ping had no answer in time (LinkKeep.pingDeadline).
        case ping
        /// The relay closed the link (its word in detail: peer-gone,
        /// shutdown …).
        case relay
        /// The server left the room (the relay's peer-gone): it closed
        /// the link, or stopped.
        case server
        /// The server isn't in the room: asleep, offline or stopped.
        case asleep
        /// The app went to the back and its grace ran out.
        case background
        /// The network changed (Wi-Fi to cellular, back online).
        case path
        /// Any other error: its words in detail.
        case error

        public var words: String {
            switch self {
            case .idle: "No reply from the relay"
            case .ping: "Ping unanswered"
            case .relay: "Relay closed"
            case .server: "Server left"
            case .asleep: "Server asleep or offline"
            case .background: "App in the background"
            case .path: "Network changed"
            case .error: "Error"
            }
        }
    }

    public struct Drop: Codable, Equatable, Sendable, Identifiable {
        public var at: Date
        public var cause: Cause
        public var detail: String
        /// When the link was up again; nil while it is still down.
        public var back: Date?

        public var id: Date { at }

        public init(at: Date, cause: Cause, detail: String = "", back: Date? = nil) {
            self.at = at
            self.cause = cause
            self.detail = detail
            self.back = back
        }

        /// How long the link was down, once it is up again.
        public var down: TimeInterval? { back.map { $0.timeIntervalSince(at) } }
    }

    /// Newest first, at most max.
    public private(set) var drops: [Drop] = []
    public static let max = 20

    public init(drops: [Drop] = []) {
        self.drops = Array(drops.prefix(Self.max))
    }

    /// Whether the link is down since the newest drop.
    public var isDown: Bool { drops.first.map { $0.back == nil } ?? false }

    /// The link dropped at for cause. A drop while it is still down from
    /// the last one only says why it stays down: the first drop's time
    /// counts, a later redial failing is no new drop.
    public mutating func dropped(at: Date, _ cause: Cause, detail: String = "") {
        if isDown { return }
        drops.insert(Drop(at: at, cause: cause, detail: detail), at: 0)
        if drops.count > Self.max { drops.removeLast(drops.count - Self.max) }
    }

    /// The link is up at at: the open drop, if any, ends.
    public mutating func up(at: Date) {
        guard isDown else { return }
        drops[0].back = at
    }

    /// The drops of the last day, by cause, for a line such as "3 today:
    /// 2 Ping unanswered, 1 Network changed".
    public func tally(since: Date) -> [(Cause, Int)] {
        var n: [Cause: Int] = [:]
        for d in drops where d.at >= since { n[d.cause, default: 0] += 1 }
        return n.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key.rawValue < $1.key.rawValue }
    }

    /// The UserDefaults key the phone keeps it under.
    public static let key = "linkLog"

    public static func load(_ defaults: UserDefaults = .standard) -> LinkLog {
        guard let d = defaults.data(forKey: key), let l = try? JSONDecoder().decode(LinkLog.self, from: d) else { return LinkLog() }
        return l
    }

    public func save(_ defaults: UserDefaults = .standard) {
        if let d = try? JSONEncoder().encode(self) { defaults.set(d, forKey: Self.key) }
    }
}

/// When the phone's link redials (T859): the backoff between dials,
/// which a network change resets, and how long it stays up behind
/// another app. (Its ping, which finds a dead link in seconds, is
/// TerminatrLink's: RelayWire.pingEvery.)
public enum LinkKeep {
    /// The wait in seconds before the next dial after fails failures in
    /// a row: none before the first, then 1 s doubling to 30 s.
    public static func backoff(_ fails: Int) -> Int {
        fails <= 0 ? 0 : min(30, 1 << min(fails - 1, 5))
    }

    /// The network as NWPathMonitor last said: whether it is usable, and
    /// the interface it goes out on ("wifi", "cellular" …).
    public struct Path: Equatable, Sendable {
        public var online: Bool
        public var via: String
        public init(online: Bool, via: String) {
            self.online = online
            self.via = via
        }
    }

    /// What a network change does to the link.
    public enum Act: Equatable, Sendable {
        case nothing
        /// Ping the relay now: the link is up, but may not have survived.
        case check
        /// Dial now, without the backoff's wait.
        case redial
        /// Drop the link and dial at once: it went out on an interface
        /// that's gone (Wi-Fi to cellular).
        case reconnect
    }

    /// What the link does when the network goes from old to new (old nil:
    /// the monitor's first word, which changes nothing); up says it is up.
    public static func onPath(from old: Path?, to new: Path, up: Bool) -> Act {
        guard let old, new.online, old != new else { return .nothing }
        if !old.online { return up ? .check : .redial }
        if old.via != new.via { return up ? .reconnect : .redial }
        return up ? .check : .nothing
    }

    /// How long the link stays up once the app goes to the back: a quick
    /// switch or a glance at another app keeps it; iOS gives a background
    /// task about 30 s.
    public static let backgroundGrace: TimeInterval = 25
}
