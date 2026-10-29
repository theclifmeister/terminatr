import Foundation

/// What a prompt's composer says once session.prompt answered, the
/// Mac's and the phone's alike.
public enum PromptSent {
    /// How the prompt went in, in the window's words.
    public static func words(_ r: AppAPI.PromptResult) -> String {
        var s: String
        switch r.via {
        case "queued": s = r.running ? "Queued: it goes once the turn ends." : "Queued: it goes once the agent is idle."
        case "steered": s = "Steered into the running turn."
        case "interrupted": s = "The turn stopped for it."
        case "channel": s = "Sent."
        case "typed": s = "Typed into its pane."
        default: s = r.via.isEmpty ? "Sent." : "Sent (\(r.via))."
        }
        if !r.why.isEmpty { s += " " + r.why }
        return s
    }

    /// Whether a queued prompt went in since its note showed (T857): the
    /// session's queue shrank, or the agent took a turn. A composer
    /// clears its "Queued" note then, which otherwise stays after the
    /// prompt shows in the conversation.
    public static func wentIn(queued old: Int, _ new: Int, working wasWorking: Bool, _ working: Bool) -> Bool {
        new < old || (!wasWorking && working)
    }
}

/// The phone's keys bar (T821): the keys a terminal wants that a
/// phone's keyboard hasn't, each as the bytes session.keys writes to
/// the session's terminal, as a terminal sends them.
public enum TerminalKey: String, CaseIterable, Sendable, Identifiable {
    case esc, tab, up, down, left, right, ctrlC, enter

    public var id: String { rawValue }

    public var data: String {
        switch self {
        case .esc: "\u{1b}"
        case .tab: "\t"
        case .up: "\u{1b}[A"
        case .down: "\u{1b}[B"
        case .right: "\u{1b}[C"
        case .left: "\u{1b}[D"
        case .ctrlC: "\u{03}"
        case .enter: "\r"
        }
    }

    /// Its key cap: a word, or nil for an arrow, which shows its symbol.
    public var cap: String? {
        switch self {
        case .esc: "Esc"
        case .tab: "Tab"
        case .ctrlC: "⌃C"
        case .enter: "⏎"
        default: nil
        }
    }

    /// Its SF Symbol.
    public var symbol: String {
        switch self {
        case .esc: "escape"
        case .tab: "arrow.right.to.line"
        case .up: "arrow.up"
        case .down: "arrow.down"
        case .left: "arrow.left"
        case .right: "arrow.right"
        case .ctrlC: "xmark.octagon"
        case .enter: "return"
        }
    }

    /// What VoiceOver says of it.
    public var spoken: String {
        switch self {
        case .esc: "Escape"
        case .tab: "Tab"
        case .up: "Up arrow"
        case .down: "Down arrow"
        case .left: "Left arrow"
        case .right: "Right arrow"
        case .ctrlC: "Control C"
        case .enter: "Return"
        }
    }
}
