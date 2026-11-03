import Foundation
import Observation

/// Whether a conversation is at its end (T803, T976): a signal read from
/// the scroll geometry, never a driver of it. At the bottom the view
/// keeps to the end (its scroll anchor is the bottom); once the user
/// scrolls up it stays where they are, and counts what arrived since,
/// for Jump to latest. Scrolling back to the bottom, a jump or a prompt
/// sent follows again.
///
/// Nothing here moves the scroll. The views lay their rows out exactly
/// (no lazy stack, whose estimated heights made every computed end
/// wrong) and only SwiftUI scrolls them: its scroll anchor while
/// following, its scrollTo for a jump. SwiftUI clamps every offset it
/// sets to the content as laid out, so the view can't come to rest past
/// the end. T803 to T930 drove the offset by hand (the Glider) and kept
/// landing it there: the blank conversation over the composer.
///
/// Two things it still says to do, both a scrollTo of SwiftUI's:
/// - A jump wins over a flick still decelerating (T829): until the
///   scroll rests or the user takes hold, a scroll up doesn't stop the
///   follow but scrolls to the end again, and so does coming to rest
///   short of it.
/// - The guard (T976): at rest with the visible rect past the content's
///   end, which can't happen, the view scrolls to the end and reports it
///   (breach), so a device that still gets there leaves evidence.
public struct Follow: Equatable, Sendable {
    /// Whether the view keeps to the end as entries come.
    public private(set) var following = true
    /// Entries that came while the user was scrolled up.
    public private(set) var unseen = 0
    /// A jump under way: from it until the scroll comes to rest or the
    /// user takes hold of it.
    public private(set) var jumping = false
    /// The newest entry id the user saw at the end.
    private var seen: Int?
    /// The scroll offset at the last geometry, to tell the user's
    /// scroll up from the content growing under a follower.
    private var offset: Double?
    /// Whether the last geometry was at the end.
    public private(set) var atEnd = true
    /// Whether the scroll is at rest (its phase idle).
    private var resting = true
    /// Whether the user holds the scroll (a drag, a trackpad touch).
    private var held = false
    /// How far the last geometry had the visible rect's foot past the
    /// content's end, in points (0 when it wasn't).
    public private(set) var past = 0.0
    /// The last geometry, for a breach's report.
    public private(set) var geometry = Geometry()

    /// A scroll geometry as the views read it.
    public struct Geometry: Equatable, Sendable, Codable {
        public var offset = 0.0
        public var visible = 0.0
        public var content = 0.0
        public var inset = 0.0
        public var top = 0.0

        public init(offset: Double = 0, visible: Double = 0, content: Double = 0, inset: Double = 0, top: Double = 0) {
            self.offset = offset
            self.visible = visible
            self.content = content
            self.inset = inset
            self.top = top
        }

        /// The offset at the end: the content's foot at the view's, past
        /// the bars over it; content shorter than the view ends at its
        /// top, -top.
        public var end: Double { max(-top, content + inset - visible) }
    }

    /// How near the end counts as at it, in points.
    public static let slack = 80.0
    /// How far past the end an offset at rest may be before it is a
    /// breach, in points: a rounding's worth.
    public static let beyond = 2.0

    public init() {}

    /// The scroll view moved or resized: at the end (within slack), it
    /// follows again; scrolled up from where it was, it stops. Content
    /// that grows while the offset stays (new output, a taller bar)
    /// leaves a follower behind the end: the result says to scroll there
    /// (the view's anchor for size changes is the top, so what a user
    /// scrolled up reads stays put). During a jump a scroll up (the
    /// momentum it cut into) doesn't stop it: the result says to scroll
    /// to the end again.
    ///
    /// visible is the scroll view's whole height, the bars over its foot
    /// included (ScrollGeometry's visibleRect), and inset those bars'
    /// (contentInsets.bottom): scrolled fully down, offset + visible is
    /// content + inset (T807). top is the bars' inset over its head.
    /// True when the view must scroll to the end: a follower behind it,
    /// a jump's momentum, or at rest past it (a breach).
    @discardableResult
    public mutating func scrolled(offset: Double, visible: Double, content: Double, inset: Double = 0, top: Double = 0, newest: Int?) -> Bool {
        let g = Geometry(offset: offset, visible: visible, content: content, inset: inset, top: top)
        // Up by the user: the offset went up by more than the end did,
        // the scroll under the user's hand or still moving from it (a
        // flick's momentum, a jump's animation). A content or view that
        // shrank pulls the offset up with its end (the scroll view's
        // clamp), and a scroll of SwiftUI's at rest can land a little up
        // (one asked before the last rows were laid out, landing after:
        // 12 points, which stopped a streaming follow under load, T976):
        // neither is the user's. A wheel or a drag is a phase of its own
        // (interacting) on the iPhone and the Mac alike.
        let user = held || !resting
        let up = user && self.offset.map { was in
            let endUp = max(0, geometry.end - g.end)
            return offset < was - 1 - (atEnd || past > 0 ? endUp : 0)
        } ?? false
        defer { self.offset = offset }
        geometry = g
        atEnd = offset + visible >= content + inset - Self.slack
        past = max(0, offset - g.end)
        if atEnd {
            if resting { jumping = false }
            following = true
            unseen = 0
            if let newest { seen = newest }
        } else if up {
            // A jump's momentum: back to the end, by SwiftUI's scroll.
            if jumping { return true }
            following = false
        }
        // The content or the view changed size under a follower (new
        // output, a fold, a rotation, the keyboard): back to the end.
        // Not while the user holds the scroll, or a jump's scroll runs.
        if following, !held, !jumping, !up, offset < g.end - 1 { return true }
        return breach
    }

