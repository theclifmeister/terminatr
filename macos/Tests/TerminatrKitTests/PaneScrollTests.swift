import Testing
@testable import TerminatrKit

@Test func paneScrollKeepsTheServersScrollback() {
    // SwiftTerm's default is 500: the first repaint's 10,000 lines must fit.
    #expect(PaneScroll.lines == 10_000)
}

@Test func paneScrollPagesHalfThePaneOnTheMainScreen() {
    #expect(PaneScroll.page(rows: 40, alternate: false) == 20)
    #expect(PaneScroll.page(rows: 1, alternate: false) == 1)
    // A full-screen program gets Shift+PgUp/PgDn.
    #expect(PaneScroll.page(rows: 40, alternate: true) == nil)
}

@Test func paneScrollTellsMouseReportsFromReplies() {
    let esc: UInt8 = 0x1b
    func csi(_ s: String) -> [UInt8] { [esc, UInt8(ascii: "[")] + Array(s.utf8) }
    // The wheel up at column 3, row 5: SGR press, X10, urxvt, 8-bit CSI.
    #expect(PaneScroll.isMouseReport(csi("<64;3;5M")))
    #expect(PaneScroll.isMouseReport(csi("<0;3;5m")))
    #expect(PaneScroll.isMouseReport(csi("M") + [96, 35, 37]))
    #expect(PaneScroll.isMouseReport(csi("96;3;5M")))
    #expect(PaneScroll.isMouseReport([0x9b] + Array("<64;3;5M".utf8)))
    // Replies the server's emulator answers: device attributes, the
    // cursor position, a mode report, focus, and a bare CSI.
    #expect(!PaneScroll.isMouseReport(csi("?1;2c")))
    #expect(!PaneScroll.isMouseReport(csi("12;40R")))
    #expect(!PaneScroll.isMouseReport(csi("?2004;2$y")))
    #expect(!PaneScroll.isMouseReport(csi("I")))
    #expect(!PaneScroll.isMouseReport(csi("M")))
    #expect(!PaneScroll.isMouseReport(csi("3;5M")))
    #expect(!PaneScroll.isMouseReport(Array("hello".utf8)))
}

@Test func paneScrollSplitsThePhonesSwipe() {
    // A program that tracks the mouse gets the wheel, a full-screen one
    // the arrows, unless it reset alternate scroll.
    #expect(PaneScroll.swipe(alternate: false, mouse: true, alternateScroll: true, keys: true) == .wheel)
    #expect(PaneScroll.swipe(alternate: true, mouse: true, alternateScroll: true, keys: true) == .wheel)
    #expect(PaneScroll.swipe(alternate: true, mouse: false, alternateScroll: true, keys: true) == .arrows)
    #expect(PaneScroll.swipe(alternate: true, mouse: false, alternateScroll: false, keys: true) == .none)
    #expect(PaneScroll.swipe(alternate: false, mouse: false, alternateScroll: true, keys: true) == .history)
    // At view nothing goes in: the scrollback, or nothing.
    #expect(PaneScroll.swipe(alternate: false, mouse: true, alternateScroll: true, keys: false) == .history)
    #expect(PaneScroll.swipe(alternate: true, mouse: true, alternateScroll: true, keys: false) == .none)
}

@Test func paneScrollCountsADragInLines() {
    #expect(PaneScroll.lines(25, step: 10) == (2, 5))
    #expect(PaneScroll.lines(-25, step: 10) == (-2, -5))
    #expect(PaneScroll.lines(9, step: 10) == (0, 9))
    #expect(PaneScroll.lines(9, step: 0) == (0, 0))
}

@Test func paneScrollMapsThePhonesOffsetToARow() {
    // 100 rows of history at 10 points: the screen is row 100.
    #expect(PaneScroll.row(offset: 0, cell: 10, history: 100) == 0)
    #expect(PaneScroll.row(offset: -40, cell: 10, history: 100) == 0)
    #expect(PaneScroll.row(offset: 455, cell: 10, history: 100) == 45)
    #expect(PaneScroll.row(offset: 1000, cell: 10, history: 100) == 100)
    // Past the screen's top the host pans the grid: still the screen.
    #expect(PaneScroll.row(offset: 1300, cell: 10, history: 100) == 100)
    // Half a row up from the screen reads the scrollback.
    #expect(!PaneScroll.back(offset: 996, cell: 10, history: 100))
    #expect(PaneScroll.back(offset: 994, cell: 10, history: 100))
    #expect(!PaneScroll.back(offset: 0, cell: 10, history: 0))
}
