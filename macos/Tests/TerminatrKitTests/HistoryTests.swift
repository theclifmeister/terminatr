import Foundation
import Testing
@testable import TerminatrKit

// A project's daily numbers (app API 55): gated on the hello, so a tm or
// a server without them hides what draws them; read again only when the
// done count moves.

@Test func historyNeedsAPI55AndTheCap() {
    let caps = AppAPI.Server(version: "0.83.0", protocol: 39, caps: ["history", "peer-history"])
    var h = AppAPI.Hello(appApi: 55, tm: "0.83.0", build: "", methods: ["project.history", "peer.history"], server: caps)
    #expect(Compatibility.readsHistory(h) && Compatibility.readsPeerHistory(h))
    h.appApi = 54
    #expect(!Compatibility.readsHistory(h) && !Compatibility.readsPeerHistory(h))
    h.appApi = 55
    h.server = AppAPI.Server(version: "0.82.0", protocol: 38, caps: [])
    #expect(!Compatibility.readsHistory(h) && !Compatibility.readsPeerHistory(h))
    h.server = caps
    h.methods = []
    #expect(!Compatibility.readsHistory(h) && !Compatibility.readsPeerHistory(h))
}

@Test func anOlderPairedServersNoPagesReadsAsNotShared() {
    let state: PeerPages.State<Int> = PeerPages.state(AppAPI.Error(code: "no-pages", message: "the paired server's tm shares no history"))
    #expect(state == .notShared)
}

@Test func historyIsReadAgainOnlyWhenTheDoneCountChanges() {
    var reads = HistoryReads()
    #expect(reads.needsRead("web", done: 3))
    #expect(!reads.needsRead("web", done: 3))
    #expect(reads.needsRead("api", done: 3))
    #expect(reads.needsRead("web", done: 4))
    reads.forget("web")
    #expect(reads.needsRead("web", done: 4))
}

@Test func historyDecodesAnEmptyProject() throws {
    let h = try JSONDecoder().decode(AppAPI.History.self, from: Data(#"{"done":0,"open":0,"dropped":0,"days":[],"timed":0}"#.utf8))
    #expect(h.days.isEmpty && h.bestDay == nil && h.typical.isEmpty && h.firstRecord.isEmpty)
}

@Test func historyDecodesTheFacts() throws {
    let h = try JSONDecoder().decode(AppAPI.History.self, from: Data(#"{"first_record":"2026-10-05","done":3,"open":1,"dropped":0,"days":[{"day":"2026-10-05","done":3}],"best_day":{"day":"2026-10-05","done":3},"timed":12,"typical_seconds":2040,"typical":"34 minutes"}"#.utf8))
    #expect(h.firstRecord == "2026-10-05" && h.bestDay?.done == 3 && h.typicalSeconds == 2040 && h.typical == "34 minutes")
}
