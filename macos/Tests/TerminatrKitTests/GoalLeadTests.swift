import Testing
import TerminatrKit

@Test func goalLeadOpensOnItsFirstSentence() {
    let g = GoalLead("One terminal for all your coding agents: a coordinator you talk to, threads that do the work. The TUI is the baseline; the apps follow it.")
    #expect(g.sentence == "One terminal for all your coding agents: a coordinator you talk to, threads that do the work.")
    #expect(g.hasMore)
}

@Test func goalLeadOfOneSentenceHasNoMoreToShow() {
    let g = GoalLead("  Rebuild the storefront on the new design system\n")
    #expect(g.whole == "Rebuild the storefront on the new design system")
    #expect(g.sentence == g.whole)
    #expect(!g.hasMore)
}

@Test func goalLeadKeepsAbbreviationsAndVersionsInTheSentence() {
    let g = GoalLead("Ship v1.2 to the store, e.g. by Friday. Then rest.")
    #expect(g.sentence == "Ship v1.2 to the store, e.g. by Friday.")
}

@Test func goalLeadStopsAtTheFirstLine() {
    let g = GoalLead("Keep the shop up\nand the rest of it below")
    #expect(g.sentence == "Keep the shop up")
    #expect(g.hasMore)
}

/// A long first sentence is cut at a clause first, then at a word, and
/// no cut ends in what only leads on or an ellipsis.
@Test func goalLeadCutsALongSentenceWithoutEllipsis() {
    let g = GoalLead("One terminal for all your coding agents: a coordinator you talk to, threads that do the work")
    #expect(g.cuts.first == "One terminal for all your coding agents: a coordinator you talk to")
    #expect(g.cuts[1] == "One terminal for all your coding agents")
    #expect(g.cuts.contains("One terminal for all your coding agents: a coordinator you"))
    #expect(g.cuts.last == "One")
    for c in g.cuts {
        #expect(!c.hasSuffix("…") && !c.hasSuffix(".") && !c.hasSuffix(",") && !c.hasSuffix(":"))
    }
}

@Test func goalLeadOfOneWordHasNoCuts() {
    #expect(GoalLead("Ship").cuts.isEmpty)
    #expect(GoalLead("").whole.isEmpty)
}
