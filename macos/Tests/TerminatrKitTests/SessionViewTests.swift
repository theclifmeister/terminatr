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
    let projects = [AppAPI.Project(slug: "p", threads: [AppAPI.Thread(record: rec), AppAPI.Thread(record: idle)], usage: usage)]
    let sessions = [
        AppAPI.Session(id: "s1", role: "coordinator", project: "q", cols: 80, rows: 24, created: created, clients: 0),
        AppAPI.Session(id: "s3", role: "coordinator", project: "p", cols: 80, rows: 24, created: created, clients: 0),
    ]
    #expect(SessionRef(project: "p").session(in: projects, sessions: sessions) == "s3")
    #expect(SessionRef(project: "p", thread: "t-1").session(in: projects, sessions: sessions) == "s2")
    #expect(SessionRef(project: "p", thread: "t-2").session(in: projects, sessions: sessions) == nil)
    #expect(SessionRef(project: "r").session(in: projects, sessions: sessions) == nil)
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
