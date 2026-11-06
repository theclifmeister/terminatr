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
    board: [AppAPI.Task(id: 6, ref: "T6", title: "Sidebar CI", status: "ready"),
            AppAPI.Task(id: 7, ref: "T7", title: "Cache eviction", status: "ready"),
            AppAPI.Task(id: 8, ref: "T8", title: "Dock badge", status: "open")],
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
    // In review: accept or send it back.
    let review = try #require(item("need:review:demo:T1"))
    #expect(review.act == .review(project: "demo", task: "T1"))
    #expect(review.act?.verb == "review")
    #expect(review.go == .project("demo"))
    // ↩ on either shows its task in the task view; an approval has none.
    #expect(review.task == PaletteItem.TaskKey(project: "demo", task: "T1"))
    #expect(item("need:blocked:demo:T3")?.task == PaletteItem.TaskKey(project: "demo", task: "T3"))
    #expect(approval.task == nil)
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
    // The tasks the app knows, once each (the board's on deck and in the
    // backlog too): delegable ones (open, ready, blocked) say so, one in
    // review is reviewed.
    let tasks = items().filter { $0.group == .tasks }
    #expect(tasks.map(\.id) == ["task:demo/T3", "task:demo/T1", "task:demo/T5", "task:demo/T6", "task:demo/T7", "task:demo/T8"])
    #expect(tasks.map(\.act) == [.delegate(project: "demo", task: "T3"), .review(project: "demo", task: "T1"), nil,
                                  .delegate(project: "demo", task: "T6"), .delegate(project: "demo", task: "T7"),
                                  .delegate(project: "demo", task: "T8")])
    #expect(tasks[3].detail == "demo · on deck")
    #expect(tasks[5].detail == "demo · backlog")
    // A task with a thread goes to it; one on the board to its project.
    #expect(tasks[2].go == .session(SessionRef(project: "demo", thread: "t-1")))
    #expect(tasks[3].go == .session(SessionRef(project: "demo", thread: "t-2")))
    #expect(tasks[4].go == .project("demo"))
    // ↩ shows each in the task view; Open goes as above.
    #expect(tasks.allSatisfy { $0.task?.project == "demo" && $0.task?.task == $0.id.replacingOccurrences(of: "task:demo/", with: "") })
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
    #expect(cache.map(\.id) == ["task:demo/T1", "task:demo/T7"])
    #expect(Palette.search(all, "zzz").isEmpty)
}

// Paired servers (T1080): threads by title, open tasks from a background
// read while the pages switch is on, every row keyed by its server.

private func peerSession(_ id: String, project: String, thread: String, task: String = "") -> AppAPI.PeerSession {
    AppAPI.PeerSession(id: id, role: "thread", project: project, thread: thread, task: task, created: t0)
}

private func sharedProject(_ name: String = "demo", pages: Bool = true, stale: Bool = false) -> AppAPI.PeerProject {
    AppAPI.PeerProject(project: name, needsYou: 0, threads: 2, ready: 1, stale: stale,
                       sessions: [peerSession("s-9", project: name, thread: "t-1", task: "T5")],
                       watched: [AppAPI.PeerThread(id: "t-1", title: "Rate limiter", task: "T5", session: "s-9"),
                                 AppAPI.PeerThread(id: "t-2", title: "Billing export"),
                                 AppAPI.PeerThread(id: "t-3", title: "Finished work", done: true)],
                       pages: pages)
}

private func peer(_ alias: String, name: String = "", state: String = "up", _ projects: [AppAPI.PeerProject]) -> AppAPI.Peer {
    AppAPI.Peer(alias: alias, name: name, state: state, projects: projects)
}

private func board(_ tasks: [AppAPI.Task], project: String = "demo") -> AppAPI.PeerPages {
    AppAPI.PeerPages(project: project, repos: 1, tasks: tasks)
}

private func read(_ peers: [AppAPI.Peer], _ answers: [String: AppAPI.PeerPages]) -> PaletteBoards {
    var b = PaletteBoards()
    for key in b.due(peers: peers, reads: true, now: t0) {
        if let a = answers[key.alias] { b.store(a, for: key) } else { b.failed(key) }
    }
    return b
}

private func paired(_ peers: [AppAPI.Peer], _ boards: PaletteBoards = PaletteBoards(), projects: [AppAPI.Project] = []) -> [PaletteItem] {
    Palette.items(projects: projects, sessions: [], peers: peers, needs: [], boards: boards, now: t0)
}

