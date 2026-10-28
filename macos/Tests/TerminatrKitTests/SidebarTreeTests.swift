import Foundation
import Testing
@testable import TerminatrKit

// The sidebar lists the TUI's rows (T762): its projects, coordinators
// and threads, in its order, folded as it folds them; the needs on top
// are the app's own, cut to three.

/// needsYou counts its board's Needs you tasks (counts).
private func project(_ slug: String, active: Bool = true, pinned: Bool = false, needsYou: Int = 0, questions: Int = 0,
                     threads: [AppAPI.Thread] = []) -> AppAPI.Project {
    AppAPI.Project(slug: slug, active: active, pinned: pinned, questions: questions, threads: threads,
                   counts: needsYou > 0 ? ["needs_you": needsYou] : [:], usage: noUsage)
}

private func thread(_ id: String, done: Bool = false, session: String = "") -> AppAPI.Thread {
    AppAPI.Thread(record: AppAPI.ThreadRecord(id: id, title: id, agent: "claude", worktree: "/w", state: "working",
                                              session: session, created: t0, reports: 0, reportAcked: 0, done: done,
                                              usage: noUsage))
}

private func coordinator(state: String = "working", reason: String = "") -> AppAPI.Session {
    AppAPI.Session(id: "s-c", role: "coordinator", project: "p", cols: 80, rows: 24, created: t0, clients: 0,
                   state: state, reason: reason)
}

@Test func projectOrder() {
    // As the TUI's TestTreeOrder: active first, the pinned ones on top
    // in their own order, then the inactive ones, pinned or not.
    let ps = [project("a"), project("b", active: false, pinned: true), project("c", pinned: true),
              project("d", active: false), project("e")]
    #expect(SidebarTree.active(ps).map(\.slug) == ["c", "a", "e"])
    #expect(SidebarTree.inactive(ps).map(\.slug) == ["b", "d"])
}

/// A case of the fixture the TUI's TestSidebarAppFixture writes from
/// its buildTree (internal/tui/testdata/sidebar-app.json).
private struct TreeCase: Decodable {
    struct P: Decodable {
        let slug: String
        let active: Bool
        let pinned: Bool?
        let needs_you: Int?
        let questions: Int?
        let threads: [T]?
    }
    struct T: Decodable {
        let id: String
        let session: String?
        let needs_you: String?
        let done: Bool?
        let reports: Int?
        let report_acked: Int?
    }
    struct S: Decodable {
        let id: String
        let role: String
        let project: String
        let thread: String?
        let state: String?
    }
    struct Peer: Decodable {
        let alias: String
        let projects: [PP]
    }
    struct PP: Decodable {
        let project: String
        let stale: Bool?
        let sessions: [S]?
        let threads: [T]?
    }
    let name: String
    let projects: [P]
    let sessions: [S]?
    let peers: [Peer]?
    let folded: [String]?
    let expanded: [String]?
    let inactive_open: Bool?
    let needs_first: Bool?
    let rows: [String]
}

