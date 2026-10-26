import Foundation
import Testing
@testable import TerminatrKit

// The window's selection (design T746): what needs, notifications,
// menu bar rows and cards show, and what a relaunch restores.

private func record(_ id: String, session: String = "") -> AppAPI.ThreadRecord {
    AppAPI.ThreadRecord(id: id, title: id, agent: "claude", worktree: "/w", state: "working", session: session,
                        created: t0, reports: 0, reportAcked: 0, usage: noUsage)
}

private let blocked = AppAPI.Task(id: 3, ref: "T3", title: "Licence", status: "blocked", thread: "t-0675")
private let review = AppAPI.Task(id: 4, ref: "T4", title: "Docs", status: "review")
private let projects = [
    AppAPI.Project(slug: "terminatr", active: true, needsYou: [blocked, review],
                   threads: [AppAPI.Thread(record: record("t-0675", session: "s-1"), task: blocked)], usage: noUsage),
    AppAPI.Project(slug: "site", active: true, usage: noUsage),
]
private let sessions = [
    session("s-1", state: "blocked", reason: "question",
            question: menu([AppAPI.QuestionItem(question: "Which?", options: [.init(label: "A"), .init(label: "B")])])),
    AppAPI.Session(id: "s-c", role: "coordinator", project: "site", cols: 80, rows: 24, created: t0, clients: 0,
                   state: "blocked", reason: "permission"),
    AppAPI.Session(id: "s-sh", role: "shell", cols: 80, rows: 24, created: t0, clients: 0),
]
private let peers = [
    AppAPI.Peer(alias: "mini", state: "up", projects: [
        AppAPI.PeerProject(project: "demo", needsYou: 1, threads: 1, ready: 0, sessions: [
            AppAPI.PeerSession(id: "mini/s1", role: "coordinator", project: "demo", created: t0),
            AppAPI.PeerSession(id: "mini/s2", role: "thread", project: "demo", thread: "t-0001", created: t0),
        ]),
    ], pane: true, panes: "view"),
]

private func route(_ project: String?, _ session: String = "") -> Selection? {
    Selection.route(project: project, session: session, projects: projects, sessions: sessions, peers: peers)
}

@Test func routesNeeds() throws {
    // Every need of these lists shows something in the window: a
    // session's need its session, a project's its dashboard.
    let needs = Need.of(projects: projects, sessions: sessions, peers: peers)
    #expect(!needs.isEmpty)
    for n in needs {
        let s = try #require(route(n.project, n.session), "\(n.id)")
        if !n.session.isEmpty, n.kind == .question || n.kind == .approval {
            guard case .session = s else {
                Issue.record("\(n.id) shows \(s)")
                continue
            }
        }
    }
    let q = try #require(needs.first { $0.kind == .question && $0.session == "s-1" })
    #expect(route(q.project, q.session) == .session(SessionRef(project: "terminatr", thread: "t-0675")))
    let a = try #require(needs.first { $0.kind == .approval })
    #expect(route(a.project, a.session) == .session(SessionRef(project: "site")))
    let r = try #require(needs.first { $0.kind == .review })
    #expect(route(r.project, r.session) == .project("terminatr"))
    let paired = try #require(needs.first { $0.kind == .network })
    #expect(route(paired.project, paired.session) == .project("demo", server: "mini"))
}

@Test func routesNotificationsAndRows() {
    // A notification's userInfo: project and session, either may be
    // empty, or name what's gone since.
    #expect(route("terminatr", "s-1") == .session(SessionRef(project: "terminatr", thread: "t-0675")))
    #expect(route("terminatr", "s-gone") == .project("terminatr"))
    #expect(route("terminatr", "s-sh") == .project("terminatr")) // a shell has no view of its own
    #expect(route("site") == .project("site"))
    #expect(route("mini/demo") == .project("demo", server: "mini"))
    #expect(route("mini/other") == nil)
    #expect(route("studio/demo") == nil)
    #expect(route("gone") == nil)
    #expect(route(nil) == nil) // Open terminatr: the window as it was
    #expect(route("") == nil)
}

@Test func routesCards() {
    let p = projects[0]
    #expect(Selection.card(p.threads[0], in: p.slug) == .session(SessionRef(project: "terminatr", thread: "t-0675")))
    // Needs you: the task's thread, else the coordinator.
    #expect(Selection.of(task: blocked, in: p) == .session(SessionRef(project: "terminatr", thread: "t-0675")))
    #expect(Selection.of(task: review, in: p) == .session(SessionRef(project: "terminatr")))
    #expect(Selection.of(task: AppAPI.Task(id: 5, ref: "T5", title: "x", status: "blocked", thread: "t-9999"), in: p)
        == .session(SessionRef(project: "terminatr")))
}

