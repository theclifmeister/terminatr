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
