import Foundation
import Testing
@testable import TerminatrKit

// The lean sidebar (T748 milestone A): the TUI's project order, a
// coordinator row only when it runs or asks, the threads worth a row,
// and the needs cut to three.

private func project(_ slug: String, active: Bool = true, pinned: Bool = false, questions: Int = 0,
                     threads: [AppAPI.Thread] = []) -> AppAPI.Project {
    AppAPI.Project(slug: slug, active: active, pinned: pinned, questions: questions, threads: threads, usage: noUsage)
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

@Test func threadRows() {
    let p = project("p", threads: [thread("t-1"), thread("t-2"), thread("t-3"), thread("t-4", done: true)])
    let waits: Set<String> = ["t-2"]
    let att = { (t: AppAPI.Thread) in
        Attention(word: waits.contains(t.record.id) ? "asks" : "working", tone: .working, why: "",
                  waits: waits.contains(t.record.id))
    }
    // What waits on the user shows; the working ones fold into more.
    var (shown, more) = SidebarTree.threads(of: p, selected: nil, unfolded: false, attention: att)
    #expect(shown.map(\.record.id) == ["t-2"])
    #expect(more == 2)
    // The selected thread shows too, in its place.
    (shown, more) = SidebarTree.threads(of: p, selected: "t-1", unfolded: false, attention: att)
    #expect(shown.map(\.record.id) == ["t-1", "t-2"])
    #expect(more == 1)
    // Unfolded: every open thread, never a done one.
    (shown, more) = SidebarTree.threads(of: p, selected: nil, unfolded: true, attention: att)
    #expect(shown.map(\.record.id) == ["t-1", "t-2", "t-3"])
    #expect(more == 0)
}

@Test func projectExpands() {
    let p = project("p")
    #expect(!SidebarTree.expands(p, selection: nil, needs: 0))
    #expect(SidebarTree.expands(p, selection: nil, needs: 1))
    #expect(SidebarTree.expands(p, selection: .project("p"), needs: 0))
    #expect(SidebarTree.expands(p, selection: .session(SessionRef(project: "p", thread: "t-1")), needs: 0))
    #expect(!SidebarTree.expands(p, selection: .project("q"), needs: 0))
    // A paired server's project of the same name isn't this one.
    #expect(!SidebarTree.expands(p, selection: .project("p", server: "mini"), needs: 0))
}

@Test func moreNeeds() {
    let n = { (k: Need.Kind, id: String) in Need(id: id, kind: k, project: "p", what: id, detail: "") }
    #expect(SidebarTree.moreNeeds([n(.review, "a"), n(.blocked, "b"), n(.review, "c"), n(.blocked, "d")]) == "+4 more: 2 review, 2 blocked")
    #expect(SidebarTree.moreNeeds([n(.question, "a")]) == "+1 more: 1 asking")
}

@Test func coordinatorAttention() {
    let p = project("p")
    // None runs and nothing asked: no row, an idle dot.
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
