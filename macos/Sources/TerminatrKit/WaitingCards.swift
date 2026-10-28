import Foundation
import Observation

/// The cards that wait on the user, shown now in any window, in the
/// order they came: the newest one not yet seen leads, and its edge
/// runs (T805). A card is seen once it has led for a few laps in front
/// of the user, or the pointer has been on it.
@MainActor @Observable
public final class WaitingCards {
    public static let shared = WaitingCards()

    public init() {}

    /// A card's arrival: cards shown within a second of each other came
    /// together (a window opening on them), and the first of those leads.
    private struct Arrival {
        var batch: Int
        var order: Int
    }

    private var arrived: [String: Arrival] = [:]
    /// How many views show each card now.
    private var shown: [String: Int] = [:]
    private var seen: Set<String> = []
    private var batch = 0
    private var order = 0
    private var last = Date.distantPast

    /// The card whose edge runs.
    public private(set) var lead: String?

    public func show(_ key: String, now: Date = Date()) {
        if arrived[key] == nil {
            if now.timeIntervalSince(last) > 1 { batch += 1 }
            last = now
            order += 1
            arrived[key] = Arrival(batch: batch, order: order)
        }
        shown[key, default: 0] += 1
        pick()
    }

    public func hide(_ key: String) {
        guard let n = shown[key] else { return }
        shown[key] = n > 1 ? n - 1 : nil
        // Cards that went for good are forgotten, a few at a time.
        if arrived.count > 200 {
            for k in arrived.keys where shown[k] == nil {
                arrived[k] = nil
                seen.remove(k)
            }
        }
        pick()
    }

    public func see(_ key: String) {
        seen.insert(key)
        pick()
    }

    private func pick() {
        let next = shown.keys
            .filter { !seen.contains($0) }
            .compactMap { k in arrived[k].map { (k, $0) } }
            .max { a, b in a.1.batch != b.1.batch ? a.1.batch < b.1.batch : a.1.order > b.1.order }?
            .0
        if next != lead { lead = next }
    }
}
