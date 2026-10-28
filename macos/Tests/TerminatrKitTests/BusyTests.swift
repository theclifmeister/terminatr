import Foundation
import Testing
@testable import TerminatrKit

// A working session shows as busy: its turn's time and what it does
// (T803).

@Test func busyOnlyWhileWorking() throws {
    var s = session("s-1")
    s.turnSince = t0
    let b = try #require(Busy.of(s))
    #expect(b.since == t0)
    #expect(b.word(now: t0.addingTimeInterval(42)) == "working · 42s")
    #expect(b.word(now: t0.addingTimeInterval(3 * 60 + 59)) == "working · 3m")
    #expect(b.word(now: t0.addingTimeInterval(3600 + 5 * 60)) == "working · 1h 5m")
    #expect(b.word(now: t0.addingTimeInterval(7200)) == "working · 2h")
    #expect(b.spoken(now: t0.addingTimeInterval(180)) == "working for 3 minutes")
    #expect(b.clock(now: t0.addingTimeInterval(45)) == "0:45")
    #expect(b.clock(now: t0.addingTimeInterval(3 * 60 + 12)) == "3:12")
    #expect(b.clock(now: t0.addingTimeInterval(3600 + 5 * 60 + 9)) == "1:05:09")
    // A clock a moment behind the server's never reads negative.
    #expect(b.word(now: t0.addingTimeInterval(-2)) == "working · 0s")
    for state in ["idle", "blocked", "exited", ""] {
        #expect(Busy.of(session("s-1", state: state)) == nil)
    }
    #expect(Busy.of(nil) == nil)
    // A tm before app API 25 sends no turn_since: busy, without a time.
    let old = try #require(Busy.of(session("s-1")))
    #expect(old.word(now: t0) == "working")
    #expect(old.spoken(now: t0) == "working")
    #expect(old.clock(now: t0) == nil)
}

/// What waits on the user wins over the mark, as Attention says.
@Test func busyYieldsToWhatWaits() {
    let s = session("s-1")
    #expect(Busy.of(thread: thread("t-1", session: "s-1"), session: s) != nil)
    #expect(Busy.of(thread: thread("t-1", task: AppAPI.Task(id: 1, ref: "T1", title: "x", status: "review"), session: "s-1"), session: s) == nil)
    #expect(Busy.of(thread: thread("t-1", session: "s-1", needsYou: "which DB?"), session: s) == nil)
    var held = s
    held.question = menu([backend])
    #expect(Busy.of(thread: thread("t-1", session: "s-1"), session: held) == nil)
}

@Test func activityFromTheLatestCall() {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [
        .init(kind: "prompt", text: "go"),
        .init(kind: "tool", text: "/w/macos/Sources/Terminatr/TerminalTab.swift", tool: "Read", call: "c1"),
        .init(kind: "result", text: "ok", call: "c1"),
        .init(kind: "tool", text: "make test\nmore", tool: "Bash", call: "c2"),
    ]))
    #expect(Activity.of(t, current: "") == "running make test")
    t.apply("transcript.items", AppAPI.TranscriptEvent(items: [
        .init(kind: "result", text: "ok", call: "c2"),
        .init(kind: "tool", text: "/w/macos/Sources/Terminatr/TerminalTab.swift", tool: "Edit", call: "c3"),
    ]))
    #expect(Activity.of(t, current: "") == "editing TerminalTab.swift")
    // Between calls: the todo in progress, else the session's current,
    // else thinking.
    t.apply("transcript.items", AppAPI.TranscriptEvent(items: [.init(kind: "result", text: "ok", call: "c3")]))
    #expect(Activity.of(t, current: "") == "thinking")
    #expect(Activity.of(t, current: "Writing the tests") == "writing the tests")
    t.apply("transcript.items", AppAPI.TranscriptEvent(items: [
        .init(kind: "todos", todos: [.init(text: "Add the bar", status: "completed"), .init(text: "Running the tests", status: "in_progress")]),
    ]))
    #expect(Activity.of(t, current: "Writing the tests") == "running the tests")
    // An acronym keeps its case.
    #expect(Activity.of(Transcript(), current: "CI fixes") == "CI fixes")
    // A question call is the user's turn, not an activity.
    t.apply("transcript.items", AppAPI.TranscriptEvent(items: [.init(kind: "tool", text: "Which?", tool: "AskUserQuestion", call: "c4")]))
    #expect(Activity.of(t, current: "") == "running the tests")
}

@Test func activityWords() {
    #expect(Activity.of(tool: "Write", argument: "/a/b/README.md") == "writing README.md")
    #expect(Activity.of(tool: "Grep", argument: "TurnSince") == "searching for TurnSince")
    #expect(Activity.of(tool: "apply_patch", argument: "") == "editing")
    #expect(Activity.of(tool: "TodoWrite", argument: "") == "updating its todos")
    #expect(Activity.of(tool: "mcp__tm__report", argument: "done") == "mcp__tm__report done")
}
