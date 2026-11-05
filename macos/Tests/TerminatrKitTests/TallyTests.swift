import Foundation
import Testing
@testable import TerminatrKit

// A project's tones (T846): their tally in order, the legend's words, and
// its monogram.

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

// A project's row (T1001): its threads in words, and the dot of what it
// speaks for.

@Test func threadSummaries() {
    #expect(Attention.Tone.summary([]) == "no threads")
    #expect(Attention.Tone.summary([.working]) == "1 working")
    #expect(Attention.Tone.summary([.working, .working]) == "2 working")
    #expect(Attention.Tone.summary([.idle]) == "1 thread")
    #expect(Attention.Tone.summary([.idle, .done]) == "2 threads")
    #expect(Attention.Tone.summary([.working, .approval, .idle]) == "3 threads")
}

@Test func strongestDot() {
    #expect(Attention.Tone.strongest(.idle, []) == .idle)
    #expect(Attention.Tone.strongest(.idle, [.working]) == .working)
    #expect(Attention.Tone.strongest(.working, [.approval, .idle]) == .approval)
    #expect(Attention.Tone.strongest(.question, [.working]) == .question)
}
