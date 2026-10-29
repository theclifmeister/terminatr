import Foundation
import Testing
@testable import TerminatrKit

// A project's card (T846): its ThreadMeter's segments and legend in the
// tones' order, and its monogram.

@Test func tallyInOrder() {
    let t = Attention.Tone.tally([.working, .idle, .approval, .working, .review, .question])
    #expect(t.map(\.tone) == [.approval, .question, .review, .working, .idle])
    #expect(t.map(\.count) == [1, 1, 1, 2, 1])
    #expect(Attention.Tone.tally([]).isEmpty)
}

@Test func legendWords() {
    #expect(Attention.Tone.approval.counted(1) == "1 allow?")
    #expect(Attention.Tone.attention.counted(1) == "1 needs you")
    #expect(Attention.Tone.attention.counted(2) == "2 need you")
    #expect(Attention.Tone.working.counted(3) == "3 working")
}

@Test func monograms() {
    #expect(Monogram.letters("acme-web") == "aw")
    #expect(Monogram.letters("billing-api") == "ba")
    #expect(Monogram.letters("Docs_Site v2") == "ds")
    #expect(Monogram.letters("terminatr") == "te")
    #expect(Monogram.letters("x") == "x")
    #expect(Monogram.letters("") == "")
}
