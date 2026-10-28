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
