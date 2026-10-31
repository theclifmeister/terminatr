import Foundation
import Testing
@testable import TerminatrKit

private let colour = AppAPI.QuestionItem(question: "Which colour?", header: "Colour",
                                         options: [.init(label: "Red", description: "Loud"), .init(label: "Blue")])
private let fruits = AppAPI.QuestionItem(question: "Which fruits?", header: "Fruits", multiSelect: true,
                                         options: [.init(label: "Apple"), .init(label: "Pear"), .init(label: "Plum")])

private func screen(_ items: [AppAPI.QuestionItem]) -> AppAPI.Question {
    AppAPI.Question(tool: "AskUserQuestion", questions: items, since: t0, screen: true)
}

@Test func questionPlanShown() {
    var answered = colour
    answered.answered = true
    // tm's own question: every open one; the agent's menu: its first.
    #expect(QuestionPlan.shown(menu([colour, fruits])) == [0, 1])
    #expect(QuestionPlan.shown(screen([colour, fruits])) == [0])
    #expect(QuestionPlan.shown(screen([answered, fruits])) == [1])
    #expect(QuestionPlan.shown(screen([answered])) == [])
}

@Test func questionPlanTag() {
    #expect(QuestionPlan.tag(screen([colour]), index: 0) == "Colour")
    #expect(QuestionPlan.tag(screen([colour, fruits]), index: 1) == "Fruits · 2 of 2")
    #expect(QuestionPlan.tag(menu([backend]), index: 0) == "Question")
    #expect(QuestionPlan.tag(menu([backend], escape: true), index: 0) == "Question · outside the sandbox")
}

@Test func questionPlanShapes() {
    let single = QuestionPlan(screen([colour]), index: 0)
    #expect(single.buttons && !single.checkboxes && single.words && !single.paneOnly && single.note == nil)
    // A multi-select: checkboxes with Submit for tm's own; the agent's
    // menu takes it in the Terminal tab, with no words either.
    let own = QuestionPlan(menu([fruits]), index: 0)
    #expect(own.checkboxes && !own.buttons && !own.paneOnly && own.words)
    let pane = QuestionPlan(screen([colour, fruits]), index: 1)
    #expect(pane.checkboxes && pane.paneOnly && !pane.words)
    #expect(pane.note?.contains("Terminal tab") == true)
    // A tm that types picks (app API 39, T900): checkboxes with Submit,
    // sent as picks; the user's words still the pane's.
    let picks = QuestionPlan(screen([colour, fruits]), index: 1, picks: true)
    #expect(picks.checkboxes && !picks.paneOnly && picks.sendsPicks && !picks.words && picks.note == nil)
    #expect(!QuestionPlan(menu([fruits]), index: 0, picks: true).sendsPicks)
    #expect(!QuestionPlan(screen([colour]), index: 0, picks: true).sendsPicks)
}

@Test func questionPlanPicks() throws {
    #expect(QuestionPlan.picks(fruits, picked: ["Plum", "Apple"]) == [0, 2])
    #expect(QuestionPlan.picks(fruits, picked: []) == [])
    // Empty picks stay out of the params: a bridge before 39 refuses a
    // field it doesn't know.
    let enc = AppAPI.encoder()
    let plain = String(decoding: try enc.encode(AppAPI.AnswerParams(id: "s", index: 1, answer: "Blue")), as: UTF8.self)
    #expect(plain == #"{"answer":"Blue","id":"s","index":1,"question":""}"#)
    let picked = String(decoding: try enc.encode(AppAPI.AnswerParams(id: "s", index: 1, answer: "", question: "Which fruits?", picks: [0, 2])), as: UTF8.self)
    #expect(picked == #"{"answer":"","id":"s","index":1,"picks":[0,2],"question":"Which fruits?"}"#)
    let act = String(decoding: try enc.encode(AppAPI.PeerNeedActParams(peer: "mini", project: "p", need: "n", act: "answer")), as: UTF8.self)
    #expect(!act.contains("picks"))
}

@Test func hasAnswerPicks() {
    func hello(_ api: Int, _ caps: [String]) -> AppAPI.Hello {
        AppAPI.Hello(appApi: api, tm: "0.70.0", build: "", server: AppAPI.Server(version: "0.70.0", protocol: 30, caps: caps))
    }
    #expect(!Compatibility.answersPicks(nil))
    #expect(Compatibility.answersPicks(hello(39, ["answer-picks"])))
    #expect(!Compatibility.answersPicks(hello(38, ["answer-picks"])))
    #expect(!Compatibility.answersPicks(hello(39, [])))
}

