import Foundation
import Testing
@testable import TerminatrKit

// Usage in thread and task details (T1053): turns and tokens written,
// cost when the record has one, nothing for none.

private let withCost = AppAPI.Usage(turns: 14, input: 12_000, output: 240_000, cacheRead: 900_000, cacheCreation: 48_000, costUsd: 3.41)
private let tokensOnly = AppAPI.Usage(turns: 3, input: 6_000, output: 80_000, cacheRead: 18_574_000, cacheCreation: 40_000, costUsd: 0)
private let plan = AppAPI.Usage(turns: 1, input: 1_000, output: 4_000, cacheRead: 700_000, cacheCreation: 0, costUsd: 0, planPct: 12, hasPlan: true)

@Test func usageWithCost() {
    #expect(withCost.line == "14 turns · 300k tokens written · 1.2M with cache reads · $3.41")
    #expect(withCost.factRows.map(\.label) == ["Turns", "Written", "With cache", "Cost"])
    #expect(withCost.factRows.last?.value == "$3.41")
}

@Test func usageTokensOnly() {
    #expect(tokensOnly.line == "3 turns · 126k tokens written · 18.7M with cache reads")
    #expect(tokensOnly.costText == nil)
    #expect(tokensOnly.factRows.map(\.label) == ["Turns", "Written", "With cache"])
}

@Test func usageWithPlan() {
    #expect(plan.line == "1 turn · 5k tokens written · 705k with cache reads · plan 12%")
    #expect(plan.factRows.last?.label == "Plan")
    #expect(plan.factRows.last?.value == "12%")
}

@Test func usageNone() {
    #expect(noUsage.line == nil)
    #expect(noUsage.factRows.isEmpty)
}

@Test func usagePartialCost() {
    let u = AppAPI.Usage(turns: 2, input: 1_000, output: 1_000, cacheRead: 0, cacheCreation: 0, costUsd: 0.5, uncosted: 1_000)
    #expect(u.costText == "$0.50 partial")
}

@Test func taskDetailCarriesTaskUsage() {
    let t = AppAPI.Task(id: 4, ref: "T4", title: "Docs", status: "ready")
    let p = AppAPI.Project(slug: "demo", active: true, board: [t], usage: noUsage, taskUsage: ["T4": tokensOnly])
    #expect(TaskDetail.of("T4", in: p)?.usage == tokensOnly)
    #expect(TaskDetail.of("T5", in: p) == nil)
}
