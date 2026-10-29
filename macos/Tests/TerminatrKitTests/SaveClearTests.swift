import Foundation
import Testing
@testable import TerminatrKit

// A save and clear as the phone follows it (T868): saving, clearing,
// done once the coordinator's context use drops.

private func coordinator(_ state: String, context: Int = 0) -> AppAPI.Session {
    AppAPI.Session(id: "s-1", role: "coordinator", project: "demo", cols: 80, rows: 24, created: t0, clients: 0, state: state,
                   context: context, contextWindow: context > 0 ? 200_000 : 0)
}

@Test func saveClearFollowsTheCoordinator() {
    let at = Date(timeIntervalSince1970: 1000)
    var c = SaveClear(coordinator: coordinator("idle", context: 144_000), at: at)
    #expect(c.phase == .asking && c.running && c.words == "Asking…")
    c.asked()
    #expect(c.phase == .saving && c.words == "Saving what it learned…")
    // Idle before its save turn: still saving.
    c.follow(coordinator("idle", context: 144_000), at: at)
    #expect(c.phase == .saving)
    // The save turn reads more.
    c.follow(coordinator("working", context: 150_000), at: at)
    #expect(c.phase == .saving)
    c.follow(coordinator("idle", context: 150_000), at: at)
    #expect(c.phase == .clearing && c.words == "Clearing…")
    #expect(!c.over(at: at.addingTimeInterval(SaveClear.longest - 1)))
    // Cleared: the server forgets the use until the next turn.
    c.follow(coordinator("idle"), at: at.addingTimeInterval(30))
    #expect(c.phase == .done(after: nil) && !c.running && c.words == "Cleared: context was 72%")
    #expect(!c.over(at: at.addingTimeInterval(30 + SaveClear.shown - 1)))
    #expect(c.over(at: at.addingTimeInterval(30 + SaveClear.shown + 1)))
    // Done stays done.
    c.follow(coordinator("working", context: 160_000), at: at)
    #expect(c.phase == .done(after: nil))
}

@Test func saveClearSaysTheDrop() {
    let at = Date(timeIntervalSince1970: 0)
    var c = SaveClear(coordinator: coordinator("idle", context: 144_000), at: at)
    c.asked()
    c.follow(coordinator("working", context: 8_000), at: at)
    #expect(c.phase == .done(after: 4) && c.words == "Cleared: context 72% → 4%")
}

@Test func saveClearSeesAShortSaveTurn() {
    let at = Date(timeIntervalSince1970: 0)
    var c = SaveClear(coordinator: coordinator("idle", context: 144_000), at: at)
    c.asked()
    // Its turn ran between two reads: it read more since.
    c.follow(coordinator("idle", context: 145_000), at: at)
    #expect(c.phase == .clearing)
}

@Test func saveClearWithoutAUse() {
    let at = Date(timeIntervalSince1970: 0)
    var c = SaveClear(coordinator: coordinator("idle"), at: at)
    c.asked()
    c.follow(coordinator("working"), at: at)
    #expect(c.phase == .saving)
    c.follow(coordinator("idle"), at: at)
    #expect(c.phase == .done(after: nil))
    // Followed too long, it goes.
    var stuck = SaveClear(coordinator: coordinator("idle"), at: at)
    stuck.asked()
    #expect(stuck.over(at: at.addingTimeInterval(SaveClear.longest + 1)))
}

@Test func saveClearFails() {
    let at = Date(timeIntervalSince1970: 0)
    var c = SaveClear(coordinator: coordinator("idle", context: 100_000), at: at)
    c.asked()
    c.follow(nil, at: at)
    #expect(c.phase == .failed("The coordinator stopped before it was cleared.") && !c.running)
    var refused = SaveClear(coordinator: coordinator("idle"), at: at)
    refused.fail("no clear prompt", at: at)
    #expect(refused.words == "no clear prompt")
    #expect(ContextUse(percent: 130, level: .high).fraction == 1 && ContextUse(percent: 25, level: .plain).fraction == 0.25)
}