@Test func questionPlanAnswers() {
    #expect(QuestionPlan.joined(fruits, picked: ["Plum", "Apple"]) == "Apple, Plum")
    #expect(QuestionPlan.joined(fruits, picked: []) == "")
    #expect(QuestionPlan.spoken(colour, option: 0) == "Option 1 of 2: Red. Loud")
    #expect(QuestionPlan.spoken(colour, option: 1) == "Option 2 of 2: Blue")
    #expect(QuestionPlan.sending("Blue", screen: true) == "Typing “Blue” into the agent's menu…")
    #expect(QuestionPlan.useTerminal("screen-only") && QuestionPlan.useTerminal("no-question"))
    #expect(!QuestionPlan.useTerminal("bad-params"))
}

@Test func questionPlanGone() {
    var a = colour
    a.answered = true
    // Answered in the pane: the session has no question, another one,
    // or this one answered; the card goes quietly.
    #expect(QuestionPlan.gone("no-question", asked: "Which colour?", now: nil))
    #expect(QuestionPlan.gone("no-question", asked: "Which colour?", now: screen([fruits])))
    #expect(QuestionPlan.gone("no-question", asked: "Which colour?", now: screen([a, fruits])))
    // Still open (the screen didn't match): the error stays; so does
    // any other refusal, and an unguarded answer while a menu is open.
    #expect(!QuestionPlan.gone("no-question", asked: "Which colour?", now: screen([colour])))
    #expect(!QuestionPlan.gone("bad-params", asked: "Which colour?", now: nil))
    #expect(!QuestionPlan.gone("no-question", asked: "", now: screen([fruits])))
    #expect(QuestionPlan.gone("no-question", asked: "", now: nil))
}

@Test func questionPlanAllAnswered() {
    var a = colour
    a.answered = true
    a.answer = "Blue"
    #expect(QuestionPlan.allAnswered(screen([colour])) == nil)
    #expect(QuestionPlan.allAnswered(screen([a]))?.contains("Terminal tab") == true)
    #expect(QuestionPlan.allAnswered(menu([a])) == "Answers sent: waiting for the agent to take them.")
}

@Test func asksUser() {
    let call = Transcript.Entry(id: 1, kind: .tool, text: "Colour", truncated: false, tool: "AskUserQuestion", call: "toolu_1")
    let bash = Transcript.Entry(id: 2, kind: .tool, text: "ls", truncated: false, tool: "Bash", call: "toolu_2")
    #expect(call.asksUser && !bash.asksUser)
}

/// The session list is pushed by a tm and server with sessions.watch;
/// an older one is read on a timer.
@Test func sessionsWatchCompatibility() {
    var h = AppAPI.Hello(appApi: 32, tm: "1", build: "", capabilities: [], methods: ["sessions.watch"])
    h.server = AppAPI.Server(version: "1", protocol: 29, caps: ["sessions-watch"])
    #expect(Compatibility.watchesSessions(h))
    h.server?.caps = []
    #expect(!Compatibility.watchesSessions(h))
    h.server?.caps = ["sessions-watch"]
    h.methods = []
    #expect(!Compatibility.watchesSessions(h))
}

/// The line under a phone card's options after a tap (T887): the
/// refusal first, the answer on its way while it goes, else none.
@Test func tappedLine() {
    #expect(QuestionPlan.tapped(sending: nil, busy: false, screen: true, said: nil) == nil)
    #expect(QuestionPlan.tapped(sending: "Yes", busy: true, screen: true, said: nil)?.text == "Typing “Yes” into the agent's menu…")
    #expect(QuestionPlan.tapped(sending: "Yes", busy: true, screen: false, said: nil)?.failed == false)
    #expect(QuestionPlan.tapped(sending: "Yes", busy: false, screen: true, said: nil) == nil) // taken: the card goes
    let refused = QuestionPlan.tapped(sending: nil, busy: false, screen: true, said: ("screen doesn't show it", true))
    #expect(refused?.text == "screen doesn't show it" && refused?.failed == true)
}
