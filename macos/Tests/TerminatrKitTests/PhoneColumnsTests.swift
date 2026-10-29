import CoreGraphics
import Testing
@testable import TerminatrKit

// The phone's list column (T849): on an open iPhone Duo it ends at the
// hinge; elsewhere it is 360 pt, half the width at most.

@Test func listEndsAtTheHinge() {
    let hinge = CGRect(x: 333, y: 0, width: 3, height: 951)
    #expect(PhoneColumns.split(669, hinge: hinge) == (333, 3))
    // A hairline hinge still leaves a hairline between them.
    #expect(PhoneColumns.split(669, hinge: CGRect(x: 334, y: 0, width: 0, height: 951)) == (334, 1))
}

@Test func listWithoutAHinge() {
    #expect(PhoneColumns.split(1032, hinge: nil) == (360, 1))
    #expect(PhoneColumns.split(585, hinge: nil) == (292.5, 1))
    // Across the display (the Duo turned), or too near an edge for a
    // column beside it: the hinge doesn't decide.
    #expect(PhoneColumns.split(951, hinge: CGRect(x: 0, y: 333, width: 951, height: 3)) == (360, 1))
    #expect(PhoneColumns.split(669, hinge: CGRect(x: 100, y: 0, width: 3, height: 951)) == (334.5, 1))
}

// A session open beside the list (T855): in portrait, or narrower than
// 800 pt, it takes the whole width; in landscape the list stays.
@Test func sessionFoldsTheListInPortrait() {
    #expect(PhoneColumns.foldsList(CGSize(width: 669, height: 951)))   // open Duo, portrait
    #expect(PhoneColumns.foldsList(CGSize(width: 1032, height: 1376))) // iPad 13", portrait
    #expect(PhoneColumns.foldsList(CGSize(width: 744, height: 600)))   // narrow iPad window
    #expect(!PhoneColumns.foldsList(CGSize(width: 951, height: 669)))  // open Duo, landscape
    #expect(!PhoneColumns.foldsList(CGSize(width: 1376, height: 1032)))
}
