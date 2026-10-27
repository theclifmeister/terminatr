import Foundation
import Testing
@testable import TerminatrKit

// At fleet size (T759): the lean projects watch's shown project, a long
// transcript drawn from its end, long prompts folded, and Markdown
// parsed once.

@Test func shownInactive() {
    let projects = [AppAPI.Project(slug: "on", active: true, usage: noUsage),
                    AppAPI.Project(slug: "off", active: false, usage: noUsage)]
    #expect(Selection.project("off").shownInactive(projects) == "off")
    #expect(Selection.session(SessionRef(project: "off", thread: "t-1")).shownInactive(projects) == "off")
    // An active project, one not listed, and a paired server's are
    // whole or not this watch's.
    #expect(Selection.project("on").shownInactive(projects) == "")
    #expect(Selection.project("gone").shownInactive(projects) == "")
    #expect(Selection.project("off", server: "peer").shownInactive(projects) == "")
}

@Test func transcriptTail() {
    var t = Transcript()
    t.apply("transcript.reset", AppAPI.TranscriptEvent(items: (0..<100).map { AppAPI.TranscriptItem(kind: "text", text: "turn \($0)") }))
    #expect(t.tail(60).map(\.id) == Array(40..<100))
    #expect(t.tail(500).count == 100)
    // A find's match before the tail draws from it on.
    #expect(t.tail(60, keeping: 10).first?.id == 10)
    #expect(t.tail(60, keeping: 70).first?.id == 40)
}

@Test func longPromptsFold() {
    func prompt(_ text: String, from: String = "") -> Transcript.Entry {
        Transcript.Entry(id: 0, kind: .prompt, text: text, truncated: false, tool: "", call: "", from: from)
    }
    let long = (1...30).map { "Step \($0)" }.joined(separator: "\n")
    let p = prompt(long)
    #expect(p.preview() == (1...10).map { "Step \($0)" }.joined(separator: "\n"))
    #expect(p.lineCount == 30)
    // A few lines over stay whole; one long line folds by characters.
    #expect(prompt((1...12).map { "Step \($0)" }.joined(separator: "\n")).preview() == nil)
    #expect(prompt(String(repeating: "a", count: 2000)).preview()?.count == 801)
    #expect(prompt("short").preview() == nil)
    // tm's prompts fold whole, to one line, and the agent's text never.
    #expect(prompt(long, from: "tm").preview() == nil)
    #expect(Transcript.Entry(id: 0, kind: .text, text: long, truncated: false, tool: "", call: "").preview() == nil)
}

@Test func markdownCached() {
    let text = "## Part\n\nSome **bold** words\n\n- one\n- two"
    #expect(SafeMarkdown.cached(text) == SafeMarkdown.blocks(text))
    #expect(SafeMarkdown.cached(text) == SafeMarkdown.blocks(text))
}
