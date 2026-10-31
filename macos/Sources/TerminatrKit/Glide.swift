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
/// It starts and lands on the real end (T896). After a fast flick up the
/// LazyVStack counts the rows near the end at its estimates, and lays
/// them out only once they are in view, a frame on: in a coordinator's
/// conversation (tm's one-line prompts between tall cards) the end then
/// moves up by more than the lead, and a start lead screens above the
/// estimate is past the real end, over the empty background. So a start
/// that leads is aimed (covered): the view shows what it showed, the
/// offset is on the start, and each frame aims again at the end that
/// frame has, until it holds (or aimFrames); then the spring runs. And
/// once the spring is done it stays on the end, moved with it, until the
/// end holds for hold frames: an offset left where the old end was
/// would be past the content, and nothing scrolls it back.
public struct Glide: Equatable, Sendable {
    /// The scroll offset now.
    public private(set) var offset: Double
    /// Its speed, points per second (positive: down, toward the end).
    public private(set) var velocity = 0.0
    /// Reduce Motion's: no spring, straight to the end.
    public let instant: Bool
    /// Whether it is aiming: the view keeps showing what it showed (a
    /// still image of it over the scroll) while the start lays out.
    public private(set) var covered = false
    /// Whether it has landed: on the end, and the end held.
    public private(set) var landed = false
    /// Where it was tapped from, and the view's height: a start aimed
    /// again is from these.
    private let from: Double
    private let visible: Double
    /// The end the start was aimed at, and the frames spent aiming.
    private var aimed: Double
    private var aims = 0
    /// The end at the last frame, to see it hold.
    private var last: Double?
    /// Whether the spring is done: it keeps to the end until that holds.
    private var settling = false
    /// Frames in a row the end held while settling.
    private var held = 0
    /// Seconds it has been settling.
    private var settled = 0.0
    /// Seconds the spring has run.
    private var elapsed = 0.0

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
    /// Frames it aims a start at most: the rows there lay out in one or
    /// two. It aims two at least: the end at the first frame may not have
    /// the start's rows yet, and holds only once a second frame agrees.
    public static let aimFrames = 4
    /// Frames in a row the end must hold, the glide on it, to have
    /// landed: rows laying out move it for a frame or two after the
    /// spring is done.
    public static let hold = 3
    /// Seconds it settles at most: an end that never holds (output
    /// streaming in each frame) is the follower's from there.
    public static let settleLongest = 0.4
    /// How far the end may move between frames and still hold.
    static let still = 0.5

    /// A glide from offset, the end at target, in a view visible tall.
    /// A start that leads is covered until it is aimed.
    public init(offset: Double, target: Double, visible: Double, instant: Bool = false) {
        self.instant = instant
        from = offset
        self.visible = max(visible, 1)
        aimed = target
        if instant {
            self.offset = target
            settling = true
            last = target
            return
        }
        self.offset = Self.start(from: offset, target: target, visible: self.visible)
        covered = self.offset > offset + Self.near
    }

    /// Where a glide to target starts: lead screens above it at most,
    /// and never past it.
    static func start(from: Double, target: Double, visible: Double) -> Double {
        min(max(from, target - lead * visible), target)
    }

    /// One frame, dt seconds on, toward target (the end now). True once
    /// it has landed there (landed): offset is then target exactly, and
    /// the end held still for hold frames.
    public mutating func step(to target: Double, dt: Double) -> Bool {
        let dt = min(max(dt, 0), 0.1)
        if covered {
            aims += 1
            if (aims > 1 && abs(target - aimed) < Self.still) || aims >= Self.aimFrames { covered = false }
            aimed = target
            offset = Self.start(from: from, target: target, visible: visible)
            return false
        }
        if settling { return settle(target, dt: dt) }
        elapsed += dt
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

    /// More output came (T901): the end moves again, so a glide that is
    /// on it stays there until the end holds anew, as long again at most.
    /// A spring still running is left as it is: it reads the end each
    /// frame anyway.
    public mutating func renew() {
        guard settling else { return }
        held = 0
        settled = 0
        landed = false
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
