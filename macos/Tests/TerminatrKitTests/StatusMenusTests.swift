import Foundation
import Testing
@testable import TerminatrKit

// T1032: what the status bar's server and working menus list.

private func thread(_ id: String, title: String, session: String) -> AppAPI.Thread {
    AppAPI.Thread(record: AppAPI.ThreadRecord(id: id, title: title, agent: "claude", worktree: "/w", state: "working",
                                              session: session, created: t0, reports: 0, reportAcked: 0, done: false,
                                              usage: noUsage))
}

private func session(_ id: String, role: String = "thread", project: String = "p", state: String = "working", since: Date? = nil) -> AppAPI.Session {
    AppAPI.Session(id: id, role: role, project: project, cols: 80, rows: 24, created: t0, clients: 0, state: state, reason: "", turnSince: since)
}

private func peerSession(_ id: String, thread: String = "t-1", state: String = "working", since: Date? = nil, asking: Bool = false, role: String = "thread") -> AppAPI.PeerSession {
    AppAPI.PeerSession(id: "mini/\(id)", role: role, project: "acme", thread: thread, created: t0, state: state, asking: asking, turnSince: since)
}

@Test func serverRowsSayConnectionAndVersion() {
    let now = t0.addingTimeInterval(240)
    let peers = [
        AppAPI.Peer(alias: "mini", name: "Mini", state: "up", version: "v0.82.1", rttMs: 37.6),
        AppAPI.Peer(alias: "lap", state: "up"),
        AppAPI.Peer(alias: "nas", state: "connecting"),
        AppAPI.Peer(alias: "old", state: "offline", reason: "version", since: t0),
    ]
    let rows = ServerList.of(connected: true, version: "0.82.1", peers: peers, rtt: [:], now: now)
    #expect(rows.map(\.text) == ["This Mac · connected · tm 0.82.1", "Mini · connected 38 ms · tm 0.82.1", "lap · connected", "nas · connecting", "old · offline 4m · old tm"])
    #expect(rows.map(\.alias) == ["", "mini", "lap", "nas", "old"])
    // The latest ping wins over the listing's, and a tiny one reads as such.
    #expect(ServerList.of(connected: true, version: "", peers: [peers[0]], rtt: ["mini": 0.4], now: now)[1].text == "Mini · connected <1 ms · tm 0.82.1")
    #expect(ServerList.of(connected: false, version: "0.82.1", peers: [], rtt: [:], now: now).map(\.text) == ["This Mac · not connected"])
}

@Test func choosingAServerLandsOnItsFirstProject() {
    let p = AppAPI.Project(slug: "p", active: true, usage: noUsage)
    let peers = [AppAPI.Peer(alias: "mini", state: "up", projects: [AppAPI.PeerProject(project: "acme", needsYou: 0, threads: 0, ready: 0)]), AppAPI.Peer(alias: "bare", state: "up")]
    #expect(ServerList.landing("", shown: .project("x", server: "mini"), entries: [.project(p)], peers: peers) == .project("p"))
    #expect(ServerList.landing("mini", shown: .project("p"), entries: [.project(p)], peers: peers) == .project("acme", server: "mini"))
    // Already there, or nothing to land on: the window stays as it is.
    #expect(ServerList.landing("mini", shown: .project("acme", server: "mini"), entries: [], peers: peers) == nil)
    #expect(ServerList.landing("", shown: .project("p"), entries: [.project(p)], peers: peers) == nil)
    #expect(ServerList.landing("bare", shown: nil, entries: [], peers: peers) == nil)
    #expect(ServerList.landing("", shown: nil, entries: [], peers: []) == nil)
}

@Test func workingListsWhatTheBarCountsByProject() {
    let now = t0.addingTimeInterval(180)
    let p = AppAPI.Project(slug: "p", active: true, threads: [
        thread("t-1", title: "first", session: "s-1"), thread("t-2", title: "idle", session: "s-2"), thread("t-3", title: "", session: ""),
    ], usage: noUsage)
    let sessions = [session("s-1", since: t0), session("s-2", state: "idle"), session("s-c", role: "coordinator", since: t0)]
    let up = AppAPI.Peer(alias: "mini", name: "Mini", state: "up", projects: [AppAPI.PeerProject(
        project: "acme", needsYou: 0, threads: 2, ready: 0,
        sessions: [peerSession("a", since: t0), peerSession("b", thread: "t-2", asking: true), peerSession("c", thread: "", role: "coordinator")],
        watched: [AppAPI.PeerThread(id: "t-1", title: "remote work", task: "T7", session: "mini/a")])])
    let down = AppAPI.Peer(alias: "gone", state: "offline", projects: [AppAPI.PeerProject(project: "x", needsYou: 0, threads: 1, ready: 0, sessions: [peerSession("z")])])
    let list = WorkingList.of(projects: [p], sessions: sessions, peers: [up, down])
    #expect(list.threads.map(\.name) == ["p", "Mini › acme"])
    #expect(list.threads[0].rows.map { $0.text(now: now) } == ["first · 3m"])
    #expect(list.threads[0].rows[0].selection == .session(SessionRef(project: "p", thread: "t-1")))
    #expect(list.threads[1].rows.map { $0.text(now: now) } == ["T7 remote work · 3m"])
    #expect(list.threads[1].rows[0].selection == .session(SessionRef(project: "acme", thread: "t-1", server: "mini")))
    // The coordinators stand apart: the bar's number is the threads'.
    #expect(list.roles.map { $0.text(now: now) } == ["p · Coordinator · 3m", "Mini › acme · Coordinator"])
    let f = StatusFacts.of(projects: [p], sessions: sessions, peers: [up, down], needs: 0)
    #expect(f.working == list.threads.reduce(0) { $0 + $1.rows.count })
    #expect(f.working == 2)
    #expect(WorkingList.of(projects: [], sessions: [], peers: []).isEmpty)
}

@Test func aLongTitleIsCutForTheMenu() {
    let r = WorkingRow(selection: .project("p"), title: String(repeating: "x", count: 200), busy: Busy(since: nil))
    #expect(r.text(now: t0).count == WorkingRow.titleMax)
    #expect(r.text(now: t0).hasSuffix("…"))
}
