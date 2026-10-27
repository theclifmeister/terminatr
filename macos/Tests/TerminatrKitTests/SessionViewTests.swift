import Foundation
import Testing
@testable import TerminatrKit

private func status(_ state: String, reason: String = "", agent: String = "claude", version: String = "") -> AppAPI.TranscriptStatus {
    AppAPI.TranscriptStatus(state: state, reason: reason, agent: agent, version: version)
}

@Test func conversationState() {
    let caps = ["pane-vt", "transcript"]
    #expect(ConversationState.of(caps: caps, agent: "claude", status: nil, failure: nil) == .loading)
    #expect(ConversationState.of(caps: caps, agent: "claude", status: status("mapped"), failure: nil) == .mapped)
    #expect(ConversationState.of(caps: caps, agent: "claude", status: status("waiting"), failure: nil) == .waiting)
    #expect(!ConversationState.of(caps: caps, agent: "claude", status: status("unmapped", reason: "untested version", version: "2.9.0"), failure: nil).available)
    #expect(!ConversationState.of(caps: ["pane-vt"], agent: "claude", status: status("mapped"), failure: nil).available)
    // Codex is terminal-only (design T734, q14), whatever its status says.
    #expect(!ConversationState.of(caps: caps, agent: "codex", status: status("mapped", agent: "codex"), failure: nil).available)
    #expect(!ConversationState.of(caps: caps, agent: "claude", status: status("mapped"), failure: "refused").available)
    if case .unavailable(let why) = ConversationState.of(caps: caps, agent: "claude", status: status("unmapped", reason: "untested version", version: "2.9.0"), failure: nil) {
        #expect(why.contains("claude 2.9.0"))
        #expect(why.contains("untested version"))
    }
}

@Test func sessionRef() throws {
    let created = Date(timeIntervalSince1970: 0)
    let usage = AppAPI.Usage(turns: 0, input: 0, output: 0, cacheRead: 0, cacheCreation: 0, costUsd: 0)
    let rec = AppAPI.ThreadRecord(id: "t-1", title: "x", agent: "claude", worktree: "/w", state: "working", session: "s2", created: created, reports: 0, reportAcked: 0, usage: usage)
    let idle = AppAPI.ThreadRecord(id: "t-2", title: "y", agent: "claude", worktree: "/w", state: "idle", created: created, reports: 0, reportAcked: 0, usage: usage)
    let projects = [AppAPI.Project(slug: "p", active: true, threads: [AppAPI.Thread(record: rec), AppAPI.Thread(record: idle)], usage: usage)]
    let sessions = [
        AppAPI.Session(id: "s1", role: "coordinator", project: "q", cols: 80, rows: 24, created: created, clients: 0),
        AppAPI.Session(id: "s3", role: "coordinator", project: "p", cols: 80, rows: 24, created: created, clients: 0),
    ]
    #expect(SessionRef(project: "p").session(in: projects, sessions: sessions) == "s3")
    #expect(SessionRef(project: "p", thread: "t-1").session(in: projects, sessions: sessions) == "s2")
    #expect(SessionRef(project: "p", thread: "t-2").session(in: projects, sessions: sessions) == nil)
    #expect(SessionRef(project: "r").session(in: projects, sessions: sessions) == nil)

    // A need's session, back to its window.
    #expect(SessionRef(sessions[1]) == SessionRef(project: "p"))
    #expect(SessionRef(AppAPI.Session(id: "s2", role: "thread", project: "p", thread: "t-1", cols: 80, rows: 24, created: created, clients: 0)) == SessionRef(project: "p", thread: "t-1"))
    #expect(SessionRef(AppAPI.Session(id: "s4", role: "shell", project: "p", cols: 80, rows: 24, created: created, clients: 0)) == nil)
    #expect(SessionRef(AppAPI.Session(id: "s5", role: "coordinator", cols: 80, rows: 24, created: created, clients: 0)) == nil)
}

