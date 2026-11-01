import Foundation
import Testing
@testable import TerminatrKit

// The conversation follows its end until the user scrolls up (T803).

/// A view 500 tall over content of the given height, scrolled to offset.
private func at(_ f: inout Follow, _ offset: Double, content: Double, newest: Int?) {
    f.scrolled(offset: offset, visible: 500, content: content, newest: newest)
}

@Test func followsAtTheEnd() {
    var f = Follow()
    #expect(f.following)
    // Output comes: the follower goes to the end, counting nothing.
    let s1 = f.arrived([0, 1, 2])
    #expect(s1)
    #expect(f.unseen == 0)
    // The content grows under it, its offset unchanged: still following.
    at(&f, 500, content: 1000, newest: 2)
    at(&f, 500, content: 1400, newest: 3)
    #expect(f.following)
    let s2 = f.arrived([0, 1, 2, 3])
    #expect(s2)
}

@Test func scrolledUpStaysAndCounts() {
    var f = Follow()
    _ = f.arrived([0, 1, 2])
    at(&f, 500, content: 1000, newest: 2)
    // The user scrolls up: it stops following.
    at(&f, 200, content: 1000, newest: 2)
    #expect(!f.following)
    #expect(f.label == "Jump to latest")
    // Output comes: no scroll, and what came is counted.
    let s3 = f.arrived([0, 1, 2, 3])
    #expect(!s3)
    #expect(f.unseen == 1)
    #expect(f.label == "Jump to latest · 1 new message")
    // A growing last entry or a taller bar moves nothing.
    at(&f, 200, content: 1300, newest: 3)
    #expect(!f.following)
    let s4 = f.arrived([0, 1, 2, 3, 4, 5])
    #expect(!s4)
    #expect(f.label == "Jump to latest · 3 new messages")
    // Scrolling down, short of the end, still doesn't follow.
    at(&f, 600, content: 1300, newest: 5)
    #expect(!f.following)
    // Earlier entries paged in front are not new.
    let s5 = f.arrived([-2, -1, 0, 1, 2, 3, 4, 5])
    #expect(!s5)
    #expect(f.unseen == 3)
    // Back at the end (within the slack): it follows again.
    at(&f, 1300 - 500 - 40, content: 1300, newest: 5)
    #expect(f.following)
    #expect(f.unseen == 0)
    let s6 = f.arrived([0, 1, 2, 3, 4, 5, 6])
    #expect(s6)
}

/// A prompt sent, or Jump to latest, follows again; what was seen then
/// counts as seen when the user scrolls up after.
@Test func jumpFollowsAgain() {
    var f = Follow()
    _ = f.arrived([0, 1])
    at(&f, 500, content: 1000, newest: 1)
    at(&f, 100, content: 1000, newest: 1)
    _ = f.arrived([0, 1, 2, 3])
    #expect(f.unseen == 2)
    f.jump(newest: 3)
    #expect(f.following)
    #expect(f.unseen == 0)
    // The jump's scroll ends at the end, at rest.
    at(&f, 1100, content: 1600, newest: 3)
    f.moved(by: false, resting: true)
    #expect(!f.jumping)
    at(&f, 100, content: 1600, newest: 3)
    at(&f, 50, content: 1600, newest: 3)
    #expect(!f.following)
    let s7 = f.arrived([0, 1, 2, 3, 4])
    #expect(!s7)
    #expect(f.unseen == 1)
}

/// The bars at the foot (composer, Working bar, cards) are a bottom
/// inset: scrolled fully down, the view's whole height reaches past the
/// content by it. The numbers are a Mac window's at its end (T807).
@Test func endBehindTheBars() {
    var f = Follow()
    _ = f.arrived([0, 1])
    // 600 tall, 166 of it the bars, over 2269 of content: the end.
    f.scrolled(offset: 1835, visible: 600, content: 2269, inset: 166, newest: 1)
    #expect(f.following)
    // Scrolled up by more than the slack, though the bars' height
    // would cover it: no longer at the end.
    f.scrolled(offset: 1735, visible: 600, content: 2269, inset: 166, newest: 1)
    #expect(!f.following)
    // Back down by hand: at the end, and following again.
    f.scrolled(offset: 1835, visible: 600, content: 2269, inset: 166, newest: 1)
    #expect(f.following)
    // A taller bar (an open card) under a follower: still following.
    f.scrolled(offset: 1835, visible: 600, content: 2269, inset: 420, newest: 1)
    #expect(f.following)
}

