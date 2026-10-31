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
/// at once, and again each frame the end moves, until it holds.
public struct Glide: Equatable, Sendable {
    /// The scroll offset now.
    public private(set) var offset: Double
    /// Its speed, points per second (positive: down, toward the end).
    public private(set) var velocity = 0.0
    /// Reduce Motion's: no spring, straight to the end.
    public let instant: Bool
    /// The end at the last frame, for an instant glide to see it hold.
    private var last: Double?

    /// The spring's stiffness (its angular frequency, per second): about
    /// 0.4 s to land.
    public static let omega = 15.0
    /// How many screens up a glide starts from, at most.
    public static let lead = 2.0
    /// Nearer than this it has landed: the last points are no motion
    /// to see.
    static let near = 3.0
    /// Seconds after which it lands wherever it is: a still end it has
    /// reached by then; one that keeps moving (output streaming in faster
    /// than the spring settles) would keep it chasing, and the follower
    /// takes over from there, as it does for each entry that comes.
    public static let longest = 0.6
    /// Seconds it has run.
    private var elapsed = 0.0

    /// A glide from offset, the end at target, in a view visible tall.
    public init(offset: Double, target: Double, visible: Double, instant: Bool = false) {
        self.instant = instant
        self.offset = instant ? target : max(offset, target - Self.lead * max(visible, 1))
        if instant { last = target }
    }

    /// One frame, dt seconds on, toward target (the end now). True once
    /// it has landed there: offset is then target exactly.
    public mutating func step(to target: Double, dt: Double) -> Bool {
        if instant {
            defer { last = target }
            offset = target
            return abs(target - (last ?? .infinity)) < Self.near
        }
        let dt = min(max(dt, 0), 0.1)
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
            offset = target
            velocity = 0
            return true
        }
        offset = target + nx
        velocity = nv
        return false
    }
}