    /// The transcript's entries now (their ids; appended ones have
    /// higher ids than the rest). Following, they are seen; scrolled up,
    /// it counts the ones newer than those seen. True when following.
    @discardableResult
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

    /// Follow again: the user sent a prompt or pressed Jump to latest.
    /// The view scrolls to the end, and until the scroll comes to rest
    /// the follow holds through the momentum it cut into; at rest at the
    /// end already, there is nothing to hold.
    public mutating func jump(newest: Int?) {
        following = true
        jumping = !(atEnd && resting)
        unseen = 0
        if let newest { seen = newest }
    }

    /// The scroll's phase changed: the user's finger on it (a drag, a
    /// trackpad touch) or the scroll at rest ends a jump; a scroll that
    /// decelerates or animates doesn't. True when the view must scroll
    /// to the end: a jump that came to rest short of it (momentum
    /// carried it up, T829), or a breach.
    @discardableResult
    public mutating func moved(by user: Bool, resting: Bool) -> Bool {
        self.resting = resting
        held = user
        if user {
            jumping = false
            return false
        }
        guard resting else { return false }
        let short = jumping && !atEnd
        jumping = false
        return short || breach
    }

    /// At rest with the visible rect past the content's end: what the
    /// layout never makes, and the guard undoes and reports.
    public var breach: Bool {
        resting && past > Self.beyond
    }

    /// The state in a line, for a debug build's trace.
    public var trace: String {
        let g = geometry
        return "following=\(following) jumping=\(jumping) held=\(held) resting=\(resting) atEnd=\(atEnd) "
            + "offset=\(Int(g.offset)) end=\(Int(g.end)) content=\(Int(g.content)) visible=\(Int(g.visible)) inset=\(Int(g.inset))"
    }

    /// Jump to latest's words: with what came meanwhile, "Jump to
    /// latest · 3 new messages".
    public var label: String { Self.label(unseen: unseen) }

    static func label(unseen: Int) -> String {
        switch unseen {
        case 0: "Jump to latest"
        case 1: "Jump to latest · 1 new message"
        default: "Jump to latest · \(unseen) new messages"
        }
    }
}

/// A conversation's Follow as the views hold it (T956): the scroll's
/// bookkeeping changes on every frame of a scroll and is kept out of
/// observation; only what the views show (following, unseen) is
/// observed, and changes only when it does. Held as a value in @State,
/// each geometry change re-ran the whole conversation's body.
@MainActor
@Observable
public final class Follower {
    @ObservationIgnored private var state = Follow()
    /// Breaches reported on this screen: one is evidence enough.
    @ObservationIgnored public private(set) var breaches = 0
    /// Follow.following, observed.
    public private(set) var following = true
    /// Follow.unseen, observed.
    public private(set) var unseen = 0

    public init() {}

    /// Jump to latest's words (Follow.label).
    public var label: String { Follow.label(unseen: unseen) }

    /// The last geometry (Follow.geometry).
    public var geometry: Follow.Geometry { state.geometry }

    /// Follow.past.
    public var past: Double { state.past }

    /// Follow.breach.
    public var breach: Bool { state.breach }

    /// Follow.trace.
    public var trace: String { state.trace }

    @discardableResult
    public func scrolled(offset: Double, visible: Double, content: Double, inset: Double = 0, top: Double = 0, newest: Int?) -> Bool {
        change { $0.scrolled(offset: offset, visible: visible, content: content, inset: inset, top: top, newest: newest) }
    }

    @discardableResult
    public func moved(by user: Bool, resting: Bool) -> Bool {
        change { $0.moved(by: user, resting: resting) }
    }

    @discardableResult
    public func arrived(_ ids: some Collection<Int>) -> Bool {
        change { $0.arrived(ids) }
    }

    public func jump(newest: Int?) { change { $0.jump(newest: newest) } }

    /// Whether a breach now is the screen's first: the one to report.
    public func firstBreach() -> Bool {
        breaches += 1
        return breaches == 1
    }

    /// Applies a change to the Follow, then publishes what the views
    /// observe, each only when it differs.
    private func change<R>(_ f: (inout Follow) -> R) -> R {
        let r = f(&state)
        if following != state.following { following = state.following }
        if unseen != state.unseen { unseen = state.unseen }
        return r
    }
}