/// Jump to latest tapped while a flick still decelerates up (T829): the
/// momentum's scrolls up don't stop the follow, each says to pull back
/// to the end, until the scroll comes to rest. A drag after it is the
/// user's own again.
@Test func jumpWinsOverMomentum() {
    var f = Follow()
    _ = f.arrived([0, 1])
    at(&f, 9500, content: 10000, newest: 1)
    // A flick: the finger on it, then let go, decelerating up.
    f.moved(by: true, resting: false)
    f.moved(by: false, resting: false)
    at(&f, 7900, content: 10000, newest: 1)
    #expect(!f.following)
    f.jump(newest: 1)
    #expect(f.following && f.jumping)
    // The momentum carries on up: still following, pulled back.
    let pull = f.scrolled(offset: 7890, visible: 500, content: 10000, newest: 1)
    #expect(pull)
    #expect(f.following)
    // Pulled to the end; the momentum ebbs; at rest the jump is over.
    let atEnd = f.scrolled(offset: 9500, visible: 500, content: 10000, newest: 1)
    let ebb = f.scrolled(offset: 9497, visible: 500, content: 10000, newest: 1)
    #expect(!atEnd && !ebb)
    #expect(f.jumping)
    f.moved(by: false, resting: true)
    #expect(!f.jumping && f.following)
    // The user's own scroll up after it stops following.
    at(&f, 9000, content: 10000, newest: 1)
    #expect(!f.following)
}

/// The user taking hold of the scroll mid-jump ends it: their scroll up
/// stops the follow, and nothing pulls them back.
@Test func grabEndsAJump() {
    var f = Follow()
    _ = f.arrived([0, 1])
    at(&f, 500, content: 2000, newest: 1)
    f.jump(newest: 1)
    f.moved(by: true, resting: false)
    #expect(!f.jumping)
    let pull = f.scrolled(offset: 400, visible: 500, content: 2000, newest: 1)
    #expect(!pull)
    #expect(!f.following)
}

/// A prompt sent at the end, at rest: no scroll comes of it, so no
/// phase ends the jump; it ends at once, and the user's next scroll up
/// (a mouse wheel's, which may show no phase) stops the follow.
@Test func jumpAtRestAtTheEnd() {
    var f = Follow()
    _ = f.arrived([0, 1])
    at(&f, 1500, content: 2000, newest: 1)
    f.jump(newest: 1)
    #expect(!f.jumping)
    at(&f, 1300, content: 2000, newest: 1)
    #expect(!f.following)
    // Mid-flick at the end, though, a jump holds until the scroll rests.
    f.moved(by: false, resting: false)
    at(&f, 1500, content: 2000, newest: 1)
    f.jump(newest: 1)
    #expect(f.jumping)
    let pull = f.scrolled(offset: 1400, visible: 500, content: 2000, newest: 1)
    #expect(pull)
    f.moved(by: false, resting: true)
    #expect(!f.jumping)
}

/// Jump to latest on the iPhone glides (T889): output that comes while
/// it runs doesn't scroll (the glide goes to the end as it is), and the
/// layout moving the offset up as rows pass neither stops the follow
/// nor pulls back. Landed, the follower follows as before.
@Test func glideHoldsTheFollow() {
    var f = Follow()
    _ = f.arrived([0, 1])
    at(&f, 9500, content: 10000, newest: 1)
    at(&f, 4000, content: 10000, newest: 1)
    #expect(!f.following)
    _ = f.arrived([0, 1, 2])
    #expect(f.unseen == 1)
    f.glide(newest: 2)
    #expect(f.following && f.gliding && !f.jumping)
    #expect(f.unseen == 0)
    // On the way: rows lay out shorter than estimated, the offset goes up.
    let pull = f.scrolled(offset: 3900, visible: 500, content: 9800, newest: 2)
    #expect(!pull)
    #expect(f.following)
    // Output comes: no scroll of the view's own, nothing counted.
    let s1 = f.arrived([0, 1, 2, 3])
    #expect(!s1)
    #expect(f.unseen == 0)
    f.scrolled(offset: 9600, visible: 500, content: 10100, newest: 3)
    f.landed()
    #expect(!f.gliding && f.following)
    let s2 = f.arrived([0, 1, 2, 3, 4])
    #expect(s2)
    // The user's own scroll up after it stops following.
    at(&f, 9000, content: 10100, newest: 4)
    #expect(!f.following)
}

