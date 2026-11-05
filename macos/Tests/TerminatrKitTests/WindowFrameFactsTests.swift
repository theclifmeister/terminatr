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
    #expect(ProjectTab.allCases.map(\.rawValue) == ["Overview", "Board", "Memory", "Library", "Mods"])
    #expect(!ProjectTab.paired.contains(.mods))
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

@Test func inspectorHeadOfAThread() {
    let steps = [AppAPI.Step(n: 1, text: "a", done: true), AppAPI.Step(n: 2, text: "b", done: false)]
    #expect(InspectorHead.thread(ref: "T1", id: "t-1", steps: steps) == InspectorHead(chip: "T1", count: "1 of 2"))
    #expect(InspectorHead.thread(ref: nil, id: "t-1", steps: []) == InspectorHead(chip: "t-1", count: ""))
}

@Test func inspectorHeadOfAProject() {
    #expect(InspectorHead.project(needs: 2, context: 65) == InspectorHead(chip: "2 need you", count: "65% context"))
    #expect(InspectorHead.project(needs: 0, context: nil) == InspectorHead(chip: "0 need you", count: ""))
}
