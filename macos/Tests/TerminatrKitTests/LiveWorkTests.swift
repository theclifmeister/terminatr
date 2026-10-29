import Foundation
import Testing
@testable import TerminatrKit

// The phone's live strip (T849): this server's working sessions, led by
// the one that has worked longest.

@Test func liveWorkLeadsWithTheLongestTurn() throws {
    var a = session("s-1", task: "T14"), b = session("s-2", task: "T16"), c = session("s-3", state: "idle")
    a.turnSince = t0.addingTimeInterval(60)
    b.turnSince = t0
    b.current = "Reading cart.spec.ts"
    var remote = session("s-4", server: "laptop")
    remote.turnSince = t0.addingTimeInterval(-600)
    let w = try #require(LiveWork.of([a, b, c, remote]))
    #expect(w.count == 2)
    #expect(w.lead.id == "s-2")
    #expect(w.word == "2 working")
    #expect(w.what == "T16 reading cart.spec.ts")
    #expect(w.ref == SessionRef(project: "terminatr", thread: "t-0675"))
}

@Test func liveWorkWithoutTurnsKeepsTheListsOrder() throws {
    let w = try #require(LiveWork.of([session("s-1", state: "blocked"), session("s-2", task: "T2"), session("s-3", task: "T3")]))
    #expect(w.lead.id == "s-2")
    #expect(w.what == "T2")
    #expect(LiveWork.of([session("s-1", state: "idle")]) == nil)
    #expect(LiveWork.of([]) == nil)
}

@Test func liveWorkCoordinator() throws {
    let s = AppAPI.Session(id: "s-9", role: "coordinator", project: "acme-web", cols: 80, rows: 24, created: t0, clients: 0,
                           state: "working", current: "Reading T15's report")
    let w = try #require(LiveWork.of([s]))
    #expect(w.ref == SessionRef(project: "acme-web"))
    #expect(w.what == "acme-web reading T15's report")
}