/// The user's finger on the scroll ends a glide: they keep what they
/// scroll to from there.
@Test func grabEndsAGlide() {
    var f = Follow()
    _ = f.arrived([0, 1])
    at(&f, 500, content: 2000, newest: 1)
    f.glide(newest: 1)
    f.moved(by: true, resting: false)
    #expect(!f.gliding)
    at(&f, 400, content: 2000, newest: 1)
    #expect(!f.following)
}

/// A glide frame by frame to a fixed end: it only moves down, never
/// past the end, and lands on it exactly in about 0.4 s.
@Test func glideLandsOnTheEnd() {
    var g = Glide(offset: 0, target: 1500, visible: 800)
    var was = g.offset, t = 0.0, landed = false
    while !landed, t < 3 {
        landed = g.step(to: 1500, dt: 1 / 60)
        t += 1 / 60
        #expect(g.offset >= was && g.offset <= 1500)
        was = g.offset
    }
    #expect(landed && g.offset == 1500)
    #expect(t > 0.25 && t < 0.8)
}

/// Far up, a glide starts lead screens from the end; near, where it is.
@Test func glideLeadsIn() {
    #expect(Glide(offset: 0, target: 50000, visible: 800).offset == 50000 - Glide.lead * 800)
    #expect(Glide(offset: 900, target: 1500, visible: 800).offset == 900)
}

/// An end that moves on the way (rows laying out taller or shorter than
/// estimated, output streaming in): the glide lands where the end is
/// when it gets there, and an end that moved up under it stops it there
/// rather than past it.
@Test func glideFollowsAMovingEnd() {
    var g = Glide(offset: 0, target: 1500, visible: 800)
    var end = 1500.0, landed = false, n = 0
    while !landed, n < 300 {
        // The end grows each frame for half a second, then holds.
        if n < 30 { end += 20 }
        landed = g.step(to: end, dt: 1 / 60)
        #expect(g.offset <= end)
        n += 1
    }
    #expect(landed && g.offset == end && end == 2100)
    var h = Glide(offset: 1000, target: 1500, visible: 800)
    _ = h.step(to: 1500, dt: 1 / 60)
    _ = h.step(to: 1500, dt: 1 / 60)
    // The content shrinks to above where it is: there, and landed.
    let at = h.offset - 10
    let l = h.step(to: at, dt: 1 / 60)
    #expect(h.offset <= at)
    #expect(l || h.offset == at)
}

/// Output streaming in faster than the spring settles: the glide still
/// lands (the follower takes over from there), rather than chase the end
/// it never quite reaches.
@Test func glideLandsWhileOutputStreams() {
    var g = Glide(offset: 0, target: 1500, visible: 800)
    var end = 1500.0, landed = false, n = 0
    while !landed, n < 600 {
        // 100 points more every 350 ms, for ever.
        if n > 0, n % 21 == 0 { end += 100 }
        landed = g.step(to: end, dt: 1 / 60)
        n += 1
    }
    #expect(landed && g.offset == end)
    #expect(n < 120)
}

/// Reduce Motion: at the end at once, and again each frame it moves,
/// landed once it holds for Glide.hold frames.
@Test func glideInstant() {
    var g = Glide(offset: 0, target: 1500, visible: 800, instant: true)
    #expect(g.offset == 1500)
    let moved = g.step(to: 1600, dt: 1 / 60)
    #expect(!moved && g.offset == 1600)
    var n = 0
    while !g.step(to: 1600, dt: 1 / 60) { n += 1 }
    #expect(n + 1 == Glide.hold && g.offset == 1600)
}

