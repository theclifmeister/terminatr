import Foundation
import Testing
@testable import TerminatrKit

private func task(_ n: Int, _ status: String, notes: String = "", steps: [AppAPI.Step] = [], thread: String = "",
                  updated: String = "") -> AppAPI.Task {
    AppAPI.Task(id: n, ref: "T\(n)", title: "task \(n)", status: status, notes: notes, steps: steps, thread: thread, updated: updated)
}

/// A task that needs the user or a thread works on comes whole, with
/// its thread; a board task comes partial until task.get fills it, and
/// a fill the watch has moved past is dropped.
@Test func taskDetailFindsTheTask() {
    var th = thread("t-1", task: task(2, "started", steps: [AppAPI.Step(n: 1, text: "a", done: true)]))
    th.record.task = "T2"
    let p = AppAPI.Project(slug: "web", active: true, needsYou: [task(1, "review", thread: "t-1")], threads: [th],
                           board: [task(3, "ready", updated: "2026-10-27")], usage: noUsage)
    let review = TaskDetail.of("T1", in: p)
    #expect(review?.thread?.record.id == "t-1" && review?.partial == false)
    #expect(TaskDetail.of("T2", in: p)?.task.steps.count == 1)
    #expect(TaskDetail.of("T2", in: p)?.thread?.record.id == "t-1")
    let board = TaskDetail.of("T3", in: p)
    #expect(board?.partial == true && board?.thread == nil)
    #expect(TaskDetail.of("T9", in: p) == nil)

    let full = task(3, "ready", notes: "Why.", updated: "2026-10-27")
    #expect(board?.filled(full).task.notes == "Why.")
    #expect(board?.filled(full).partial == false)
    #expect(board?.filled(task(3, "started", notes: "Why.", updated: "2026-10-27")).task.notes == "")
    #expect(board?.filled(nil).partial == true)
}

/// Blocked on is the latest "blocked (date):" note, as the TUI's.
@Test func taskDetailBlockedOn() {
    let notes = "Start.\n\nblocked (2026-10-01): old\n\nblocked (2026-10-02): needs the\nAPI key"
    #expect(TaskDetail(project: "p", task: task(1, "blocked", notes: notes)).blockedOn == "needs the API key")
    #expect(TaskDetail(project: "p", task: task(1, "blocked", notes: "Just notes.")).blockedOn == "")
    #expect(TaskDetail(project: "p", task: task(1, "ready", notes: notes)).blockedOn == nil)
}

/// How to check a task in review: the report's ## Check, then the
/// notes that say so; where its PR stands, from the ticker's state.
@Test func taskDetailReview() {
    var th = thread("t-1", pr: 12)
    th.check = ["Run tm x"]
    th.prState = "MERGED"
    let notes = "review (2026-10-05): How to check: open the app\n\nCheck:\n\nTo check: see\nthe log"
    let d = TaskDetail(project: "p", task: task(1, "review", notes: notes), thread: th)
    #expect(d.checks == ["Run tm x", "open the app", "see the log"])
    #expect(d.ship == TaskDetail.Ship(text: "PR #12 merged", state: .merged))
    th.prState = "OPEN"
    #expect(TaskDetail(project: "p", task: task(1, "review"), thread: th).ship?.text == "PR #12 open, not merged yet")
    th.prState = "CLOSED"
    #expect(TaskDetail(project: "p", task: task(1, "review"), thread: th).ship?.state == .closed)
    th.pr = 0
    th.reportPr = "https://github.com/o/r/pull/3"
    #expect(TaskDetail(project: "p", task: task(1, "review"), thread: th).ship?.text == "https://github.com/o/r/pull/3")
    #expect(TaskDetail(project: "p", task: task(1, "review")).ship == nil)
    #expect(TaskDetail(project: "p", task: task(1, "review")).checks == [])
    #expect(TaskDetail(project: "p", task: task(1, "started"), thread: th).checks == nil)
}

@Test func taskDetailSteps() {
    let steps = [AppAPI.Step(n: 1, text: "a", done: true), AppAPI.Step(n: 2, text: "b", done: false)]
    #expect(TaskDetail(project: "p", task: task(1, "started", steps: steps)).stepsDone == "1 of 2 steps done")
}

@Test func taskViewNeedsAPI14() {
    var h = AppAPI.Hello(appApi: 14, tm: "v0.57.0", build: "x")
    #expect(!Compatibility.getsTasks(h))
    h.methods = [AppAPI.Methods.taskGet.name]
    #expect(Compatibility.getsTasks(h))
    h.appApi = 13
    #expect(!Compatibility.getsTasks(h))
}
