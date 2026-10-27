import Foundation
import Testing
@testable import TerminatrKit

// Consecutive tool calls fold into one group (T763).

private func entry(_ id: Int, _ kind: Transcript.Entry.Kind, tool: String = "", result: Transcript.Result? = nil) -> Transcript.Entry {
    Transcript.Entry(id: id, kind: kind, text: "", truncated: false, tool: tool, call: "c\(id)", result: result)
}

private let ok = Transcript.Result(text: "", lines: 1, error: false)
private let failed = Transcript.Result(text: "no such file", lines: 1, error: true)

@Test func toolRunsGroup() {
    let entries = [
        entry(0, .prompt),
        entry(1, .tool, tool: "Read", result: ok),
        entry(2, .tool, tool: "Bash", result: ok),
        entry(3, .tool, tool: "Read", result: ok),
        entry(4, .text),
        entry(5, .tool, tool: "Edit", result: ok),
        entry(6, .note),
        entry(7, .tool, tool: "Read", result: ok),
        entry(8, .tool, tool: "", result: ok),
    ]
    let rows = Transcript.rows(entries)
    #expect(rows.map(\.id) == [0, 1, 4, 5, 6, 7])
    guard case .tools(let run) = rows[1] else { Issue.record("no run"); return }
    #expect(run.calls.map(\.id) == [1, 2, 3])
    #expect(run.headline == "Read ×2 · Bash ×1")
    #expect(run.spoken == "3 tool calls: Read 2, Bash 1")
    #expect(run.state == .done)
    #expect(run.live == nil)
    // A single call stays an entry; a call without a tool is "tool".
    guard case .entry(let single) = rows[3] else { Issue.record("a group of one"); return }
    #expect(single.id == 5)
    guard case .tools(let last) = rows[5] else { Issue.record("no run"); return }
    #expect(last.headline == "Read ×1 · tool ×1")
    // The row a find's match is drawn in.
    #expect(Transcript.row(of: 3, in: rows) == 1)
    #expect(Transcript.row(of: 4, in: rows) == 4)
    #expect(Transcript.row(of: 99, in: rows) == nil)
}

@Test func toolRunState() {
    let running = Transcript.ToolRun(calls: [entry(1, .tool, tool: "Read", result: failed), entry(2, .tool, tool: "Bash")])
    #expect(running.state == .running)
    #expect(running.live?.id == 2)
    let failing = Transcript.ToolRun(calls: [entry(1, .tool, tool: "Read", result: failed), entry(2, .tool, tool: "Bash", result: ok)])
    #expect(failing.state == .failed)
    // An earlier call still running (parallel calls) keeps the run
    // running, but only a last one shows live.
    let parallel = Transcript.ToolRun(calls: [entry(1, .tool, tool: "Read"), entry(2, .tool, tool: "Read", result: ok)])
    #expect(parallel.state == .running)
    #expect(parallel.live == nil)
}

@Test func toolRunsStream() {
    // Calls appending keep the run's id; a second call makes a group of
    // the first's id.
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [
        AppAPI.TranscriptItem(kind: "text", text: "Looking."),
        AppAPI.TranscriptItem(kind: "tool", text: "a.go", tool: "Read", call: "x"),
    ]))
    #expect(Transcript.rows(t.entries).map(\.id) == [0, 1])
    t.apply("transcript.items", AppAPI.TranscriptEvent(items: [
        AppAPI.TranscriptItem(kind: "result", text: "", call: "x"),
        AppAPI.TranscriptItem(kind: "tool", text: "b.go", tool: "Read", call: "y"),
    ]))
    let rows = Transcript.rows(t.entries)
    #expect(rows.map(\.id) == [0, 1])
    guard case .tools(let run) = rows[1] else { Issue.record("no run"); return }
    #expect(run.live?.text == "b.go")
}

@Test func tailKeepsRuns() {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items:
        [AppAPI.TranscriptItem(kind: "text", text: "start")]
        + (0..<10).map { AppAPI.TranscriptItem(kind: "tool", text: "f\($0)", tool: "Read", call: "c\($0)") }
        + [AppAPI.TranscriptItem(kind: "text", text: "end")]))
    // The last 3 would start within the run: they start at its first call.
    #expect(t.tail(3).first?.id == 1)
    #expect(t.tail(1).map(\.id) == [11])
    #expect(t.tail(60).first?.id == 0)
}

@Test func askUserQuestionStandsAlone() {
    let rows = Transcript.rows([
        entry(1, .tool, tool: "Read", result: ok),
        entry(2, .tool, tool: "Read", result: ok),
        entry(3, .tool, tool: "AskUserQuestion"),
        entry(4, .tool, tool: "Bash", result: ok),
    ])
    #expect(rows.map(\.id) == [1, 3, 4])
    guard case .entry(let e) = rows[1] else { Issue.record("AskUserQuestion grouped"); return }
    #expect(e.asksUser && e.result == nil)
}
