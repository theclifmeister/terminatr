import Foundation

/// The phone's pairings (T852): one per server, each by the server's id
/// (the SHA-256 of its key, 64 hex digits, as a pairing code and
/// peer.list name it), with its own device key, write key, level and
/// link. The phone lists them in the order they were paired; pairing
/// again with one replaces it in its place.
public enum Pairings {
    /// ids with id added: last when it is new, in its place when the
    /// phone pairs with that server again.
    public static func adding(_ id: String, to ids: [String]) -> [String] {
        ids.contains(id) ? ids : ids + [id]
    }

    public static func removing(_ id: String, from ids: [String]) -> [String] {
        ids.filter { $0 != id }
    }

    /// A server the phone pairs with directly, as a paired server's
    /// peer.list may name it too.
    public struct Direct: Equatable, Sendable {
        public var id: String
        public var name: String

        public init(id: String, name: String) {
            self.id = id
            self.name = name
        }
    }

    /// Whether peer, seen through a server's link, is one of the
    /// servers the phone pairs with directly: then the phone shows it
    /// once, as its own server, and not again under the one that links
    /// to it. By its id; from a tm that sends none, by its name (the
    /// one it gives itself, unless the user named it otherwise there).
    public static func isDirect(_ peer: AppAPI.Peer, _ direct: [Direct]) -> Bool {
        if !peer.id.isEmpty { return direct.contains { $0.id.lowercased() == peer.id.lowercased() } }
        let name = peer.name.trimmingCharacters(in: .whitespaces)
        return !name.isEmpty && direct.contains { $0.name.caseInsensitiveCompare(name) == .orderedSame }
    }

    /// peers less those the phone pairs with directly.
    public static func shown(_ peers: [AppAPI.Peer], _ direct: [Direct]) -> [AppAPI.Peer] {
        direct.isEmpty ? peers : peers.filter { !isDirect($0, direct) }
    }
}

extension Need {
    /// Every server's needs in one list, each with its server's key, as
    /// Need.of orders one server's: what waits on an answer first, in
    /// the servers' order, then the reviews.
    public static func merge<Key>(_ each: [(Key, [Need])]) -> [(server: Key, need: Need)] {
        let all = each.flatMap { k, needs in needs.map { (server: k, need: $0) } }
        return all.filter { !$0.need.isReview } + all.filter(\.need.isReview)
    }
}

extension LinkLog {
    /// The UserDefaults key a pairing's log is kept under, by its
    /// server's id; "" is the one server's from before T852 (move).
    public static func key(_ pairing: String) -> String {
        pairing.isEmpty ? key : key + "." + pairing
    }

    public static func load(pairing: String, _ defaults: UserDefaults = .standard) -> LinkLog {
        guard let d = defaults.data(forKey: key(pairing)), let l = try? JSONDecoder().decode(LinkLog.self, from: d) else { return LinkLog() }
        return l
    }

    public func save(pairing: String, _ defaults: UserDefaults = .standard) {
        if let d = try? JSONEncoder().encode(self) { defaults.set(d, forKey: Self.key(pairing)) }
    }

    /// Moves the log kept under the old key to pairing's.
    public static func move(to pairing: String, _ defaults: UserDefaults = .standard) {
        guard !pairing.isEmpty, let d = defaults.data(forKey: key) else { return }
        defaults.set(d, forKey: key(pairing))
        defaults.removeObject(forKey: key)
    }

    public static func forget(pairing: String, _ defaults: UserDefaults = .standard) {
        defaults.removeObject(forKey: key(pairing))
    }
}
