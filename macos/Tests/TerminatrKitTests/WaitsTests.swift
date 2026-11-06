import Foundation
import Testing
@testable import TerminatrKit

// Waits-for on a board row (T1049): "after T9" while a link is unmet,
// "2 wait" on the task they wait for, nothing otherwise; a program task
// keeps its acme-api/T12 form.

@Test func waitsSayNothingWithoutLinks() {
    #expect(Waits.words(AppAPI.Task(id: 1, ref: "T1", title: "A", status: "ready")) == "")
}

@Test func waitsWords() {
    func t(_ refs: [String], _ n: Int) -> AppAPI.Task {
        AppAPI.Task(id: 1, ref: "T1", title: "A", status: "ready", waitsFor: refs, waiters: n)
    }
    #expect(Waits.words(t(["T912"], 0)) == "after T912")
    #expect(Waits.words(t([], 2)) == "2 wait")
    #expect(Waits.words(t(["T9"], 1)) == "after T9 · 1 wait")
    #expect(Waits.words(t(["T9", "acme-api/T12"], 0)) == "after T9, acme-api/T12")
    #expect(Waits.words(t(["T9", "T10", "T11", "acme-api/T12"], 0)) == "after T9, T10 +2")
    #expect(Waits.spoken(t(["T9"], 2)) == "comes after T9, 2 tasks wait for it")
}

@Test func waitsDecodeFromTheBoard() throws {
    let json = #"{"id":2,"ref":"T2","title":"B","status":"ready","waits_for":["T1","acme-api/T12"],"waiters":3}"#
    let t = try JSONDecoder().decode(AppAPI.Task.self, from: Data(json.utf8))
    #expect(Waits.words(t) == "after T1, acme-api/T12 · 3 wait")
    // An older tm sends neither.
    let old = try JSONDecoder().decode(AppAPI.Task.self, from: Data(#"{"id":2,"ref":"T2","title":"B","status":"ready"}"#.utf8))
    #expect(Waits.words(old) == "")
}