@Test func rowsAsTheTUI() throws {
    // The same file the TUI's test checks buildTree against: change the
    // rule on one side alone and one of the two fails.
    let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../../internal/tui/testdata/sidebar-app.json").standardized
    struct File: Decodable { let cases: [TreeCase] }
    let cases = try JSONDecoder().decode(File.self, from: Data(contentsOf: url)).cases
    #expect(cases.count >= 10)
    for c in cases {
        let ps = c.projects.map { p in
            project(p.slug, active: p.active, pinned: p.pinned ?? false, needsYou: p.needs_you ?? 0, questions: p.questions ?? 0,
                    threads: (p.threads ?? []).map { t in
                var th = thread(t.id, done: t.done ?? false, session: t.session ?? "")
                th.record.reports = t.reports ?? 0
                th.record.reportAcked = t.report_acked ?? 0
                if let q = t.needs_you {
                    th.status = AppAPI.ThreadStatus(percent: 0, stepsDone: 0, stepsTotal: 0, todosDone: 0, todosTotal: 0,
                                                    needsYou: q, selfPercent: -1, updated: t0)
                }
                return th
            })
        }
        let ss = (c.sessions ?? []).map { s in
            AppAPI.Session(id: s.id, role: s.role, project: s.project, cols: 80, rows: 24, created: t0, clients: 0, state: s.state ?? "")
        }
        // The bridge's copy of the mirror: a pane's id is "<alias>/<id>".
        let peers = (c.peers ?? []).map { pe in
            AppAPI.Peer(alias: pe.alias, state: "up", projects: pe.projects.map { pp in
                AppAPI.PeerProject(project: pp.project, needsYou: 0, threads: 0, ready: 0, stale: pp.stale ?? false,
                                   sessions: (pp.sessions ?? []).map { s in
                                       AppAPI.PeerSession(id: "\(pe.alias)/\(s.id)", role: s.role, project: s.project,
                                                          thread: s.thread ?? "", created: t0, state: s.state ?? "")
                                   },
                                   watched: (pp.threads ?? []).map { t in
                                       AppAPI.PeerThread(id: t.id, title: t.id, session: t.session.map { "\(pe.alias)/\($0)" } ?? "",
                                                         needsYou: t.needs_you != nil, done: t.done ?? false)
                                   })
            })
        }
        var nav = Navigation()
        for f in c.folded ?? [] {
            if f.hasPrefix("@") {
                // A paired server's group, or its project: "@<alias>[/<slug>]".
                let parts = f.dropFirst().split(separator: "/", maxSplits: 1).map(String.init)
                nav.collapsed.insert(Navigation.sectionKey(server: parts[0], project: parts.count > 1 ? parts[1] : ""))
            } else {
                nav.fold(Navigation.sectionKey(server: "", project: f))
            }
        }
        for slug in c.expanded ?? [] { nav.unfold(Navigation.sectionKey(server: "", project: slug)) }
        nav.showInactive = c.inactive_open ?? false
        #expect(SidebarTree.rows(ps, sessions: ss, peers: peers, nav: nav, needsFirst: c.needs_first ?? false) == c.rows, "\(c.name)")
    }
}

@Test func projectOpens() {
    let p = project("p"), old = project("old", active: false)
    var nav = Navigation()
    // An active project lists its rows until ← folds it; an inactive
    // one only once → expands it, as the TUI's view.Tree.
    #expect(SidebarTree.opens(p, nav: nav))
    #expect(!SidebarTree.opens(old, nav: nav))
    nav.fold(Navigation.sectionKey(server: "", project: "p"))
    nav.unfold(Navigation.sectionKey(server: "", project: "old"))
    #expect(!SidebarTree.opens(p, nav: nav))
    #expect(SidebarTree.opens(old, nav: nav))
    // Folded, it still lists the session the window shows (the app's own).
    nav.select(.session(SessionRef(project: "p", thread: "t-1")))
    #expect(SidebarTree.opens(p, nav: nav))
    // A paired server's project of the same name isn't this one.
    nav.select(.session(SessionRef(project: "p", server: "mini")))
    #expect(!SidebarTree.opens(p, nav: nav))
}

@Test func pairedRows() {
    let s = { (id: String, role: String, thread: String) in
        AppAPI.PeerSession(id: "mini/\(id)", role: role, project: "acme", thread: thread, task: thread.isEmpty ? "" : "T1", created: t0)
    }
    var p = AppAPI.PeerProject(project: "acme", needsYou: 0, threads: 2, ready: 0,
                               sessions: [s("s-1", "thread", "t-1"), s("s-c", "coordinator", "")],
                               watched: [AppAPI.PeerThread(id: "t-0", title: "idle", task: "T9"),
                                         AppAPI.PeerThread(id: "t-1", title: "runs", session: "mini/s-1")])
    // Its coordinator first, then its watched threads, running or not.
    var rows = SidebarTree.sessions(of: p, alias: "mini")
    #expect(rows.map(\.ref) == [SessionRef(project: "acme", server: "mini"), SessionRef(project: "acme", thread: "t-0", server: "mini"),
                                SessionRef(project: "acme", thread: "t-1", server: "mini")])
    #expect(rows[0].session?.id == "mini/s-c" && rows[1].session == nil && rows[2].session?.id == "mini/s-1")
    #expect(rows.map(\.task) == ["", "T9", "T1"])
    // A thread's row carries its title (the label a local row has), the coordinator's none.
    #expect(rows.map(\.title) == ["", "idle", "runs"])
    // A bridge before app API 10: the threads a session runs.
    rows = SidebarTree.sessions(of: p, alias: "mini", watched: false)
    #expect(rows.map(\.ref) == [SessionRef(project: "acme", server: "mini"), SessionRef(project: "acme", thread: "t-1", server: "mini")])
    // None runs: its row still shows, as the TUI's.
    p.sessions = [s("s-1", "thread", "t-1")]
    rows = SidebarTree.sessions(of: p, alias: "mini")
    #expect(rows.count == 3 && rows[0].session == nil && rows[0].ref.isCoordinator)
    // A watched thread that doesn't run stays selectable.
    let peers = [AppAPI.Peer(alias: "mini", state: "up", projects: [p])]
    #expect(Selection.session(SessionRef(project: "acme", thread: "t-0", server: "mini")).exists(projects: [], peers: peers))
    #expect(!Selection.session(SessionRef(project: "acme", thread: "t-7", server: "mini")).exists(projects: [], peers: peers))
    // Stale: the project's row alone.
    p.stale = true
    #expect(SidebarTree.sessions(of: p, alias: "mini").isEmpty)
}

