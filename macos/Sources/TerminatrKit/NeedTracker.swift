import Foundation

/// Which needs are new since the last look and which went: a need is
/// notified once, when it appears, and its notification withdrawn when
/// it goes. The first look only learns what is there, so starting the
/// app doesn't replay every need as a notification; the menu bar lists
/// them.
public struct NeedTracker: Sendable {
    private var seen: Set<String>?

    public init() {}

    /// The needs in needs that weren't there last time, and the ids of
    /// those that were and aren't now.
    public mutating func update(_ needs: [Need]) -> (fresh: [Need], gone: [String]) {
        let ids = Set(needs.map(\.id))
        defer { seen = ids }
        guard let seen else { return ([], []) }
        return (needs.filter { !seen.contains($0.id) }, seen.subtracting(ids).sorted())
    }

    /// Forgets what was seen: the next update learns again (a new
    /// bridge, after the old one ended).
    public mutating func reset() {
        seen = nil
    }
}
