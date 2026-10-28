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
    f.follow(newest: 3)
    #expect(f.following)
    #expect(f.unseen == 0)
    at(&f, 100, content: 1600, newest: 3)
    at(&f, 50, content: 1600, newest: 3)
    #expect(!f.following)
    let s7 = f.arrived([0, 1, 2, 3, 4])
    #expect(!s7)
    #expect(f.unseen == 1)
}