@Test func needsFirstOrder() {
    // As the TUI's needsFirstOrder: in each group, below the pinned
    // ones, those that need you first; an inactive project's threads
    // count only once it is expanded.
    var asks = thread("t-1")
    asks.status = AppAPI.ThreadStatus(percent: 0, stepsDone: 0, stepsTotal: 0, todosDone: 0, todosTotal: 0,
                                      needsYou: "go?", selfPercent: -1, updated: t0)
    let ps = [project("a"), project("b", questions: 1), project("c", pinned: true), project("d", pinned: true, threads: [asks]),
              project("i", active: false), project("j", active: false, threads: [asks])]
    #expect(SidebarTree.active(ps).map(\.slug) == ["c", "d", "a", "b"])
    #expect(SidebarTree.active(ps, needsFirst: true).map(\.slug) == ["d", "c", "b", "a"])
    var nav = Navigation()
    #expect(SidebarTree.inactive(ps, needsFirst: true, nav: nav).map(\.slug) == ["i", "j"])
    nav.unfold(Navigation.sectionKey(server: "", project: "j"))
    #expect(SidebarTree.inactive(ps, needsFirst: true, nav: nav).map(\.slug) == ["j", "i"])
}

@Test func moreNeeds() {
    let n = { (k: Need.Kind, id: String) in Need(id: id, kind: k, project: "p", what: id, detail: "") }
    #expect(SidebarTree.moreNeeds([n(.review, "a"), n(.blocked, "b"), n(.review, "c"), n(.blocked, "d")]) == "+4 more: 2 review, 2 blocked")
    #expect(SidebarTree.moreNeeds([n(.question, "a")]) == "+1 more: 1 asking")
}

@Test func coordinatorAttention() {
    let p = project("p")
    // None runs and nothing asked: an idle dot.
    let none = Attention.of(coordinator: nil, in: p)
    #expect(!none.waits && none.tone == .idle)
    // Its open questions (tm ask) wait even while none runs.
    let asked = Attention.of(coordinator: nil, in: project("p", questions: 2))
    #expect(asked.waits && asked.word == "asks" && asked.why == "has 2 questions")
    #expect(Attention.of(coordinator: coordinator(), in: p).tone == .working)
    #expect(Attention.of(coordinator: coordinator(state: "idle"), in: p).tone == .idle)
    let q = Attention.of(coordinator: coordinator(state: "blocked", reason: "question"), in: p)
    #expect(q.waits && q.tone == .question)
    let a = Attention.of(coordinator: coordinator(state: "blocked", reason: "permission"), in: p)
    #expect(a.waits && a.word == "allow?")
}

@Test func peerTag() {
    // The header's tag after the server's name: offline first, else
    // what its panes allow here, and VoiceOver hears it whole.
    var p = AppAPI.Peer(alias: "work-laptop", state: "up", panes: "type")
    #expect(PeerTag.of(p).word == "write" && !PeerTag.of(p).offline)
    #expect(PeerTag.label(p) == "work-laptop, paired server, write access")
    p.panes = "view"
    #expect(PeerTag.of(p).word == "view")
    p.panes = "off"
    #expect(PeerTag.of(p).word == "off")
    p.state = "connecting"
    #expect(PeerTag.of(p).offline && PeerTag.label(p) == "work-laptop, paired server, offline")
    p.name = "Work laptop"
    #expect(PeerTag.label(p).hasPrefix("Work laptop, "))
}