@Test func remoteSessionRef() throws {
    let created = Date(timeIntervalSince1970: 0)
    let coord = AppAPI.PeerSession(id: "mini/s1", role: "coordinator", project: "demo", created: created)
    let thread = AppAPI.PeerSession(id: "mini/s2", role: "thread", project: "demo", thread: "t-0001", task: "T7", created: created)
    let shell = AppAPI.PeerSession(id: "mini/s3", role: "shell", project: "demo", created: created)
    let peers = [AppAPI.Peer(alias: "mini", state: "up", projects: [AppAPI.PeerProject(project: "demo", needsYou: 0, threads: 1, ready: 0, sessions: [coord, thread, shell])], pane: true, panes: "view")]

    let r = try #require(SessionRef(thread, of: "mini"))
    #expect(r == SessionRef(project: "demo", thread: "t-0001", server: "mini"))
    #expect(r.isRemote)
    #expect(r.session(in: peers)?.id == "mini/s2")
    #expect(SessionRef(coord, of: "mini")?.session(in: peers)?.id == "mini/s1")
    #expect(SessionRef(shell, of: "mini") == nil)
    #expect(SessionRef(project: "demo", thread: "t-0002", server: "mini").session(in: peers) == nil)
    #expect(SessionRef(project: "demo", thread: "t-0001", server: "studio").session(in: peers) == nil)
    // This server's lists never name a paired server's session.
    #expect(r.session(in: [], sessions: [AppAPI.Session(id: "s2", role: "thread", project: "demo", thread: "t-0001", cols: 80, rows: 24, created: created, clients: 0)]) == nil)

    // A window restored from an app before paired servers is this server's.
    let old = try JSONDecoder().decode(SessionRef.self, from: Data(#"{"project":"p","thread":"t-1"}"#.utf8))
    #expect(old == SessionRef(project: "p", thread: "t-1"))
    let round = try JSONDecoder().decode(SessionRef.self, from: JSONEncoder().encode(r))
    #expect(round == r)

    // A paired session whose conversation can't open is terminal-only;
    // one that can reads as this server's do.
    let closed = RemoteConversation.closed("no")
    #expect(ConversationState.of(caps: ["pane-vt", "transcript"], agent: "claude", status: nil, failure: nil, remote: closed) == .unavailable("no"))
    #expect(ConversationState.of(caps: ["pane-vt", "transcript"], agent: "claude", status: nil, failure: nil, remote: .view) == .loading)
    #expect(ConversationState.of(caps: ["pane-vt", "transcript"], agent: "claude", status: status("mapped"), failure: nil, remote: .write) == .mapped)
    // One that opened stays shown when the link drops (the server opens
    // it again), and a refusal says why.
    #expect(ConversationState.of(caps: ["pane-vt", "transcript"], agent: "claude", status: status("mapped"), failure: nil, remote: closed) == .mapped)
    #expect(!ConversationState.of(caps: ["pane-vt", "transcript"], agent: "claude", status: nil, failure: "mini doesn't share conversations with this server", remote: .view).available)
}

@Test func remoteConversation() {
    let caps = ["pane-vt", "transcript", "pane-vt-remote", "transcript-remote"]
    func peer(_ state: String = "up", transcript: Bool = true, panes: String, conversations: String = "on") -> AppAPI.Peer {
        AppAPI.Peer(alias: "mini", state: state, pane: true, panes: panes, transcript: transcript, conversations: conversations)
    }
    #expect(RemoteConversation.of(caps: caps, peer: peer(panes: "type"), alias: "mini") == .write)
    #expect(RemoteConversation.of(caps: caps, peer: peer(panes: "view"), alias: "mini") == .view)
    // Unsaid (an answer from before the switch was read) is on.
    #expect(RemoteConversation.of(caps: caps, peer: peer(panes: "view", conversations: ""), alias: "mini") == .view)
    #expect(RemoteConversation.of(caps: caps, peer: peer(panes: "type"), alias: "mini").sends)
    #expect(!RemoteConversation.of(caps: caps, peer: peer(panes: "view"), alias: "mini").sends)
    #expect(RemoteConversation.of(caps: caps, peer: peer(panes: "view"), alias: "mini").reads)
    for (c, p, words) in [(caps, peer(panes: "off"), "tm peer panes"), (caps, peer(panes: "type", conversations: "off"), "tm peer conversations"),
                          (caps, peer("down", panes: "type"), "offline"), (caps, peer(transcript: false, panes: "type"), "needs an update"),
                          (["pane-vt", "transcript", "pane-vt-remote"], peer(panes: "type"), "tm update"), (caps, nil, "isn't paired")] {
        let rc = RemoteConversation.of(caps: c, peer: p, alias: "mini")
        guard case .closed(let why) = rc else {
            Issue.record("opens: \(c) \(String(describing: p))")
            continue
        }
        #expect(why.contains(words), "\(why)")
        #expect(!rc.reads && !rc.sends)
    }
    #expect(RemoteConversation.ended("conversations-off", alias: "mini") == "mini stopped sharing conversations with this server.")
    #expect(RemoteConversation.ended("exited", alias: "mini").contains("ended on mini"))
    #expect(RemoteConversation.ended("odd", alias: "mini").contains("(odd)"))
}

@Test func remotePane() {
    let caps = ["pane-vt", "transcript", "pane-vt-remote"]
    func peer(_ state: String = "up", pane: Bool = true, panes: String) -> AppAPI.Peer {
        AppAPI.Peer(alias: "mini", state: state, pane: pane, panes: panes)
    }
    #expect(RemotePane.of(caps: caps, peer: peer(panes: "type"), alias: "mini") == .write)
    #expect(RemotePane.of(caps: caps, peer: peer(panes: "view"), alias: "mini") == .view)
    for (c, p) in [(caps, peer(panes: "off")), (caps, peer(panes: "")), (caps, peer("down", panes: "type")),
                   (caps, peer(pane: false, panes: "type")), (["pane-vt"], peer(panes: "type")), (caps, nil)] {
        guard case .closed(let why) = RemotePane.of(caps: c, peer: p, alias: "mini") else {
            Issue.record("opens: \(c) \(String(describing: p))")
            continue
        }
        #expect(!why.isEmpty)
    }
}

private func item(_ kind: String, _ text: String = "", tool: String = "", call: String = "", lines: Int = 0, error: Bool = false) -> AppAPI.TranscriptItem {
    AppAPI.TranscriptItem(kind: kind, text: text, tool: tool, call: call, error: error, lines: lines)
}

@Test func transcriptFolds() {
    var t = Transcript()
    t.start(AppAPI.TranscriptStatus(state: "mapped"))
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [
        item("prompt", "Start on your task."),
        item("tool", "internal/server/projectcache.go", tool: "Read", call: "c1"),
        item("result", "package server", call: "c1", lines: 212),
        item("result", "orphan", call: "c0"),
    ], skipped: 5))
    #expect(t.skipped == 5)
    #expect(t.entries.map(\.kind) == [.prompt, .tool, .result])
    #expect(t.entries[1].result == Transcript.Result(text: "package server", lines: 212, error: false))
    #expect(t.pendingTool == nil)

    t.apply("transcript.items", AppAPI.TranscriptEvent(items: [
        item("text", "Running the tests."),
        item("tool", "go test ./...", tool: "Bash", call: "c2"),
        AppAPI.TranscriptItem(kind: "todos", todos: [AppAPI.TranscriptTodo(text: "a", status: "pending")]),
    ]))
    #expect(t.pendingTool?.text == "go test ./...")
    #expect(t.todos.count == 1)
    #expect(t.entries.count == 5)

    t.apply("transcript.status", AppAPI.TranscriptEvent(status: AppAPI.TranscriptStatus(state: "unmapped", reason: "format changed")))
    #expect(t.status?.state == "unmapped")

    // A reset starts over (/clear).
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [item("prompt", "again")]))
    #expect(t.entries.map(\.text) == ["again"])
    #expect(t.todos.isEmpty)
    #expect(t.skipped == 0)
    t.apply("transcript.closed", AppAPI.TranscriptEvent(reason: "exited"))
    #expect(t.closed == "exited")
}

@Test func transcriptLimit() {
    var t = Transcript(limit: 3)
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: (1...5).map { item("text", "\($0)") }, skipped: 2))
    #expect(t.entries.map(\.text) == ["3", "4", "5"])
    #expect(t.skipped == 4)
    #expect(Set(t.entries.map(\.id)).count == 3)
}
