import CoreGraphics
import Testing
import TerminatrKit

@Suite struct NavChromeTests {
    private let upright = CGSize(width: 402, height: 874)
    private let onItsSide = CGSize(width: 874, height: 402)

    @Test func phoneHasTheBarUprightAndTheRailOnItsSide() {
        #expect(NavChrome.place(pad: false, regular: false, size: upright) == .bar)
        #expect(NavChrome.place(pad: false, regular: false, size: onItsSide) == .rail)
    }

    @Test func openDuoHasTheRailEitherWayUp() {
        #expect(NavChrome.place(pad: false, regular: true, size: CGSize(width: 585, height: 750)) == .rail)
        #expect(NavChrome.place(pad: false, regular: true, size: CGSize(width: 750, height: 585)) == .rail)
    }

    @Test func iPadHasTheSidebarAndANarrowWindowTheBar() {
        #expect(NavChrome.place(pad: true, regular: true, size: CGSize(width: 1194, height: 834)) == .sidebar)
        #expect(NavChrome.place(pad: true, regular: true, size: CGSize(width: 834, height: 1194)) == .sidebar)
        #expect(NavChrome.place(pad: true, regular: false, size: CGSize(width: 320, height: 800)) == .bar)
    }

    @Test func sidebarLiesOverTheContentUprightOrWhenTight() {
        #expect(NavChrome.sidebarOverlays(CGSize(width: 834, height: 1194)))
        #expect(NavChrome.sidebarOverlays(CGSize(width: 900, height: 700)))
        #expect(!NavChrome.sidebarOverlays(CGSize(width: 1133, height: 744)))
        #expect(!NavChrome.sidebarOverlays(CGSize(width: 1194, height: 834)))
    }
}

@Test func sidebarGrowsWithItsTextAndThenLiesOver() {
    #expect(NavChrome.sidebarWidth(scale: 1) == NavChrome.sidebarWidth)
    #expect(NavChrome.sidebarWidth(scale: 0.8) == NavChrome.sidebarWidth)
    #expect(NavChrome.sidebarWidth(scale: 1.5) == NavChrome.sidebarWidth * 1.5)
    let landscape = CGSize(width: 1133, height: 744)
    #expect(!NavChrome.sidebarOverlays(landscape))
    #expect(NavChrome.sidebarOverlays(landscape, sidebar: NavChrome.sidebarWidth(scale: 1.5)))
}
