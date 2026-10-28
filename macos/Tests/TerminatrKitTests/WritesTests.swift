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