@Test func selectionExists() {
    func exists(_ s: Selection) -> Bool { s.exists(projects: projects, peers: peers) }
    #expect(exists(.project("site")))
    #expect(!exists(.project("gone")))
    #expect(exists(.session(SessionRef(project: "site")))) // a coordinator shows while it isn't running
    #expect(exists(.session(SessionRef(project: "terminatr", thread: "t-0675"))))
    #expect(!exists(.session(SessionRef(project: "terminatr", thread: "t-0001"))))
    #expect(exists(.project("demo", server: "mini")))
    #expect(exists(.session(SessionRef(project: "demo", thread: "t-0001", server: "mini"))))
    #expect(!exists(.session(SessionRef(project: "demo", thread: "t-0002", server: "mini"))))
    #expect(!exists(.project("demo", server: "studio")))
}

@Test func history() {
    let a = Selection.project("terminatr"), b = Selection.session(SessionRef(project: "terminatr")), c = Selection.project("site")
    var n = Navigation()
    #expect(!n.canGoBack)
    n.select(a)
    #expect(!n.canGoBack) // nothing was shown before
    n.select(b)
    n.select(b) // the same again is no step
    n.select(c)
    #expect(n.back == [a, b])
    n.goBack()
    #expect(n.selection == b)
    n.goBack()
    #expect(n.selection == a)
    #expect(!n.canGoBack)
    n.goBack() // nothing more: stays
    #expect(n.selection == a)
    n.goForward()
    #expect(n.selection == b)
    #expect(n.canGoForward)
    n.select(c) // a new step drops the way forward
    #expect(!n.canGoForward)
    #expect(n.back == [a, b])

    var long = Navigation()
    for i in 0...(Navigation.historyLimit + 10) {
        long.select(.project("p\(i)"))
    }
    #expect(long.back.count == Navigation.historyLimit)
    #expect(long.back.last == .project("p\(Navigation.historyLimit + 9)"))
}

@Test func prune() {
    let thread = Selection.session(SessionRef(project: "terminatr", thread: "t-0675"))
    let gone = Selection.session(SessionRef(project: "terminatr", thread: "t-0001"))
    func keep(_ s: Selection) -> Bool { s.exists(projects: projects, peers: peers) }

    var n = Navigation()
    n.select(thread)
    n.select(.project("gone"))
    n.select(thread)
    n.select(gone)
    n.prune(fallback: .project("site"), keep: keep)
    // A removed thread falls back to its project; the history keeps
    // what's left, without the same twice in a row.
    #expect(n.selection == .project("terminatr"))
    #expect(n.back == [thread])

    var o = Navigation(selection: .project("gone"))
    o.prune(fallback: .project("site"), keep: keep)
    #expect(o.selection == .project("site"))
    var none = Navigation()
    none.prune(fallback: .project("site"), keep: keep)
    #expect(none.selection == .project("site"))
    #expect(!none.canGoBack)
}

@Test func restore() throws {
    var n = Navigation()
    n.select(.project("terminatr"))
    n.select(.session(SessionRef(project: "demo", thread: "t-0001", server: "mini")))
    n.collapsed = [Navigation.needsSection, Navigation.sectionKey(server: "mini", project: "demo")]
    n.showInactive = true
    n.unfolded = [Navigation.moreKey(project: "terminatr")]
    let back = Navigation.restore(n.encoded())
    #expect(back == n)
    #expect(back.selection == .session(SessionRef(project: "demo", thread: "t-0001", server: "mini")))
    #expect(back.canGoBack)

    // A save from before the inactive fold and the "N more" rows.
    let old = Navigation.restore(Data(#"{"selection": {"project": {"_0": "site", "server": ""}}, "back": [], "forward": [], "collapsed": ["needs"]}"#.utf8))
    #expect(old.selection == .project("site"))
    #expect(old.collapsed == ["needs"])
    #expect(!old.showInactive && old.unfolded.isEmpty)

    // Nothing saved, or what this release can't read: afresh.
    #expect(Navigation.restore(nil) == Navigation())
    #expect(Navigation.restore(Data("{\"selection\": 7}".utf8)) == Navigation())
    #expect(Navigation.restore(Data("garbage".utf8)) == Navigation())
}

@Test func warmSet() {
    var w = WarmSet<String>(limit: 3)
    #expect(w.touch("a").isEmpty)
    #expect(w.touch("b").isEmpty)
    #expect(w.touch("c").isEmpty)
    #expect(w.touch("a").isEmpty) // back to the front
    #expect(w.touch("d") == ["b"])
    #expect(w.keys == ["c", "a", "d"])
    let removed = w.remove("a"), again = w.remove("a")
    #expect(removed && !again)
    #expect(w.touch("e").isEmpty)
}
