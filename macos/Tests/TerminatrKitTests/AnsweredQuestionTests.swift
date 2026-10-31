import Foundation
import Testing
@testable import TerminatrKit

// An answered question shows each question and the answer picked, not
// its tool line (T903).

private func item(_ kind: String, call: String, tool: String = "", text: String = "", error: Bool = false,
                  answers: [AppAPI.TranscriptAnswer] = []) -> AppAPI.TranscriptItem {
    AppAPI.TranscriptItem(kind: kind, text: text, tool: tool, call: call, error: error, answers: answers)
}

private let parts = AppAPI.TranscriptAnswer(header: "Parts", question: "Which parts?", picked: ["Server", "Mac app"], typed: "docs too")

@Test func answeredQuestionOutcome() {
    var t = Transcript()
    t.apply("transcript.items", AppAPI.TranscriptEvent(items: [
        item("tool", call: "a", tool: "AskUserQuestion"),
        item("result", call: "a", text: "Your questions have been answered: …", answers: [parts]),
        item("tool", call: "b", tool: "AskUserQuestion"),
        item("result", call: "b", text: "The user doesn't want to proceed with this tool use.", error: true),
        item("tool", call: "c", tool: "AskUserQuestion"),
        item("result", call: "c", text: "Your questions have been answered: …"),
        item("tool", call: "d", tool: "Bash", text: "ls"),
        item("result", call: "d", text: "boom", error: true),
        item("tool", call: "e", tool: "AskUserQuestion"),
        // A result whose call came before the items the server sent.
        item("result", call: "z", answers: [parts]),
    ]))
    let o = t.entries.map(\.questionOutcome)
    #expect(o[0] == .answered([parts]))
    #expect(o[1] == .declined("The user doesn't want to proceed with this tool use."))
    // A tm before app API 40 sends no answers: the tool line stays.
    #expect(o[2] == nil)
    #expect(o[3] == nil)
    // Still open: the question card answers it.
    #expect(o[4] == nil)
    #expect(t.entries[5].kind == .result && o[5] == .answered([parts]))
}

@Test func answerWords() {
    #expect(parts.words == "Server, Mac app, \u{201C}docs too\u{201D}")
    #expect(parts.spoken == "Parts: Which parts? Answered Server, Mac app, \u{201C}docs too\u{201D}.")
    let bare = AppAPI.TranscriptAnswer(question: "Name?", picked: ["tm"], notes: "short")
    #expect(bare.spoken == "Name? Answered tm. Notes: short.")
}

@Test func answersDecode() throws {
    let json = #"{"kind":"result","call":"a","answers":[{"header":"H","question":"Q","picked":["x","y"]},{"question":"R","typed":"mine"}]}"#
    let it = try JSONDecoder().decode(AppAPI.TranscriptItem.self, from: Data(json.utf8))
    #expect(it.answers.map(\.picked) == [["x", "y"], []])
    #expect(it.answers[1].typed == "mine" && it.answers[1].header == "")
}
