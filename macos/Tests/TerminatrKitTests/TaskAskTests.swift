import Testing
@testable import TerminatrKit

// A task ask's outcome row (T769): its words, symbol and tone, when it
// keeps the buttons away, and when the board has done with it.

@Test func taskAskAskedWords() {
    var a = TaskAsk(task: "T748", kind: .accept, status: "review")
    #expect(a.text == "Asking the coordinator to accept T748…")
    #expect(a.tone == .neutral)
    a.phase = .asked
    #expect(a.text == "Accept requested: the coordinator will merge and close T748.")
    #expect(a.symbol == "checkmark.circle.fill")
    #expect(a.tone == .good)

    let back = TaskAsk(task: "T2", kind: .sendBack(note: "fix it"), status: "review", phase: .asked)
    #expect(back.text == "Send back requested: the coordinator will pass your note to T2's thread.")
    #expect(back.symbol == "arrow.uturn.backward.circle")
    #expect(back.tone == .action)

    let d = TaskAsk(task: "T3", kind: .delegate, status: "blocked", phase: .asked)
    #expect(d.text == "Delegate requested: the coordinator will start a thread for T3.")
    #expect(d.symbol == "person.badge.plus")
}

@Test func taskAskWaitingAndFailed() {
    let w = TaskAsk(task: "T1", kind: .accept, status: "review", phase: .waiting)
    #expect(w.text == "T1 waits on the coordinator already.")
    #expect(w.tone == .neutral)
    #expect(w.holdsButtons)

    let f = TaskAsk(task: "T1", kind: .sendBack(note: "n"), status: "review", phase: .failed("T1 isn't in review any more."))
    #expect(f.text == "T1 isn't in review any more.")
    #expect(f.tone == .attention)
    #expect(f.symbol == "exclamationmark.triangle.fill")
    #expect(!f.holdsButtons)
    #expect(f.retryLabel == "Retry sending T1 back")
}

@Test func taskAskStandsWhileTheStatusHolds() {
    let a = TaskAsk(task: "T1", kind: .accept, status: "review", phase: .asked)
    #expect(a.holdsButtons)
    #expect(a.stands("review"))
    #expect(!a.stands("done"))
    #expect(TaskAsk.key("demo", "T1") == "demo/T1")
}
