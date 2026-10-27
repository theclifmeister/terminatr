import Testing
@testable import TerminatrKit

private let small = AppAPI.PaneSize(cols: 80, rows: 24)
private let big = AppAPI.PaneSize(cols: 120, rows: 40)
private let wide = AppAPI.PaneSize(cols: 160, rows: 40)

@Test func paneFitClaimsOnOpen() {
    var f = PaneFit()
    // Not laid out yet, or the stream's size not known: nothing.
    #expect(f.claim(fitting: nil, pane: small, keys: true, open: true) == nil)
    #expect(f.claim(fitting: big, pane: nil, keys: true, open: true) == nil)
    #expect(f.claim(fitting: big, pane: small, keys: true, open: true) == big)
    #expect(f.claimed == big)
}

@Test func paneFitNeverWithoutKeysOrStream() {
    var f = PaneFit()
    #expect(f.claim(fitting: big, pane: small, keys: false, open: true) == nil)
    #expect(f.claim(fitting: big, pane: small, keys: true, open: false) == nil)
    #expect(f.claimed == nil)
}

@Test func paneFitNotWhenAlreadyThatSize() {
    var f = PaneFit()
    #expect(f.claim(fitting: big, pane: big, keys: true, open: true) == nil)
}

@Test func paneFitOncePerSize() {
    var f = PaneFit()
    #expect(f.claim(fitting: big, pane: small, keys: true, open: true) == big)
    // Another client (the TUI) resized the pane back: the same room
    // doesn't fight it, neither on a layout nor on keys.
    #expect(f.claim(fitting: big, pane: small, keys: true, open: true) == nil)
    // The room changed: claims its size, and again when it is back.
    #expect(f.claim(fitting: wide, pane: small, keys: true, open: true) == wide)
    #expect(f.claim(fitting: big, pane: wide, keys: true, open: true) == big)
}

@Test func paneFitClaimsAgainOnANewStream() {
    var f = PaneFit()
    #expect(f.claim(fitting: big, pane: small, keys: true, open: true) == big)
    f.opened()
    #expect(f.claimed == nil)
    #expect(f.claim(fitting: big, pane: small, keys: true, open: true) == big)
}

@Test func paneFitSettlesOnTheLastMove() {
    var f = PaneFit()
    // A live drag: each move supersedes the one before.
    let moves = (0..<30).map { _ in f.moved() }
    #expect(moves.dropLast().allSatisfy { !f.settled($0) })
    #expect(f.settled(moves.last!))
    // Opening a stream keeps a pending move.
    let m = f.moved()
    f.opened()
    #expect(f.settled(m))
}
