import Foundation
import Testing
@testable import TerminatrKit

// The command palette (⌘K, T748 milestone E): where each row goes and
// what ⌘↩ does there (E1).

private func threadSession(_ id: String, thread: String, state: String = "idle", reason: String = "",
                           question: AppAPI.Question? = nil) -> AppAPI.Session {
    AppAPI.Session(id: id, role: "thread", project: "demo", thread: thread, cols: 80, rows: 24, created: t0, clients: 0,
                   state: state, reason: reason, question: question)
}

private let demo = AppAPI.Project(
    slug: "demo", active: true,
    needsYou: [AppAPI.Task(id: 3, ref: "T3", title: "Login page", status: "blocked"),
               AppAPI.Task(id: 1, ref: "T1", title: "Cache layer", status: "review")],
    threads: [thread("t-1", task: AppAPI.Task(id: 5, ref: "T5", title: "Network popup", status: "started"), session: "s-1"),
              thread("t-2", task: AppAPI.Task(id: 6, ref: "T6", title: "Sidebar CI", status: "ready"), session: "s-2"),
              thread("t-3", session: "s-3"),
              thread("t-4")],
    usage: noUsage)

private let sessions = [
    threadSession("s-1", thread: "t-1", state: "blocked", reason: "permission"),
    threadSession("s-2", thread: "t-2", state: "blocked", reason: "question", question: menu([backend])),
    threadSession("s-3", thread: "t-3"),
]

private let projects = [demo, AppAPI.Project(slug: "old", active: false, usage: noUsage)]

private func items() -> [PaletteItem] {
    let needs = Need.of(projects: projects, sessions: sessions, now: t0)
    return Palette.items(projects: projects, sessions: sessions, peers: [], needs: needs, now: t0)
}

private func item(_ id: String) -> PaletteItem? {
    items().first { $0.id == id }
}

@Test func paletteNeeds() throws {
    let approval = try #require(item("need:approval:s-1"))
    #expect(approval.go == .session(SessionRef(project: "demo", thread: "t-1")))
    #expect(approval.act == .approve(SessionRef(project: "demo", thread: "t-1")))
    #expect(approval.act?.verb == "allow")
    let question = try #require(items().first { $0.id.hasPrefix("need:question:s-2") })
    #expect(question.act == .answer(SessionRef(project: "demo", thread: "t-2")))
    // A blocked task: its coordinator is asked to delegate it.
    #expect(item("need:blocked:demo:T3")?.act == .delegate(project: "demo", task: "T3"))
    // In review: accepting it is the dashboard's, not the palette's.
    let review = try #require(item("need:review:demo:T1"))
    #expect(review.act == nil)
    #expect(review.go == .project("demo"))
}

@Test func paletteSessions() throws {
    // A running, idle thread takes a message; one without a session
    // only goes.
    #expect(item("session:demo/t-3")?.act == .prompt(SessionRef(project: "demo", thread: "t-3")))
    let stopped = try #require(item("session:demo/t-4"))
    #expect(stopped.act == nil)
    #expect(stopped.go == .session(SessionRef(project: "demo", thread: "t-4")))
    #expect(item("session:demo/t-1")?.title == "T5 thread t-1")
    // No coordinator runs: no row for it.
    #expect(item("session:demo/") == nil)
    // An inactive project is still somewhere to go.
    #expect(item("project:old")?.detail == "inactive")
}

@Test func paletteTasks() {
    // The tasks the app knows, once each: delegable ones (open, ready,
    // blocked) say so.
    let tasks = items().filter { $0.group == .tasks }
    #expect(tasks.map(\.id) == ["task:demo/T3", "task:demo/T1", "task:demo/T5", "task:demo/T6"])
    #expect(tasks.map { $0.act != nil } == [true, false, false, true])
    #expect(tasks.last?.detail == "demo · on deck")
    // A task with a thread goes to it.
    #expect(tasks[2].go == .session(SessionRef(project: "demo", thread: "t-1")))
}

@Test func paletteSearch() {
    let all = items() + [PaletteItem(id: "a", group: .actions, title: "Next thing that needs you")]
    // No query: the needs, the projects and the actions, no threads or tasks.
    let empty = Palette.search(all, "")
    #expect(!empty.contains { $0.group == .threads || $0.group == .tasks })
    #expect(empty.contains { $0.id == "a" })
    #expect(empty.first?.group == .needs)
    // Words anywhere in the title or the detail; titles that begin with
    // the query first in their group.
    let found = Palette.search(all, "sidebar")
    #expect(found.map(\.id).contains("session:demo/t-2"))
    #expect(found.map(\.id).contains("task:demo/T6"))
    #expect(Palette.search(all, "t6").filter { $0.group == .tasks }.map(\.id) == ["task:demo/T6"])
    let cache = Palette.search(all, "cache").filter { $0.group == .tasks }
    #expect(cache.map(\.id) == ["task:demo/T1"])
    #expect(Palette.search(all, "zzz").isEmpty)
}
