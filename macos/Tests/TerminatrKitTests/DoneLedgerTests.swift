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
/// project alone as its params; 57 answers the groups.
@Test func doneGroupsNeedAPI57() {
    var h = AppAPI.Hello(appApi: 56, tm: "v0.82.0", build: "x", methods: [AppAPI.Methods.taskDone.name])
    #expect(Compatibility.listsDone(h) && !Compatibility.groupsDone(h))
    h.appApi = 57
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

/// What a tm before app API 57 reads: only the project.
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

private func line(_ label: String, repo: String = "", at offset: Int) -> AppAPI.DoneRelease {
    AppAPI.DoneRelease(label: label, time: "05:03", repo: repo, tag: "v\(label)", at: "2026-11-06T05:03:00Z", offset: offset)
}

private func words(_ entries: [DoneLedger.Entry]) -> [String] {
    entries.map {
        switch $0 {
        case .task(let t): t.ref
        case .release(let r): r.label
        }
    }
}

/// Lines sit where tm says among the rows, two at one minute in the order
/// they came; one past the rows read waits for them; a fold hides its own.
@Test func releaseLinesSitAmongTheRows() {
    var l = DoneLedger()
    var day = group("Today", "2026-11-06", "2026-11-06", count: 3, open: true, tasks: [task(1), task(2)], next: 2)
    day.releases = [line("a 1.0", repo: "a", at: 0), line("b 2.0", repo: "b", at: 1), line("b 1.9", repo: "b", at: 1), line("a 0.9", repo: "a", at: 3)]
    let first = l.take(AppAPI.DoneTasks(tasks: [], groups: [day], total: 3))
    #expect(first.isEmpty)
    #expect(words(l.entries(in: day)) == ["a 1.0", "T1", "b 2.0", "b 1.9", "T2"])
    l.append(day, AppAPI.DoneTasks(tasks: [task(3)]))
    #expect(words(l.entries(in: day)) == ["a 1.0", "T1", "b 2.0", "b 1.9", "T2", "T3", "a 0.9"])
    // The same tag in two repos: each line keeps its row.
    #expect(Set(l.entries(in: day).map(\.id)).count == 7)
    _ = l.press(day)
    #expect(l.entries(in: day).isEmpty)
}

/// No tags, no lines; and with one repo, nothing to filter by.
@Test func noReleasesNoLines() {
    var l = DoneLedger()
    let day = group("Today", "2026-11-06", "2026-11-06", count: 1, open: true, tasks: [task(1)])
    _ = l.take(AppAPI.DoneTasks(tasks: [], groups: [day], total: 1))
    #expect(words(l.entries(in: day)) == ["T1"] && l.repos.isEmpty)
}

/// Changing the repo forgets the rows of the last, not which folds are open.
@Test func aRepoFilterReadsAgain() {
    var l = DoneLedger()
    let week = group("19–25 Oct", "2026-10-19", "2026-10-25", count: 3)
    let repos = [AppAPI.DoneRepo(repo: "a", done: 2, releases: 128), AppAPI.DoneRepo(repo: "none", done: 1)]
    _ = l.take(AppAPI.DoneTasks(tasks: [], groups: [week], total: 3, repos: repos))
    #expect(l.repos == repos)
    let reads = l.press(week)
    #expect(reads)
    l.set(week, AppAPI.DoneTasks(tasks: [task(1)]))
    l.refilter()
    #expect(l.groups == nil && l.tasks(in: week).isEmpty && l.isOpen(week))
    let again = l.take(AppAPI.DoneTasks(tasks: [], groups: [week], total: 1, repos: repos))
    #expect(again.first?.limit == DoneLedger.page)
    #expect(DoneRepoWords.title("") == "All repos" && DoneRepoWords.title("none") == "No repo" && DoneRepoWords.title("a") == "a")
    #expect(DoneRepoWords.releases(1) == "1 release" && DoneRepoWords.releases(6) == "6 releases")
}

/// A tm before app API 68 refuses repo in task.done's params.
@Test func olderTMIsNotSentARepo() throws {
    var h = AppAPI.Hello(appApi: 67, tm: "v0.82.0", build: "x", methods: [AppAPI.Methods.taskDone.name])
    #expect(!Compatibility.filtersDone(h))
    h.appApi = 68
    #expect(Compatibility.filtersDone(h))
    let old = AppAPI.TaskDoneNoRepoParams(AppAPI.TaskDoneParams(project: "p", groups: true, tz: "UTC"))
    let keys = try JSONSerialization.jsonObject(with: JSONEncoder().encode(old)) as? [String: Any]
    #expect(keys?["repo"] == nil && keys?["project"] as? String == "p" && keys?["groups"] as? Bool == true)
}

/// The line at the user's last action: among an open group's rows, gone with
/// the fold, above a heading for a day no group holds, or below them all.
@Test func theLineStandsWhereTmSays() {
    let day = AppAPI.DoneGroup(kind: "day", title: "Today", from: "2026-11-06", to: "2026-11-06", count: 3, open: true, tasks: (1...3).map(task))
    let week = AppAPI.DoneGroup(kind: "week", title: "19–25 Oct", from: "2026-10-19", to: "2026-10-25", count: 2)
    var l = DoneLedger()
    _ = l.take(AppAPI.DoneTasks(tasks: [], groups: [day, week], total: 5))
    #expect(l.mark == nil && l.markBelow == nil && l.markAbove(day) == nil && l.markRows(in: day) == nil)

    func read(_ mark: AppAPI.DoneMark) { _ = l.take(AppAPI.DoneTasks(tasks: [], groups: [day, week], total: 5, mark: mark)) }
    read(AppAPI.DoneMark(at: "x", title: "you last acted at 10:00", group: 0, rows: 2))
    #expect(l.markRows(in: day) == 2 && l.markRows(in: week) == nil && l.markAbove(day) == nil && l.markBelow == nil)
    let folded = l.press(day)
    #expect(!folded && l.markRows(in: day) == nil)
    let opened = l.press(day)
    #expect(!opened && l.markRows(in: day) == 2)

    // Fewer rows read than tm counted: the line stays below what is there.
    read(AppAPI.DoneMark(at: "x", title: "t", group: 0, rows: 9))
    #expect(l.markRows(in: day) == 3)

    read(AppAPI.DoneMark(at: "x", title: "t", group: 1, between: true))
    #expect(l.markAbove(week)?.title == "t" && l.markAbove(day) == nil && l.markRows(in: week) == nil && l.markBelow == nil)

    read(AppAPI.DoneMark(at: "x", title: "t", group: 2, between: true))
    #expect(l.markBelow?.title == "t" && l.markAbove(day) == nil && l.markAbove(week) == nil)

    // The answer without it clears it.
    _ = l.take(AppAPI.DoneTasks(tasks: [], groups: [day, week], total: 5))
    #expect(l.mark == nil && l.markBelow == nil)
}

/// A tm before app API 67 sends no line; one that does says where it stands.
@Test func aMarkDecodes() throws {
    let old = try JSONDecoder().decode(AppAPI.DoneTasks.self, from: Data(#"{"tasks":[],"total":1}"#.utf8))
    #expect(old.mark == nil)
    let new = try JSONDecoder().decode(AppAPI.DoneTasks.self, from: Data(#"{"tasks":[],"mark":{"at":"2026-11-05T22:14:00Z","title":"you last acted at 23:14 on the iPhone","group":1}}"#.utf8))
    #expect(new.mark?.title == "you last acted at 23:14 on the iPhone" && new.mark?.rows == 0 && new.mark?.between == false)
}

/// The line at the last action and release lines in one gap stand by time.
@Test func theMarkStandsAmongReleaseLinesByTime() throws {
    var l = DoneLedger()
    var day = group("Today", "2026-11-06", "2026-11-06", count: 2, open: true, tasks: [task(1), task(2)])
    var newer = line("2.0", at: 1), older = line("1.9", at: 1)
    newer.at = "2026-11-06T09:00:00Z"
    older.at = "2026-11-06T07:00:00Z"
    day.releases = [newer, older]
    let mark = AppAPI.DoneMark(at: "2026-11-06T08:00:00Z", title: "you last acted at 08:00", group: 0, rows: 1)
    let first = l.take(AppAPI.DoneTasks(tasks: [], groups: [day], total: 2, mark: mark))
    #expect(first.isEmpty)
    #expect(words(l.entries(in: day)) == ["T1", "2.0", "1.9", "T2"])
    // Below T1 and 2.0, above 1.9.
    #expect(l.markEntry(in: day) == 2)
    let other = group("Yesterday", "2026-11-05", "2026-11-05", count: 1, open: true, tasks: [task(3)])
    #expect(l.markEntry(in: other) == nil)
}