@Test func palettePairedThreadsAreFoundByTitle() throws {
    let all = paired([peer("st", name: "studio", [sharedProject()])])
    let running = try #require(Palette.search(all, "rate limiter").first)
    #expect(running.id == "session:st/demo/t-1")
    #expect(running.title == "T5 Rate limiter")
    #expect(running.detail == "demo on studio")
    #expect(running.go == .session(SessionRef(project: "demo", thread: "t-1", server: "st")))
    #expect(running.act == nil)
    // Watched without a session: still there, as the sidebar lists it.
    let stopped = try #require(Palette.search(all, "billing").first)
    #expect(stopped.id == "session:st/demo/t-2")
    #expect(stopped.title == "Billing export")
    // Still found by what found it before, and by the server's alias.
    #expect(Palette.search(all, "t-2").map(\.id).contains("session:st/demo/t-2"))
    #expect(Palette.search(all, "T5").map(\.id).contains("session:st/demo/t-1"))
    #expect(Palette.search(all, "studio billing").map(\.id) == ["session:st/demo/t-2"])
    #expect(Palette.search(all, "st billing").map(\.id) == ["session:st/demo/t-2"])
    // A done thread isn't one to go to, as on this server.
    #expect(!all.contains { $0.id == "session:st/demo/t-3" })
    // A stale mirror lists no more than it did.
    let stale = paired([peer("st", [sharedProject(stale: true)])])
    #expect(stale.filter { $0.group == .threads }.map(\.id) == ["session:st/demo/t-1"])
}

@Test func palettePairedThreadWithoutAWatchIsAsBefore() {
    let p = AppAPI.PeerProject(project: "demo", needsYou: 0, threads: 1, ready: 0,
                               sessions: [peerSession("s-9", project: "demo", thread: "t-1", task: "T5")])
    let rows = paired([peer("st", [p])]).filter { $0.group == .threads }
    #expect(rows.map(\.title) == ["T5 t-1"])
}

@Test func palettePairedTasksWithPagesOn() throws {
    let peers = [peer("st", name: "studio", [sharedProject()])]
    let boards = read(peers, ["st": board([AppAPI.Task(id: 5, ref: "T5", title: "Rate limiter", status: "started"),
                                           AppAPI.Task(id: 7, ref: "T7", title: "Invoice run", status: "ready"),
                                           AppAPI.Task(id: 8, ref: "T8", title: "Old thing", status: "done")])])
    let tasks = paired(peers, boards).filter { $0.group == .tasks }
    #expect(tasks.map(\.id) == ["task:st/demo/T5", "task:st/demo/T7"])
    let t = tasks[1]
    #expect(t.title == "T7 Invoice run")
    #expect(t.detail == "demo on studio · on deck")
    #expect(t.task == PaletteItem.TaskKey(project: "demo", task: "T7", server: "st"))
    #expect(t.go == .project("demo", server: "st"))
    // Read only: nothing for ⌘↩ to do there.
    #expect(t.act == nil)
    #expect(Palette.search(tasks, "invoice").map(\.id) == ["task:st/demo/T7"])
}

@Test func palettePairedTasksWithPagesOff() {
    let on = [peer("st", [sharedProject()])]
    let boards = read(on, ["st": board([AppAPI.Task(id: 7, ref: "T7", title: "Invoice run", status: "ready")])])
    // Never read while off.
    let off = [peer("st", [sharedProject(pages: false)])]
    var b = PaletteBoards()
    #expect(b.due(peers: off, reads: true, now: t0).isEmpty)
    // Turned off since: its rows go before the next read, then the board.
    #expect(!paired(off, boards).contains { $0.group == .tasks })
    var pruned = boards
    pruned.prune(peers: off, reads: true)
    #expect(pruned.tasks(PaletteBoards.Key(alias: "st", project: "demo")).isEmpty)
    // The server says so first (pages-off is an error the read reports).
    #expect(!paired(on, read(on, [:])).contains { $0.group == .tasks })
}

@Test func palettePairedTasksOffline() {
    let up = [peer("st", [sharedProject()]), peer("ho", [sharedProject()])]
    let boards = read(up, ["st": board([AppAPI.Task(id: 7, ref: "T7", title: "Invoice run", status: "ready")]),
                           "ho": board([AppAPI.Task(id: 7, ref: "T7", title: "Home job", status: "ready")])])
    let down = [peer("st", state: "reconnecting", [sharedProject()]), up[1]]
    // The server that went off loses its rows at once; the other keeps its.
    #expect(paired(down, boards).filter { $0.group == .tasks }.map(\.id) == ["task:ho/demo/T7"])
    // It isn't read while off, and its board is forgotten.
    var b = boards
    #expect(b.due(peers: down, reads: true, now: t0.addingTimeInterval(PaletteBoards.readEvery)).map(\.alias) == ["ho"])
    #expect(b.tasks(PaletteBoards.Key(alias: "st", project: "demo")).isEmpty)
    // An answer landing after it went off stays out.
    b.store(board([AppAPI.Task(id: 9, ref: "T9", title: "Late", status: "ready")]), for: PaletteBoards.Key(alias: "st", project: "demo"))
    #expect(b.tasks(PaletteBoards.Key(alias: "st", project: "demo")).isEmpty)
    // Back up: read at once.
    #expect(b.due(peers: up, reads: true, now: t0.addingTimeInterval(PaletteBoards.readEvery)).map(\.alias) == ["st"])
}

