import Foundation
import Testing
@testable import TerminatrKit

// A tail's reset, then transcript.page's earlier items in front (T792).

@Test func pagePrependsBeforeTheTail() {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: [
        AppAPI.TranscriptItem(kind: "result", text: "done", call: "c1", lines: 1),
        AppAPI.TranscriptItem(kind: "text", text: "after"),
    ], skipped: 3, before: "cur2"))
    #expect(t.before == "cur2")
    #expect(t.entries.map(\.kind) == [.result, .text])
    let ids = t.entries.map(\.id)

    t.prepend(AppAPI.TranscriptPage(items: [
        AppAPI.TranscriptItem(kind: "prompt", text: "go"),
        AppAPI.TranscriptItem(kind: "tool", tool: "Bash", call: "c1"),
    ], before: "cur1"))
    // The orphan result folds into its call, now in the conversation.
    #expect(t.entries.map(\.kind) == [.prompt, .tool, .text])
    #expect(t.entries[1].result?.text == "done")
    #expect(t.before == "cur1")
    #expect(t.skipped == 1)
    // The ids the view had stay; the new ones are below and all differ.
    #expect(t.entries.last?.id == ids.last)
    #expect(Set(t.entries.map(\.id)).count == 3)
    #expect(t.entries.map(\.id) == t.entries.map(\.id).sorted())

    t.prepend(AppAPI.TranscriptPage(items: [AppAPI.TranscriptItem(kind: "note", text: "start")]))
    #expect(t.before == "")
    #expect(t.entries.first?.kind == .note)
    #expect(Set(t.entries.map(\.id)).count == 4)
}
