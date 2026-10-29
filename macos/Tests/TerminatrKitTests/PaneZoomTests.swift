import CoreGraphics
import Testing
@testable import TerminatrKit

@Suite struct PaneZoomTests {
    // A monospaced font's cell is about 0.6 of its size wide.
    let advance: CGFloat = 0.6

    @Test func fitsThePaneWidth() {
        // 80 columns in 600 points: 7.5 points a column, a 12.5 pt font.
        #expect(PaneZoom.size(fitting: 600, cols: 80, advance: advance, zoom: 1) == 12.5)
        // In 620, 12.92 pt, on half points.
        #expect(PaneZoom.size(fitting: 620, cols: 80, advance: advance, zoom: 1) == 12.5)
    }

    @Test func aWidePaneStopsAtTheSmallestAndPans() {
        #expect(PaneZoom.size(fitting: 380, cols: 200, advance: advance, zoom: 1) == PaneZoom.smallest)
    }

    @Test func aNarrowPaneStopsAtTheFittedTop() {
        #expect(PaneZoom.size(fitting: 1000, cols: 40, advance: advance, zoom: 1) == PaneZoom.fitted)
    }

    @Test func aPinchScalesUpToTheLargest() {
        #expect(PaneZoom.size(fitting: 600, cols: 80, advance: advance, zoom: 1.6) == 20)
        #expect(PaneZoom.size(fitting: 600, cols: 80, advance: advance, zoom: 10) == PaneZoom.largest)
    }

    @Test func theZoomIsClampedToWhatChangesTheSize() {
        let fit: CGFloat = 10
        #expect(PaneZoom.clamp(zoom: 10, fitted: fit) == PaneZoom.largest / fit)
        #expect(PaneZoom.clamp(zoom: 0.1, fitted: fit) == PaneZoom.smallest / fit)
        #expect(PaneZoom.clamp(zoom: 1.2, fitted: fit) == 1.2)
    }

    @Test func noRoomOrColumnsIsTheSmallest() {
        #expect(PaneZoom.fit(room: 0, cols: 80, advance: advance) == PaneZoom.smallest)
        #expect(PaneZoom.fit(room: 300, cols: 0, advance: advance) == PaneZoom.smallest)
    }
}
