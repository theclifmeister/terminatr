import Foundation

/// Whether a conversation follows its end (T803): at the bottom it keeps
/// following what the agent writes; once the user scrolls up it stays
/// where they are and counts what arrived since, for Jump to latest.
/// Scrolling back to the bottom, a jump or a prompt sent follows again.
/// The views feed it their scroll geometry, its phases and the
/// transcript's entry ids, and scroll to the end only when it says so.
///
/// A jump wins over a scroll still running (T829): tapped while a flick
/// decelerates, the momentum carries on up and SwiftUI's scroll to the
/// end loses to it. Until the scroll comes to rest, or the user takes
/// hold of it, a jump keeps following through scrolls up and has the
/// view pull back to the end.
///
/// On the iPhone a jump is a glide (T889, Glide): the view drives the
/// scroll to the end itself, frame by frame, and says when it landed.
/// Until then nothing else scrolls it: output that comes doesn't scroll
/// to the end (the glide reads the end each frame), and the layout
/// moving the offset while rows pass doesn't stop the follow or pull
/// back. The user taking hold of the scroll ends it.
///
/// The follower's moves to the end (new output, T901) are glides too,
/// instant ones (Glide's instant): scrollTo aimed at the end a
/// LazyVStack estimates and stopped short of it while output streamed.
/// The view marks them with glide(newest:) as well, so the same holds
/// while one keeps to the end.
public struct Follow: Equatable, Sendable {
    /// Whether the view keeps to the end as entries come.
    public private(set) var following = true
    /// Entries that came while the user was scrolled up.
    public private(set) var unseen = 0
    /// A jump under way: from it until the scroll comes to rest or the
    /// user takes hold of it.
    public private(set) var jumping = false
    /// A glide under way (glide(newest:)): from it until it lands or the
    /// user takes hold of the scroll.
    public private(set) var gliding = false
    /// The newest entry id the user saw at the end.
    private var seen: Int?
    /// The scroll offset at the last geometry, to tell the user's
    /// scroll up from the content growing under a follower.
    private var offset: Double?
    /// Whether the last geometry was at the end.
    private var atEnd = true
    /// Whether the scroll is at rest (its phase idle).
    private var resting = true

    /// How near the end counts as at it, in points.
    public static let slack = 80.0

    public init() {}

    /// The scroll view moved or resized: at the end (within slack), it
    /// follows again; scrolled up from where it was, it stops. Content
    /// that grows while the offset stays (new output, a taller bar)
    /// changes nothing: the follower is scrolled down to it. During a
    /// jump a scroll up (the momentum it cut into) doesn't stop it: the
    /// result says to scroll to the end again.
    ///
    /// visible is the scroll view's whole height, the bars over its foot
    /// included (ScrollGeometry's visibleRect), and inset those bars'
    /// (contentInsets.bottom): scrolled fully down, offset + visible is
    /// content + inset. Not containerSize: it leaves the insets out
    /// already, and less the inset again a view at its end read as the
    /// bars' height short of it (T807).
    @discardableResult
    public mutating func scrolled(offset: Double, visible: Double, content: Double, inset: Double = 0, newest: Int?) -> Bool {
        defer { self.offset = offset }
        atEnd = offset + visible >= content + inset - Self.slack
        if atEnd {
            // At rest there: no momentum left to fight.
            if resting { jumping = false }
            following = true
            unseen = 0
            if let newest { seen = newest }
        } else if gliding {
            // The glide's own frames, and rows laying out as it passes.
        } else if let was = self.offset, offset < was - 1 {
            if jumping { return true }
            following = false
        }
        return false
    }

    /// The transcript's entries now (their ids; appended ones have
    /// higher ids than the rest). Whether the view scrolls to its end:
    /// when following; otherwise it counts the entries newer than the
    /// ones seen.
    /// While a glide runs it says no: the glide goes to the end as it
    /// is at each frame, and a scroll of the view's own would cut it.
    public mutating func arrived(_ ids: some Collection<Int>) -> Bool {
        if following {
            seen = ids.max() ?? seen
            unseen = 0
            return !gliding
        }
        let seen = self.seen ?? Int.min
        unseen = ids.reduce(0) { $0 + ($1 > seen ? 1 : 0) }
        return false
    }

    /// Follow again: the user sent a prompt or pressed Jump to latest.
    /// The view scrolls to the end, and keeps to it until the scroll
    /// comes to rest (moved(by:)); at rest at the end already, there is
    /// nothing to keep to.
    public mutating func jump(newest: Int?) {
        following = true
        jumping = !(atEnd && resting)
        unseen = 0
        if let newest { seen = newest }
    }

    /// Jump to latest on the iPhone: follow again, by a glide the view
    /// drives to the end (Glide) and ends with landed(); it stopped any
    /// momentum first, so there is none to fight (no jumping).
    public mutating func glide(newest: Int?) {
        following = true
        jumping = false
        gliding = true
        unseen = 0
        if let newest { seen = newest }
    }

    /// The glide is over: it landed at the end, or the user took hold of
    /// the scroll (they keep what they scroll to from there).
    public mutating func landed() {
        gliding = false
    }

    /// The scroll's phase changed: the user's finger on it (a drag, a
    /// trackpad touch) or the scroll at rest ends a jump; a scroll that
    /// decelerates or animates doesn't. The finger ends a glide too.
    public mutating func moved(by user: Bool, resting: Bool) {
        self.resting = resting
        if user || resting { jumping = false }
        if user { gliding = false }
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