/// A glide frame by frame to end(n), the end the view has at frame n:
/// the frames it took, and how far past the end it ever was, covered
/// frames left out (the view shows its still image then).
private func run(_ g: inout Glide, frames: Int = 600, end: (Int) -> Double) -> (n: Int, past: Double, covered: Int) {
    var n = 0, past = -Double.infinity, covered = 0
    while !g.landed, n < frames {
        let e = end(n)
        _ = g.step(to: e, dt: 1 / 60)
        if g.covered { covered += 1 } else { past = max(past, g.offset - e) }
        n += 1
    }
    return (n, past, covered)
}

/// After a fast flick up the rows near the end lay out a frame after
/// they come into view, far shorter than estimated (a coordinator's
/// one-line tm prompts, T896): the end moves up by more than the lead.
/// The start is aimed under the cover until the end holds, then the
/// spring runs from lead screens above the real end, never past it,
/// and lands on it.
@Test func glideAimsItsStartAtTheRealEnd() {
    var g = Glide(offset: 5000, target: 15600, visible: 874)
    #expect(g.covered && g.offset == 15600 - Glide.lead * 874)
    // Laid out, the start's rows move the end up by 2300, then it holds.
    let r = run(&g) { n in n == 0 ? 15600 : 13300 }
    #expect(g.landed && g.offset == 13300)
    #expect(r.covered == 2 && r.past <= 0)
    // Uncovered on the start for the real end.
    var h = Glide(offset: 5000, target: 15600, visible: 874)
    _ = h.step(to: 13300, dt: 1 / 60)
    #expect(h.covered)
    _ = h.step(to: 13300, dt: 1 / 60)
    #expect(!h.covered && h.offset == 13300 - Glide.lead * 874)
}

/// The Mac's aim (T895, aimsUp off: no still image there): the end
/// moving up as the start's rows lay out leaves the start where it was,
/// nearer the end; nothing moves up, and it lands on the end.
@Test func glideAimedDownOnlyNeverMovesUp() {
    var g = Glide(offset: 5000, target: 10000, visible: 500, aimsUp: false)
    #expect(g.covered && g.offset == 9000)
    var last = g.offset, n = 0
    while !g.landed, n < 600 {
        _ = g.step(to: n == 0 ? 10000 : 9950, dt: 1 / 60)
        #expect(g.offset >= last - 0.001 && g.offset <= 9950 + 0.001)
        last = g.offset
        n += 1
    }
    #expect(g.landed && g.offset == 9950)
}

/// Aimed down only, an end that moved up past the start puts it on the
/// end: never past it, and still never up from where it was.
@Test func glideAimedDownOnlyPastTheEndTakesTheEnd() {
    var g = Glide(offset: 5000, target: 15600, visible: 874, aimsUp: false)
    _ = g.step(to: 13300, dt: 1 / 60)
    #expect(g.offset == 13300)
    _ = g.step(to: 13300, dt: 1 / 60)
    #expect(!g.covered && g.offset == 13300)
}

/// SwiftUI moved the offset between frames (rows above changed height,
/// T895): the glide goes on from where it is, and still lands on the
/// end.
@Test func glideGoesOnFromALayoutShift() {
    var g = Glide(offset: 9000, target: 10000, visible: 500)
    _ = g.step(to: 10000, dt: 1 / 60)
    let was = g.offset
    g.moved(by: -117)
    #expect(g.offset == was - 117)
    var n = 0
    while !g.landed, n < 600 {
        _ = g.step(to: 9883, dt: 1 / 60)
        n += 1
    }
    #expect(g.landed && g.offset == 9883)
}

/// An end that never holds under the cover (output streaming in each
/// frame): it aims at most aimFrames, then glides.
@Test func glideAimsAtMostAimFrames() {
    var g = Glide(offset: 0, target: 20000, visible: 800)
    var n = 0
    while g.covered, n < 100 {
        _ = g.step(to: 20000 + 50 * Double(n + 1), dt: 1 / 60)
        n += 1
    }
    #expect(n == Glide.aimFrames)
}

