import Testing
@testable import TerminatrKit

// T1031: the one section holding the keyboard shows the focus border; none
// does when the window doesn't have the keys or the sidebar has them.
struct SectionFocusTests {
    private func focus(present: Set<WindowSection> = [.sidebar, .main, .inspector]) -> SectionFocus {
        var f = SectionFocus()
        f.present = present
        return f
    }

    @Test func theSectionHoldingTheKeyboardShowsIt() {
        var f = focus()
        for s in [WindowSection.files, .inspector, .main] {
            f.present.insert(s)
            f.hold(s)
            #expect(f.border(windowHasKeys: true) == s)
        }
    }

    @Test func theSidebarNeverShowsItAndNoOtherDoesForIt() {
        var f = focus()
        f.hold(.main)
        f.hold(.sidebar)
        #expect(f.border(windowHasKeys: true) == nil)
        // Nothing holds, with the sidebar the last that did.
        f.hold(nil)
        #expect(f.border(windowHasKeys: true) == nil)
    }

    @Test func theSidebarHiddenGivesWayToTheMainPane() {
        var f = focus()
        f.hold(.sidebar)
        f.hold(nil)
        f.present = [.main]
        #expect(f.border(windowHasKeys: true) == .main)
    }

    @Test func noneInAnInactiveWindow() {
        var f = focus()
        f.hold(.main)
        #expect(f.border(windowHasKeys: false) == nil)
    }

    @Test func theLastHolderStaysWhileNothingHolds() {
        var f = focus()
        f.hold(.inspector)
        f.hold(nil)
        #expect(f.home == .inspector)
        #expect(f.border(windowHasKeys: true) == .inspector)
    }

    @Test func aSectionThatWentGivesWayToTheMainPane() {
        var f = focus()
        f.hold(.inspector)
        f.hold(nil)
        f.present = [.sidebar, .main]
        #expect(f.home == .main)
        #expect(f.border(windowHasKeys: true) == .main)
    }

    @Test func aHolderNotShownCountsAsGone() {
        var f = focus(present: [.main])
        f.hold(.files)
        #expect(f.border(windowHasKeys: true) == .main)
    }
}
