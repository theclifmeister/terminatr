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

// Saved widths from before stay valid; there is no greatest any more.
@Test func savedWidthsOnlyHaveALeast() {
    #expect(WindowColumns.sidebar(saved: 100) == WindowColumns.sidebarMin)
    #expect(WindowColumns.sidebar(saved: 900) == 900)
    #expect(WindowColumns.sidebar(saved: 310) == 310)
    #expect(WindowColumns.sidebar(saved: 420) == 420)
    #expect(InspectorColumn.width(saved: 900) == 900)
    #expect(InspectorColumn.width(saved: 560) == 560)
}

@Test func wideSavedWidthsFitAWideWindow() {
    let f = WindowColumns.fit(window: 2400, sidebar: 700, inspector: 800)
    #expect(f.sidebar == 700)
    #expect(f.inspector == 800)
}

// One panel (a session window's inspector, or the main window with the
// sidebar hidden): it can take all but the main pane's least.
@Test func onePanelLimit() {
    // 1000 - (2 + 9 + 340 + 9 + 8)
    #expect(WindowColumns.limit(.inspector, window: 1000, sidebarShown: false, inspectorShown: true) == 632)
    // 1000 - (2 + 9 + 340 + 8)
    #expect(WindowColumns.limit(.sidebar, window: 1000, sidebarShown: true, inspectorShown: false) == 641)
}

// Two panels: the other stays at its least.
@Test func twoPanelsLimit() {
    // 1000 - (2 + 240 + 9 + 340 + 9 + 8)
    #expect(WindowColumns.limit(.inspector, window: 1000, sidebarShown: true, inspectorShown: true) == 392)
    // 1000 - (2 + 9 + 340 + 9 + 300 + 8)
    #expect(WindowColumns.limit(.sidebar, window: 1000, sidebarShown: true, inspectorShown: true) == 332)
}

// A panel at its limit leaves the others exactly their least: the window
// is full, not over.
@Test func aPanelAtItsLimitFillsTheWindow() {
    let w = 1400.0
    let lim = WindowColumns.limit(.sidebar, window: w, sidebarShown: true, inspectorShown: true)
    let f = WindowColumns.fit(window: w, sidebar: lim, inspector: 406, keep: .sidebar)
    #expect(f.sidebar == lim)
    #expect(f.inspector == InspectorColumn.min)
    #expect(WindowColumns.needs(sidebar: f.sidebar, inspector: f.inspector, main: WindowColumns.detailMin) == w)
    let lim2 = WindowColumns.limit(.inspector, window: w, sidebarShown: true, inspectorShown: true)
    let g = WindowColumns.fit(window: w, sidebar: 296, inspector: lim2, keep: .inspector)
    #expect(g.inspector == lim2)
    #expect(g.sidebar == WindowColumns.sidebarMin)
    #expect(WindowColumns.needs(sidebar: g.sidebar, inspector: g.inspector, main: WindowColumns.detailMin) == w)
}

// Three panels: the sidebar, the main pane's own and the inspector with
// Files or the inspector; the main pane counts as the third.
@Test func theLastSizedPanelGivesWayLast() {
    let a = WindowColumns.fit(window: 1000, sidebar: 500, inspector: 500)
    #expect(a.inspector == InspectorColumn.min)
    #expect(a.sidebar == 332)
    let b = WindowColumns.fit(window: 1000, sidebar: 500, inspector: 500, keep: .inspector)
    #expect(b.sidebar == WindowColumns.sidebarMin)
    #expect(b.inspector == 392)
}

// A narrow window: the limits never go under a panel's own least, and
// the saved widths come back when the window grows.
@Test func aNarrowWindowKeepsLeastAndSavedWidths() {
    #expect(WindowColumns.limit(.sidebar, window: 500, sidebarShown: true, inspectorShown: true) == WindowColumns.sidebarMin)
    #expect(WindowColumns.limit(.inspector, window: 500, sidebarShown: true, inspectorShown: true) == InspectorColumn.min)
    let narrow = WindowColumns.fit(window: WindowColumns.window, sidebar: 600, inspector: 700)
    #expect(narrow.sidebar! >= WindowColumns.sidebarMin)
    #expect(narrow.inspector == InspectorColumn.min)
    let wide = WindowColumns.fit(window: 2600, sidebar: 600, inspector: 700)
    #expect(wide.sidebar == 600)
    #expect(wide.inspector == 700)
}

// The other panels hidden: a lone panel's limit is the same however the
// window is shaped around it.
@Test func hiddenPanelsDontHoldRoom() {
    let both = WindowColumns.limit(.sidebar, window: 1200, sidebarShown: true, inspectorShown: true)
    let alone = WindowColumns.limit(.sidebar, window: 1200, sidebarShown: true, inspectorShown: false)
    #expect(alone - both == 9 + InspectorColumn.min)
}