/// Near the end there is no lead to aim: no cover, and never a start
/// past the end.
@Test func glideNearIsUncovered() {
    #expect(!Glide(offset: 900, target: 1500, visible: 800).covered)
    let past = Glide(offset: 2000, target: 1500, visible: 800)
    #expect(!past.covered && past.offset == 1500)
}

/// The rows near the end lay out shorter as the spring lands (T896):
/// it keeps to the end as it shrinks, lands on the real end once it
/// holds for Glide.hold frames, and is never past it.
@Test func glideLandsOnTheRealEnd() {
    var g = Glide(offset: 1200, target: 2000, visible: 800)
    #expect(!g.covered)
    // The spring is done in about 25 frames; the end shrinks 60 points a
    // frame from frame 20 to 28, then holds.
    let r = run(&g) { n in 2000 - 60 * Double(min(max(n - 19, 0), 9)) }
    #expect(g.landed && g.offset == 1460)
    #expect(r.past <= 0)
    #expect(r.n >= 29 + Glide.hold - 1)
}

/// An end that never holds (output streaming in each frame): the glide
/// still lands, on the end, within longest and settleLongest.
@Test func glideLandsOnAnEndThatNeverHolds() {
    var g = Glide(offset: 9000, target: 10000, visible: 800)
    let r = run(&g) { n in 10000 + (n % 2 == 0 ? 7 : -7) * Double(n) }
    #expect(g.landed && r.past <= 0)
    #expect(Double(r.n) / 60 <= Glide.longest + Glide.settleLongest + 2.0 / 60)
}

/// The follower's glide (T901): output that comes while it settles
/// keeps it on the end until that holds again, settleLongest anew; a
/// spring still running is left as it is.
@Test func glideRenewedByOutput() {
    var g = Glide(offset: 1500, target: 1500, visible: 800, instant: true)
    for _ in 0..<(Glide.hold - 1) { _ = g.step(to: 1500, dt: 1 / 60) }
    g.renew()
    // The next entry's estimate, then its height: on it each frame.
    let first = g.step(to: 1580, dt: 1 / 60)
    #expect(!first && g.offset == 1580)
    let second = g.step(to: 1640, dt: 1 / 60)
    #expect(!second && g.offset == 1640)
    var n = 0
    while !g.step(to: 1640, dt: 1 / 60) { n += 1 }
    #expect(n + 1 == Glide.hold && g.offset == 1640)

    // Renewed near settleLongest, it settles that long again.
    var h = Glide(offset: 0, target: 1000, visible: 800, instant: true)
    var t = 0
    while t < 20 { _ = h.step(to: 1000 + Double(t) * 3, dt: 1 / 60); t += 1 }
    #expect(!h.landed)
    h.renew()
    t = 0
    while !h.step(to: 2000 + Double(t) * 3, dt: 1 / 60) { t += 1 }
    #expect(Double(t + 1) / 60 >= Glide.settleLongest - 1e-9 && h.offset == 2000 + Double(t) * 3)

    var s = Glide(offset: 0, target: 1500, visible: 800)
    _ = s.step(to: 1500, dt: 1 / 60)
    let before = s
    s.renew()
    #expect(s == before)
}

/// The follower's glide marks the follow as gliding: rows laying out
/// as it goes don't stop the follow, new output doesn't scroll on its
/// own, and the user's finger ends it.
@Test func followGlideKeepsFollowing() {
    var f = Follow()
    let toEnd = f.arrived([1, 2])
    #expect(toEnd)
    f.glide(newest: nil)
    f.scrolled(offset: 1000, visible: 800, content: 2400, newest: 2)
    f.scrolled(offset: 900, visible: 800, content: 2600, newest: 2)
    #expect(f.following && f.gliding)
    let again = f.arrived([1, 2, 3])
    #expect(!again)
    f.moved(by: true, resting: false)
    #expect(!f.gliding && f.following)
}
