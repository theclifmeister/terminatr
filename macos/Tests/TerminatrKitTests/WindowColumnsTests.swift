import Testing
@testable import TerminatrKit

// T777: the columns' least widths fit in each window at its least; else
// the split view and the window fight over them and AppKit stops the app.
@Test func columnsFitTheMainWindow() {
    #expect(WindowColumns.mainNeeds <= WindowColumns.window)
}

@Test func columnsFitTheSessionWindow() {
    #expect(WindowColumns.sessionNeeds <= WindowColumns.sessionWindow)
}

@Test func sidebarRangeIsOrdered() {
    #expect(WindowColumns.sidebarMin <= WindowColumns.sidebarIdeal)
    #expect(WindowColumns.sidebarIdeal <= WindowColumns.sidebarMax)
}

// The detail at its least still has room for a conversation's cards.
@Test func detailLeastIsAConversationsWidth() {
    #expect(WindowColumns.detailMin >= 320)
}
