import Foundation

/// Jump to latest's scroll on the iPhone (T889): one glide, frame by
/// frame, to the end as it is at each frame. SwiftUI's animated
/// scrollTo aims once, at an end a LazyVStack only estimates: as the
/// rows it passes lay out, the end moves, and the scroll stops short of
/// it or is pulled back after (two moves, or a second tap). A glide
/// reads the end again each frame, so it lands where the end is when it
/// gets there, streamed output included.
///
/// The motion is a critically damped spring: it eases in and out, never
/// passes the end, and takes about as long for a page as for a hundred.
/// From further than lead screens up it starts that far from the end:
/// the rows between are a blur at any speed, and laying them all out on
/// the way costs frames. With Reduce Motion it is instant: at the end
/// at once.
///
/// It lands on the real end (T896): once the spring is done it stays on
/// the end, moved with it, until the end holds still for hold frames.
/// After a fast flick up the content's height still counts estimates for
/// the rows near the end, and those rows laying out shrink it after the
/// spring is done. An offset left where the old end was is then past
/// the content, over the empty background, and nothing scrolls it back.
/// The view clamps it between frames too (clamp(to:)): the rows its
/// frame lays out can pull the end up under it before the frame draws.
public struct Glide: Equatable, Sendable {
    /// The scroll offset now.
    public private(set) var offset: Double
    /// Its speed, points per second (positive: down, toward the end).
    public private(set) var velocity = 0.0
    /// Reduce Motion's: no spring, straight to the end.
    public let instant: Bool
    /// Whether it has landed: on the end, and the end held (step, then
    /// clamp, say for the frame).
    public private(set) var landed = false
    /// The end at the last frame, to see it hold.
    private var last: Double?
    /// Whether the spring is done: it keeps to the end until that holds.
    private var settling = false
    /// Frames in a row the end held while settling.
    private var held = 0
    /// Seconds it has been settling.
    private var settled = 0.0

    /// The spring's stiffness (its angular frequency, per second): about
    /// 0.4 s to land.
    public static let omega = 15.0
    /// How many screens up a glide starts from, at most.
    public static let lead = 2.0
    /// Nearer than this the spring is done: the last points are no
    /// motion to see.
    static let near = 3.0
    /// Seconds after which the spring is done wherever it is: a still
    /// end it has reached by then; one that keeps moving (output
    /// streaming in faster than the spring settles) would keep it
    /// chasing, and the follower takes over from there, as it does for
    /// each entry that comes.
    public static let longest = 0.6
    /// Frames in a row the end must hold, the glide on it, to have
    /// landed: rows laying out move it for a frame or two after the
    /// spring is done.
    public static let hold = 3
    /// Seconds it settles at most: an end that never holds (output
    /// streaming in each frame) is the follower's from there.
    public static let settleLongest = 0.4
    /// How far the end may move between frames and still hold.
    static let still = 0.5
    /// Seconds it has run.
    private var elapsed = 0.0

    /// A glide from offset, the end at target, in a view visible tall.
    public init(offset: Double, target: Double, visible: Double, instant: Bool = false) {
        self.instant = instant
        self.offset = instant ? target : min(max(offset, target - Self.lead * max(visible, 1)), target)
        if instant {
            settling = true
            last = target
        }
    }

    /// One frame, dt seconds on, toward target (the end now). True once
    /// it has landed there (landed): offset is then target exactly, and
    /// the end held still for hold frames.
    public mutating func step(to target: Double, dt: Double) -> Bool {
        let dt = min(max(dt, 0), 0.1)
        elapsed += dt
        if settling { return settle(target, dt: dt) }
        let x = offset - target
        let e = exp(-Self.omega * dt)
        let k = velocity + Self.omega * x
        var nx = (x + k * dt) * e
        var nv = (velocity - Self.omega * k * dt) * e
        // Never past the end (a target that moved up under it, content
        // that shrank): it stops there, rather than bounce.
        if nx > 0 {
            nx = 0
            nv = 0
        }
        if abs(nx) < Self.near || elapsed >= Self.longest {
            settling = true
            last = nil
            return settle(target, dt: 0)
        }
        offset = target + nx
        velocity = nv
        return false
    }

    /// The end as the frame's layout left it, before the frame draws: an
    /// offset past it goes back onto it (rows that laid out shorter than
    /// estimated pulled it up). True when it moved the offset; the end
    /// then has not held, and it has not landed, unless it has settled
    /// for settleLongest already.
    @discardableResult
    public mutating func clamp(to end: Double) -> Bool {
        guard offset > end else { return false }
        offset = end
        velocity = 0
        if settling {
            held = 0
            last = end
            landed = settled >= Self.settleLongest
        }
        return true
    }

    /// The spring is done: on the end, wherever it moves, until it holds.
    private mutating func settle(_ target: Double, dt: Double) -> Bool {
        settled += dt
        offset = target
        velocity = 0
        if let last, abs(target - last) < Self.still { held += 1 } else { held = 0 }
        last = target
        landed = held >= Self.hold || settled >= Self.settleLongest
        return landed
    }
}
