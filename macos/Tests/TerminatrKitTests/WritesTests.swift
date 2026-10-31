import Foundation
import Testing
@testable import TerminatrKit

// The phone's write level (T821): its keys bar's bytes, and what a
// composer says once a prompt went in.

@Test func terminalKeysAreWhatATerminalSends() {
    let bytes = Dictionary(uniqueKeysWithValues: TerminalKey.allCases.map { ($0, Array($0.data.utf8)) })
    #expect(bytes[.esc] == [0x1b])
    #expect(bytes[.tab] == [0x09])
    #expect(bytes[.ctrlC] == [0x03])
    #expect(bytes[.enter] == [0x0d])
    #expect(bytes[.up] == [0x1b, 0x5b, 0x41] && bytes[.down] == [0x1b, 0x5b, 0x42])
    #expect(bytes[.right] == [0x1b, 0x5b, 0x43] && bytes[.left] == [0x1b, 0x5b, 0x44])
    // Every key has a cap or a symbol, and words for VoiceOver.
    for k in TerminalKey.allCases {
        #expect(!k.symbol.isEmpty && !k.spoken.isEmpty)
    }
}

@Test func promptSentWords() {
    #expect(PromptSent.words(.init(via: "queued", running: true)) == "Queued: it goes once the turn ends.")
    #expect(PromptSent.words(.init(via: "queued")) == "Queued: it goes once the agent is idle.")
    #expect(PromptSent.words(.init(via: "steered")) == "Steered into the running turn.")
    #expect(PromptSent.words(.init(via: "typed", why: "Its pane is on billing.")) == "Typed into its pane. Its pane is on billing.")
    #expect(PromptSent.words(.init(via: "")) == "Sent.")
}

@Test func queuedPromptWentIn() {
    // The queue shrank: it went in, whatever the agent does now.
    #expect(PromptSent.wentIn(queued: 1, 0, working: true, true))
    #expect(PromptSent.wentIn(queued: 2, 1, working: false, false))
    // The agent took a turn.
    #expect(PromptSent.wentIn(queued: 1, 1, working: false, true))
    // Still waiting: the queue held or grew, the turn goes on or ended.
    #expect(!PromptSent.wentIn(queued: 0, 1, working: true, true))
    #expect(!PromptSent.wentIn(queued: 1, 1, working: true, false))
    #expect(!PromptSent.wentIn(queued: 1, 1, working: false, false))
}

/// A queued prompt shows until the conversation has it or the queue no
/// longer holds it (T893).
@Test func queuedPromptsWaitTillTheyGoIn() {
    let t0 = Date(timeIntervalSince1970: 1_000_000)
    let a = QueuedPrompt(text: "first", at: t0), b = QueuedPrompt(text: "second ", files: 1, at: t0.addingTimeInterval(5))
    #expect(QueuedPrompt.waiting([a, b], prompts: [], queued: 2) == [a, b])
    // Nothing queued: none, whatever the conversation says.
    #expect(QueuedPrompt.waiting([a, b], prompts: [], queued: 0).isEmpty)
    // In the conversation, with its file's path after the text.
    #expect(QueuedPrompt.waiting([a, b], prompts: [(text: "second\n\nuploads/x.png", at: t0.addingTimeInterval(9))], queued: 2) == [a])
    // An older prompt of the same words doesn't count; one with no time does.
    #expect(QueuedPrompt.waiting([a], prompts: [(text: "first", at: t0.addingTimeInterval(-3600))], queued: 1) == [a])
    #expect(QueuedPrompt.waiting([a], prompts: [(text: "first", at: nil)], queued: 1).isEmpty)
    // The queue shrank: the oldest went.
    #expect(QueuedPrompt.waiting([a, b], prompts: [], queued: 1) == [b])
}