@Test func palettePairedTasksOlderTm() {
    // This tm doesn't read pages: nothing is read.
    let peers = [peer("st", [sharedProject()])]
    var b = PaletteBoards()
    #expect(PaletteBoards.readable(peers: peers, reads: false).isEmpty)
    #expect(b.due(peers: peers, reads: false, now: t0).isEmpty)
    // That server's older tm says no pages: the same.
    #expect(PaletteBoards.readable(peers: [peer("st", [sharedProject(pages: false)])], reads: true).isEmpty)
}

@Test func paletteBoardsReadNowAndThenAndStayBounded() {
    let peers = [peer("st", [sharedProject()])]
    var b = PaletteBoards()
    #expect(b.due(peers: peers, reads: true, now: t0).count == 1)
    // A reopened palette doesn't read again, nor while a read is out.
    #expect(b.due(peers: peers, reads: true, now: t0.addingTimeInterval(PaletteBoards.readEvery - 1)).isEmpty)
    #expect(b.due(peers: peers, reads: true, now: t0.addingTimeInterval(PaletteBoards.readEvery)).count == 1)
    // A board keeps a row's worth of its first open tasks.
    let many = (1...500).map { AppAPI.Task(id: $0, ref: "T\($0)", title: "Task \($0)", status: "open", notes: "long",
                                           steps: [AppAPI.Step(n: 1, text: "step", done: false)]) }
    let key = PaletteBoards.Key(alias: "st", project: "demo")
    b.store(board(many), for: key)
    #expect(b.tasks(key).count == PaletteBoards.maxTasks)
    #expect(b.tasks(key).allSatisfy { $0.steps.isEmpty && $0.notes.isEmpty })
    // A failed read: fewer rows, no retry before its time.
    b.failed(key)
    #expect(b.tasks(key).isEmpty)
    // So many boards at most.
    let wide = [peer("st", (0..<40).map { sharedProject("p\($0)") })]
    #expect(PaletteBoards.readable(peers: wide, reads: true).count == PaletteBoards.maxBoards)
}

@Test func palettePairedRowsAreKeyedByServer() throws {
    let peers = [peer("st", name: "studio", [sharedProject()]), peer("ho", name: "home", [sharedProject()])]
    let boards = read(peers, ["st": board([AppAPI.Task(id: 7, ref: "T7", title: "Studio job", status: "ready")]),
                              "ho": board([AppAPI.Task(id: 7, ref: "T7", title: "Home job", status: "open")])])
    let rows = paired(peers, boards, projects: [demo])
    let ids = rows.map(\.id)
    #expect(Set(ids).count == ids.count)
    let studio = try #require(rows.first { $0.id == "task:st/demo/T7" })
    let home = try #require(rows.first { $0.id == "task:ho/demo/T7" })
    #expect(studio.title == "T7 Studio job" && studio.detail == "demo on studio · on deck")
    #expect(home.title == "T7 Home job" && home.detail == "demo on home · backlog")
    #expect(studio.task != home.task)
    // This server's demo has a T7 too: it stays this server's.
    #expect(rows.first { $0.id == "task:demo/T7" }?.task == PaletteItem.TaskKey(project: "demo", task: "T7"))
}

@Test func paletteThisServerFirstOnATie() {
    let peers = [peer("st", [sharedProject()])]
    let boards = read(peers, ["st": board([AppAPI.Task(id: 7, ref: "T7", title: "Cache eviction there", status: "ready")])])
    let rows = paired(peers, boards, projects: [demo])
    #expect(Palette.search(rows, "T7").filter { $0.group == .tasks }.map(\.id) == ["task:demo/T7", "task:st/demo/T7"])
    #expect(Palette.search(rows, "demo").filter { $0.group == .projects }.map(\.id) == ["project:demo", "project:st/demo"])
    // Not a tie: a title that begins with the words still goes first.
    let army = AppAPI.Project(slug: "army", active: true, usage: noUsage)
    let found = Palette.search(paired([peer("st", [sharedProject("my-demo")])], projects: [army]), "my")
    #expect(found.filter { $0.group == .projects }.map(\.id) == ["project:st/my-demo", "project:army"])
}

@Test func paletteHoldsThePickedRowWhenABoardLands() {
    #expect(Palette.held(at: 1, from: ["a", "b", "c"], to: ["x", "a", "b", "c"]) == 2)
    #expect(Palette.held(at: 5, from: ["a", "b"], to: ["x", "a", "b"]) == 2)
    #expect(Palette.held(at: 1, from: ["a", "b"], to: ["a"]) == 1)
    #expect(Palette.held(at: 0, from: [], to: ["a"]) == 0)
}

@Test func peerTaskSheetFindsItsTask() {
    let p = board([AppAPI.Task(id: 7, ref: "T7", title: "Job", status: "ready")])
    #expect(PeerPages.task("T7", in: p, server: "studio") == .shared(p.tasks[0]))
    #expect(PeerPages.task("T8", in: p, server: "studio") == .failed("T8 isn't on demo's board on studio any more."))
}
