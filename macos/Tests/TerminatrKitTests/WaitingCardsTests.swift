import Foundation
import Testing
@testable import TerminatrKit

// The newest waiting card not yet seen leads, and its edge runs (T805).

@MainActor @Test func newestCardLeads() {
    let c = WaitingCards()
    c.show("a", now: t0)
    #expect(c.lead == "a")
    c.show("b", now: t0.addingTimeInterval(10))
    #expect(c.lead == "b")
    c.hide("b")
    #expect(c.lead == "a")
}

@MainActor @Test func cardsComingTogetherLeadFromTheTop() {
    // A window opening on three cards: the first, the one ⌘↩ takes.
    let c = WaitingCards()
    c.show("q0", now: t0)
    c.show("q1", now: t0.addingTimeInterval(0.1))
    c.show("approval", now: t0.addingTimeInterval(0.2))
    #expect(c.lead == "q0")
}

@MainActor @Test func seenCardsGiveTheLeadBack() {
    let c = WaitingCards()
    c.show("a", now: t0)
    c.show("b", now: t0.addingTimeInterval(10))
    c.see("b")
    #expect(c.lead == "a")
    c.see("a")
    #expect(c.lead == nil)
    // Shown again, a seen card stays seen.
    c.hide("a")
    c.show("a", now: t0.addingTimeInterval(20))
    #expect(c.lead == nil)
}

@MainActor @Test func cardShownTwiceStaysUntilBothGo() {
    let c = WaitingCards()
    c.show("a", now: t0)
    c.show("a", now: t0.addingTimeInterval(5))
    c.hide("a")
    #expect(c.lead == "a")
    c.hide("a")
    #expect(c.lead == nil)
}
