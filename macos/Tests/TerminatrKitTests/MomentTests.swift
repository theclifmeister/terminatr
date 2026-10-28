import Foundation
import Testing
@testable import TerminatrKit

// The entry a need was asked at, for the iPhone's conversation beside it (T826).

private func transcript(_ items: [AppAPI.TranscriptItem]) -> Transcript {
    var t = Transcript()
    t.apply("transcript.items", AppAPI.TranscriptEvent(items: items))
    return t
}

private let asked = Date(timeIntervalSince1970: 1_000_000)

@Test func approvalIsItsWaitingCall() {
    let t = transcript([
        AppAPI.TranscriptItem(kind: "tool", tool: "Bash", call: "c1"),
        AppAPI.TranscriptItem(kind: "result", text: "ok", call: "c1", lines: 1),
        AppAPI.TranscriptItem(kind: "text", text: "Now the migration."),
        AppAPI.TranscriptItem(kind: "tool", tool: "Bash", call: "c2"),
    ])
    #expect(t.moment(.approval) == t.entries.last?.id)
    // Every call has its result: no dialog asks about one.
    let done = transcript([
        AppAPI.TranscriptItem(kind: "tool", tool: "Bash", call: "c1"),
        AppAPI.TranscriptItem(kind: "result", text: "ok", call: "c1", lines: 1),
    ])
    #expect(done.moment(.approval) == nil)
}

@Test func questionIsItsAskingCall() {
    let t = transcript([
        AppAPI.TranscriptItem(kind: "tool", tool: "AskUserQuestion", call: "q1", at: asked.addingTimeInterval(-600)),
        AppAPI.TranscriptItem(kind: "result", text: "A", call: "q1", lines: 1),
        AppAPI.TranscriptItem(kind: "text", text: "Which ranking?", at: asked.addingTimeInterval(-3)),
        AppAPI.TranscriptItem(kind: "tool", tool: "AskUserQuestion", call: "q2", at: asked.addingTimeInterval(-2)),
    ])
    let last = t.entries.last?.id
    #expect(t.moment(.question, since: asked) == last)
    #expect(t.moment(.question) == last)
    // Asked before the second call: the first one.
    #expect(t.moment(.question, since: asked.addingTimeInterval(-500)) == t.entries.first?.id)
}

@Test func questionWithoutACallIsTheLastEntryBeforeIt() {
    // A menu the agent drew on its screen: no call asks it.
    let t = transcript([
        AppAPI.TranscriptItem(kind: "text", text: "Before", at: asked.addingTimeInterval(-60)),
        AppAPI.TranscriptItem(kind: "text", text: "Pick one", at: asked),
        AppAPI.TranscriptItem(kind: "prompt", text: "later", at: asked.addingTimeInterval(120)),
    ])
    #expect(t.moment(.question, since: asked) == t.entries[1].id)
    #expect(t.moment(.question) == nil)
    #expect(t.moment(.review, since: asked) == nil)
}
