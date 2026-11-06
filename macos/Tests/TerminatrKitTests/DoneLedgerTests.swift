import Foundation
import Testing
@testable import TerminatrKit

private func task(_ id: Int) -> AppAPI.Task {
    AppAPI.Task(id: id, ref: "T\(id)", title: "Task \(id)", status: "done")
}

private func group(_ title: String, _ from: String, _ to: String, count: Int, open: Bool = false,
                   tasks: [AppAPI.Task] = [], next: Int = 0, archived: Int = 0) -> AppAPI.DoneGroup {
    AppAPI.DoneGroup(kind: open ? "day" : "week", title: title, from: from, to: to, count: count, archived: archived, open: open, tasks: tasks, next: next)
}

/// One day holding 200 tasks: 30 rows come open, the rest read on.
@Test func aBigDayReadsOn() {
    var l = DoneLedger()
    let day = group("Today", "2026-11-06", "2026-11-06", count: 200, open: true, tasks: (1...30).map(task), next: 30)
    #expect(l.groups == nil)
    let again = l.take(AppAPI.DoneTasks(tasks: [], groups: [day], total: 200))
    #expect(again.isEmpty && l.total == 200)
    #expect(l.isOpen(day) && l.tasks(in: day).count == 30 && l.more(in: day) == 170 && l.next(in: day) == 30)
    l.append(day, AppAPI.DoneTasks(tasks: (31...80).map(task), next: 80))
    #expect(l.tasks(in: day).count == 80 && l.more(in: day) == 120)
    // A later read keeps what was read on: it reads it again.
    let more = l.take(AppAPI.DoneTasks(tasks: [], groups: [day], total: 200))
    #expect(more.count == 1 && more[0].limit == 80)
    #expect(l.tasks(in: day).count == 80)
    l.set(day, AppAPI.DoneTasks(tasks: (1...200).map(task)))
    #expect(l.more(in: day) == 0)
}

/// A fold opens with a read, and only then; an open day folds and opens
/// again with the rows it came with.
@Test func foldsOpenWithARead() {
    var l = DoneLedger()
    let day = group("Today", "2026-11-06", "2026-11-06", count: 1, open: true, tasks: [task(9)])
    let week = group("19–25 Oct", "2026-10-19", "2026-10-25", count: 3, archived: 1)
    let first = l.take(AppAPI.DoneTasks(tasks: [], groups: [day, week], total: 4))
    #expect(first.isEmpty)
    #expect(!l.isOpen(week) && l.tasks(in: week).isEmpty && l.more(in: week) == 0)
    let reads = l.press(week)
    #expect(reads && l.isOpen(week))
    l.set(week, AppAPI.DoneTasks(tasks: [task(3), task(2), task(1)]))
    #expect(l.tasks(in: week).map(\.ref) == ["T3", "T2", "T1"])
    // Open again needs no read while the rows are there.
    let folded = l.press(week)
    #expect(!folded && !l.isOpen(week) && l.tasks(in: week).isEmpty)
    let opened = l.press(week)
    #expect(!opened && l.tasks(in: week).count == 3)
    let dayFolded = l.press(day)
    #expect(!dayFolded && !l.isOpen(day))
    let dayOpened = l.press(day)
    #expect(!dayOpened && l.tasks(in: day).count == 1)
    // The next read keeps the fold open and reads its rows again.
    let again = l.take(AppAPI.DoneTasks(tasks: [], groups: [day, week], total: 4))
    #expect(again.count == 1 && again[0].group.key == week.key && again[0].limit == DoneLedger.page)
    #expect(l.tasks(in: week).count == 3)
}

/// A tm before Done's groups lists the board's done tasks whole, with the
/// project alone as its params; 55 answers the groups.
@Test func doneGroupsNeedAPI55() {
    var h = AppAPI.Hello(appApi: 54, tm: "v0.82.0", build: "x", methods: [AppAPI.Methods.taskDone.name])
    #expect(Compatibility.listsDone(h) && !Compatibility.groupsDone(h))
    h.appApi = 55
    #expect(Compatibility.groupsDone(h))
    #expect(AppAPI.Methods.taskDoneAll.name == AppAPI.Methods.taskDone.name)
}

/// Under its day's heading a row says the time, and nothing for a day.
@Test func aRowUnderItsDaySaysTheTime() {
    var c = Calendar(identifier: .gregorian)
    c.timeZone = TimeZone(identifier: "Asia/Tokyo")!
    #expect(Dashboard.finishedTime("2026-11-05T20:30:00Z", calendar: c) == "05:30")
    #expect(Dashboard.finishedTime("2026-11-05", calendar: c) == "")
    #expect(Dashboard.finishedTime("", calendar: c) == "")
}

/// What a tm before app API 55 reads: only the project.
@Test func theOldParamsAreTheProjectAlone() throws {
    let old = try JSONSerialization.jsonObject(with: JSONEncoder().encode(AppAPI.ProjectParams(project: "web"))) as? [String: Any]
    #expect(old?.count == 1)
}

/// An archived task says so; a row with no groups in the answer has none.
@Test func aDoneAnswerDecodes() throws {
    let old = try JSONDecoder().decode(AppAPI.DoneTasks.self, from: Data(#"{"tasks":[{"id":1,"ref":"T1","title":"x","status":"done"}]}"#.utf8))
    #expect(old.groups.isEmpty && old.total == 0 && old.next == 0 && !old.tasks[0].archived)
    let new = try JSONDecoder().decode(AppAPI.DoneTasks.self, from: Data(#"""
    {"tasks":[],"total":2,"groups":[{"kind":"month","title":"September","from":"2026-09-01","to":"2026-09-30","count":2,"archived":2,
     "tasks":[{"id":1,"ref":"T1","title":"x","status":"done","archived":true}]}]}
    """#.utf8))
    #expect(new.groups[0].key == "2026-09-01:2026-09-30" && new.groups[0].archived == 2 && new.groups[0].tasks[0].archived && !new.groups[0].open)
}
