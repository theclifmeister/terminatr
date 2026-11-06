import Foundation
import Testing
@testable import TerminatrKit

// T1000: what the flat window frame's title row, status bar and inspector
// say.

private func thread(_ id: String, session: String) -> AppAPI.Thread {
    AppAPI.Thread(record: AppAPI.ThreadRecord(id: id, title: id, agent: "claude", worktree: "/w", state: "working",
                                              session: session, created: t0, reports: 0, reportAcked: 0, done: false,
                                              usage: noUsage))
}

private func session(_ id: String, state: String) -> AppAPI.Session {
    AppAPI.Session(id: id, role: "thread", project: "p", cols: 80, rows: 24, created: t0, clients: 0, state: state, reason: "")
}

@Test func projectTabsFollowWhatTmHas() {
    #expect(ProjectTab.tabs(nil) == [.overview, .board])
    #expect(ProjectTab.allCases.map(\.rawValue) == ["Overview", "Board", "Memory", "Library", "Designs", "Mods"])
    #expect(!ProjectTab.paired(nil).contains(.mods))
    #expect(!ProjectTab.paired(nil).contains(.designs))
    var h = AppAPI.Hello(appApi: 31, tm: "1", build: "", capabilities: [], methods: ["design.list"])
    h.server = AppAPI.Server(version: "1", protocol: 28, caps: ["designs"])
    #expect(ProjectTab.tabs(h).last == .designs)
    #expect(ProjectTab.paired(h).last == .designs)
    h.server?.caps = []
    #expect(!ProjectTab.tabs(h).contains(.designs))
    #expect(!ProjectTab.paired(h).contains(.designs))
}

@Test func tileLetters() {
    #expect(ProjectTile.letters("terminatr") == "te")
    #expect(ProjectTile.letters("terminatr-mods") == "tm")
    #expect(ProjectTile.letters("api_gateway") == "ag")
    #expect(ProjectTile.letters("x") == "x")
    #expect(ProjectTile.letters("Toe") == "to")
}

@Test func statusBarCountsThreadsAtWork() {
    let p = AppAPI.Project(slug: "p", active: true, threads: [thread("a", session: "s-a"), thread("b", session: "s-b"), thread("c", session: "")], usage: noUsage)
    let f = StatusFacts.of(projects: [p], sessions: [session("s-a", state: "working"), session("s-b", state: "idle")], peers: [], needs: 2)
    #expect(f == StatusFacts(servers: 1, working: 1, needs: 2))
    #expect(f.server == "This Mac")
    #expect(f.workingWord == "1 working")
    #expect(f.needsWord == "2 need you")
}

@Test func statusBarWords() {
    #expect(StatusFacts(servers: 2, working: 0, needs: 1).server == "This Mac · 2 servers")
    #expect(StatusFacts(servers: 2, working: 0, needs: 1).needsWord == "1 needs you")
    #expect(StatusFacts(servers: 1, working: 0, needs: 0).needsWord == "0 need you")
}

@Test func inspectorHeadNamesWhatItShows() {
    let idle = Attention(word: "idle", tone: .idle, why: "", waits: false)
    #expect(InspectorHead.coordinator(project: "acme", state: idle).name == .role(.coordinator, "Coordinator"))
    #expect(InspectorHead.coordinator(project: "acme", state: idle).spoken == "Coordinator of acme, idle")
    #expect(InspectorHead.coordinator(project: "acme", server: "mini", state: nil).spoken == "Coordinator of mini › acme")
    #expect(InspectorHead.director(program: "fleet", state: idle).name == .role(.director, "Director"))
    #expect(InspectorHead.project("acme", state: idle).name == .role(.coordinator, "acme"))
    #expect(InspectorHead.project("acme", state: idle).spoken == "Project acme, coordinator, idle")
    #expect(InspectorHead.project("acme", state: nil).spoken == "Project acme")
    #expect(InspectorHead.program("fleet", state: idle).name == .role(.director, "fleet"))
    #expect(InspectorHead.thread(task: "T1", id: "t-1", state: idle).name == .ids(task: "T1", thread: "t-1"))
    #expect(InspectorHead.thread(task: "T1", id: "t-1", state: idle).spoken == "T1 t-1, idle")
    #expect(InspectorHead.thread(task: nil, id: "t-1", server: "mini", state: nil).spoken == "t-1 on mini")
    #expect(InspectorHead.empty.spoken == "")
}

@Test func aSessionsOwnState() {
    #expect(Attention.session(state: "working", reason: "").tone == .working)
    #expect(Attention.session(state: "idle", reason: "").word == "idle")
    #expect(Attention.session(state: "blocked", reason: "permission").word == "needs approval")
    #expect(Attention.session(state: "blocked", reason: "").word == "blocked")
    #expect(Attention.session(state: "idle", reason: "", asking: true).word == "asks")
}

@Test func foldSummaries() {
    #expect(InspectorSummary.project(threads: 2, onDeck: 5) == "2 threads · 5 on deck")
    #expect(InspectorSummary.project(threads: 1, onDeck: 0) == "1 thread · 0 on deck")
    #expect(InspectorSummary.peerProject(threads: 3, paused: true, stale: true) == "3 threads · paused · stale")
    #expect(InspectorSummary.program(projects: 2, questions: 0) == "2 projects")
    #expect(InspectorSummary.program(projects: 1, questions: 1) == "1 project · 1 question")
    #expect(InspectorSummary.done(3, of: 4) == "3 of 4 done")
    #expect(InspectorSummary.agent("claude", model: "opus") == "claude · opus")
    #expect(InspectorSummary.agent("claude", model: "", queued: 2) == "claude · 2 queued prompts")
    #expect(InspectorSummary.prompts(1) == "1 prompt")
    #expect(InspectorSummary.prompts(2) == "2 prompts")
    #expect(InspectorSummary.paired(server: "mini", link: "online", stale: true) == "mini · online · stale")
}

@Test func theNextNeedIsPicked() {
    // The first starts picked.
    #expect(NeedPick.next(picked: nil, was: [], now: ["a", "b"]) == "a")
    // A click's pick stays while the list changes around it.
    #expect(NeedPick.next(picked: "b", was: ["a", "b"], now: ["b", "c"]) == "b")
    // Answered: the one after it, not the first.
    #expect(NeedPick.next(picked: "b", was: ["a", "b", "c"], now: ["a", "c"]) == "c")
    // The last answered: the one before it.
    #expect(NeedPick.next(picked: "c", was: ["a", "b", "c"], now: ["a", "b"]) == "b")
    // The next went as well: the next still there.
    #expect(NeedPick.next(picked: "a", was: ["a", "b", "c"], now: ["c"]) == "c")
    // Nothing left to pick.
    #expect(NeedPick.next(picked: "a", was: ["a"], now: []) == nil)
    // A new need after they all went.
    #expect(NeedPick.next(picked: nil, was: [], now: ["z"]) == "z")
}
