import CoreGraphics
import Testing
@testable import TerminatrKit

/// Three buttons with their gaps, as a working thread's composer has them.
private let threeButtons: CGFloat = 261

@Test func theComposerKeepsItsButtonsBesideAFieldWithRoom() {
    #expect(!ComposerFit.stacks(width: 900, buttons: threeButtons))
    #expect(!ComposerFit.stacks(width: threeButtons + ComposerFit.leastField, buttons: threeButtons))
}

@Test func theComposerMovesItsButtonsUnderANarrowField() {
    // A 1280 window with a 420 sidebar and a 462 panel: about 100 left.
    #expect(ComposerFit.stacks(width: 374, buttons: threeButtons))
    #expect(ComposerFit.stacks(width: threeButtons + ComposerFit.leastField - 1, buttons: threeButtons))
}

@Test func aSendAloneStaysBesideTheField() {
    #expect(!ComposerFit.stacks(width: 374, buttons: 87))
}

@Test func aLargerTypeScaleAsksForAWiderField() {
    let least = ComposerFit.leastField * 1.5
    #expect(ComposerFit.stacks(width: 600, buttons: threeButtons, leastField: least))
}
