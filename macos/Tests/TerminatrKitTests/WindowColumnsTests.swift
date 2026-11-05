import Testing
@testable import TerminatrKit

// The columns' least widths fit in each window at its least.
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

// T1000: the reference's window, measured: 1876 wide, a pane of 1146.
@Test func theReferencesWidthsAddUp() {
    #expect(WindowColumns.needs(sidebar: 296, inspector: 406, main: 1146) == 1876)
    #expect(WindowColumns.sidebar(saved: nil) == 296)
    #expect(InspectorColumn.width(saved: nil) == 406)
}

@Test func theReferencesNeedsLineLandsWhereItDoes() {
    // Its lower section takes 476 of the sidebar's 1076.
    #expect(WindowColumns.needsHeight(sidebar: 1076) == 476)
    #expect(WindowColumns.needsHeight(sidebar: 538) == 238)
}

@Test func columnsKeepTheirWidthsWhenTheyFit() {
    let f = WindowColumns.fit(window: 1876, sidebar: 296, inspector: 406)
    #expect(f.sidebar == 296)
    #expect(f.inspector == 406)
}

@Test func theInspectorGivesWayFirstThenTheSidebar() {
    // Narrow enough for the inspector alone to give way.
    let a = WindowColumns.fit(window: 1000, sidebar: 296, inspector: 406)
    #expect(a.sidebar == 296)
    // 1000 - (2 + 296 + 9 + 340 + 9 + 8)
    #expect(a.inspector == 336.0)
    // At the window's least, both do.
    let b = WindowColumns.fit(window: 930, sidebar: 296, inspector: 406)
    #expect(b.inspector == InspectorColumn.min)
    // 930 - (2 + 9 + 340 + 9 + 300 + 8)
    #expect(b.sidebar == 262.0)
    // The main pane keeps its least.
    #expect(WindowColumns.needs(sidebar: b.sidebar, inspector: b.inspector, main: WindowColumns.detailMin) == 930)
}

@Test func aHiddenColumnStaysHidden() {
    let f = WindowColumns.fit(window: 980, sidebar: nil, inspector: nil)
    #expect(f.sidebar == nil)
    #expect(f.inspector == nil)
}

@Test func savedWidthsStayInRange() {
    #expect(WindowColumns.sidebar(saved: 100) == WindowColumns.sidebarMin)
    #expect(WindowColumns.sidebar(saved: 900) == WindowColumns.sidebarMax)
    #expect(WindowColumns.sidebar(saved: 310) == 310)
}
