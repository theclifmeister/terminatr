import Foundation

/// Whether a conversation follows its end (T803): at the bottom it keeps
/// following what the agent writes; once the user scrolls up it stays
/// where they are and counts what arrived since, for Jump to latest.
/// Scrolling back to the bottom, a jump or a prompt sent follows again.
/// The views feed it their scroll geometry and the transcript's entry
/// ids, and scroll to the end only when it says so.
public struct Follow: Equatable, Sendable {
    /// Whether the view keeps to the end as entries come.
    public private(set) var following = true
    /// Entries that came while the user was scrolled up.
    public private(set) var unseen = 0
    /// The newest entry id the user saw at the end.
    private var seen: Int?
    /// The scroll offset at the last geometry, to tell the user's
    /// scroll up from the content growing under a follower.
    private var offset: Double?

    /// How near the end counts as at it, in points.
    public static let slack = 80.0

    public init() {}

    /// The scroll view moved or resized: at the end (within slack), it
    /// follows again; scrolled up from where it was, it stops. Content
    /// that grows while the offset stays (new output, a taller bar)
    /// changes nothing: the follower is scrolled down to it.
    ///
    /// visible is the scroll view's whole height, the bars over its foot
    /// included (ScrollGeometry's visibleRect), and inset those bars'
    /// (contentInsets.bottom): scrolled fully down, offset + visible is
    /// content + inset. Not containerSize: it leaves the insets out
    /// already, and less the inset again a view at its end read as the
    /// bars' height short of it (T807).
    public mutating func scrolled(offset: Double, visible: Double, content: Double, inset: Double = 0, newest: Int?) {
        defer { self.offset = offset }
        if offset + visible >= content + inset - Self.slack {
            follow(newest: newest)
        } else if let was = self.offset, offset < was - 1 {
            following = false
        }
    }

    /// The transcript's entries now (their ids; appended ones have
    /// higher ids than the rest). Whether the view scrolls to its end:
    /// when following; otherwise it counts the entries newer than the
    /// ones seen.
    public mutating func arrived(_ ids: some Collection<Int>) -> Bool {
        if following {
            seen = ids.max() ?? seen
            unseen = 0
            return true
        }
        let seen = self.seen ?? Int.min
        unseen = ids.reduce(0) { $0 + ($1 > seen ? 1 : 0) }
        return false
    }

    /// Follow again: the user sent a prompt, pressed Jump to latest, or
    /// scrolled back to the end. The view scrolls to the end.
    public mutating func follow(newest: Int?) {
        following = true
        unseen = 0
        if let newest { seen = newest }
    }

    /// Jump to latest's words: with what came meanwhile, "Jump to
    /// latest · 3 new messages".
    public var label: String {
        switch unseen {
        case 0: "Jump to latest"
        case 1: "Jump to latest · 1 new message"
        default: "Jump to latest · \(unseen) new messages"
        }
    }
}
