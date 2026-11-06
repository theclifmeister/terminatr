import Foundation
import Testing
@testable import TerminatrKit

// A task's priority (T927): the board lists each group by it, must
// first and none last, keeping the board's order within one; the Priority
// menu shows only where the link's tm sets it.

@Test func priorityRanks() {
    #expect(Priority.rank("must") < Priority.rank("should"))
    #expect(Priority.rank("should") < Priority.rank("could"))
    #expect(Priority.rank("could") < Priority.rank(""))
    #expect(Priority.rank("urgent") == Priority.rank(""))
    #expect(Priority.choices == ["must", "should", "could", ""])
    #expect(Priority.word("") == "none" && Priority.word("must") == "must")
    #expect(Priority.settable("ready") && Priority.settable("review") && !Priority.settable("done") && !Priority.settable("dropped"))
}

@Test func boardSortsByPriority() {
    let p = AppAPI.Project(slug: "demo", active: true,
                           board: [AppAPI.Task(id: 1, ref: "T1", title: "A", status: "ready"),
                                   AppAPI.Task(id: 2, ref: "T2", title: "B", status: "ready", priority: "could"),
                                   AppAPI.Task(id: 3, ref: "T3", title: "C", status: "open", priority: "should"),
                                   AppAPI.Task(id: 4, ref: "T4", title: "D", status: "ready", priority: "must"),
                                   AppAPI.Task(id: 5, ref: "T5", title: "E", status: "ready", priority: "must"),
                                   AppAPI.Task(id: 6, ref: "T6", title: "F", status: "open")],
                           usage: noUsage)
    #expect(Dashboard.onDeck(p).map(\.ref) == ["T4", "T5", "T2", "T1"])
    #expect(Dashboard.backlog(p).map(\.ref) == ["T3", "T6"])
}

@Test func prioritySetsOnlyWithTheMethodAndCap() {
    let server = AppAPI.Server(version: "1", protocol: 32, caps: [Priority.cap])
    let ok = AppAPI.Hello(appApi: 42, tm: "1", build: "", methods: [AppAPI.Methods.taskPriority.name], server: server)
    #expect(Priority.sets(ok))
    var view = ok
    view.methods = [] // a view-level device's hello lacks the write
    #expect(!Priority.sets(view))
    var old = ok
    old.server = AppAPI.Server(version: "1", protocol: 32, caps: [])
    #expect(!Priority.sets(old))
    var older = ok
    older.appApi = 41
    #expect(!Priority.sets(older))
}

// A done row's time (T1043): in the viewer's zone, the day only when only
// that is known, nothing when nothing dates it.
@Test func doneRowsSayWhenTheyFinished() {
    var cal = Calendar(identifier: .gregorian)
    cal.timeZone = TimeZone(secondsFromGMT: 3600)!
    let now = try! Date("2026-11-06T11:00:00Z", strategy: .iso8601)
    for (at, want) in [("", ""), ("soon", ""), ("2026-13-99x", ""),
                       ("2026-11-06T09:10:44Z", "10:10"),
                       ("2026-11-05T23:30:00Z", "00:30"), // today here, yesterday in UTC
                       ("2026-11-05T09:10:44Z", "5 Nov 10:10"),
                       ("2025-11-06T09:10:44Z", "6 Nov 2025 10:10"),
                       ("2026-10-05", "5 Oct"), ("2025-10-05", "5 Oct 2025")] {
        #expect(Dashboard.finished(at, now: now, calendar: cal) == want, "\(at)")
    }
}

// An older tm's task.done has no done_at: its tasks decode with none and
// keep the order they came in, filtered or not.
@Test func olderTMsDoneTasksKeepTheirOrder() throws {
    let json = #"{"tasks":[{"id":3,"ref":"T3","title":"Must","status":"done","priority":"must"},{"id":1,"ref":"T1","title":"Other","status":"done"},{"id":2,"ref":"T2","title":"Other too","status":"done"}]}"#
    let done = try JSONDecoder().decode(AppAPI.DoneTasks.self, from: Data(json.utf8)).tasks
    #expect(done.map(\.id) == [3, 1, 2])
    #expect(done.allSatisfy { $0.doneAt.isEmpty && Dashboard.finished($0.doneAt).isEmpty })
    #expect(Dashboard.filtered(done, by: "other").map(\.id) == [1, 2])
}
